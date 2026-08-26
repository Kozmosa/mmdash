export const articleSidebarMinWidth = 260;
export const articleSidebarMaxWidth = 520;
export const articleSidebarDefaultWidth = 320;

export function clampArticleSidebarWidth(value: number): number {
  if (!Number.isFinite(value)) return articleSidebarDefaultWidth;
  return Math.min(
    articleSidebarMaxWidth,
    Math.max(articleSidebarMinWidth, Math.round(value)),
  );
}
