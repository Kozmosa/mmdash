"use client";

import { useRef, type KeyboardEvent, type PointerEvent } from "react";

import {
  articleOutlineDefaultHeight,
  articleOutlineMaxHeight,
  articleOutlineMinHeight,
  clampArticleOutlineHeight,
} from "./article-layout";

export function ArticleOutlineResizeHandle({
  height,
  onResize,
  onResizeEnd,
}: {
  height: number;
  onResize: (height: number) => void;
  onResizeEnd: (height: number) => void;
}) {
  const drag = useRef<
    | {
        pointerId: number;
        startHeight: number;
        startY: number;
      }
    | undefined
  >(undefined);

  const start = (event: PointerEvent<HTMLButtonElement>) => {
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    drag.current = {
      pointerId: event.pointerId,
      startHeight: height,
      startY: event.clientY,
    };
  };

  const move = (event: PointerEvent<HTMLButtonElement>) => {
    const active = drag.current;
    if (!active || active.pointerId !== event.pointerId) return;
    onResize(
      clampArticleOutlineHeight(
        active.startHeight + (active.startY - event.clientY),
      ),
    );
  };

  const finish = (event: PointerEvent<HTMLButtonElement>) => {
    const active = drag.current;
    if (!active || active.pointerId !== event.pointerId) return;
    const next = clampArticleOutlineHeight(
      active.startHeight + (active.startY - event.clientY),
    );
    drag.current = undefined;
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    onResizeEnd(next);
  };

  const resizeWithKeyboard = (event: KeyboardEvent<HTMLButtonElement>) => {
    if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
    event.preventDefault();
    onResizeEnd(
      clampArticleOutlineHeight(height + (event.key === "ArrowUp" ? 16 : -16)),
    );
  };

  return (
    <button
      aria-label="拖动调整目录高度"
      aria-orientation="horizontal"
      aria-valuemax={articleOutlineMaxHeight}
      aria-valuemin={articleOutlineMinHeight}
      aria-valuenow={height}
      className="group relative z-10 flex h-2 w-full shrink-0 cursor-row-resize items-center justify-center touch-none border-t bg-transparent transition-colors hover:bg-primary/20 focus-visible:bg-primary/20 focus-visible:outline-none"
      onDoubleClick={() => onResizeEnd(articleOutlineDefaultHeight)}
      onKeyDown={resizeWithKeyboard}
      onPointerCancel={finish}
      onPointerDown={start}
      onPointerMove={move}
      onPointerUp={finish}
      role="separator"
      title="拖动调整目录高度；双击恢复默认"
      type="button"
    >
      <div className="h-0.5 w-8 rounded-full bg-muted-foreground/30 transition-colors group-hover:bg-primary/60" />
    </button>
  );
}
