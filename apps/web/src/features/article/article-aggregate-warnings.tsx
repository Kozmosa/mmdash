import type { ArticleAggregate } from "./types";

const labels: Record<string, string> = {
  builds: "构建记录",
  "chapter_tags.bootstrap": "章节 Tag 初始化",
  chapter_tags: "章节 Tag",
  commits: "Commit 记录",
  commit_operations: "Commit 操作",
  references: "固定引用",
  releases: "Release 记录",
  templates: "论文模板",
  "templates.bootstrap": "默认模板初始化",
};

export function ArticleAggregateWarnings({
  warnings,
}: {
  warnings?: ArticleAggregate["warnings"];
}) {
  if (!warnings?.length) return null;
  return (
    <div
      aria-label="论文部分区域暂不可用"
      className="rounded-lg border border-amber-500/40 bg-amber-500/5 p-3 text-sm text-amber-800 dark:text-amber-200"
      role="status"
    >
      <p className="font-medium">论文已以降级模式打开</p>
      <ul className="mt-1 list-disc pl-5 text-xs">
        {warnings.map((warning) => (
          <li key={warning.component}>
            {labels[warning.component] ?? warning.component}：{warning.message}
          </li>
        ))}
      </ul>
    </div>
  );
}
