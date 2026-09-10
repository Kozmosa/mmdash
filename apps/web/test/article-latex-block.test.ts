// @vitest-environment jsdom

import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import UniqueID from "@tiptap/extension-unique-id";
import { describe, expect, it } from "vitest";

import {
  LatexBlock,
  MathInline,
  MathBlock,
  paragraphAsRawTexEnvironment,
} from "@/features/article/article-nodes";
import { cumcmSkeletonNodes } from "@/features/article/article-cumcm";
import { migrateLegacyArticleBlocks } from "@/features/article/article-editor";
import { runSlashItemForTest } from "@/features/article/slash-command";

function buildEditor() {
  return new Editor({
    extensions: [
      StarterKit.configure({ undoRedo: false }),
      LatexBlock,
      MathBlock,
      MathInline,
      // Production registers the block id attribute through UniqueID; the
      // migration preserves the downgraded paragraph's id on the new node.
      UniqueID.configure({
        types: ["paragraph", "heading", "latexBlock"],
      }),
    ],
  });
}

describe("latexBlock schema and insertion", () => {
  it("registers latexBlock in the block group", () => {
    const editor = buildEditor();
    expect(editor.schema.nodes.latexBlock?.spec.group).toContain("block");
    editor.destroy();
  });

  it("keeps CUMCM skeleton latexBlock nodes as latexBlock", () => {
    const editor = buildEditor();
    editor.commands.insertContent(cumcmSkeletonNodes());
    const types = (editor.getJSON().content ?? []).map((node) => node.type);
    expect(types).toContain("latexBlock");
    const latexTexts = (editor.state.doc.content.toJSON() as never[]).length;
    expect(latexTexts).toBeGreaterThan(0);
    editor.destroy();
  });

  it('inserts a raw LaTeX block from the "/" menu', () => {
    const editor = buildEditor();
    editor.commands.setContent("<p>/</p>");
    runSlashItemForTest("LaTeX 块", editor, { from: 1, to: 2 });
    const first = editor.getJSON().content?.[0] as
      | { content?: Array<{ text?: string; type?: string }>; type?: string }
      | undefined;
    expect(first?.type).toBe("latexBlock");
    expect(first?.content?.[0]?.text).toContain("\\begin{center}");
    expect(first?.content?.[0]?.text).toContain("\\end{center}");
    editor.destroy();
  });
});

describe("paragraphAsRawTexEnvironment", () => {
  it("accepts a paragraph whose whole text is one raw environment", () => {
    const editor = buildEditor();
    editor.commands.setContent({
      type: "doc",
      content: [
        {
          type: "paragraph",
          content: [
            {
              type: "text",
              text: "\\begin{problem}\n问题1的数学表达……\n\\label{pro:1}\n\\end{problem}",
            },
          ],
        },
      ],
    } as never);
    const paragraph = editor.state.doc.firstChild!;
    expect(paragraphAsRawTexEnvironment(paragraph)).toBe(
      "\\begin{problem}\n问题1的数学表达……\n\\label{pro:1}\n\\end{problem}",
    );
    editor.destroy();
  });

  it("accepts the hard-break variant produced by pasted environments", () => {
    const editor = buildEditor();
    editor.commands.setContent({
      type: "doc",
      content: [
        {
          type: "paragraph",
          content: [
            { type: "text", text: "\\begin{table}[H]" },
            { type: "hardBreak" },
            { type: "text", text: "\\centering" },
            { type: "hardBreak" },
            { type: "text", text: "\\end{table}" },
            { type: "hardBreak" },
          ],
        },
      ],
    } as never);
    const paragraph = editor.state.doc.firstChild!;
    expect(paragraphAsRawTexEnvironment(paragraph)).toBe(
      "\\begin{table}[H]\n\\centering\n\\end{table}",
    );
    editor.destroy();
  });

  it("rejects ordinary prose and partial environments", () => {
    const editor = buildEditor();
    editor.commands.setContent(
      "<p>普通正文段落。</p><p>只有 \\begin{problem} 的开头</p>",
    );
    editor.state.doc.forEach((node) => {
      expect(paragraphAsRawTexEnvironment(node)).toBeUndefined();
    });
    editor.destroy();
  });
});

describe("migrateLegacyArticleBlocks", () => {
  it("converts downgraded raw-TeX paragraphs back into latexBlock", () => {
    const editor = buildEditor();
    editor.commands.setContent({
      type: "doc",
      content: [
        {
          type: "paragraph",
          attrs: { id: "block-problem-1" },
          content: [
            {
              type: "text",
              text: "\\begin{problem}\n问题1的数学表达……\n\\label{pro:1}\n\\end{problem}",
            },
          ],
        },
        {
          type: "paragraph",
          content: [
            { type: "text", text: "\\begin{assumption}" },
            { type: "hardBreak" },
            { type: "text", text: "本文假设……" },
            { type: "hardBreak" },
            { type: "text", text: "\\end{assumption}" },
          ],
        },
        { type: "paragraph", content: [{ type: "text", text: "普通段落" }] },
      ],
    } as never);

    expect(migrateLegacyArticleBlocks(editor)).toBe(true);
    const json = editor.getJSON();
    const types = (json.content ?? []).map((node) => node.type);
    expect(types).toEqual(["latexBlock", "latexBlock", "paragraph"]);
    expect(json.content?.[0]?.attrs?.id).toBe("block-problem-1");
    const text = editor.state.doc.firstChild?.textContent ?? "";
    expect(text).toContain("\\begin{problem}");
    expect(text).toContain("\\end{problem}");
    editor.destroy();
  });

  it("leaves clean documents untouched", () => {
    const editor = buildEditor();
    editor.commands.setContent({
      type: "doc",
      content: [{ type: "paragraph", content: [{ type: "text", text: "hi" }] }],
    } as never);
    expect(migrateLegacyArticleBlocks(editor)).toBe(false);
    editor.destroy();
  });
});
