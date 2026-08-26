export const articleSidebarMinWidth = 220;
export const articleEditorMinWidth = 360;
export const articleSidebarDefaultWidth = 320;
export const articleSidebarDefaultRatio = 0.25;
export const articleSidebarMinRatio = 0.15;
export const articleSidebarMaxRatio = 0.75;
export const articleSidebarAbsoluteMaxWidth = 1400;

export const articleOutlineMinHeight = 100;
export const articleOutlineMaxHeight = 600;
export const articleOutlineDefaultHeight = 220;

export function clampArticleSidebarRatio(
  ratio: number,
  containerWidth?: number,
): number {
  if (!Number.isFinite(ratio)) return articleSidebarDefaultRatio;
  if (containerWidth !== undefined && containerWidth > 0) {
    const minRatio = Math.min(
      articleSidebarMaxRatio,
      articleSidebarMinWidth / containerWidth,
    );
    const maxRatio = Math.max(
      minRatio,
      (containerWidth - articleEditorMinWidth) / containerWidth,
    );
    return Math.min(maxRatio, Math.max(minRatio, ratio));
  }
  return Math.min(
    articleSidebarMaxRatio,
    Math.max(articleSidebarMinRatio, ratio),
  );
}

export function clampArticleSidebarWidth(
  value: number,
  containerWidth?: number,
): number {
  if (!Number.isFinite(value)) return articleSidebarDefaultWidth;
  const maxWidth =
    containerWidth !== undefined && containerWidth > 0
      ? Math.max(articleSidebarMinWidth, containerWidth - articleEditorMinWidth)
      : articleSidebarAbsoluteMaxWidth;
  return Math.min(
    maxWidth,
    Math.max(articleSidebarMinWidth, Math.round(value)),
  );
}

export function clampArticleOutlineHeight(value: number): number {
  if (!Number.isFinite(value)) return articleOutlineDefaultHeight;
  return Math.min(
    articleOutlineMaxHeight,
    Math.max(articleOutlineMinHeight, Math.round(value)),
  );
}
