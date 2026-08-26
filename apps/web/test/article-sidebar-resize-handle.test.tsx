import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ArticleSidebarResizeHandle } from "@/features/article/article-sidebar-resize-handle";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("ArticleSidebarResizeHandle", () => {
  it("resizes with pointer capture and persists the final width", () => {
    const onResize = vi.fn();
    const onResizeEnd = vi.fn();
    const setPointerCapture = vi.fn();
    const hasPointerCapture = vi.fn(() => true);
    const releasePointerCapture = vi.fn();
    Object.defineProperties(HTMLElement.prototype, {
      hasPointerCapture: { configurable: true, value: hasPointerCapture },
      releasePointerCapture: {
        configurable: true,
        value: releasePointerCapture,
      },
      setPointerCapture: { configurable: true, value: setPointerCapture },
    });
    class TestPointerEvent extends MouseEvent {
      readonly pointerId: number;

      constructor(type: string, init: PointerEventInit = {}) {
        super(type, init);
        this.pointerId = init.pointerId ?? 0;
      }
    }
    vi.stubGlobal("PointerEvent", TestPointerEvent);

    render(
      <ArticleSidebarResizeHandle
        onResize={onResize}
        onResizeEnd={onResizeEnd}
        width={320}
      />,
    );
    const handle = screen.getByRole("separator", {
      name: "拖动调整左栏宽度",
    });
    fireEvent.pointerDown(handle, { clientX: 100, pointerId: 7 });
    fireEvent.pointerMove(handle, { clientX: 180, pointerId: 7 });
    fireEvent.pointerUp(handle, { clientX: 180, pointerId: 7 });

    expect(setPointerCapture).toHaveBeenCalledWith(7);
    expect(onResize).toHaveBeenCalledWith(400);
    expect(onResizeEnd).toHaveBeenCalledWith(400);
    expect(hasPointerCapture).toHaveBeenCalledWith(7);
    expect(releasePointerCapture).toHaveBeenCalledWith(7);
  });

  it("supports keyboard resizing and a double-click reset", () => {
    const onResizeEnd = vi.fn();
    render(
      <ArticleSidebarResizeHandle
        onResize={vi.fn()}
        onResizeEnd={onResizeEnd}
        width={320}
      />,
    );
    const handle = screen.getByRole("separator");
    fireEvent.keyDown(handle, { key: "ArrowRight" });
    fireEvent.doubleClick(handle);
    expect(onResizeEnd.mock.calls.map(([width]) => width)).toEqual([336, 320]);
  });
});
