import { File, Folder } from "lucide-react";

import { cn } from "@/lib/cn";

export type FileTreeNode = {
  children?: readonly FileTreeNode[];
  id: string;
  kind: "directory" | "file";
  name: string;
  status?: "added" | "modified" | "unchanged";
};

export function FileTree({
  "aria-label": ariaLabel = "文件树",
  nodes,
}: Readonly<{
  "aria-label"?: string;
  nodes: readonly FileTreeNode[];
}>) {
  return (
    <div
      aria-label={ariaLabel}
      className="rounded-xl border border-border bg-card p-2 text-sm"
      role="tree"
    >
      {nodes.length > 0 ? (
        <FileTreeLevel level={1} nodes={nodes} />
      ) : (
        <p className="p-4 text-center text-muted-foreground">暂无文件</p>
      )}
    </div>
  );
}

function FileTreeLevel({
  level,
  nodes,
}: Readonly<{ level: number; nodes: readonly FileTreeNode[] }>) {
  return (
    <ul className="space-y-0.5" role="group">
      {nodes.map((node) => {
        const Icon = node.kind === "directory" ? Folder : File;
        return (
          <li key={node.id} role="treeitem">
            <div
              className="flex min-h-8 items-center gap-2 rounded-md px-2 hover:bg-muted"
              style={{ paddingInlineStart: `${(level - 1) * 16 + 8}px` }}
            >
              <Icon
                aria-hidden="true"
                className={cn(
                  "size-4 shrink-0 text-muted-foreground",
                  node.kind === "directory" ? "fill-muted" : null,
                )}
              />
              <span className="truncate">{node.name}</span>
              {node.status && node.status !== "unchanged" ? (
                <span
                  className={cn(
                    "ml-auto text-[10px] font-semibold uppercase",
                    node.status === "added"
                      ? "text-emerald-600"
                      : "text-amber-600",
                  )}
                >
                  {node.status === "added" ? "A" : "M"}
                </span>
              ) : null}
            </div>
            {node.children?.length ? (
              <FileTreeLevel level={level + 1} nodes={node.children} />
            ) : null}
          </li>
        );
      })}
    </ul>
  );
}
