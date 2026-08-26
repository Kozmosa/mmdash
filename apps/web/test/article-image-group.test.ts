// @vitest-environment jsdom

import { Editor } from "@tiptap/core";
import { TableKit } from "@tiptap/extension-table";
import StarterKit from "@tiptap/starter-kit";
import { describe, expect, it } from "vitest";

import {
  articleImageGroupContext,
  deleteArticleImageNode,
  mergeArticleImageWithNeighbor,
  removeArticleImageFromGroup,
  ungroupArticleImages,
} from "@/features/article/article-image-group";
import { createArticleNodes } from "@/features/article/article-nodes";

function image(index: number) {
  return {
    attrs: {
      alt: `图片 ${index}`,
      caption: `子题注 ${index}`,
      src: `https://example.test/${index}.png`,
    },
    type: "articleImage",
  };
}

function makeEditor(count: number) {
  return new Editor({
    content: {
      content: Array.from({ length: count }, (_, index) => image(index + 1)),
      type: "doc",
    },
    extensions: [
      StarterKit,
      ...createArticleNodes("project-1"),
      TableKit.configure({ table: false }),
    ],
  });
}

describe("article image groups", () => {
  it("merges adjacent images into a multi-row group without losing captions", () => {
    const editor = makeEditor(5);

    expect(mergeArticleImageWithNeighbor(editor, 0, "after")).toBe(true);
    for (let count = 2; count < 5; count += 1) {
      const nextImagePosition = editor.state.doc.child(0).nodeSize;
      expect(
        mergeArticleImageWithNeighbor(editor, nextImagePosition, "before"),
      ).toBe(true);
    }

    const group = editor.state.doc.child(0);
    expect(editor.state.doc.childCount).toBe(2);
    expect(group.type.name).toBe("articleImageGroup");
    expect(group.childCount).toBe(5);
    expect(group.attrs.columns).toBe(2);
    expect(
      Array.from(
        { length: group.childCount },
        (_, index) => group.child(index).attrs.caption,
      ),
    ).toEqual(["子题注 1", "子题注 2", "子题注 3", "子题注 4", "子题注 5"]);
    expect(articleImageGroupContext(editor, 1).inGroup).toBe(true);
    expect(editor.state.doc.child(1).type.name).toBe("paragraph");
    editor.destroy();
  });

  it("removes, deletes, and ungroups child images without corrupting the document", () => {
    const editor = makeEditor(3);
    expect(mergeArticleImageWithNeighbor(editor, 0, "after")).toBe(true);
    const thirdPosition = editor.state.doc.child(0).nodeSize;
    expect(mergeArticleImageWithNeighbor(editor, thirdPosition, "before")).toBe(
      true,
    );

    expect(removeArticleImageFromGroup(editor, 2)).toBe(true);
    expect(editor.state.doc.child(0).type.name).toBe("articleImageGroup");
    expect(editor.state.doc.child(0).childCount).toBe(2);
    expect(editor.state.doc.child(1).attrs.caption).toBe("子题注 2");

    expect(deleteArticleImageNode(editor, 1)).toBe(true);
    expect(editor.state.doc.child(0).type.name).toBe("articleImage");
    expect(editor.state.doc.child(0).attrs.caption).toBe("子题注 3");

    editor.commands.setContent({
      content: [
        {
          attrs: { caption: "大题注", columns: 1 },
          content: [image(1), image(2)],
          type: "articleImageGroup",
        },
      ],
      type: "doc",
    });
    expect(ungroupArticleImages(editor, 0)).toBe(true);
    expect(editor.state.doc.childCount).toBe(3);
    expect(editor.state.doc.child(0).attrs.caption).toBe("子题注 1");
    expect(editor.state.doc.child(1).attrs.caption).toBe("子题注 2");
    expect(editor.state.doc.child(2).type.name).toBe("paragraph");
    editor.destroy();
  });
});
