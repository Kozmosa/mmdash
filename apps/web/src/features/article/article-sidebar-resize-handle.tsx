"use client";

import { useRef, type KeyboardEvent, type PointerEvent } from "react";

import {
  articleSidebarDefaultRatio,
  clampArticleSidebarRatio,
} from "./article-layout";

export function ArticleSidebarResizeHandle({
  onResize,
  onResizeEnd,
  ratio,
}: {
  onResize: (ratio: number) => void;
  onResizeEnd: (ratio: number) => void;
  ratio: number;
}) {
  const drag = useRef<
    | {
        containerWidth: number;
        pointerId: number;
        startRatio: number;
        startX: number;
      }
    | undefined
  >(undefined);

  const start = (event: PointerEvent<HTMLButtonElement>) => {
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    const parentContainer = event.currentTarget.closest<HTMLElement>(
      "[data-article-workbench-container]",
    );
    const containerWidth =
      parentContainer?.clientWidth || window.innerWidth || 1024;
    drag.current = {
      containerWidth,
      pointerId: event.pointerId,
      startRatio: ratio,
      startX: event.clientX,
    };
  };

  const move = (event: PointerEvent<HTMLButtonElement>) => {
    const active = drag.current;
    if (!active || active.pointerId !== event.pointerId) return;
    const deltaX = event.clientX - active.startX;
    onResize(
      clampArticleSidebarRatio(
        active.startRatio + deltaX / active.containerWidth,
        active.containerWidth,
      ),
    );
  };

  const finish = (event: PointerEvent<HTMLButtonElement>) => {
    const active = drag.current;
    if (!active || active.pointerId !== event.pointerId) return;
    const deltaX = event.clientX - active.startX;
    const nextRatio = clampArticleSidebarRatio(
      active.startRatio + deltaX / active.containerWidth,
      active.containerWidth,
    );
    drag.current = undefined;
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    onResizeEnd(nextRatio);
  };

  const resizeWithKeyboard = (event: KeyboardEvent<HTMLButtonElement>) => {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    event.preventDefault();
    const step = event.key === "ArrowRight" ? 0.02 : -0.02;
    onResizeEnd(clampArticleSidebarRatio(ratio + step));
  };

  return (
    <button
      aria-label="拖动调整左栏宽度"
      aria-orientation="vertical"
      aria-valuemax={100}
      aria-valuemin={0}
      aria-valuenow={Math.round(ratio * 100)}
      className="absolute inset-y-0 right-0 z-20 w-1.5 cursor-col-resize touch-none bg-transparent transition-colors hover:bg-primary/25 focus-visible:bg-primary/25 focus-visible:outline-none"
      onDoubleClick={() => onResizeEnd(articleSidebarDefaultRatio)}
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
