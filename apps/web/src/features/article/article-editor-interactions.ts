export type EditorPoint = { x: number; y: number };

export type EditorRectangle = {
  bottom: number;
  left: number;
  right: number;
  top: number;
};

export function rectangleFromPoints(
  start: EditorPoint,
  end: EditorPoint,
): EditorRectangle {
  return {
    bottom: Math.max(start.y, end.y),
    left: Math.min(start.x, end.x),
    right: Math.max(start.x, end.x),
    top: Math.min(start.y, end.y),
  };
}

export function rectanglesIntersect(
  left: EditorRectangle,
  right: EditorRectangle,
): boolean {
  return !(
    left.right < right.left ||
    left.left > right.right ||
    left.bottom < right.top ||
    left.top > right.bottom
  );
}

export function dropIndicatorOffset(
  boundaryClientY: number,
  surfaceClientTop: number,
  surfaceScrollTop: number,
): number {
  return Math.max(4, boundaryClientY - surfaceClientTop + surfaceScrollTop);
}

export function dropTargetPosition(
  blockPosition: number,
  blockNodeSize: number,
  before: boolean,
): number {
  return before ? blockPosition : blockPosition + blockNodeSize;
}

export function moveArrayItem<T>(
  items: readonly T[],
  sourceIndex: number,
  targetIndex: number,
): T[] {
  if (
    sourceIndex < 0 ||
    sourceIndex >= items.length ||
    targetIndex < 0 ||
    targetIndex >= items.length ||
    sourceIndex === targetIndex
  ) {
    return [...items];
  }
  const next = [...items];
  const [moved] = next.splice(sourceIndex, 1);
  next.splice(targetIndex, 0, moved!);
  return next;
}
