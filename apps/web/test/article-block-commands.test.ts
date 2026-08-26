// @vitest-environment jsdom

import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { TableKit } from "@tiptap/extension-table";
import UniqueID from "@tiptap/extension-unique-id";
import { NodeSelection } from "@tiptap/pm/state";
import { describe, expect, it } from "vitest";

import {
  convertArticleBlock,
  deleteSelectedArticleBlock,
  duplicateArticleBlock,
  insertArticleBlock,
  moveArticleBlock,
  replaceArticleImageWithArtifact,
  selectArticleBlock,
} from "@/features/article/article-block-commands";
import { createArticleNodes } from "@/features/article/article-nodes";

function makeEditor() {
  return new Editor({
    content: {
      content: [
        {
          attrs: { id: "block-a" },
          content: [{ text: "第一块", type: "text" }],
          type: "paragraph",
        },
        {
          attrs: { id: "block-b" },
          content: [{ text: "第二块", type: "text" }],
          type: "paragraph",
        },
      ],
      type: "doc",
    },
    extensions: [
      StarterKit,
      UniqueID.configure({
        types: ["paragraph", "heading", "blockquote", "codeBlock"],
      }),
    ],
  });
}

describe("Article block commands", () => {
  it("selects and deletes one block while preserving an empty paragraph", () => {
    const editor = makeEditor();
    expect(selectArticleBlock(editor, 0)).toBe(true);
    expect(deleteSelectedArticleBlock(editor.view)).toBe(true);
    expect(editor.getJSON().content).toHaveLength(1);
    expect(editor.getJSON().content?.[0]).toMatchObject({
      type: "paragraph",
    });
    expect(editor.getText()).toBe("第二块");

    editor.commands.setContent({
      content: [{ attrs: { id: "only" }, type: "paragraph" }],
      type: "doc",
    });
    expect(selectArticleBlock(editor, 0)).toBe(true);
    expect(deleteSelectedArticleBlock(editor.view)).toBe(true);
    expect(editor.getJSON().content?.[0]).toMatchObject({ type: "paragraph" });
    expect(editor.getJSON().content?.[0]?.content).toBeUndefined();
    editor.destroy();
  });

  it("can select a visible block without changing the editor scroll", () => {
    const editor = makeEditor();
    expect(selectArticleBlock(editor, 0, { scrollIntoView: false })).toBe(true);
    expect(editor.state.selection).toBeInstanceOf(NodeSelection);
    expect(editor.state.selection.from).toBe(0);
    editor.destroy();
  });

  it("duplicates and reorders blocks with a single selected block", () => {
    const editor = makeEditor();
    expect(duplicateArticleBlock(editor, 0)).toBe(true);
    expect(editor.getText()).toBe("第一块\n\n第一块\n\n第二块");
    const firstSize = editor.state.doc.child(0).nodeSize;
    expect(moveArticleBlock(editor, firstSize, "down")).toBe(true);
    expect(editor.getText()).toBe("第一块\n\n第二块\n\n第一块");
    editor.destroy();
  });

  it("inserts an empty paragraph above or below the selected block", () => {
    const editor = makeEditor();
    expect(insertArticleBlock(editor, 0, "before")).toBe(true);
    expect(editor.getJSON().content).toHaveLength(3);
    expect(editor.getJSON().content?.[0]?.type).toBe("paragraph");
    expect(
      insertArticleBlock(editor, editor.state.doc.child(0).nodeSize, "after"),
    ).toBe(true);
    expect(editor.getJSON().content).toHaveLength(4);
    editor.destroy();
  });

  it("converts a block to a heading and then to a quote", () => {
    const editor = makeEditor();
    expect(convertArticleBlock(editor, 0, "heading2")).toBe(true);
    expect(editor.getJSON().content?.[0]).toMatchObject({
      attrs: { level: 2 },
      type: "heading",
    });
    expect(convertArticleBlock(editor, 0, "blockquote")).toBe(true);
    expect(editor.getJSON().content?.[0]).toMatchObject({
      content: [{ type: "heading" }],
      type: "blockquote",
    });
    expect(convertArticleBlock(editor, 0, "heading3")).toBe(true);
    expect(editor.getJSON().content?.[0]).toMatchObject({
      attrs: { level: 3 },
      content: [{ text: "第一块", type: "text" }],
      type: "heading",
    });
    editor.destroy();
  });

  it("replaces a local image with an immutable Artifact at the same block", () => {
    const editor = new Editor({
      content: {
        content: [
          {
            attrs: {
              align: "right",
              alt: "old alt",
              caption: "图 1",
              id: "image-block",
              src: "https://example.com/old.png",
              width: 65,
            },
            type: "articleImage",
          },
        ],
        type: "doc",
      },
      extensions: [
        StarterKit,
        ...createArticleNodes("project-1"),
        TableKit.configure({ table: false }),
        UniqueID.configure({
          types: ["articleImage", "artifactReference"],
        }),
      ],
    });

    expect(
      replaceArticleImageWithArtifact(editor, 0, {
        artifactId: "artifact-1",
        mimeType: "image/png",
        referenceId: "reference-1",
        title: "new.png",
        versionId: "version-1",
      }),
    ).toBe(true);
    expect(editor.getJSON().content?.[0]).toEqual(
      expect.objectContaining({
        attrs: expect.objectContaining({
          align: "right",
          alt: "old alt",
          artifactId: "artifact-1",
          caption: "图 1",
          id: "image-block",
          referenceId: "reference-1",
          versionId: "version-1",
          width: 65,
        }),
        type: "artifactReference",
      }),
    );
    editor.destroy();
  });
});
