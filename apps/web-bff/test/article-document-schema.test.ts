import { describe, expect, it } from "vitest";

import { articleDocumentSchema } from "../src/article/document-schema.js";

const image = (index: number) => ({
  attrs: {
    alt: `图片 ${index}`,
    caption: `子题注 ${index}`,
    src: `https://example.test/${index}.png`,
  },
  type: "articleImage",
});

describe("article collaboration document schema", () => {
  it("accepts a wrapping image group as one collaborative block", () => {
    const document = articleDocumentSchema.nodeFromJSON({
      content: [
        {
          attrs: {
            caption: "组合大题注",
            columns: 3,
            id: "image-group-1",
          },
          content: Array.from({ length: 7 }, (_, index) => image(index + 1)),
          type: "articleImageGroup",
        },
      ],
      type: "doc",
    });

    expect(() => document.check()).not.toThrow();
    const group = document.child(0);
    expect(group.type.name).toBe("articleImageGroup");
    expect(group.attrs.columns).toBe(3);
    expect(group.childCount).toBe(7);
    expect(group.child(0).attrs.caption).toBe("子题注 1");
  });

  it("accepts a raw TeX latexBlock as a top-level block", () => {
    // Drafts saved by editors with the latexBlock node must load through
    // this schema (legacy Tiptap-JSON rooms) instead of throwing.
    const document = articleDocumentSchema.nodeFromJSON({
      content: [
        {
          attrs: { id: "latex-1" },
          content: [
            {
              text: "\\begin{problem}\n问题重述\n\\end{problem}",
              type: "text",
            },
          ],
          type: "latexBlock",
        },
      ],
      type: "doc",
    });

    expect(() => document.check()).not.toThrow();
    expect(document.child(0).type.name).toBe("latexBlock");
    expect(document.child(0).attrs.id).toBe("latex-1");
    expect(document.child(0).textContent).toContain("\\begin{problem}");
  });
});
