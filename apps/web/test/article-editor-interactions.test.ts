import { describe, expect, it } from "vitest";

import {
  dropIndicatorOffset,
  dropTargetPosition,
  moveArrayItem,
  rectangleFromPoints,
  rectanglesIntersect,
} from "@/features/article/article-editor-interactions";

describe("article editor interactions", () => {
  it("builds a direction-independent marquee rectangle", () => {
    expect(rectangleFromPoints({ x: 80, y: 60 }, { x: 20, y: 10 })).toEqual({
      bottom: 60,
      left: 20,
      right: 80,
      top: 10,
    });
  });

  it("selects every block intersected by the marquee", () => {
    expect(
      rectanglesIntersect(
        { bottom: 80, left: 20, right: 90, top: 20 },
        { bottom: 120, left: 40, right: 140, top: 70 },
      ),
    ).toBe(true);
    expect(
      rectanglesIntersect(
        { bottom: 20, left: 0, right: 20, top: 0 },
        { bottom: 60, left: 40, right: 80, top: 40 },
      ),
    ).toBe(false);
  });

  it("keeps a drop line attached to document content while scrolling", () => {
    expect(dropIndicatorOffset(260, 100, 180)).toBe(340);
  });

  it("uses the indicated block boundary as the insertion position", () => {
    expect(dropTargetPosition(12, 8, true)).toBe(12);
    expect(dropTargetPosition(12, 8, false)).toBe(20);
  });

  it("moves table rows and columns without mutating the source", () => {
    const source = ["A", "B", "C"];
    expect(moveArrayItem(source, 0, 2)).toEqual(["B", "C", "A"]);
    expect(source).toEqual(["A", "B", "C"]);
  });
});
