// @vitest-environment jsdom

import { render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { PDFReader } from "@/components/ui/pdf-reader";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("PDFReader", () => {
  it("does not surface an AbortError when request cleanup throws", () => {
    class ThrowingAbortController {
      readonly signal = { aborted: false };

      abort() {
        throw new DOMException(
          "signal is aborted without reason",
          "AbortError",
        );
      }
    }

    vi.stubGlobal("AbortController", ThrowingAbortController);
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise<never>(() => {})),
    );

    const view = render(
      <PDFReader
        title="测试 PDF"
        transfer={{ headers: {}, url: "https://example.test/paper.pdf" }}
      />,
    );

    expect(() => view.unmount()).not.toThrow();
  });
});
