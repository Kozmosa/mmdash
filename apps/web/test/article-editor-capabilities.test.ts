import { Editor } from "@tiptap/core";
import { Mathematics } from "@tiptap/extension-mathematics";
import { TableKit } from "@tiptap/extension-table";
import StarterKit from "@tiptap/starter-kit";
import { describe, expect, it, vi } from "vitest";

import { createArticleNodes } from "@/features/article/article-nodes";
import {
  openArticleImageUploadEvent,
  openArticleSidebarEvent,
  runSlashItemForTest,
} from "@/features/article/slash-command";
import {
  droppedLocalImage,
  migrateLegacyTableCaptions,
  parseArticleArtifactDrop,
  parseArticleZoteroDrop,
  sameArticleOutline,
  sameTagPositions,
} from "@/features/article/article-editor";

describe("Article editor block capabilities", () => {
  it("deduplicates unchanged outline and tag-rail measurements", () => {
    const outline = [{ id: "chapter-1", level: 1, text: "方法" }];
    expect(sameArticleOutline(outline, [...outline])).toBe(true);
    expect(
      sameArticleOutline(outline, [{ ...outline[0]!, text: "结果" }]),
    ).toBe(false);
    expect(sameTagPositions({ block: 120 }, { block: 120.25 })).toBe(true);
    expect(sameTagPositions({ block: 120 }, { block: 121 })).toBe(false);
  });

  it("renders KaTeX and inserts a GFM-compatible table from slash commands", () => {
    const editor = new Editor({
      content: "<p>/</p>",
      extensions: [
        StarterKit,
        Mathematics.configure({ katexOptions: { throwOnError: false } }),
        TableKit,
      ],
    });
    runSlashItemForTest("公式块", editor, { from: 1, to: 2 });
    expect(editor.getJSON().content?.[0]?.type).toBe("blockMath");
    expect(editor.view.dom.innerHTML).toContain("katex");

    editor.commands.setContent("<p>/</p>");
    runSlashItemForTest("表格", editor, { from: 1, to: 2 });
    const table = editor.getJSON().content?.[0];
    expect(table?.type).toBe("table");
    const firstRow = (
      table as
        | {
            content?: Array<{
              type?: string;
              content?: Array<{ type?: string }>;
            }>;
          }
        | undefined
    )?.content?.[0];
    expect(
      firstRow?.type === "tableRow" &&
        firstRow.content?.every(
          (cell: { type?: string }) => cell.type === "tableHeader",
        ),
    ).toBe(true);

    editor.commands.setContent("<p>/</p>");
    runSlashItemForTest("代码块", editor, { from: 1, to: 2 });
    expect(editor.getJSON().content?.[0]?.type).toBe("codeBlock");
    editor.destroy();
  });

  it("accepts only complete immutable Artifact Version drag payloads", () => {
    expect(
      parseArticleArtifactDrop(
        JSON.stringify({
          artifactId: "artifact-1",
          filename: "figure.png",
          mimeType: "image/png",
          title: "Figure",
          versionId: "version-1",
        }),
      ),
    ).toMatchObject({ artifactId: "artifact-1", versionId: "version-1" });
    expect(() =>
      parseArticleArtifactDrop(JSON.stringify({ artifactId: "artifact-1" })),
    ).toThrow("数据不完整");
  });

  it("prioritizes an existing Artifact drag over browser-provided image files", () => {
    const file = new File(["image"], "figure.png", { type: "image/png" });
    expect(
      droppedLocalImage({
        files: [file] as unknown as FileList,
        types: ["Files", "application/vnd.mmdash.article-artifact+json"],
      }),
    ).toBeUndefined();
    expect(
      droppedLocalImage({
        files: [file] as unknown as FileList,
        types: ["Files"],
      }),
    ).toBe(file);
  });

  it("routes image, Model, Experiment, and Zotero slash commands", () => {
    const editor = new Editor({
      content: "<p>/</p>",
      extensions: [StarterKit],
    });
    const upload = vi.fn();
    const sidebar = vi.fn();
    window.addEventListener(openArticleImageUploadEvent, upload);
    window.addEventListener(openArticleSidebarEvent, sidebar);

    runSlashItemForTest("图片", editor, { from: 1, to: 2 });
    editor.commands.setContent("<p>/</p>");
    runSlashItemForTest("Model 引用", editor, { from: 1, to: 2 });
    editor.commands.setContent("<p>/</p>");
    runSlashItemForTest("Experiment 引用", editor, { from: 1, to: 2 });
    editor.commands.setContent("<p>/</p>");
    runSlashItemForTest("Zotero 引用", editor, { from: 1, to: 2 });

    expect(upload).toHaveBeenCalledOnce();
    expect(sidebar.mock.calls.map(([event]) => event.detail)).toEqual([
      { panel: "reference", referenceKind: "model_snapshot" },
      { panel: "reference", referenceKind: "experiment_result" },
      { panel: "zotero" },
    ]);
    window.removeEventListener(openArticleImageUploadEvent, upload);
    window.removeEventListener(openArticleSidebarEvent, sidebar);
    editor.destroy();
  });

  it("parses the Zotero drag payload used by the article editor", () => {
    expect(
      parseArticleZoteroDrop(
        JSON.stringify({
          citationKey: "smith2026",
          itemKey: "ABCD1234",
          title: "A paper",
          version: 7,
        }),
      ),
    ).toEqual({
      citationKey: "smith2026",
      itemKey: "ABCD1234",
      raw: {},
      title: "A paper",
      version: 7,
    });
    expect(() =>
      parseArticleZoteroDrop(
        JSON.stringify({ itemKey: "ABCD1234", title: "A paper" }),
      ),
    ).toThrow("Zotero 条目拖拽数据不完整");
  });

  it("keeps a table caption on the table node and removes it with the table", () => {
    const editor = new Editor({
      content: {
        content: [
          {
            attrs: { caption: "表 1：结果" },
            content: [
              {
                content: [
                  {
                    content: [{ text: "A", type: "text" }],
                    type: "tableHeader",
                  },
                  {
                    content: [{ text: "B", type: "text" }],
                    type: "tableHeader",
                  },
                ],
                type: "tableRow",
              },
              {
                content: [
                  { content: [{ text: "1", type: "text" }], type: "tableCell" },
                  { content: [{ text: "2", type: "text" }], type: "tableCell" },
                ],
                type: "tableRow",
              },
            ],
            type: "table",
          },
        ],
        type: "doc",
      },
      extensions: [
        StarterKit,
        ...createArticleNodes("project-1"),
        TableKit.configure({ table: false }),
      ],
    });

    expect(editor.getJSON().content?.[0]?.attrs?.caption).toBe("表 1：结果");
    expect(editor.getHTML()).toContain("表 1：结果");
    expect(editor.getHTML().indexOf("表 1：结果")).toBeLessThan(
      editor.getHTML().indexOf("<tbody"),
    );

    editor.view.dispatch(
      editor.state.tr.delete(0, editor.state.doc.child(0).nodeSize),
    );
    expect(
      editor.getJSON().content?.some((node) => node.type === "table"),
    ).toBe(false);
    expect(
      editor.getJSON().content?.some((node) => node.type === "tableCaption"),
    ).toBe(false);
    editor.destroy();
  });

  it("migrates a legacy table caption without reading past the final block", () => {
    const editor = new Editor({
      content: {
        content: [
          { attrs: { caption: "表 1：旧草稿结果" }, type: "tableCaption" },
          {
            content: [
              {
                content: [
                  {
                    content: [{ type: "paragraph" }],
                    type: "tableCell",
                  },
                ],
                type: "tableRow",
              },
            ],
            type: "table",
          },
          ...Array.from({ length: 40 }, () => ({ type: "paragraph" })),
        ],
        type: "doc",
      },
      extensions: [
        StarterKit,
        ...createArticleNodes("project-1"),
        TableKit.configure({ table: false }),
      ],
    });

    expect(() => migrateLegacyTableCaptions(editor)).not.toThrow();
    expect(editor.getJSON().content?.[0]).toMatchObject({
      attrs: { caption: "表 1：旧草稿结果" },
      type: "table",
    });
    expect(editor.getJSON().content).toHaveLength(41);
    editor.destroy();
  });

  it("uses native table commands for row, column, and header flows", () => {
    const editor = new Editor({
      extensions: [
        StarterKit,
        ...createArticleNodes("project-1"),
        TableKit.configure({ table: false }),
      ],
    });
    editor.commands.insertTable({ cols: 2, rows: 2, withHeaderRow: true });
    expect(editor.commands.addRowAfter()).toBe(true);
    expect(editor.commands.addColumnAfter()).toBe(true);
    expect(editor.commands.toggleHeaderRow()).toBe(true);
    const table = editor.getJSON().content?.[0] as
      { content?: Array<{ content?: unknown[] }> } | undefined;
    expect(table?.content).toHaveLength(3);
    expect(table?.content?.[0]?.content).toHaveLength(3);
    editor.destroy();
  });
});
