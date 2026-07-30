"use client";

import { useInfiniteQuery } from "@tanstack/react-query";
import {
  ChevronDown,
  ChevronRight,
  File,
  FileSymlink,
  Folder,
  GitBranch,
} from "lucide-react";
import { type KeyboardEvent, useState } from "react";

import { Button } from "@/components/ui/button";
import type {
  RepoTreeEntry,
  RepoTreePage,
  RepoWorkspaceKind,
} from "@/features/repo/types";
import { apiClient } from "@/lib/api-client";
import { cn } from "@/lib/cn";

type RepoTreeProps = {
  onSelect: (path: string) => void;
  projectId: string;
  revision: string;
  selectedPath: string;
  workspace: RepoWorkspaceKind;
};

export function RepoTree(props: Readonly<RepoTreeProps>) {
  return (
    <div
      aria-label="Repository 文件树"
      className="min-h-72 rounded-xl border bg-card p-2 text-sm"
      role="tree"
    >
      <DirectoryLevel {...props} level={1} path="" />
    </div>
  );
}

function DirectoryLevel({
  level,
  onSelect,
  path,
  projectId,
  revision,
  selectedPath,
  workspace,
}: Readonly<RepoTreeProps & { level: number; path: string }>) {
  const query = useInfiniteQuery({
    getNextPageParam: (lastPage: RepoTreePage) =>
      lastPage.has_more ? lastPage.next_cursor : undefined,
    initialPageParam: "",
    queryFn: ({ pageParam, signal }) =>
      apiClient.request<RepoTreePage>(
        `/projects/${encodeURIComponent(projectId)}/repository/tree`,
        {
          query: {
            cursor: pageParam || undefined,
            limit: 200,
            path: path || undefined,
            revision,
            workspace,
          },
          signal,
        },
      ),
    queryKey: ["repo-tree", projectId, workspace, revision, path],
  });

  if (query.isPending) {
    return (
      <p aria-live="polite" className="p-2 text-xs text-muted-foreground">
        正在读取目录…
      </p>
    );
  }
  if (query.isError) {
    return (
      <div className="p-2 text-xs text-red-700" role="alert">
        目录读取失败。
        <Button
          className="ml-2"
          onClick={() => void query.refetch()}
          size="sm"
          variant="ghost"
        >
          重试
        </Button>
      </div>
    );
  }

  const entries = query.data.pages.flatMap((page) => page.items);
  if (entries.length === 0) {
    return (
      <p className="p-2 text-xs text-muted-foreground">
        {path ? "空目录" : "该 revision 没有文件"}
      </p>
    );
  }
  return (
    <div role={level === 1 ? "presentation" : "group"}>
      {entries.map((entry, index) => (
        <TreeItem
          entry={entry}
          first={level === 1 && index === 0}
          key={`${entry.kind}:${entry.path}`}
          level={level}
          onSelect={onSelect}
          projectId={projectId}
          revision={revision}
          selectedPath={selectedPath}
          workspace={workspace}
        />
      ))}
      {query.hasNextPage ? (
        <Button
          className="ml-6 mt-1"
          disabled={query.isFetchingNextPage}
          onClick={() => void query.fetchNextPage()}
          size="sm"
          variant="ghost"
        >
          {query.isFetchingNextPage ? "正在载入…" : "载入更多"}
        </Button>
      ) : null}
    </div>
  );
}

function TreeItem({
  entry,
  first,
  level,
  onSelect,
  projectId,
  revision,
  selectedPath,
  workspace,
}: Readonly<
  RepoTreeProps & {
    entry: RepoTreeEntry;
    first: boolean;
    level: number;
  }
>) {
  const [expanded, setExpanded] = useState(false);
  const directory = entry.kind === "directory";
  const selected = !directory && selectedPath === entry.path;
  const Icon =
    entry.kind === "directory"
      ? Folder
      : entry.kind === "symlink"
        ? FileSymlink
        : entry.kind === "submodule"
          ? GitBranch
          : File;

  return (
    <div role="none">
      <button
        aria-expanded={directory ? expanded : undefined}
        aria-level={level}
        aria-selected={selected}
        className={cn(
          "flex min-h-9 w-full items-center gap-1.5 rounded-md pr-2 text-left outline-none hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring",
          selected ? "bg-accent font-medium" : null,
        )}
        onClick={() => {
          if (directory) {
            setExpanded((value) => !value);
          } else {
            onSelect(entry.path);
          }
        }}
        onKeyDown={(event) =>
          handleTreeKey(event, directory, expanded, setExpanded)
        }
        role="treeitem"
        style={{ paddingInlineStart: `${(level - 1) * 16 + 6}px` }}
        tabIndex={selected || first ? 0 : -1}
        type="button"
      >
        {directory ? (
          expanded ? (
            <ChevronDown aria-hidden="true" className="size-3.5 shrink-0" />
          ) : (
            <ChevronRight aria-hidden="true" className="size-3.5 shrink-0" />
          )
        ) : (
          <span aria-hidden="true" className="size-3.5 shrink-0" />
        )}
        <Icon
          aria-hidden="true"
          className={cn(
            "size-4 shrink-0 text-muted-foreground",
            directory ? "fill-muted" : null,
          )}
        />
        <span className="truncate">{entry.name}</span>
        {entry.size !== null && !directory ? (
          <span className="ml-auto text-[10px] text-muted-foreground">
            {formatBytes(entry.size)}
          </span>
        ) : null}
      </button>
      {directory && expanded ? (
        <DirectoryLevel
          level={level + 1}
          onSelect={onSelect}
          path={entry.path}
          projectId={projectId}
          revision={revision}
          selectedPath={selectedPath}
          workspace={workspace}
        />
      ) : null}
    </div>
  );
}

function handleTreeKey(
  event: KeyboardEvent<HTMLButtonElement>,
  directory: boolean,
  expanded: boolean,
  setExpanded: (expanded: boolean) => void,
) {
  const item = event.currentTarget;
  const tree = item.closest('[role="tree"]');
  if (!tree) return;
  const visibleItems = [
    ...tree.querySelectorAll<HTMLButtonElement>('button[role="treeitem"]'),
  ];
  const index = visibleItems.indexOf(item);
  let target: HTMLButtonElement | undefined;
  switch (event.key) {
    case "ArrowDown":
      target = visibleItems[index + 1];
      break;
    case "ArrowUp":
      target = visibleItems[index - 1];
      break;
    case "Home":
      target = visibleItems[0];
      break;
    case "End":
      target = visibleItems.at(-1);
      break;
    case "ArrowRight":
      if (directory && !expanded) {
        setExpanded(true);
      } else if (directory) {
        target = visibleItems[index + 1];
      }
      break;
    case "ArrowLeft":
      if (directory && expanded) {
        setExpanded(false);
      } else {
        target = parentTreeItem(item);
      }
      break;
    case "Enter":
    case " ":
      return;
    default:
      return;
  }
  event.preventDefault();
  target?.focus();
}

function parentTreeItem(
  item: HTMLButtonElement,
): HTMLButtonElement | undefined {
  const group = item.parentElement?.parentElement;
  if (group?.getAttribute("role") !== "group") {
    return undefined;
  }
  return (
    group.parentElement?.querySelector<HTMLButtonElement>(
      ":scope > button[role='treeitem']",
    ) ?? undefined
  );
}

function formatBytes(value: number): string {
  if (value < 1_024) return `${value} B`;
  if (value < 1_024 * 1_024) return `${(value / 1_024).toFixed(1)} KB`;
  return `${(value / (1_024 * 1_024)).toFixed(1)} MB`;
}
