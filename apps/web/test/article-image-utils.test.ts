import { describe, expect, it } from "vitest";

import {
  availableThumbnailURL,
  isTransientImageURL,
  normalizeImageWidth,
} from "@/features/article/article-image-utils";

describe("Article image persistence", () => {
  it("clamps image width", () => {
    expect(normalizeImageWidth(5)).toBe(20);
    expect(normalizeImageWidth(67)).toBe(67);
    expect(normalizeImageWidth(120)).toBe(100);
  });

  it("rejects signed download URLs but allows stable public URLs", () => {
    expect(
      isTransientImageURL(
        "https://objects.example/figure.png?X-Amz-Signature=secret",
      ),
    ).toBe(true);
    expect(isTransientImageURL("https://example.com/figure.png?v=2")).toBe(
      false,
    );
  });

  it("selects only an available thumbnail preview", () => {
    expect(
      availableThumbnailURL([
        {
          preview_type: "thumbnail",
          status: "processing",
          transfer: null,
        },
        {
          preview_type: "image",
          status: "available",
          transfer: { url: "https://example.com/original" },
        },
        {
          preview_type: "thumbnail",
          status: "available",
          transfer: { url: "https://example.com/thumb" },
        },
      ]),
    ).toBe("https://example.com/thumb");
  });
});
