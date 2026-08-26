import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ArticleSidebarResizeHandle } from "@/features/article/article-sidebar-resize-handle";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("ArticleSidebarResizeHandle", () => {
  it("resizes with pointer capture and persists the final ratio", () => {
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

    const { container } = render(
      <div data-article-workbench-container style={{ width: 1000 }}>
        <ArticleSidebarResizeHandle
          onResize={onResize}
          onResizeEnd={onResizeEnd}
          ratio={0.25}
        />
      </div>,
    );
    const workbenchContainer = container.querySelector(
      "[data-article-workbench-container]",
    );
    if (workbenchContainer) {
      Object.defineProperty(workbenchContainer, "clientWidth", {
        configurable: true,
        value: 1000,
      });
    }
    const handle = screen.getByRole("separator", {
      name: "拖动调整左栏宽度",
    });
    fireEvent.pointerDown(handle, { clientX: 100, pointerId: 7 });
    fireEvent.pointerMove(handle, { clientX: 150, pointerId: 7 });
    fireEvent.pointerUp(handle, { clientX: 150, pointerId: 7 });

    expect(setPointerCapture).toHaveBeenCalledWith(7);
    expect(onResize).toHaveBeenCalledWith(0.3);
    expect(onResizeEnd).toHaveBeenCalledWith(0.3);
    expect(hasPointerCapture).toHaveBeenCalledWith(7);
    expect(releasePointerCapture).toHaveBeenCalledWith(7);
  });

  it("supports keyboard resizing and a double-click reset", () => {
    const onResizeEnd = vi.fn();
    render(
      <ArticleSidebarResizeHandle
        onResize={vi.fn()}
        onResizeEnd={onResizeEnd}
        ratio={0.25}
      />,
    );
    const handle = screen.getByRole("separator");
    fireEvent.keyDown(handle, { key: "ArrowRight" });
    fireEvent.doubleClick(handle);
    expect(onResizeEnd.mock.calls.map(([ratio]) => ratio)).toEqual([0.27, 0.25]);
  });
});
