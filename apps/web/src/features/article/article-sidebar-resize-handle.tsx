"use client";

import { useRef, type KeyboardEvent, type PointerEvent } from "react";

import {
  articleSidebarDefaultWidth,
  articleSidebarMaxWidth,
  articleSidebarMinWidth,
  clampArticleSidebarWidth,
} from "./article-layout";

export function ArticleSidebarResizeHandle({
  onResize,
  onResizeEnd,
  width,
}: {
  onResize: (width: number) => void;
  onResizeEnd: (width: number) => void;
  width: number;
}) {
  const drag = useRef<
    | {
        pointerId: number;
        startWidth: number;
        startX: number;
      }
    | undefined
  >(undefined);

  const start = (event: PointerEvent<HTMLButtonElement>) => {
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    drag.current = {
      pointerId: event.pointerId,
      startWidth: width,
      startX: event.clientX,
    };
  };

  const move = (event: PointerEvent<HTMLButtonElement>) => {
    const active = drag.current;
    if (!active || active.pointerId !== event.pointerId) return;
    onResize(
      clampArticleSidebarWidth(
        active.startWidth + event.clientX - active.startX,
      ),
    );
  };

  const finish = (event: PointerEvent<HTMLButtonElement>) => {
    const active = drag.current;
    if (!active || active.pointerId !== event.pointerId) return;
    const next = clampArticleSidebarWidth(
      active.startWidth + event.clientX - active.startX,
    );
    drag.current = undefined;
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    onResizeEnd(next);
  };

  const resizeWithKeyboard = (event: KeyboardEvent<HTMLButtonElement>) => {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    event.preventDefault();
    onResizeEnd(
      clampArticleSidebarWidth(width + (event.key === "ArrowRight" ? 16 : -16)),
    );
  };

  return (
    <button
      aria-label="拖动调整左栏宽度"
      aria-orientation="vertical"
      aria-valuemax={articleSidebarMaxWidth}
      aria-valuemin={articleSidebarMinWidth}
      aria-valuenow={width}
      className="absolute inset-y-0 right-0 z-20 w-1.5 cursor-col-resize touch-none bg-transparent transition-colors hover:bg-primary/25 focus-visible:bg-primary/25 focus-visible:outline-none"
      onDoubleClick={() => onResizeEnd(articleSidebarDefaultWidth)}
      onKeyDown={resizeWithKeyboard}
      onPointerCancel={finish}
      onPointerDown={start}
      onPointerMove={move}
      onPointerUp={finish}
      role="separator"
      title="拖动调整左栏宽度；双击恢复默认"
      type="button"
    />
  );
}
