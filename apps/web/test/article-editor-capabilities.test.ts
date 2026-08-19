import { Editor } from "@tiptap/core";
import { Mathematics } from "@tiptap/extension-mathematics";
import { TableKit } from "@tiptap/extension-table";
import StarterKit from "@tiptap/starter-kit";
import { describe, expect, it } from "vitest";

import { runSlashItemForTest } from "@/features/article/slash-command";
import { parseArticleArtifactDrop } from "@/features/article/article-editor";

describe("Article editor block capabilities", () => {
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
});
