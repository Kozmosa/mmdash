import { describe, expect, it } from "vitest";

import {
  articleSidebarDefaultWidth,
  clampArticleSidebarWidth,
} from "@/features/article/article-layout";

describe("Article writing layout", () => {
  it("keeps drag-resized sidebar widths within the usable range", () => {
    expect(clampArticleSidebarWidth(100)).toBe(260);
    expect(clampArticleSidebarWidth(391.6)).toBe(392);
    expect(clampArticleSidebarWidth(900)).toBe(520);
    expect(clampArticleSidebarWidth(Number.NaN)).toBe(
      articleSidebarDefaultWidth,
    );
  });
});
