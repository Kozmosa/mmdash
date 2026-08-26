import { describe, expect, it } from "vitest";

import { visibleArticleOutline } from "@/features/article/article-outline";

const outline = [
  { id: "a", level: 1, text: "A" },
  { id: "a-1", level: 2, text: "A.1" },
  { id: "a-1-1", level: 3, text: "A.1.1" },
  { id: "a-2", level: 2, text: "A.2" },
  { id: "b", level: 1, text: "B" },
];

describe("Article outline", () => {
  it("hides only descendants of a collapsed chapter", () => {
    expect(
      visibleArticleOutline(outline, new Set(["a-1"])).map((item) => item.id),
    ).toEqual(["a", "a-1", "a-2", "b"]);
    expect(
      visibleArticleOutline(outline, new Set(["a"])).map((item) => item.id),
    ).toEqual(["a", "b"]);
  });
});
