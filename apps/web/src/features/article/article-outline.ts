import type { ArticleOutlineItem } from "./article-editor";

export function visibleArticleOutline(
  outline: ArticleOutlineItem[],
  collapsedIds: ReadonlySet<string>,
): ArticleOutlineItem[] {
  const visible: ArticleOutlineItem[] = [];
  const collapsedLevels: number[] = [];
  for (const item of outline) {
    while (
      collapsedLevels.length &&
      item.level <= collapsedLevels[collapsedLevels.length - 1]!
    ) {
      collapsedLevels.pop();
    }
    if (!collapsedLevels.length) visible.push(item);
    if (!collapsedLevels.length && collapsedIds.has(item.id)) {
      collapsedLevels.push(item.level);
    }
  }
  return visible;
}
