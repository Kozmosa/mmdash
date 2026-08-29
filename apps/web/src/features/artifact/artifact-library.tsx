"use client";

import {
  useInfiniteQuery,
  type InfiniteData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  ArchiveRestore,
  Check,
  Download,
  File,
  FileArchive,
  FileText,
  FilterX,
  FolderPlus,
  FolderOpen,
  Plus,
  Search,
  Trash2,
  UploadCloud,
  X,
} from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

import { EmptyState } from "@/components/states/empty-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { useCurrentProject } from "@/components/providers/project-provider";
import { apiClient } from "@/lib/api-client";
import { cn } from "@/lib/cn";

import { artifactApi } from "./artifact-api";
import {
  ArtifactFolderBrowser,
  artifactMoveMime,
} from "./artifact-folder-browser";
import { ArtifactFolderDeleteActions } from "./artifact-folder-delete-actions";
import {
  artifactFolderDescendantIds,
  flattenArtifactFolders,
} from "./artifact-folders";
import { ArtifactDetailDrawer } from "./artifact-detail-drawer";
import { ArtifactUploader, formatBytes } from "./artifact-uploader";
import type {
  ArtifactDetail,
  ArtifactFolder,
  ArtifactKind,
  ArtifactListFilters,
  ArtifactPage,
  ArtifactSource,
  ArtifactStatus,
} from "./types";

export function ArtifactLibrary() {
  const project = useCurrentProject();
  const queryClient = useQueryClient();
  const router = useRouter();
  const searchParams = useSearchParams();
  const setupMode = searchParams.get("setup") === "1";
  const [detailId, setDetailId] = useState<string | undefined>(
    searchParams.get("artifact") ?? undefined,
  );
  const [filters, setFilters] = useState<ArtifactListFilters>({
    limit: 50,
  });
  const [selected, setSelected] = useState<string[]>([]);
  const [trashView, setTrashView] = useState(
    searchParams.get("view") === "trash",
  );
  const [uploaderOpen, setUploaderOpen] = useState(false);
  const [folder, setFolder] = useState<string | null>(null);
  const folderTree = useQuery({
    enabled: !trashView,
    queryFn: () => artifactApi.listFolders(project.id),
    queryKey: ["artifact-folders", project.id],
  });
  const folderOptions = useMemo(
    () => flattenArtifactFolders(folderTree.data?.items ?? []),
    [folderTree.data],
  );
  const selectedFolder = folderOptions.find(
    (item) => item.folder.folder_id === folder,
  )?.folder;
  const refreshFoldersAndArtifacts = async () => {
    await Promise.all([
      queryClient.invalidateQueries({
        queryKey: ["artifact-folders", project.id],
      }),
      queryClient.invalidateQueries({ queryKey: ["artifacts", project.id] }),
    ]);
  };
  const createFolder = useMutation({
    mutationFn: (name: string) =>
      artifactApi.createFolder(
        project.id,
        name,
        selectedFolder?.folder_id ?? null,
      ),
    onError: (error) => toast.error(error.message),
    onSuccess: async (created) => {
      await refreshFoldersAndArtifacts();
      setFolder(created.folder_id);
      toast.success("文件夹已创建");
    },
  });
  const renameFolder = useMutation({
    mutationFn: (name: string) =>
      artifactApi.renameFolder(project.id, selectedFolder!.folder_id, name),
    onError: (error) => toast.error(error.message),
    onSuccess: async () => {
      await refreshFoldersAndArtifacts();
      toast.success("文件夹已重命名");
    },
  });
  const deleteFolder = useMutation({
    mutationFn: (recursive: boolean) =>
      artifactApi.deleteFolder(
        project.id,
        selectedFolder!.folder_id,
        recursive,
      ),
    onError: (error) => toast.error(error.message),
    onSuccess: async () => {
      setFolder(null);
      await refreshFoldersAndArtifacts();
      toast.success("文件夹结构已删除，所含 Artifact 已移到根目录");
    },
  });
  const moveFolder = useMutation({
    mutationFn: ({
      folderId,
      parentFolderId,
    }: {
      folderId: string;
      parentFolderId: string | null;
    }) => artifactApi.moveFolder(project.id, folderId, parentFolderId),
    onError: (error) => toast.error(error.message),
    onSuccess: async () => {
      await refreshFoldersAndArtifacts();
      toast.success("文件夹已移动");
    },
  });
  const moveArtifact = useMutation({
    mutationFn: ({
      artifactId,
      folderId,
    }: {
      artifactId: string;
      folderId: string | null;
    }) => artifactApi.moveArtifact(project.id, artifactId, folderId),
    onError: (error) => toast.error(error.message),
    onSuccess: async () => {
      await refreshFoldersAndArtifacts();
      toast.success("Artifact 已移动");
    },
  });

  const projectDetail = useQuery({
    enabled: setupMode,
    queryFn: () =>
      apiClient.request<{
        id: string;
        source_artifact_ids: string[];
      }>(`/projects/${encodeURIComponent(project.id)}`),
    queryKey: ["project-source-artifacts", project.id],
  });
  useEffect(() => {
    if (setupMode && projectDetail.data) {
      setSelected(projectDetail.data.source_artifact_ids);
    }
  }, [projectDetail.data, setupMode]);

  const artifacts = useInfiniteQuery<
    ArtifactPage,
    Error,
    InfiniteData<ArtifactPage, string | undefined>,
    readonly unknown[],
    string | undefined
  >({
    getNextPageParam: (lastPage) =>
      lastPage.has_more ? (lastPage.next_cursor ?? undefined) : undefined,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => {
      const pageFilters = { ...filters, cursor: pageParam };
      return trashView
        ? artifactApi.listTrash(project.id, {
            cursor: pageFilters.cursor,
            kind: pageFilters.kind,
            limit: pageFilters.limit,
            tag: pageFilters.tag,
          })
        : artifactApi.list(project.id, pageFilters);
    },
    queryKey: ["artifacts", project.id, trashView, filters] as const,
  });
  const allItems = useMemo(
    () => artifacts.data?.pages.flatMap((page) => page.items) ?? [],
    [artifacts.data],
  );
  const items = useMemo(
    () => allItems.filter((item) => item.artifact.folder_id === folder),
    [allItems, folder],
  );
  const canUpload = ["owner", "maintainer", "editor"].includes(
    project.role ?? "",
  );
  const excludedFolderTargets = selectedFolder
    ? artifactFolderDescendantIds(selectedFolder)
    : new Set<string>();
  if (selectedFolder) excludedFolderTargets.add(selectedFolder.folder_id);
  const unavailableSelected = selected.filter(
    (artifactId) =>
      !items.some((item) => item.artifact.artifact_id === artifactId),
  );

  const saveSources = useMutation({
    mutationFn: () =>
      apiClient.request(`/projects/${encodeURIComponent(project.id)}`, {
        body: { source_artifact_ids: selected },
        method: "PATCH",
      }),
    onError(error) {
      toast.error(error.message);
    },
    onSuccess() {
      toast.success("题目原始文件已绑定到项目");
      void queryClient.invalidateQueries({
        queryKey: ["project-source-artifacts", project.id],
      });
      void queryClient.invalidateQueries({
        queryKey: ["project-home", project.id],
      });
      router.push(`/projects/${encodeURIComponent(project.id)}`);
    },
  });

  return (
    <div className="grid gap-6">
      <header className="flex flex-wrap items-start gap-4">
        <span className="flex size-11 items-center justify-center rounded-xl border border-border bg-card shadow-sm">
          <FolderOpen aria-hidden="true" className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-2xl font-semibold tracking-tight">项目文件库</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            稳定 Artifact、不可变版本、短期受控传输，以及可恢复的独立回收站。
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          {!trashView ? (
            <Button
              disabled={createFolder.isPending}
              onClick={() => {
                const name = window.prompt(
                  selectedFolder
                    ? `在“${selectedFolder.name}”中新建文件夹`
                    : "在项目根目录新建文件夹",
                );
                if (name?.trim()) createFolder.mutate(name.trim());
              }}
              variant="outline"
            >
              <FolderPlus aria-hidden="true" className="size-4" />
              新建文件夹
            </Button>
          ) : null}
          {selectedFolder && !trashView ? (
            <>
              <Button
                disabled={renameFolder.isPending}
                onClick={() => {
                  const name = window.prompt(
                    "重命名文件夹",
                    selectedFolder.name,
                  );
                  if (name?.trim() && name.trim() !== selectedFolder.name)
                    renameFolder.mutate(name.trim());
                }}
                variant="outline"
              >
                重命名
              </Button>
              <select
                aria-label="移动当前文件夹"
                className="h-9 rounded-md border border-input bg-background px-3 text-sm"
                defaultValue={selectedFolder.parent_folder_id ?? "root"}
                disabled={moveFolder.isPending}
                onChange={(event) =>
                  moveFolder.mutate({
                    folderId: selectedFolder.folder_id,
                    parentFolderId:
                      event.target.value === "root" ? null : event.target.value,
                  })
                }
              >
                <option value="root">移动到根目录</option>
                {folderOptions
                  .filter(
                    ({ folder: option }) =>
                      !excludedFolderTargets.has(option.folder_id),
                  )
                  .map(({ depth, folder: option }) => (
                    <option key={option.folder_id} value={option.folder_id}>
                      {"　".repeat(depth)}
                      {option.name}
                    </option>
                  ))}
              </select>
              <ArtifactFolderDeleteActions
                folderName={selectedFolder.name}
                onDelete={(recursive) => deleteFolder.mutate(recursive)}
                pending={deleteFolder.isPending}
              />
            </>
          ) : null}
          <Button
            onClick={() => {
              setDetailId(undefined);
              setTrashView((value) => !value);
            }}
            variant="outline"
          >
            {trashView ? (
              <ArchiveRestore aria-hidden="true" className="size-4" />
            ) : (
              <Trash2 aria-hidden="true" className="size-4" />
            )}
            {trashView ? "返回文件库" : "回收站"}
          </Button>
          {canUpload && !trashView ? (
            <Button onClick={() => setUploaderOpen(true)}>
              <UploadCloud aria-hidden="true" className="size-4" />
              上传文件
            </Button>
          ) : null}
        </div>
      </header>

      {!trashView ? (
        <ArtifactFolderBrowser
          canManage={canUpload}
          currentFolderId={folder}
          folders={folderTree.data?.items ?? []}
          onMoveArtifact={(artifactId, folderId) =>
            moveArtifact.mutate({ artifactId, folderId })
          }
          onMoveFolder={(folderId, parentFolderId) =>
            moveFolder.mutate({ folderId, parentFolderId })
          }
          onNavigate={setFolder}
        />
      ) : null}

      {setupMode ? (
        <Card className="border-primary/30 bg-primary/5">
          <CardHeader className="pb-3">
            <div className="flex flex-wrap items-start gap-4">
              <span className="flex size-9 items-center justify-center rounded-lg bg-primary text-primary-foreground">
                <Plus aria-hidden="true" className="size-4" />
              </span>
              <div className="min-w-0 flex-1">
                <CardTitle>完成项目题目文件设置</CardTitle>
                <p className="mt-1 text-sm text-muted-foreground">
                  项目已创建。请上传题目文件并选择要写入 source_artifact_ids[]
                  的可用 Artifact。
                </p>
              </div>
              <Button
                disabled={saveSources.isPending}
                onClick={() => saveSources.mutate()}
              >
                <Check aria-hidden="true" className="size-4" />
                保存并返回首页
              </Button>
            </div>
          </CardHeader>
          {unavailableSelected.length > 0 ? (
            <CardContent className="grid gap-2 border-t border-primary/15 pt-4">
              <p className="text-sm font-medium">
                以下已绑定文件当前不在可用文件列表中
              </p>
              <p className="text-xs text-muted-foreground">
                它们可能已进入回收站。移除后保存即可解除项目绑定。
              </p>
              <div className="flex flex-wrap gap-2">
                {unavailableSelected.map((artifactId) => (
                  <Button
                    key={artifactId}
                    onClick={() =>
                      setSelected((current) =>
                        current.filter((id) => id !== artifactId),
                      )
                    }
                    size="sm"
                    variant="outline"
                  >
                    移除 {artifactId}
                    <X aria-hidden="true" className="size-3.5" />
                  </Button>
                ))}
              </div>
            </CardContent>
          ) : null}
        </Card>
      ) : null}

      <ArtifactFilters
        filters={filters}
        onChange={setFilters}
        trashView={trashView}
      />

      <section aria-label={trashView ? "回收站文件" : "项目文件"}>
        {artifacts.isLoading ? (
          <p className="text-sm text-muted-foreground">正在读取文件库…</p>
        ) : null}
        {artifacts.error ? (
          <p className="text-sm text-destructive">{artifacts.error.message}</p>
        ) : null}
        {!artifacts.isLoading && items.length === 0 ? (
          <EmptyState
            description={
              trashView
                ? "回收站为空。普通删除的文件会保留全部版本并出现在这里。"
                : "上传第一份文件，或调整筛选条件。"
            }
            title={trashView ? "回收站为空" : "还没有匹配的文件"}
          />
        ) : null}
        <ArtifactSelector
          folders={trashView ? undefined : (folderTree.data?.items ?? [])}
          onAssignFolder={(artifactId, folderId) =>
            moveArtifact.mutate({ artifactId, folderId })
          }
          items={items}
          onOpen={(item) => setDetailId(item.artifact.artifact_id)}
          onSelectedChange={setSelected}
          selected={selected}
          selectionMode={setupMode && !trashView}
        />
        {artifacts.hasNextPage ? (
          <div className="mt-5 flex justify-center">
            <Button
              disabled={artifacts.isFetchingNextPage}
              onClick={() => void artifacts.fetchNextPage()}
              variant="outline"
            >
              {artifacts.isFetchingNextPage ? "正在加载…" : "加载更多"}
            </Button>
          </div>
        ) : null}
      </section>

      <ArtifactUploader
        defaultKind={setupMode ? "problem" : "attachment"}
        folderId={folder}
        onClose={() => setUploaderOpen(false)}
        onComplete={() => {
          void queryClient.invalidateQueries({
            queryKey: ["artifacts", project.id],
          });
        }}
        open={uploaderOpen}
        projectId={project.id}
      />
      <ArtifactDetailDrawer
        artifactId={detailId}
        initialDetail={items.find(
          (item) => item.artifact.artifact_id === detailId,
        )}
        onClose={() => setDetailId(undefined)}
        projectId={project.id}
        trashView={trashView}
      />
    </div>
  );
}

export function ArtifactSelector({
  folders,
  onAssignFolder,
  items,
  onOpen,
  onSelectedChange,
  selected,
  selectionMode,
}: Readonly<{
  folders?: ArtifactFolder[];
  onAssignFolder?: (artifactId: string, folderId: string | null) => void;
  items: ArtifactDetail[];
  onOpen: (item: ArtifactDetail) => void;
  onSelectedChange: (ids: string[]) => void;
  selected: string[];
  selectionMode: boolean;
}>) {
  return (
    <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
      {items.map((item) => {
        const version = item.current_version;
        const checked = selected.includes(item.artifact.artifact_id);
        const selectable =
          item.artifact.status === "available" &&
          version?.status === "available";
        return (
          <Card
            className={cn(
              "group cursor-pointer transition hover:border-primary/40 hover:shadow-card-hover",
              checked ? "border-primary ring-1 ring-primary/30" : null,
            )}
            key={item.artifact.artifact_id}
            draggable={Boolean(folders && onAssignFolder)}
            onClick={() => onOpen(item)}
            onDragStart={(event) => {
              if (!folders || !onAssignFolder) return;
              event.dataTransfer.effectAllowed = "move";
              event.dataTransfer.setData(
                artifactMoveMime,
                JSON.stringify({ artifactId: item.artifact.artifact_id }),
              );
            }}
          >
            <CardHeader className="pb-3">
              <div className="flex items-start gap-3">
                {selectionMode ? (
                  <input
                    aria-label={`选择 ${item.artifact.name}`}
                    checked={checked}
                    className="mt-1 size-4 accent-primary"
                    disabled={!selectable}
                    onChange={(event) => {
                      event.stopPropagation();
                      onSelectedChange(
                        event.target.checked
                          ? [
                              ...new Set([
                                ...selected,
                                item.artifact.artifact_id,
                              ]),
                            ]
                          : selected.filter(
                              (id) => id !== item.artifact.artifact_id,
                            ),
                      );
                    }}
                    onClick={(event) => event.stopPropagation()}
                    type="checkbox"
                  />
                ) : null}
                <span className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                  <ArtifactIcon mimeType={version?.mime_type} />
                </span>
                <div className="min-w-0 flex-1">
                  <CardTitle className="truncate text-base">
                    {item.artifact.name}
                  </CardTitle>
                  <p className="mt-1 truncate text-xs text-muted-foreground">
                    {version?.filename ?? "等待首个版本"}
                  </p>
                </div>
                <Badge>{item.artifact.kind}</Badge>
              </div>
            </CardHeader>
            <CardContent className="grid gap-3">
              <div className="flex items-center justify-between text-xs text-muted-foreground">
                <span>{item.artifact.source}</span>
                <span>{version ? formatBytes(version.size_bytes) : "—"}</span>
              </div>
              <div className="flex min-h-6 flex-wrap gap-1.5">
                {item.artifact.tags.slice(0, 4).map((tag) => (
                  <Badge key={tag}>{tag}</Badge>
                ))}
                {item.artifact.tags.length > 4 ? (
                  <Badge>+{item.artifact.tags.length - 4}</Badge>
                ) : null}
              </div>
              <div className="flex items-center justify-between border-t border-border pt-3">
                <span className="text-xs text-muted-foreground">
                  {item.artifact.status}
                  {version ? ` · v${version.version_no}` : ""}
                </span>
                {version?.status === "available" ? (
                  <Button
                    aria-label={`下载 ${item.artifact.name}`}
                    onClick={async (event) => {
                      event.stopPropagation();
                      try {
                        const grant = await artifactApi.download(
                          item.artifact.project_id,
                          item.artifact.artifact_id,
                        );
                        window.location.assign(grant.transfer.url);
                      } catch (error) {
                        toast.error(
                          error instanceof Error
                            ? error.message
                            : "下载授权失败",
                        );
                      }
                    }}
                    size="icon"
                    variant="ghost"
                  >
                    <Download aria-hidden="true" className="size-4" />
                  </Button>
                ) : null}
              </div>
              {folders && onAssignFolder ? (
                <p className="text-[11px] text-muted-foreground">
                  拖到上方文件夹或路径即可整理
                </p>
              ) : null}
            </CardContent>
          </Card>
        );
      })}
    </div>
  );
}

function ArtifactFilters({
  filters,
  onChange,
  trashView,
}: Readonly<{
  filters: ArtifactListFilters;
  onChange: (filters: ArtifactListFilters) => void;
  trashView: boolean;
}>) {
  const hasActiveFilters = Boolean(
    filters.tag ||
    filters.kind ||
    (!trashView && (filters.source || filters.status)),
  );

  return (
    <div className="flex flex-wrap items-center gap-2">
      <div className="relative min-w-[200px] flex-1 max-w-sm">
        <Search
          aria-hidden="true"
          className="pointer-events-none absolute left-3 top-2.5 size-4 text-muted-foreground"
        />
        <Input
          aria-label="按标签筛选"
          className="h-9 pl-9 bg-background border-border/80"
          onChange={(event) =>
            onChange({ ...filters, tag: event.target.value || undefined })
          }
          placeholder="精确标签筛选"
          value={filters.tag ?? ""}
        />
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <FilterSelect
          label="类型"
          onChange={(value) =>
            onChange({
              ...filters,
              kind: (value || undefined) as ArtifactKind | undefined,
            })
          }
          options={[
            ["problem", "题目"],
            ["attachment", "附件"],
            ["experiment_result", "实验结果"],
            ["model_file", "模型文件"],
            ["article_build", "论文构建"],
            ["agent", "Agent 产物"],
            ["other", "其他"],
          ]}
          value={filters.kind ?? ""}
        />
        {!trashView ? (
          <FilterSelect
            label="来源"
            onChange={(value) =>
              onChange({
                ...filters,
                source: (value || undefined) as ArtifactSource | undefined,
              })
            }
            options={[
              ["user_upload", "用户上传"],
              ["experiment", "实验"],
              ["model", "模型"],
              ["article", "论文"],
              ["agent", "Agent"],
              ["system", "系统"],
            ]}
            value={filters.source ?? ""}
          />
        ) : null}
        {!trashView ? (
          <FilterSelect
            label="状态"
            onChange={(value) =>
              onChange({
                ...filters,
                status: (value || undefined) as ArtifactStatus | undefined,
              })
            }
            options={[
              ["available", "可用"],
              ["pending_upload", "上传中"],
            ]}
            value={filters.status ?? ""}
          />
        ) : null}
        {hasActiveFilters && (
          <Button
            onClick={() => onChange({ limit: 50 })}
            variant="ghost"
            size="sm"
            className="h-9 px-3 text-xs text-muted-foreground hover:text-foreground"
          >
            <FilterX aria-hidden="true" className="mr-1.5 size-3.5" />
            清除筛选
          </Button>
        )}
      </div>
    </div>
  );
}

function FilterSelect({
  label,
  onChange,
  options,
  value,
}: Readonly<{
  label: string;
  onChange: (value: string) => void;
  options: [string, string][];
  value: string;
}>) {
  return (
    <select
      aria-label={label}
      className="h-9 min-w-[110px] rounded-md border border-input bg-background px-3 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
      onChange={(event) => onChange(event.target.value)}
      value={value}
    >
      <option value="">全部{label}</option>
      {options.map(([optionValue, optionLabel]) => (
        <option key={optionValue} value={optionValue}>
          {optionLabel}
        </option>
      ))}
    </select>
  );
}

function ArtifactIcon({ mimeType }: Readonly<{ mimeType?: string }>) {
  if (mimeType === "application/pdf" || mimeType?.startsWith("text/")) {
    return <FileText aria-hidden="true" className="size-5" />;
  }
  if (mimeType?.includes("zip") || mimeType?.includes("tar")) {
    return <FileArchive aria-hidden="true" className="size-5" />;
  }
  return <File aria-hidden="true" className="size-5" />;
}
