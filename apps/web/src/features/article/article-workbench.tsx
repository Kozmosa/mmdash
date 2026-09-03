"use client";

import { HocuspocusProvider, WebSocketStatus } from "@hocuspocus/provider";
import {
  useInfiniteQuery,
  type InfiniteData,
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import { gunzipSync, strFromU8, unzipSync } from "fflate";
import {
  Boxes,
  CheckCircle2,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  CircleAlert,
  CircleX,
  Clock3,
  Copy,
  Download,
  FileArchive,
  FileCode2,
  FilePenLine,
  FileUp,
  FileText,
  Folder,
  FolderOpen,
  FolderPlus,
  Library,
  ListTree,
  LoaderCircle,
  RefreshCw,
  Users,
} from "lucide-react";
import Image from "next/image";
import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type DragEvent,
} from "react";
import * as Y from "yjs";
import { toast } from "sonner";

import { useCurrentProject } from "@/components/providers/project-provider";
import { useCurrentUser } from "@/components/providers/user-provider";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { PDFReader } from "@/components/ui/pdf-reader";
import { cn } from "@/lib/cn";
import { artifactApi } from "@/features/artifact/artifact-api";
import { ArtifactFolderDeleteActions } from "@/features/artifact/artifact-folder-delete-actions";
import {
  ArtifactFolderBrowser,
  artifactMoveMime,
} from "@/features/artifact/artifact-folder-browser";
import { findArtifactFolder } from "@/features/artifact/artifact-folders";
import { MultipartUploadTask } from "@/features/artifact/multipart-upload";
import type { ArtifactDetail, ArtifactPage } from "@/features/artifact/types";
import type { ProjectPermissions } from "@/features/repo/types";
import { apiClient } from "@/lib/api-client";
import { ApiError } from "@/lib/api-client";

import { articleApi } from "./api";
import { registerArticleCollaborationProvider } from "./article-collaboration-sync";
import { ArticleAggregateWarnings } from "./article-aggregate-warnings";
import { ArticleReferencePanel } from "./article-reference-panel";
import { visibleArticleOutline } from "./article-outline";
import { availableThumbnailURL } from "./article-image-utils";
import {
  articleEditorMinWidth,
  articleOutlineDefaultHeight,
  articleSidebarDefaultRatio,
  articleSidebarMinWidth,
  clampArticleOutlineHeight,
  clampArticleSidebarRatio,
} from "./article-layout";
import { ArticleOutlineResizeHandle } from "./article-outline-resize-handle";
import { ArticleSidebarResizeHandle } from "./article-sidebar-resize-handle";
import {
  copiedTemplateManifest,
  copyArticleTemplateToArtifact,
} from "./article-template-copy";
import {
  ArticleEditor,
  articleOutlineActiveEvent,
  articleOutlineNavigateEvent,
  articleArtifactMime,
  articleZoteroMime,
  type ArticleArtifactDrop,
  type ArticleOutlineItem,
  type ArticleZoteroDrop,
} from "./article-editor";
import { convertOverleafZip, inspectOverleafZip } from "./overleaf-import";
import {
  forwardSyncPoint,
  parseSyncTex,
  reverseSyncPoint,
  type SyncTexPoint,
} from "./synctex";
import type {
  ArticleAggregate,
  ArticleBlock,
  ArticleBuild,
  ArticleBuildOutput,
  ArticleCommitOperation,
  ArticleDraft,
  ArticleRelease,
  ArticleTemplateManifest,
  ZoteroCollection,
  ZoteroItem,
} from "./types";
import {
  openArticleSidebarEvent,
  openArtifactLibraryEvent,
} from "./slash-command";

type WorkspaceTab = "write" | "history" | "templates";
type ConnectionState =
  WebSocketStatus | "offline" | "syncing" | "synced" | "failed";
type PresenceUser = { clientId: number; color: string; name: string };

const templateDefaults: ArticleTemplateManifest = {
  bibliography_target: "references.bib",
  bibliography_tool: "auto",
  content_target: "manuscript.tex",
  engine: "auto",
  entrypoint: "main.tex",
  name: "论文模板",
  output: "main.pdf",
  schema_version: "1.0",
  version: "1.0.0",
};

export function ArticleWorkbench() {
  const project = useCurrentProject();
  const currentUser = useCurrentUser();
  const queryClient = useQueryClient();
  const [tab, setTab] = useState<WorkspaceTab>("write");
  const [provider, setProvider] = useState<HocuspocusProvider>();
  const [connection, setConnection] = useState<ConnectionState>(
    WebSocketStatus.Connecting,
  );
  const [synced, setSynced] = useState(false);
  const [unsyncedChanges, setUnsyncedChanges] = useState(0);
  const [presence, setPresence] = useState<PresenceUser[]>([]);
  const [error, setError] = useState<string>();

  const aggregate = useQuery({
    queryFn: () => articleApi.aggregate(project.id),
    queryKey: ["article", project.id],
    refetchInterval: (query) => {
      const data = query.state.data;
      const hasActiveOperation = data?.commit_operations?.some(
        (operation) =>
          operation.status === "queued" ||
          operation.status === "running" ||
          operation.status === "retry_wait",
      );
      const hasActiveBuild = data?.builds.some((build) =>
        ["queued", "running"].includes(build.status),
      );
      return hasActiveOperation || hasActiveBuild ? 2_000 : 15_000;
    },
  });
  const permissions = useQuery({
    queryFn: () =>
      apiClient.request<ProjectPermissions>(
        `/projects/${encodeURIComponent(project.id)}/permissions`,
      ),
    queryKey: ["project-permissions", project.id],
  });
  const canEdit =
    permissions.data?.permissions.includes("project.article.edit") ?? false;
  const canBuild =
    permissions.data?.permissions.includes("project.article.build") ?? false;
  const canRelease =
    permissions.data?.permissions.includes("project.article.release") ?? false;
  const canManageTemplate =
    permissions.data?.permissions.includes("project.article.template.manage") ??
    false;
  const collaborator = useMemo(
    () => ({
      color: colorFor(
        `${project.id}:${provider?.document.clientID ?? "pending"}`,
      ),
      name: currentUser?.displayName || currentUser?.email || "当前用户",
    }),
    [currentUser?.displayName, currentUser?.email, project.id, provider],
  );

  useEffect(() => {
    const document = new Y.Doc();
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const next = new HocuspocusProvider({
      document,
      flushDelay: 250,
      name: `article:${project.id}`,
      onAuthenticationFailed: ({ reason }) => {
        setConnection("failed");
        setError(`协同认证失败：${reason}`);
      },
      onAwarenessChange: ({ states }) => {
        setPresence(
          states.map((state) => ({
            clientId: state.clientId,
            color:
              typeof state.user?.color === "string"
                ? state.user.color
                : "#64748b",
            name:
              typeof state.user?.name === "string" ? state.user.name : "协作者",
          })),
        );
      },
      onStateless: ({ payload }) => {
        const block = articleBlockFromCollaborationEvent(payload);
        if (block) updateArticleBlockCache(queryClient, project.id, block);
      },
      onStatus: ({ status }) => setConnection(status),
      onSynced: ({ state }) => {
        setSynced(state);
        if (state) setConnection("synced");
      },
      onUnsyncedChanges: ({ number }) => {
        setUnsyncedChanges(number);
        if (number > 0) setConnection(navigator.onLine ? "syncing" : "offline");
      },
      sessionAwareness: true,
      token: "browser-session",
      url: `${protocol}//${window.location.host}/api/projects/${encodeURIComponent(project.id)}/article/collaboration`,
    });
    const unregisterCollaborationProvider =
      registerArticleCollaborationProvider(project.id, next);
    const offline = () => setConnection("offline");
    const online = () => {
      setConnection(WebSocketStatus.Connecting);
      next.connect();
    };
    window.addEventListener("offline", offline);
    window.addEventListener("online", online);
    setProvider(next);
    return () => {
      unregisterCollaborationProvider();
      window.removeEventListener("offline", offline);
      window.removeEventListener("online", online);
      next.destroy();
      document.destroy();
      setProvider(undefined);
    };
  }, [project.id, queryClient]);

  const refresh = useCallback(async () => {
    await queryClient.invalidateQueries({ queryKey: ["article", project.id] });
  }, [project.id, queryClient]);

  const flush = useMutation({
    mutationFn: () => articleApi.flush(project.id),
    onError: (value) => setError(value.message),
    onSuccess: async () => {
      setUnsyncedChanges(0);
      setConnection("synced");
      await refresh();
    },
  });
  const forceFlush = useCallback(() => flush.mutate(), [flush]);

  if (aggregate.isPending || permissions.isPending) {
    return <LoadingState label="正在装入论文工作区…" />;
  }
  if (aggregate.error || !aggregate.data) {
    return (
      <ErrorState
        message={
          aggregate.error
            ? articleActionMessage(aggregate.error)
            : "论文工作区不可用"
        }
      />
    );
  }

  const data = aggregate.data;
  return (
    <section
      className="flex h-full min-h-0 flex-1 flex-col gap-3"
      aria-labelledby="article-title"
    >
      <header className="flex shrink-0 flex-wrap items-start gap-4">
        <div className="flex size-10 items-center justify-center rounded-lg border bg-card shadow-xs">
          <FilePenLine className="size-5" />
        </div>
        <div className="min-w-0 flex-1">
          <h1 className="text-2xl font-semibold" id="article-title">
            论文写作
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            协同草稿、固定引用、可复现 LaTeX 构建与不可变 Release。
          </p>
        </div>
        <SyncBadge connection={connection} pending={unsyncedChanges} />
        <div className="flex items-center gap-1 text-xs text-muted-foreground">
          <Users className="size-4" />
          {presence.length || 1} 人在线
        </div>
      </header>

      <nav
        aria-label="论文工作区"
        className="flex shrink-0 flex-wrap gap-2 border-b pb-2"
      >
        {(
          [
            ["write", "写作"],
            ["history", "版本历史"],
            ["templates", "模板"],
          ] as const
        ).map(([value, label]) => (
          <Button
            key={value}
            onClick={() => setTab(value)}
            size="sm"
            variant={tab === value ? "default" : "ghost"}
          >
            {label}
          </Button>
        ))}
      </nav>

      {error ? (
        <div className="shrink-0 flex items-center gap-2 rounded-lg border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">
          <CircleAlert className="size-4" />
          {error}
        </div>
      ) : null}
      {data?.warnings?.length ? (
        <div className="shrink-0">
          <ArticleAggregateWarnings warnings={data.warnings} />
        </div>
      ) : null}
      {tab === "write" ? (
        <WritingWorkspace
          canEdit={canEdit}
          canBuild={canBuild}
          canRelease={canRelease}
          collaborator={collaborator}
          data={data}
          onFlush={forceFlush}
          onOpenHistory={() => setTab("history")}
          onRefresh={refresh}
          onOpenTemplates={() => setTab("templates")}
          provider={provider}
          synced={synced}
        />
      ) : null}
      {tab === "history" ? (
        <VersionHistoryWorkspace
          canBuild={canBuild}
          canRelease={canRelease}
          data={data}
          onRefresh={refresh}
          projectId={project.id}
        />
      ) : null}
      {tab === "templates" ? (
        <TemplateWorkspace
          canManage={canManageTemplate}
          data={data}
          onRefresh={refresh}
          projectId={project.id}
        />
      ) : null}
    </section>
  );
}

export function WritingWorkspace({
  canBuild,
  canEdit,
  canRelease,
  collaborator,
  data,
  onFlush,
  onOpenTemplates,
  onRefresh,
  provider,
  synced,
}: Readonly<{
  canBuild: boolean;
  canEdit: boolean;
  canRelease: boolean;
  collaborator: { color: string; name: string };
  data: ArticleAggregate;
  onFlush: () => void;
  onOpenHistory: () => void;
  onOpenTemplates: () => void;
  onRefresh: () => Promise<void>;
  provider?: HocuspocusProvider;
  synced: boolean;
}>) {
  const queryClient = useQueryClient();
  const [panel, setPanel] = useState<
    "reference" | "artifact" | "zotero" | "pdf"
  >("reference");
  const [collapsed, setCollapsed] = useState(false);
  const [sidebarRatio, setSidebarRatio] = useState(articleSidebarDefaultRatio);
  const [outline, setOutline] = useState<ArticleOutlineItem[]>([]);
  const [activeOutlineId, setActiveOutlineId] = useState("");
  const [collapsedOutlineIds, setCollapsedOutlineIds] = useState<Set<string>>(
    () => new Set(),
  );
  const [referenceKind, setReferenceKind] = useState<
    "experiment_result" | "model_snapshot" | "problem"
  >("model_snapshot");
  const [commitOpen, setCommitOpen] = useState(false);
  const [immersive, setImmersive] = useState(false);
  const [outlineHeight, setOutlineHeight] = useState(
    articleOutlineDefaultHeight,
  );
  const projectId = data.draft.project_id;
  const ratioPreferenceKey = `mmdash-article-sidebar-ratio:${projectId}`;
  const outlineHeightPreferenceKey = `mmdash-article-outline-height:${projectId}`;

  useEffect(() => {
    if (!immersive) return;
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setImmersive(false);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [immersive]);
  useEffect(() => {
    const rawRatio = window.localStorage.getItem(ratioPreferenceKey);
    if (rawRatio !== null && rawRatio !== "") {
      const parsed = Number(rawRatio);
      if (Number.isFinite(parsed)) {
        const normalized = parsed > 1 ? parsed / 1280 : parsed;
        setSidebarRatio(clampArticleSidebarRatio(normalized));
      }
    }
    const rawHeight = window.localStorage.getItem(outlineHeightPreferenceKey);
    if (rawHeight !== null && rawHeight !== "") {
      const savedHeight = Number(rawHeight);
      if (Number.isFinite(savedHeight))
        setOutlineHeight(clampArticleOutlineHeight(savedHeight));
    }
  }, [outlineHeightPreferenceKey, ratioPreferenceKey]);
  useEffect(() => {
    const openArtifact = () => {
      setCollapsed(false);
      setPanel("artifact");
    };
    const openSidebar = (event: Event) => {
      const detail = (
        event as CustomEvent<{
          panel?: "reference" | "artifact" | "zotero" | "pdf";
          referenceKind?: "experiment_result" | "model_snapshot" | "problem";
        }>
      ).detail;
      if (!detail?.panel) return;
      setCollapsed(false);
      setPanel(detail.panel);
      if (detail.referenceKind) setReferenceKind(detail.referenceKind);
    };
    window.addEventListener(openArtifactLibraryEvent, openArtifact);
    window.addEventListener(openArticleSidebarEvent, openSidebar);
    return () => {
      window.removeEventListener(openArtifactLibraryEvent, openArtifact);
      window.removeEventListener(openArticleSidebarEvent, openSidebar);
    };
  }, []);
  useEffect(() => {
    const activate = (event: Event) => {
      const id = (event as CustomEvent<{ id?: string }>).detail?.id;
      if (id) setActiveOutlineId(id);
    };
    window.addEventListener(articleOutlineActiveEvent, activate);
    return () =>
      window.removeEventListener(articleOutlineActiveEvent, activate);
  }, []);
  const visibleOutline = visibleArticleOutline(outline, collapsedOutlineIds);
  const persistSidebarRatio = (next: number) => {
    const normalized = clampArticleSidebarRatio(next);
    setSidebarRatio(normalized);
    window.localStorage.setItem(ratioPreferenceKey, String(normalized));
  };
  const persistOutlineHeight = (next: number) => {
    const normalized = clampArticleOutlineHeight(next);
    setOutlineHeight(normalized);
    window.localStorage.setItem(outlineHeightPreferenceKey, String(normalized));
  };
  const insertArtifact = useCallback(
    async (artifact: ArticleArtifactDrop) => {
      const reference = await articleApi.addReference(projectId, {
        metadata: { filename: artifact.filename, mime_type: artifact.mimeType },
        reference_type: "artifact",
        source_object_id: artifact.artifactId,
        source_version_id: artifact.versionId,
        title: artifact.title,
      });
      await onRefresh();
      return reference;
    },
    [onRefresh, projectId],
  );
  const reviewBlock = useCallback(
    async (blockId: string, contentFingerprint: string) => {
      let reviewed: ArticleBlock;
      try {
        reviewed = await articleApi.reviewBlock(
          projectId,
          blockId,
          contentFingerprint,
        );
      } catch (error) {
        if (
          !(error instanceof ApiError) ||
          error.code !== "ARTICLE_BLOCK_CHANGED"
        ) {
          throw error;
        }
        const draft = await articleApi.flush(projectId);
        updateArticleDraftCache(queryClient, projectId, draft);
        const refreshedBlock = draft.blocks.find(
          (block) => block.block_id === blockId,
        );
        if (!refreshedBlock?.content_fingerprint) {
          throw new Error("同步后找不到该块，请重新选择后审阅");
        }
        reviewed = await articleApi.reviewBlock(
          projectId,
          blockId,
          refreshedBlock.content_fingerprint,
        );
      }
      updateArticleBlockCache(queryClient, projectId, reviewed);
    },
    [projectId, queryClient],
  );
  const reviewChapter = useCallback(
    async (chapterTagId: string) => {
      await articleApi.flush(projectId);
      await articleApi.reviewChapter(projectId, chapterTagId);
      await onRefresh();
    },
    [onRefresh, projectId],
  );
  const insertZotero = useCallback(
    async (item: ArticleZoteroDrop) => {
      const existing = data.references.find(
        (reference) =>
          reference.reference_type === "zotero" &&
          reference.source_object_id === item.itemKey &&
          reference.source_version_id === String(item.version),
      );
      if (existing) return existing;
      const reference = await articleApi.addReference(projectId, {
        citation_key: item.citationKey,
        metadata: item.raw,
        reference_type: "zotero",
        source_object_id: item.itemKey,
        source_version_id: String(item.version),
        title: item.title,
      });
      await onRefresh();
      return reference;
    },
    [data.references, onRefresh, projectId],
  );
  return (
    <>
      <div
        className={cn(
          "flex min-h-0 flex-1 gap-3",
          immersive
            ? "fixed inset-0 z-50 h-screen w-screen overflow-hidden bg-background p-2.5 md:p-3"
            : "h-full w-full",
        )}
        data-article-workbench-container
      >
        <aside
          className={cn(
            "relative flex h-full min-h-0 shrink-0 flex-col overflow-hidden rounded-lg border bg-card",
            collapsed ? "w-12" : "",
          )}
          style={
            collapsed
              ? undefined
              : {
                  maxWidth: `calc(100% - ${articleEditorMinWidth}px)`,
                  minWidth: articleSidebarMinWidth,
                  width: `${sidebarRatio * 100}%`,
                }
          }
        >
          <div className="flex shrink-0 items-center border-b p-2 gap-1">
            {!collapsed ? (
              <div
                className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
                onWheel={(event) => {
                  if (event.deltaY !== 0) {
                    event.currentTarget.scrollLeft += event.deltaY;
                  }
                }}
              >
                {(
                  [
                    ["reference", "参考"],
                    ["artifact", "Artifact"],
                    ["zotero", "Zotero"],
                    ["pdf", "PDF"],
                  ] as const
                ).map(([value, label]) => (
                  <Button
                    className="shrink-0"
                    key={value}
                    onClick={() => setPanel(value)}
                    size="sm"
                    variant={panel === value ? "secondary" : "ghost"}
                  >
                    {label}
                  </Button>
                ))}
              </div>
            ) : null}
            <Button
              aria-label={collapsed ? "展开左栏" : "折叠左栏"}
              className={cn("shrink-0", collapsed ? "mx-auto" : "ml-auto")}
              onClick={() => setCollapsed((value) => !value)}
              size={collapsed ? "icon" : "sm"}
              variant="ghost"
            >
              {collapsed ? (
                <ChevronRight className="size-4" />
              ) : (
                <ChevronLeft className="size-4" />
              )}
            </Button>
          </div>
          {collapsed ? (
            <Button
              aria-label="展开论文目录"
              className="mx-auto mt-auto mb-2"
              onClick={() => setCollapsed(false)}
              size="icon"
              title="论文目录"
              variant="ghost"
            >
              <ListTree className="size-4" />
            </Button>
          ) : null}
          {!collapsed ? (
            <>
              <div
                className={cn(
                  "min-h-0 flex-1 overflow-auto p-3",
                  ["pdf", "reference"].includes(panel) &&
                    "flex flex-col overflow-hidden",
                )}
              >
                {panel === "reference" ? (
                  <ArticleReferencePanel
                    canEdit={canEdit}
                    data={data}
                    initialKind={referenceKind}
                    onRefresh={onRefresh}
                  />
                ) : null}
                {panel === "artifact" ? (
                  <ArtifactPanel canEdit={canEdit} projectId={projectId} />
                ) : null}
                {panel === "zotero" ? (
                  <WritingZoteroPanel
                    canEdit={canEdit}
                    data={data}
                    onRefresh={onRefresh}
                    projectId={projectId}
                  />
                ) : null}
                {panel === "pdf" ? (
                  <WritingPDFPanel
                    canBuild={canBuild}
                    data={data}
                    onRefresh={onRefresh}
                    projectId={projectId}
                  />
                ) : null}
              </div>
              <ArticleOutlineResizeHandle
                height={outlineHeight}
                onResize={setOutlineHeight}
                onResizeEnd={persistOutlineHeight}
              />
              <nav
                aria-label="论文目录"
                className="shrink-0 min-h-0 overflow-auto p-3"
                style={{ height: outlineHeight }}
              >
                <p className="mb-2 text-xs font-medium">目录</p>
                {outline.length ? (
                  <div className="grid gap-0.5">
                    {visibleOutline.map((item) => {
                      const itemIndex = outline.indexOf(item);
                      const hasChildren =
                        (outline[itemIndex + 1]?.level ?? 0) > item.level;
                      const folded = collapsedOutlineIds.has(item.id);
                      return (
                        <div
                          className="flex min-w-0 items-center"
                          key={item.id}
                        >
                          <button
                            aria-label={folded ? "展开章节" : "折叠章节"}
                            className={`flex size-6 shrink-0 items-center justify-center rounded hover:bg-muted ${hasChildren ? "visible" : "invisible"}`}
                            onClick={() =>
                              setCollapsedOutlineIds((current) => {
                                const next = new Set(current);
                                if (next.has(item.id)) next.delete(item.id);
                                else next.add(item.id);
                                return next;
                              })
                            }
                            style={{
                              marginLeft: `${Math.max(0, item.level - 1) * 0.65}rem`,
                            }}
                            type="button"
                          >
                            {folded ? (
                              <ChevronRight className="size-3" />
                            ) : (
                              <ChevronDown className="size-3" />
                            )}
                          </button>
                          <button
                            aria-current={
                              activeOutlineId === item.id
                                ? "location"
                                : undefined
                            }
                            className="min-w-0 flex-1 truncate rounded px-1 py-1 text-left text-xs text-muted-foreground hover:bg-muted hover:text-foreground aria-[current=location]:bg-primary/10 aria-[current=location]:font-medium aria-[current=location]:text-foreground"
                            onClick={() =>
                              window.dispatchEvent(
                                new CustomEvent(articleOutlineNavigateEvent, {
                                  detail: { id: item.id },
                                }),
                              )
                            }
                            title={item.text}
                            type="button"
                          >
                            {item.text}
                          </button>
                        </div>
                      );
                    })}
                  </div>
                ) : (
                  <p className="text-xs text-muted-foreground">
                    添加标题后将在此生成目录
                  </p>
                )}
              </nav>
            </>
          ) : null}
          {!collapsed ? (
            <ArticleSidebarResizeHandle
              onResize={setSidebarRatio}
              onResizeEnd={persistSidebarRatio}
              ratio={sidebarRatio}
            />
          ) : null}
        </aside>
        <main
          className="flex h-full min-h-0 min-w-0 flex-1 flex-col gap-2.5"
          style={{ minWidth: articleEditorMinWidth }}
        >
          {!immersive ? (
            <div className="flex shrink-0 flex-wrap items-center gap-2 rounded-lg border bg-card px-3 py-2">
              <Badge>草稿 r{data.draft.draft_revision}</Badge>
              <span className="text-xs text-muted-foreground">
                {data.unreviewed_blocks} 个未审阅块 · 完成度{" "}
                {Math.round(data.section_completion * 100)}%
              </span>
              <Button
                className="ml-auto"
                disabled={!canEdit || !synced}
                onClick={() => setCommitOpen(true)}
                size="sm"
              >
                Commit…
              </Button>
            </div>
          ) : null}
          {provider && synced ? (
            <div className="flex min-h-0 flex-1 flex-col">
              <ArticleEditor
                blocks={data.draft.blocks}
                canCommit={canEdit && synced}
                canEdit={canEdit}
                chapterTags={data.chapter_tags}
                collaborator={collaborator}
                draftRevision={data.draft.draft_revision}
                immersive={immersive}
                onFlush={onFlush}
                onInsertArtifact={insertArtifact}
                onInsertZotero={insertZotero}
                onOpenCommit={() => setCommitOpen(true)}
                onOutlineChange={setOutline}
                onReviewBlock={reviewBlock}
                onReviewChapter={reviewChapter}
                onToggleImmersive={() => setImmersive((value) => !value)}
                projectId={projectId}
                provider={provider}
              />
            </div>
          ) : (
            <LoadingState label="正在同步协同草稿…" />
          )}
        </main>
      </div>
      {commitOpen ? (
        <CommitDialog
          canBuild={canBuild}
          canRelease={canRelease}
          data={data}
          onClose={() => setCommitOpen(false)}
          onOpenTemplates={() => {
            setCommitOpen(false);
            onOpenTemplates();
          }}
          onRefresh={onRefresh}
        />
      ) : null}
    </>
  );
}

function ArtifactPanel({
  canEdit,
  projectId,
}: Readonly<{
  canEdit: boolean;
  projectId: string;
}>) {
  const queryClient = useQueryClient();
  const [folder, setFolder] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const artifacts = useInfiniteQuery<
    ArtifactPage,
    Error,
    InfiniteData<ArtifactPage, string | undefined>,
    readonly unknown[],
    string | undefined
  >({
    getNextPageParam: (lastPage) =>
      lastPage.has_more ? (lastPage.next_cursor ?? undefined) : undefined,
    initialPageParam: undefined,
    queryFn: ({ pageParam }) =>
      artifactApi.list(projectId, {
        cursor: pageParam,
        limit: 50,
        status: "available",
      }),
    queryKey: ["article-artifacts", projectId],
  });
  const folderTree = useQuery({
    queryFn: () => artifactApi.listFolders(projectId),
    queryKey: ["artifact-folders", projectId],
  });
  const selectedFolder = findArtifactFolder(
    folderTree.data?.items ?? [],
    folder,
  );
  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({
        queryKey: ["artifact-folders", projectId],
      }),
      queryClient.invalidateQueries({
        queryKey: ["article-artifacts", projectId],
      }),
      queryClient.invalidateQueries({ queryKey: ["artifacts", projectId] }),
    ]);
  };
  const createFolder = useMutation({
    mutationFn: (name: string) =>
      artifactApi.createFolder(
        projectId,
        name,
        selectedFolder?.folder_id ?? null,
      ),
    onSuccess: async (created) => {
      await refresh();
      setFolder(created.folder_id);
    },
  });
  const moveArtifact = useMutation({
    mutationFn: ({
      artifactId,
      folderId,
    }: {
      artifactId: string;
      folderId: string | null;
    }) => artifactApi.moveArtifact(projectId, artifactId, folderId),
    onSuccess: refresh,
  });
  const renameFolder = useMutation({
    mutationFn: (name: string) =>
      artifactApi.renameFolder(projectId, selectedFolder!.folder_id, name),
    onSuccess: refresh,
  });
  const moveFolder = useMutation({
    mutationFn: ({
      folderId,
      parentFolderId,
    }: {
      folderId: string;
      parentFolderId: string | null;
    }) => artifactApi.moveFolder(projectId, folderId, parentFolderId),
    onSuccess: refresh,
  });
  const deleteFolder = useMutation({
    mutationFn: (recursive: boolean) =>
      artifactApi.deleteFolder(projectId, selectedFolder!.folder_id, recursive),
    onSuccess: async () => {
      setFolder(null);
      await refresh();
    },
  });
  const normalizedSearch = search.trim().toLocaleLowerCase();
  const visibleArtifacts = (artifacts.data?.pages ?? [])
    .flatMap((page) => page.items)
    .filter((item) => {
      const inFolder = item.artifact.folder_id === folder;
      const matchesSearch =
        !normalizedSearch ||
        item.artifact.name.toLocaleLowerCase().includes(normalizedSearch) ||
        item.current_version?.filename
          .toLocaleLowerCase()
          .includes(normalizedSearch);
      return inFolder && matchesSearch;
    });
  return (
    <div className="space-y-2">
      <p className="text-xs text-muted-foreground">
        拖入时自动固定当前不可变 Version；文件夹由项目共享。
      </p>
      <Input
        aria-label="搜索 Article Artifact"
        onChange={(event) => setSearch(event.target.value)}
        placeholder="搜索标题或文件名"
        value={search}
      />
      <div className="flex justify-end gap-2">
        <Button
          aria-label="新建 Artifact 文件夹"
          disabled={!canEdit || createFolder.isPending}
          onClick={() => {
            const name = window.prompt(
              selectedFolder
                ? `在“${selectedFolder.name}”中新建文件夹`
                : "在项目根目录新建文件夹",
            );
            if (name?.trim()) createFolder.mutate(name.trim());
          }}
          size="sm"
          variant="outline"
        >
          <FolderPlus className="size-3.5" />
        </Button>
      </div>
      {selectedFolder ? (
        <div className="grid grid-cols-2 gap-1">
          <Button
            disabled={!canEdit || renameFolder.isPending}
            onClick={() => {
              const name = window.prompt("重命名文件夹", selectedFolder.name);
              if (name?.trim() && name.trim() !== selectedFolder.name) {
                renameFolder.mutate(name.trim());
              }
            }}
            size="sm"
            variant="outline"
          >
            重命名
          </Button>
          <ArtifactFolderDeleteActions
            compact
            folderName={selectedFolder.name}
            onDelete={(recursive) => deleteFolder.mutate(recursive)}
            pending={!canEdit || deleteFolder.isPending}
          />
        </div>
      ) : null}
      <ArtifactFolderBrowser
        canManage={canEdit}
        compact
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
      {createFolder.error ||
      renameFolder.error ||
      moveFolder.error ||
      deleteFolder.error ? (
        <p className="text-xs text-destructive">
          {
            (
              createFolder.error ??
              renameFolder.error ??
              moveFolder.error ??
              deleteFolder.error
            )?.message
          }
        </p>
      ) : null}
      {visibleArtifacts.map((item) => (
        <ArticleArtifactCard
          canEdit={canEdit}
          item={item}
          key={item.artifact.artifact_id}
        />
      ))}
      {artifacts.isPending || folderTree.isPending ? (
        <LoadingState label="正在读取 Artifact…" />
      ) : null}
      {artifacts.error || folderTree.error || moveArtifact.error ? (
        <p className="text-sm text-destructive">
          {(artifacts.error ?? folderTree.error ?? moveArtifact.error)?.message}
        </p>
      ) : null}
      {artifacts.hasNextPage ? (
        <Button
          className="w-full"
          disabled={artifacts.isFetchingNextPage}
          onClick={() => void artifacts.fetchNextPage()}
          size="sm"
          variant="outline"
        >
          {artifacts.isFetchingNextPage ? "正在加载…" : "加载更多"}
        </Button>
      ) : null}
    </div>
  );
}

function ArticleArtifactCard({
  canEdit,
  item,
}: Readonly<{
  canEdit: boolean;
  item: ArtifactDetail;
}>) {
  const version = item.current_version;
  const image = version?.mime_type.startsWith("image/") ?? false;
  const download = useQuery({
    enabled: image && Boolean(version),
    queryFn: async () => {
      try {
        const previews = await artifactApi.listPreviews(
          item.artifact.project_id,
          item.artifact.artifact_id,
          version!.version_id,
        );
        const thumbnailURL = availableThumbnailURL(previews.items);
        if (thumbnailURL) return { transfer: { url: thumbnailURL } };
      } catch {
        // Preview generation is best effort; fall back to the immutable source.
      }
      return artifactApi.download(
        item.artifact.project_id,
        item.artifact.artifact_id,
        version!.version_id,
      );
    },
    queryKey: [
      "article-artifact-preview",
      item.artifact.artifact_id,
      version?.version_id,
    ],
  });
  const drag = (event: DragEvent<HTMLDivElement>) => {
    if (!canEdit || !version) return;
    const payload: ArticleArtifactDrop = {
      artifactId: item.artifact.artifact_id,
      filename: version.filename,
      mimeType: version.mime_type,
      title: item.artifact.name,
      versionId: version.version_id,
    };
    event.dataTransfer.effectAllowed = "copyMove";
    event.dataTransfer.setData(
      artifactMoveMime,
      JSON.stringify({ artifactId: item.artifact.artifact_id }),
    );
    event.dataTransfer.setData(articleArtifactMime, JSON.stringify(payload));
  };
  return (
    <div
      className="cursor-grab overflow-hidden rounded-md border active:cursor-grabbing"
      draggable={canEdit && Boolean(version)}
      onDragStart={drag}
    >
      {image && download.data?.transfer.url ? (
        <div className="relative h-32 bg-muted/30">
          <Image
            alt={item.artifact.name}
            className="object-contain p-2"
            draggable={false}
            fill
            sizes="320px"
            src={download.data.transfer.url}
            unoptimized
          />
        </div>
      ) : (
        <div className="flex h-20 items-center justify-center bg-muted/30 text-xs text-muted-foreground">
          {version?.mime_type ?? "无可用版本"}
        </div>
      )}
      <div className="bg-white p-3 text-slate-900 dark:bg-card dark:text-card-foreground">
        <p className="truncate text-sm font-medium">{item.artifact.name}</p>
        <p className="truncate text-[11px] text-muted-foreground">
          {version?.filename} · {item.artifact.kind}
        </p>
        <p className="mt-1 text-[11px] text-muted-foreground">
          拖到编辑器即可固定版本并插入块
        </p>
        <p className="mt-2 text-[11px] text-muted-foreground">
          拖到文件夹可整理；拖到编辑器可插入
        </p>
      </div>
    </div>
  );
}

function WritingZoteroPanel({
  canEdit,
  projectId,
}: Readonly<{
  canEdit: boolean;
  data: ArticleAggregate;
  onRefresh: () => Promise<void>;
  projectId: string;
}>) {
  const [selectedCollectionKey, setSelectedCollectionKey] = useState<
    string | null
  >(null);
  const [query, setQuery] = useState("");

  const collectionsQuery = useQuery({
    queryKey: ["article-zotero-collections", projectId],
    queryFn: () => articleApi.listZoteroCollections(projectId),
  });

  const itemsQuery = useQuery({
    queryKey: [
      "article-zotero-items",
      projectId,
      selectedCollectionKey,
      query.trim(),
    ],
    queryFn: () =>
      articleApi.listZoteroItems(projectId, {
        collection: selectedCollectionKey ?? undefined,
        q: query.trim() || undefined,
      }),
  });

  const collections = collectionsQuery.data?.items ?? [];
  const items = itemsQuery.data?.items ?? [];

  const currentCollection = collections.find(
    (c) => c.collection_key === selectedCollectionKey,
  );

  const childCollections = collections.filter((c) =>
    selectedCollectionKey === null
      ? !c.parent_collection_key
      : c.parent_collection_key === selectedCollectionKey,
  );

  const breadcrumbPath = useMemo(() => {
    const path: ZoteroCollection[] = [];
    let curr = currentCollection;
    while (curr) {
      path.unshift(curr);
      curr = collections.find(
        (c) => c.collection_key === curr?.parent_collection_key,
      );
    }
    return path;
  }, [collections, currentCollection]);

  return (
    <div className="space-y-3">
      <p className="text-xs text-muted-foreground">
        拖拽条目到编辑器任意位置即可插入 Zotero 引用并自动固定版本。
      </p>
      <div className="flex gap-2">
        <Input
          aria-label="左栏搜索 Zotero"
          onChange={(event) => setQuery(event.target.value)}
          placeholder="作者、标题、关键词"
          value={query}
        />
        {query ? (
          <Button
            onClick={() => setQuery("")}
            size="sm"
            title="清空搜索"
            variant="ghost"
          >
            清空
          </Button>
        ) : null}
      </div>

      {collections.length > 0 ? (
        <section
          aria-label="Zotero 分类浏览器"
          className="space-y-1.5 rounded-lg border bg-muted/15 p-2"
        >
          <nav
            aria-label="Zotero 分类路径"
            className="flex min-h-8 flex-wrap items-center gap-0.5 rounded-md bg-background px-1.5 text-xs shadow-sm"
          >
            <button
              aria-current={
                selectedCollectionKey === null ? "location" : undefined
              }
              className={cn(
                "inline-flex items-center gap-1 rounded px-1.5 py-1 text-xs font-medium transition-colors hover:bg-muted",
                selectedCollectionKey === null
                  ? "bg-primary/10 font-semibold text-primary"
                  : "text-muted-foreground",
              )}
              onClick={() => setSelectedCollectionKey(null)}
              type="button"
            >
              <Library className="size-3.5" />
              <span>全部条目</span>
            </button>
            {breadcrumbPath.map((col) => (
              <span className="contents" key={col.collection_key}>
                <ChevronRight
                  aria-hidden="true"
                  className="size-3 text-muted-foreground"
                />
                <button
                  aria-current={
                    col.collection_key === selectedCollectionKey
                      ? "location"
                      : undefined
                  }
                  className={cn(
                    "inline-flex items-center gap-1 rounded px-1.5 py-1 text-xs font-medium transition-colors hover:bg-muted",
                    col.collection_key === selectedCollectionKey
                      ? "bg-primary/10 font-semibold text-primary"
                      : "text-muted-foreground",
                  )}
                  onClick={() => setSelectedCollectionKey(col.collection_key)}
                  type="button"
                >
                  <FolderOpen className="size-3.5 text-amber-500" />
                  <span className="max-w-[120px] truncate">{col.name}</span>
                </button>
              </span>
            ))}
          </nav>

          {childCollections.length > 0 ? (
            <div className="grid gap-1 pt-1">
              {childCollections.map((col) => (
                <button
                  className="flex items-center justify-between gap-2 rounded-md border border-border/40 bg-background/80 px-2 py-1.5 text-left text-xs shadow-2xs transition-colors hover:bg-background hover:text-foreground"
                  key={col.collection_key}
                  onClick={() => setSelectedCollectionKey(col.collection_key)}
                  type="button"
                >
                  <div className="flex min-w-0 items-center gap-1.5">
                    <Folder className="size-3.5 shrink-0 text-amber-500" />
                    <span className="truncate font-medium">{col.name}</span>
                  </div>
                  <div className="flex shrink-0 items-center gap-1 text-[10px] text-muted-foreground">
                    {col.num_collections > 0 ? (
                      <span>{col.num_collections} 子分类 · </span>
                    ) : null}
                    <span>{col.num_items} 条目</span>
                    <ChevronRight className="size-3 text-muted-foreground/60" />
                  </div>
                </button>
              ))}
            </div>
          ) : null}
        </section>
      ) : null}

      {collectionsQuery.isPending || itemsQuery.isPending ? (
        <LoadingState label="正在读取 Zotero Library…" />
      ) : null}

      {collectionsQuery.error || itemsQuery.error ? (
        <p className="text-xs text-destructive">
          {(collectionsQuery.error ?? itemsQuery.error)?.message}
        </p>
      ) : null}

      <div className="space-y-2">
        {items.map((item) => (
          <ZoteroItemCard canEdit={canEdit} item={item} key={item.item_key} />
        ))}
      </div>

      {!itemsQuery.isPending && !items.length && !itemsQuery.error ? (
        <Empty label={query ? "未找到匹配条目" : "当前分类暂无条目"} />
      ) : null}
    </div>
  );
}

function ZoteroItemCard({
  canEdit,
  item,
}: Readonly<{
  canEdit: boolean;
  item: ZoteroItem;
}>) {
  const drag = (event: DragEvent<HTMLDivElement>) => {
    if (!canEdit) return;
    event.dataTransfer.effectAllowed = "copy";
    event.dataTransfer.setData(
      articleZoteroMime,
      JSON.stringify({
        citationKey: item.citation_key,
        itemKey: item.item_key,
        title: item.title,
        version: item.version,
        raw: item.raw,
      }),
    );
  };

  return (
    <div
      className={cn(
        "group cursor-grab rounded-md border bg-card p-2.5 text-card-foreground shadow-2xs transition-shadow active:cursor-grabbing hover:border-primary/50 hover:shadow-xs",
        !canEdit && "cursor-default opacity-80",
      )}
      draggable={canEdit}
      onDragStart={drag}
    >
      <div className="flex items-start justify-between gap-2">
        <p className="line-clamp-2 text-xs font-semibold leading-snug">
          {item.title}
        </p>
        <span className="shrink-0 rounded bg-muted/60 px-1 py-0.5 font-mono text-[10px] text-muted-foreground">
          @{item.citation_key}
        </span>
      </div>
      <p className="mt-1 line-clamp-1 text-[11px] text-muted-foreground">
        {item.authors.length ? item.authors.join(", ") : "无作者信息"}
        {item.year ? ` · ${item.year}` : ""}
        {item.item_type ? ` · ${item.item_type}` : ""}
      </p>
      <div className="mt-2 flex items-center justify-between text-[10px] text-muted-foreground/80">
        <span className="flex items-center gap-1 text-primary/80 group-hover:text-primary">
          拖到编辑器插入引用
        </span>
        {item.doi ? (
          <span className="max-w-[120px] truncate" title={item.doi}>
            DOI: {item.doi}
          </span>
        ) : null}
      </div>
    </div>
  );
}

function WritingPDFPanel({
  canBuild,
  data,
  onRefresh,
  projectId,
}: Readonly<{
  canBuild: boolean;
  data: ArticleAggregate;
  onRefresh: () => Promise<void>;
  projectId: string;
}>) {
  const template = data.templates.find((item) => item.status === "ready");
  const latest = data.builds.find((item) => item.build_kind === "preview");
  const latestSuccessful = latestSuccessfulArticlePreview(data.builds);
  const pdf = latestSuccessful?.outputs.find((item) => item.role === "pdf");
  const latestLog = latest?.outputs.find((item) => item.role === "log");
  const [transfer, setTransfer] = useState<{
    headers: Record<string, string>;
    url: string;
  }>();
  const [readError, setReadError] = useState<string>();
  useEffect(() => {
    let active = true;
    setTransfer(undefined);
    setReadError(undefined);
    if (!pdf) {
      return () => {
        active = false;
      };
    }
    void artifactApi
      .download(projectId, pdf.artifact_id, pdf.version_id)
      .then((grant) => {
        if (active) setTransfer(grant.transfer);
      })
      .catch((error: unknown) => {
        if (active)
          setReadError(
            error instanceof Error ? error.message : "草稿 PDF 读取失败",
          );
      });
    return () => {
      active = false;
    };
  }, [pdf, projectId]);
  const preview = useMutation({
    mutationFn: async () => {
      if (!template) throw new Error("没有可用模板");
      return articleApi.createPreview(projectId, {
        bibliography_tool: "auto",
        draft_revision: data.draft.draft_revision,
        engine: "auto",
        template_id: template.template_id,
      });
    },
    onSuccess: onRefresh,
  });
  return (
    <div className="flex h-full min-h-0 flex-col gap-3">
      <div className="shrink-0 space-y-3">
        <Button
          className="w-full"
          disabled={!canBuild || !template || preview.isPending}
          onClick={() => preview.mutate()}
          variant="outline"
        >
          手动生成当前草稿 PDF
        </Button>
        <p className="text-xs text-muted-foreground">
          仅保留最新草稿预览，不进入正式 Build 历史，也不能创建 Release。
        </p>
        {latest ? <BuildProgress build={latest} /> : null}
        {latest?.error_message ? (
          <div className="rounded-md border border-destructive/40 bg-destructive/5 p-2 text-xs text-destructive">
            <p className="font-medium">
              {latest.error_code || "ARTICLE_BUILD_FAILED"}
            </p>
            <p className="mt-1">{latest.error_message}</p>
          </div>
        ) : null}
        {latestLog ? (
          <OutputButton output={latestLog} projectId={projectId} />
        ) : null}
        {preview.error ? (
          <p className="text-xs text-destructive">{preview.error.message}</p>
        ) : null}
        {readError ? (
          <p className="text-xs text-destructive">{readError}</p>
        ) : null}
      </div>
      <div className="min-h-0 flex-1 overflow-hidden rounded-md border bg-muted/20">
        {transfer ? (
          <PDFReader
            className="block h-full min-h-0 w-full border-0"
            title={`草稿 r${latestSuccessful?.draft_revision ?? ""} PDF 预览`}
            transfer={transfer}
          />
        ) : latestSuccessful?.status === "succeeded" && pdf ? (
          <LoadingState label="正在读取草稿 PDF…" />
        ) : (
          <Empty label="尚无可用草稿 PDF" />
        )}
      </div>
    </div>
  );
}

export function latestSuccessfulArticlePreview(
  builds: ArticleBuild[],
): ArticleBuild | undefined {
  return builds.find(
    (build) =>
      build.build_kind === "preview" &&
      build.status === "succeeded" &&
      build.outputs.some((output) => output.role === "pdf"),
  );
}

export function articleActionMessage(error: Error): string {
  if (!(error instanceof ApiError)) return error.message;
  if (error.code === "ARTICLE_REPOSITORY_NOT_CONFIGURED")
    return "尚未配置项目仓库。请先到项目设置连接 Repo，并设置 Article 分支映射。";
  if (error.code === "ARTICLE_REPOSITORY_NOT_READY")
    return "Article 分支尚未就绪。请到项目设置检查 Repo 连接和分支映射，然后重试。";
  if (error.code === "ARTICLE_REPOSITORY_CONFLICT")
    return "Article 分支已发生变化。请刷新草稿与仓库状态后重试。";
  return `${error.message}${error.requestId ? `（请求 ${error.requestId}）` : ""}`;
}

function operationStateLabel(status: ArticleCommitOperation["status"]): string {
  return {
    failed: "失败",
    queued: "排队中",
    retry_wait: "等待重试",
    running: "执行中",
    succeeded: "已完成",
  }[status];
}

function operationStageLabel(operation: ArticleCommitOperation): string {
  if (operation.status === "failed" || operation.stage === "failed") {
    return "操作失败";
  }
  if (operation.operation_kind === "publication") {
    return {
      committing: "固定 Commit 中",
      completed: "Commit 已确认，Build/Release 排队中",
      failed: "操作失败",
      publishing: "Commit 已确认，Build/Release 排队中",
      queued: "等待固定 Commit",
    }[operation.stage];
  }
  return {
    committing: "固定 Commit 中",
    completed: "远端 Commit 已确认",
    failed: "操作失败",
    publishing: "处理 Commit 结果",
    queued: "等待固定 Commit",
  }[operation.stage];
}

export function ArticleOperationStatus({
  operations,
}: Readonly<{
  operations: ArticleCommitOperation[];
}>) {
  const [open, setOpen] = useState(false);
  const active = operations.some((operation) =>
    ["queued", "running", "retry_wait"].includes(operation.status),
  );
  const latest = operations[0];
  const state = active
    ? "active"
    : latest?.status === "failed"
      ? "failed"
      : latest?.status === "succeeded"
        ? "succeeded"
        : "idle";
  const label = {
    active: "有 Commit / Release 操作正在执行",
    failed: "最近的 Commit / Release 操作失败",
    idle: "暂无 Commit / Release 操作",
    succeeded: "最近的 Commit / Release 操作已完成",
  }[state];
  return (
    <>
      <Button
        aria-label={label}
        className="rounded-full"
        onClick={() => setOpen(true)}
        size="icon"
        title={label}
        variant="outline"
      >
        {state === "active" ? (
          <LoaderCircle className="size-4 animate-spin" />
        ) : state === "failed" ? (
          <CircleX className="size-4 text-destructive" />
        ) : state === "succeeded" ? (
          <CheckCircle2 className="size-4 text-emerald-600" />
        ) : (
          <Clock3 className="size-4 text-muted-foreground" />
        )}
      </Button>
      {open ? (
        <ArticleOperationQueueDialog
          onClose={() => setOpen(false)}
          operations={operations}
        />
      ) : null}
    </>
  );
}

function ArticleOperationQueueDialog({
  onClose,
  operations,
}: Readonly<{
  onClose: () => void;
  operations: ArticleCommitOperation[];
}>) {
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/45 p-4"
      onMouseDown={(event) => {
        if (event.currentTarget === event.target) onClose();
      }}
    >
      <Card
        aria-labelledby="article-operation-queue-title"
        aria-modal="true"
        className="flex max-h-[min(46rem,calc(100vh-2rem))] w-full max-w-3xl flex-col"
        role="dialog"
      >
        <CardHeader className="flex-row items-start justify-between gap-3 space-y-0">
          <div>
            <CardTitle id="article-operation-queue-title">
              Commit / Release 队列
            </CardTitle>
            <p className="mt-1 text-sm text-muted-foreground">
              最近 {operations.length} 条后台操作
            </p>
          </div>
          <Button onClick={onClose} size="sm" variant="ghost">
            关闭
          </Button>
        </CardHeader>
        <CardContent className="min-h-0 space-y-2 overflow-y-auto">
          {operations.map((operation) => (
            <div
              className="rounded-lg border bg-muted/20 p-3 text-sm"
              key={operation.operation_id}
            >
              <div className="flex flex-wrap items-center gap-2">
                {["queued", "running", "retry_wait"].includes(
                  operation.status,
                ) ? (
                  <LoaderCircle className="size-4 animate-spin text-muted-foreground" />
                ) : operation.status === "succeeded" ? (
                  <CheckCircle2 className="size-4 text-emerald-600" />
                ) : (
                  <CircleX className="size-4 text-destructive" />
                )}
                <span className="font-medium">
                  {operation.operation_kind === "publication"
                    ? "提交并发布"
                    : "论文 Commit"}
                </span>
                <Badge>{operationStateLabel(operation.status)}</Badge>
                <span className="text-xs text-muted-foreground">
                  {operationStageLabel(operation)} · 草稿 r
                  {operation.draft_revision}
                </span>
                <time className="ml-auto text-xs text-muted-foreground">
                  {new Date(operation.updated_at).toLocaleString()}
                </time>
              </div>
              {operation.commit_sha ? (
                <p className="mt-2 font-mono text-xs text-muted-foreground">
                  Commit {operation.commit_sha}
                </p>
              ) : null}
              {operation.status === "failed" ? (
                <details className="mt-2 rounded-md border border-destructive/30 bg-destructive/5 p-2 text-xs">
                  <summary className="cursor-pointer font-medium text-destructive">
                    查看失败日志
                  </summary>
                  <dl className="mt-2 grid gap-1 font-mono text-muted-foreground sm:grid-cols-[7rem_minmax(0,1fr)]">
                    <dt>error_code</dt>
                    <dd className="break-all">
                      {operation.error_code ?? "ARTICLE_OPERATION_FAILED"}
                    </dd>
                    <dt>operation_id</dt>
                    <dd className="break-all">{operation.operation_id}</dd>
                    <dt>attempts</dt>
                    <dd>
                      {operation.attempts} / {operation.max_attempts}
                    </dd>
                    <dt>updated_at</dt>
                    <dd>{operation.updated_at}</dd>
                  </dl>
                </details>
              ) : null}
            </div>
          ))}
          {!operations.length ? <Empty label="暂无后台操作" /> : null}
        </CardContent>
      </Card>
    </div>
  );
}

export function CommitDialog({
  canBuild,
  canRelease,
  data,
  onClose,
  onOpenTemplates,
  onRefresh,
}: Readonly<{
  canBuild: boolean;
  canRelease: boolean;
  data: ArticleAggregate;
  onClose: () => void;
  onOpenTemplates: () => void;
  onRefresh: () => Promise<void>;
}>) {
  const ready = data.templates.filter((item) => item.status === "ready");
  const [message, setMessage] = useState(
    "docs(article): save manuscript revision",
  );
  const [templateId, setTemplateId] = useState(ready[0]?.template_id ?? "");
  const [tag, setTag] = useState(`v0.1.${data.releases.length + 1}`);
  const [title, setTitle] = useState("论文版本");
  const [notes, setNotes] = useState("");
  const commit = useMutation({
    mutationFn: async () => {
      const draft = await articleApi.flush(data.draft.project_id);
      return articleApi.createCommit(
        data.draft.project_id,
        draft.draft_revision,
        message.trim(),
      );
    },
    onSuccess: async () => {
      onClose();
      toast.info("论文 Commit 已进入后台队列，可以继续编辑");
      await onRefresh();
    },
  });
  const publish = useMutation({
    mutationFn: async () => {
      const draft = await articleApi.flush(data.draft.project_id);
      return articleApi.createPublication(data.draft.project_id, {
        bibliography_tool: "auto",
        draft_revision: draft.draft_revision,
        engine: "auto",
        idempotency_key: crypto.randomUUID(),
        message: message.trim(),
        notes,
        tag: tag.trim(),
        template_id: templateId,
        title: title.trim(),
      });
    },
    onSuccess: async () => {
      onClose();
      toast.info("提交并发布已进入后台队列，可以继续编辑");
      await onRefresh();
    },
  });
  const error = commit.error ?? publish.error;
  return (
    <div
      aria-modal="true"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/45 p-4"
      onMouseDown={(event) => {
        if (event.currentTarget === event.target) onClose();
      }}
      role="dialog"
    >
      <Card className="w-full max-w-xl">
        <CardHeader>
          <CardTitle>创建论文 Commit</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <Input
            aria-label="Commit message"
            onChange={(event) => setMessage(event.target.value)}
            value={message}
          />
          <div className="border-t pt-3">
            <p className="mb-2 text-sm font-medium">提交并发布</p>
            <select
              aria-label="发布模板"
              className="h-9 w-full rounded-md border bg-background px-3 text-sm"
              disabled={!ready.length}
              onChange={(event) => setTemplateId(event.target.value)}
              value={templateId}
            >
              {!ready.length ? <option value="">没有已就绪模板</option> : null}
              {ready.map((item) => (
                <option key={item.template_id} value={item.template_id}>
                  {item.manifest.name} {item.manifest.version}
                </option>
              ))}
            </select>
            {!ready.length ? (
              <div className="mt-2 flex items-center justify-between rounded-md border border-amber-300 bg-amber-50 p-2 text-xs text-amber-900 dark:bg-amber-950 dark:text-amber-100">
                <span>发布前需先导入模板并通过测试构建。</span>
                <Button onClick={onOpenTemplates} size="sm" variant="outline">
                  打开模板页
                </Button>
              </div>
            ) : null}
            <div className="mt-2 grid gap-2 sm:grid-cols-2">
              <Input
                aria-label="Release tag"
                onChange={(event) => setTag(event.target.value)}
                value={tag}
              />
              <Input
                aria-label="Release title"
                onChange={(event) => setTitle(event.target.value)}
                value={title}
              />
            </div>
            <textarea
              aria-label="Release notes"
              className="mt-2 min-h-20 w-full rounded-md border bg-background p-3 text-sm"
              onChange={(event) => setNotes(event.target.value)}
              value={notes}
            />
          </div>
          {error ? (
            <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">
              <p>{articleActionMessage(error)}</p>
              {error instanceof ApiError &&
              error.code.startsWith("ARTICLE_REPOSITORY_") ? (
                <a
                  className="mt-2 inline-block underline"
                  href={`/projects/${encodeURIComponent(data.draft.project_id)}/settings`}
                >
                  打开项目 Repo 设置
                </a>
              ) : null}
            </div>
          ) : null}
          <div className="flex justify-end gap-2">
            <Button onClick={onClose} variant="ghost">
              取消
            </Button>
            <Button
              disabled={
                !message.trim() || commit.isPending || publish.isPending
              }
              onClick={() => commit.mutate()}
              variant="outline"
            >
              仅提交
            </Button>
            <Button
              disabled={
                !canBuild ||
                !canRelease ||
                !message.trim() ||
                !templateId ||
                !tag.trim() ||
                !title.trim() ||
                commit.isPending ||
                publish.isPending
              }
              onClick={() => publish.mutate()}
            >
              提交并发布
            </Button>
          </div>
          <p className="text-xs text-muted-foreground">
            提交并发布按 Commit → Build → Release 执行；Build 失败时 Commit
            保留且不会创建 Release，可从版本历史重试同一 Commit 的 Build。
          </p>
        </CardContent>
      </Card>
    </div>
  );
}

function VersionHistoryWorkspace(
  props: Readonly<{
    canBuild: boolean;
    canRelease: boolean;
    data: ArticleAggregate;
    onRefresh: () => Promise<void>;
    projectId: string;
  }>,
) {
  const [tab, setTab] = useState<"commits" | "releases">("commits");
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <div className="flex gap-2">
          <Button
            onClick={() => setTab("commits")}
            variant={tab === "commits" ? "default" : "outline"}
          >
            Commits
          </Button>
          <Button
            onClick={() => setTab("releases")}
            variant={tab === "releases" ? "default" : "outline"}
          >
            Releases
          </Button>
        </div>
        <ArticleOperationStatus
          operations={props.data.commit_operations ?? []}
        />
      </div>
      {tab === "commits" ? (
        <BuildWorkspace
          canBuild={props.canBuild}
          data={props.data}
          onRefresh={props.onRefresh}
          projectId={props.projectId}
        />
      ) : (
        <ReleaseWorkspace
          canRelease={props.canRelease}
          data={props.data}
          onRefresh={props.onRefresh}
          projectId={props.projectId}
        />
      )}
    </div>
  );
}

function BuildWorkspace({
  canBuild,
  data,
  onRefresh,
  projectId,
}: Readonly<{
  canBuild: boolean;
  data: ArticleAggregate;
  onRefresh: () => Promise<void>;
  projectId: string;
}>) {
  const readyTemplates = data.templates.filter(
    (template) => template.status === "ready",
  );
  const [templateId, setTemplateId] = useState(
    readyTemplates[0]?.template_id ?? "",
  );
  const [commitId, setCommitId] = useState(data.commits[0]?.commit_id ?? "");
  const action = useMutation({
    mutationFn: async (kind: "preview" | "formal") => {
      if (kind === "preview") {
        return articleApi.createPreview(projectId, {
          bibliography_tool: "auto",
          draft_revision: data.draft.draft_revision,
          engine: "auto",
          template_id: templateId,
        });
      }
      return articleApi.createBuild(projectId, {
        bibliography_tool: "auto",
        commit_id: commitId,
        engine: "auto",
        idempotency_key: crypto.randomUUID(),
        template_id: templateId,
      });
    },
    onSuccess: onRefresh,
  });
  const retry = useMutation({
    mutationFn: (id: string) => articleApi.retryBuild(projectId, id),
    onSuccess: onRefresh,
  });
  return (
    <div className="grid gap-5 xl:grid-cols-[22rem_minmax(0,1fr)]">
      <Card>
        <CardHeader>
          <CardTitle>启动构建</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <label className="block text-xs text-muted-foreground">
            模板
            <select
              className="mt-1 h-9 w-full rounded-md border bg-background px-3 text-sm"
              value={templateId}
              onChange={(event) => setTemplateId(event.target.value)}
            >
              {readyTemplates.map((template) => (
                <option key={template.template_id} value={template.template_id}>
                  {template.manifest.name} {template.manifest.version}
                </option>
              ))}
            </select>
          </label>
          <label className="block text-xs text-muted-foreground">
            正式构建 Commit
            <select
              className="mt-1 h-9 w-full rounded-md border bg-background px-3 text-sm"
              value={commitId}
              onChange={(event) => setCommitId(event.target.value)}
            >
              {data.commits.map((commit) => (
                <option key={commit.commit_id} value={commit.commit_id}>
                  {commit.commit_sha.slice(0, 12)} · {commit.message}
                </option>
              ))}
            </select>
          </label>
          <Button
            className="w-full"
            disabled={
              !canBuild ||
              !templateId ||
              data.draft.draft_revision < 1 ||
              action.isPending
            }
            onClick={() => action.mutate("preview")}
            variant="outline"
          >
            预览当前草稿
          </Button>
          <Button
            className="w-full"
            disabled={!canBuild || !templateId || !commitId || action.isPending}
            onClick={() => action.mutate("formal")}
          >
            构建固定 Commit
          </Button>
          {!readyTemplates.length ? (
            <p className="text-xs text-destructive">
              请先注册并通过测试的模板。
            </p>
          ) : null}
        </CardContent>
      </Card>
      <div className="space-y-3">
        {data.commits.map((commit) => {
          const builds = data.builds.filter(
            (build) =>
              build.build_kind === "formal" &&
              build.commit_id === commit.commit_id,
          );
          const releases = data.releases.filter(
            (release) => release.commit_id === commit.commit_id,
          );
          return (
            <Card key={commit.commit_id}>
              <CardHeader>
                <CardTitle className="text-base">{commit.message}</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3">
                <div className="flex flex-wrap gap-2 text-xs">
                  <code>{commit.commit_sha.slice(0, 12)}</code>
                  <span>草稿 r{commit.draft_revision}</span>
                  {releases.map((release) => (
                    <Badge key={release.release_id}>{release.tag}</Badge>
                  ))}
                </div>
                {builds.map((build) => (
                  <BuildCard
                    build={build}
                    key={build.build_id}
                    onRetry={
                      build.status === "failed" && canBuild
                        ? () => retry.mutate(build.build_id)
                        : undefined
                    }
                    projectId={projectId}
                  />
                ))}
                {!builds.length ? (
                  <Empty label="此 Commit 尚无 Build；可从左侧创建零到多个构建" />
                ) : null}
              </CardContent>
            </Card>
          );
        })}
        {!data.commits.length ? <Empty label="尚无论文 Commit" /> : null}
      </div>
    </div>
  );
}

function BuildCard({
  build,
  onRetry,
  projectId,
}: Readonly<{ build: ArticleBuild; onRetry?: () => void; projectId: string }>) {
  return (
    <Card>
      <CardContent className="pt-6">
        <div className="flex flex-wrap items-center gap-2">
          <BuildStatus status={build.status} />
          <Badge>{build.build_kind}</Badge>
          <span className="font-mono text-xs">
            {build.commit_sha?.slice(0, 12) ?? `draft r${build.draft_revision}`}
          </span>
          <span className="ml-auto text-xs text-muted-foreground">
            {new Date(build.updated_at).toLocaleString()}
          </span>
        </div>
        <p className="mt-2 text-xs text-muted-foreground">
          {build.engine} · {build.bibliography_tool} · template{" "}
          {build.template_version_id.slice(0, 8)}
        </p>
        <BuildProgress build={build} />
        {build.error_message ? (
          <p className="mt-2 text-sm text-destructive">
            {build.error_code}: {build.error_message}
          </p>
        ) : null}
        <div className="mt-3 flex flex-wrap gap-2">
          {build.outputs.map((output) => (
            <OutputButton
              key={output.role}
              output={output}
              projectId={projectId}
            />
          ))}
          {onRetry ? (
            <Button onClick={onRetry} size="sm" variant="outline">
              <RefreshCw className="size-3.5" />
              重试
            </Button>
          ) : null}
        </div>
      </CardContent>
    </Card>
  );
}

function ReleaseWorkspace({
  canRelease,
  data,
  onRefresh,
  projectId,
}: Readonly<{
  canRelease: boolean;
  data: ArticleAggregate;
  onRefresh: () => Promise<void>;
  projectId: string;
}>) {
  const eligible = data.builds.filter(
    (build) =>
      build.build_kind === "formal" &&
      build.status === "succeeded" &&
      build.commit_id,
  );
  const [buildId, setBuildId] = useState(eligible[0]?.build_id ?? "");
  const [tag, setTag] = useState(`v0.1.${data.releases.length + 1}`);
  const [title, setTitle] = useState("论文版本");
  const [notes, setNotes] = useState("");
  const [selectedId, setSelectedId] = useState(data.releases[0]?.release_id);
  const selected =
    data.releases.find((release) => release.release_id === selectedId) ??
    data.releases[0];
  const release = useMutation({
    mutationFn: () => {
      const build = eligible.find((item) => item.build_id === buildId);
      if (!build?.commit_id) throw new Error("请选择成功的正式构建");
      return articleApi.createRelease(projectId, {
        build_id: buildId,
        commit_id: build.commit_id,
        notes,
        tag: tag.trim(),
        title: title.trim(),
      });
    },
    onSuccess: async (value) => {
      setSelectedId(value.release_id);
      await onRefresh();
    },
  });
  return (
    <div className="grid gap-5 xl:grid-cols-[23rem_minmax(0,1fr)]">
      <aside className="space-y-4">
        <Card>
          <CardHeader>
            <CardTitle>创建不可变 Release</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <select
              className="h-9 w-full rounded-md border bg-background px-3 text-sm"
              value={buildId}
              onChange={(event) => setBuildId(event.target.value)}
            >
              {eligible.map((build) => (
                <option key={build.build_id} value={build.build_id}>
                  {build.commit_sha?.slice(0, 12)} ·{" "}
                  {build.build_id.slice(0, 8)}
                </option>
              ))}
            </select>
            <Input
              aria-label="Release tag"
              value={tag}
              onChange={(event) => setTag(event.target.value)}
            />
            <Input
              aria-label="Release title"
              value={title}
              onChange={(event) => setTitle(event.target.value)}
            />
            <textarea
              aria-label="Release notes"
              className="min-h-24 w-full rounded-md border bg-background p-3 text-sm"
              value={notes}
              onChange={(event) => setNotes(event.target.value)}
            />
            <Button
              className="w-full"
              disabled={
                !canRelease ||
                !buildId ||
                !tag.trim() ||
                !title.trim() ||
                release.isPending
              }
              onClick={() => release.mutate()}
            >
              发布
            </Button>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-base">历史版本</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            {data.releases.map((item) => (
              <button
                className={`w-full rounded-md border p-3 text-left ${selected?.release_id === item.release_id ? "border-foreground/50" : ""}`}
                key={item.release_id}
                onClick={() => setSelectedId(item.release_id)}
                type="button"
              >
                <p className="font-medium">
                  {item.tag} · {item.title}
                </p>
                <p className="mt-1 font-mono text-[11px] text-muted-foreground">
                  {item.commit_sha.slice(0, 12)}
                </p>
              </button>
            ))}
            {!data.releases.length ? <Empty label="尚无 Release" /> : null}
          </CardContent>
        </Card>
      </aside>
      <ReleaseDetail projectId={projectId} release={selected} />
    </div>
  );
}

function ReleaseDetail({
  projectId,
  release,
}: Readonly<{ projectId: string; release?: ArticleRelease }>) {
  const [sources, setSources] = useState<Record<string, string>>({});
  const [selectedFile, setSelectedFile] = useState("");
  const [selectedLine, setSelectedLine] = useState<number>();
  const [pdfURL, setPdfURL] = useState<string>();
  const [files, setFiles] = useState<string[]>([]);
  const [syncPoints, setSyncPoints] = useState<SyncTexPoint[]>([]);
  const [pdfPage, setPdfPage] = useState(1);
  const [verticalPercent, setVerticalPercent] = useState(50);
  useEffect(() => {
    let active = true;
    let objectURL = "";
    if (!release) {
      setSources({});
      setSelectedFile("");
      setPdfURL(undefined);
      setFiles([]);
      setSyncPoints([]);
      return;
    }
    const texOutput = release.outputs.find(
      (item) => item.role === "tex_source",
    );
    const pdfOutput = release.outputs.find((item) => item.role === "pdf");
    const reportOutput = release.outputs.find(
      (item) => item.role === "build_report",
    );
    const sourceOutput = release.outputs.find(
      (item) => item.role === "source_zip",
    );
    const syncOutput = release.outputs.find((item) => item.role === "synctex");
    void Promise.all([
      texOutput ? readArtifactText(projectId, texOutput) : Promise.resolve(""),
      pdfOutput
        ? readArtifactBlobURL(projectId, pdfOutput)
        : Promise.resolve(""),
      reportOutput
        ? readArtifactText(projectId, reportOutput)
        : Promise.resolve("{}"),
      sourceOutput
        ? readArtifactBytes(projectId, sourceOutput)
        : Promise.resolve(new Uint8Array()),
      syncOutput
        ? readArtifactBytes(projectId, syncOutput)
        : Promise.resolve(new Uint8Array()),
    ]).then(([source, url, report, sourceZip, syncTex]) => {
      if (!active) {
        if (url) URL.revokeObjectURL(url);
        return;
      }
      objectURL = url;
      setPdfURL(url || undefined);
      setPdfPage(1);
      setSelectedLine(undefined);
      let sourceTree: Record<string, string> = {};
      let sourceFiles: string[] = [];
      if (sourceZip.length) {
        const entries = unzipSync(sourceZip);
        sourceFiles = Object.keys(entries)
          .filter((name) => !name.endsWith("/"))
          .sort();
        sourceTree = Object.fromEntries(
          Object.entries(entries)
            .filter(
              ([name, bytes]) =>
                /\.(?:bib|cls|json|md|sty|tex)$/iu.test(name) &&
                bytes.length <= 2 * 1024 * 1024,
            )
            .map(([name, bytes]) => [name, strFromU8(bytes)]),
        );
      }
      if (!Object.keys(sourceTree).length && texOutput)
        sourceTree[texOutput.filename] = source;
      try {
        const parsed = JSON.parse(report) as { source_files?: unknown };
        if (!sourceFiles.length && Array.isArray(parsed.source_files))
          sourceFiles = parsed.source_files.filter(
            (item): item is string => typeof item === "string",
          );
      } catch {
        /* The immutable source ZIP remains authoritative. */
      }
      if (!sourceFiles.length)
        sourceFiles = release.outputs.map((item) => item.filename);
      const initialFile =
        sourceTree["main.tex"] !== undefined
          ? "main.tex"
          : (Object.keys(sourceTree)[0] ?? "");
      setSources(sourceTree);
      setFiles(sourceFiles);
      setSelectedFile(initialFile);
      if (syncTex.length) {
        try {
          setSyncPoints(
            parseSyncTex(
              strFromU8(gunzipSync(syncTex)),
              Object.keys(sourceTree),
            ),
          );
        } catch {
          setSyncPoints([]);
        }
      } else setSyncPoints([]);
    });
    return () => {
      active = false;
      if (objectURL) URL.revokeObjectURL(objectURL);
    };
  }, [projectId, release]);
  if (!release)
    return <Empty label="创建 Release 后可查看 PDF、TeX、源码树和构建报告" />;
  const roles = new Map(release.outputs.map((output) => [output.role, output]));
  const lines = (sources[selectedFile] ?? "").split("\n");
  const forward = (line: number) => {
    const point = forwardSyncPoint(syncPoints, selectedFile, line);
    setSelectedLine(line);
    if (point) setPdfPage(point.page);
  };
  const reverse = () => {
    const point = reverseSyncPoint(syncPoints, pdfPage, verticalPercent);
    if (!point) return;
    setSelectedFile(point.file);
    setSelectedLine(point.line);
    setTimeout(
      () =>
        document
          .getElementById(`article-source-line-${point.line}`)
          ?.scrollIntoView({ block: "center" }),
      0,
    );
  };
  return (
    <div className="min-w-0 space-y-4">
      <Card>
        <CardHeader>
          <CardTitle>
            {release.tag} · {release.title}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">
            {release.notes || "无发布说明"}
          </p>
          <dl className="mt-4 grid gap-2 text-xs sm:grid-cols-4">
            <div>
              <dt className="text-muted-foreground">Commit</dt>
              <dd className="font-mono">{release.commit_sha.slice(0, 16)}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">模板版本</dt>
              <dd className="font-mono">
                {release.template_version_id.slice(0, 16)}
              </dd>
            </div>
            <div>
              <dt className="text-muted-foreground">引擎</dt>
              <dd>{release.engine}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">发布时间</dt>
              <dd>{new Date(release.created_at).toLocaleString()}</dd>
            </div>
          </dl>
          <div className="mt-4 flex flex-wrap gap-2">
            {(["pdf", "source_zip", "build_report", "synctex"] as const).map(
              (role) =>
                roles.get(role) ? (
                  <Button
                    key={role}
                    onClick={() =>
                      downloadArtifactOutput(projectId, roles.get(role)!)
                    }
                    size="sm"
                    variant="outline"
                  >
                    {outputIcon(role)}下载 {outputLabel(role)}
                  </Button>
                ) : null,
            )}
          </div>
        </CardContent>
      </Card>
      <div className="grid min-h-[46rem] overflow-hidden rounded-lg border bg-card xl:grid-cols-[15rem_minmax(20rem,1fr)_minmax(24rem,1.2fr)]">
        <section className="overflow-auto border-r p-3">
          <h2 className="mb-3 text-sm font-semibold">TeX 文件树</h2>
          <div className="font-mono text-xs">
            <p>source/</p>
            {files.map((name) =>
              sources[name] !== undefined ? (
                <button
                  className={`block w-full truncate py-1 pl-3 text-left ${selectedFile === name ? "bg-muted font-semibold" : ""}`}
                  key={name}
                  onClick={() => {
                    setSelectedFile(name);
                    setSelectedLine(undefined);
                  }}
                  type="button"
                >
                  ├─ {name}
                </button>
              ) : (
                <p
                  className="truncate py-1 pl-3 text-muted-foreground"
                  key={name}
                >
                  ├─ {name}
                </p>
              ),
            )}
          </div>
        </section>
        <section className="min-w-0 overflow-auto border-r">
          <div className="sticky top-0 z-10 border-b bg-card p-3 text-sm font-semibold">
            只读源码 · {selectedFile || "main.tex"}
          </div>
          <ol className="min-h-full py-4 font-mono text-xs leading-5">
            {lines.map((line, index) => (
              <li
                className={
                  selectedLine === index + 1
                    ? "bg-amber-100 dark:bg-amber-950"
                    : ""
                }
                id={`article-source-line-${index + 1}`}
                key={`${index}-${line.slice(0, 16)}`}
              >
                <button
                  className="flex w-full text-left"
                  onClick={() => forward(index + 1)}
                  type="button"
                >
                  <span className="w-12 shrink-0 select-none pr-3 text-right text-muted-foreground">
                    {index + 1}
                  </span>
                  <span className="whitespace-pre-wrap">{line || " "}</span>
                </button>
              </li>
            ))}
          </ol>
        </section>
        <section className="min-w-0">
          <div className="flex flex-wrap items-center gap-2 border-b bg-card p-2 text-xs">
            <span className="font-semibold">固定 PDF</span>
            {syncPoints.length ? (
              <>
                <Input
                  aria-label="PDF page"
                  className="h-7 w-16"
                  min={1}
                  onChange={(event) =>
                    setPdfPage(Math.max(1, Number(event.target.value) || 1))
                  }
                  type="number"
                  value={pdfPage}
                />
                <label>
                  纵向 {verticalPercent}%{" "}
                  <input
                    aria-label="PDF vertical position"
                    className="w-20 align-middle"
                    min={0}
                    max={100}
                    onChange={(event) =>
                      setVerticalPercent(Number(event.target.value))
                    }
                    type="range"
                    value={verticalPercent}
                  />
                </label>
                <Button onClick={reverse} size="sm" variant="outline">
                  反向定位 TeX
                </Button>
              </>
            ) : null}
          </div>
          {pdfURL ? (
            <iframe
              className="h-[43rem] w-full"
              src={`${pdfURL}#page=${pdfPage}&zoom=page-width`}
              title={`${release.tag} PDF`}
            />
          ) : (
            <LoadingState label="正在读取固定 PDF…" />
          )}
        </section>
      </div>
      {roles.has("synctex") ? (
        <p className="text-xs text-muted-foreground">
          SyncTeX 正反向定位：点击源码行可跳转 PDF 页；选择 PDF
          页与纵向位置可回到最接近的固定源码行。源码、PDF
          与映射均来自同一次不可变 Build。
        </p>
      ) : null}
    </div>
  );
}

async function readArtifactText(projectId: string, output: ArticleBuildOutput) {
  const grant = await artifactApi.download(
    projectId,
    output.artifact_id,
    output.version_id,
  );
  const response = await fetch(grant.transfer.url, {
    headers: grant.transfer.headers,
  });
  if (!response.ok) throw new Error("固定构建文件读取失败");
  return response.text();
}
async function readArtifactBlobURL(
  projectId: string,
  output: ArticleBuildOutput,
) {
  const grant = await artifactApi.download(
    projectId,
    output.artifact_id,
    output.version_id,
  );
  const response = await fetch(grant.transfer.url, {
    headers: grant.transfer.headers,
  });
  if (!response.ok) throw new Error("固定构建文件读取失败");
  return URL.createObjectURL(await response.blob());
}
async function readArtifactBytes(
  projectId: string,
  output: ArticleBuildOutput,
) {
  const grant = await artifactApi.download(
    projectId,
    output.artifact_id,
    output.version_id,
  );
  const response = await fetch(grant.transfer.url, {
    headers: grant.transfer.headers,
  });
  if (!response.ok) throw new Error("固定构建文件读取失败");
  return new Uint8Array(await response.arrayBuffer());
}
async function downloadArtifactOutput(
  projectId: string,
  output: ArticleBuildOutput,
) {
  const grant = await artifactApi.download(
    projectId,
    output.artifact_id,
    output.version_id,
  );
  const response = await fetch(grant.transfer.url, {
    headers: grant.transfer.headers,
  });
  if (!response.ok) throw new Error("下载失败");
  const url = URL.createObjectURL(await response.blob());
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = output.filename;
  anchor.click();
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

function TemplateWorkspace({
  canManage,
  data,
  onRefresh,
  projectId,
}: Readonly<{
  canManage: boolean;
  data: ArticleAggregate;
  onRefresh: () => Promise<void>;
  projectId: string;
}>) {
  const [mode, setMode] = useState<"standard" | "overleaf">("overleaf");
  const [artifactId, setArtifactId] = useState("");
  const [versionId, setVersionId] = useState("");
  const [manifest, setManifest] = useState(templateDefaults);
  const [source, setSource] = useState<File>();
  const [candidates, setCandidates] = useState<string[]>([]);
  const [entrypoint, setEntrypoint] = useState("");
  const [inspection, setInspection] = useState("");
  const register = useMutation({
    mutationFn: () =>
      articleApi.registerTemplate(
        projectId,
        artifactId.trim(),
        versionId.trim(),
        manifest,
      ),
    onSuccess: onRefresh,
  });
  const importTemplate = useMutation({
    mutationFn: async () => {
      if (!source || !entrypoint)
        throw new Error("请选择普通 Overleaf ZIP 和主文件");
      const converted = await convertOverleafZip(source, entrypoint, {
        name: manifest.name,
        version: manifest.version,
        engine: manifest.engine,
        bibliography_tool: manifest.bibliography_tool,
      });
      const detail = await new MultipartUploadTask({
        description: "由 mmdash Overleaf 导入向导转换的版本化 Article 模板",
        file: converted.file,
        idempotencyKey: crypto.randomUUID(),
        kind: "attachment",
        name: converted.manifest.name,
        projectId,
        tags: ["article-template", "overleaf-import"],
      }).start();
      if (!detail.current_version)
        throw new Error("转换后的模板 Artifact 尚不可用");
      return articleApi.registerTemplate(
        projectId,
        detail.artifact.artifact_id,
        detail.current_version.version_id,
        converted.manifest,
      );
    },
    onSuccess: onRefresh,
  });
  const copyTemplate = useMutation({
    mutationFn: async (template: ArticleAggregate["templates"][number]) => {
      const detail = await copyArticleTemplateToArtifact(projectId, template);
      const currentVersion = detail.current_version;
      if (!currentVersion)
        throw new Error("模板副本上传完成，但不可变版本尚不可用");
      return { currentVersion, detail, template };
    },
    onSuccess: ({ currentVersion, detail, template }) => {
      setMode("standard");
      setArtifactId(detail.artifact.artifact_id);
      setVersionId(currentVersion.version_id);
      setManifest(copiedTemplateManifest(template));
      setInspection(
        "已复制为普通 Artifact；可在项目文件库下载修改或上传新版本，再返回此处校验注册。",
      );
    },
  });
  const retryTemplateTest = useMutation({
    mutationFn: (buildId: string) => articleApi.retryBuild(projectId, buildId),
    onSuccess: onRefresh,
  });
  const choose = async (file?: File) => {
    setSource(file);
    setCandidates([]);
    setEntrypoint("");
    setInspection("");
    if (!file) return;
    try {
      const value = await inspectOverleafZip(file);
      setCandidates(value.candidates);
      setEntrypoint(value.candidates[0] ?? "");
      setInspection(
        `${value.fileCount} 个文件 · 解压 ${formatBytes(value.expandedBytes)}`,
      );
    } catch (error) {
      setInspection(error instanceof Error ? error.message : "ZIP 检查失败");
    }
  };
  return (
    <div className="grid gap-5 xl:grid-cols-[25rem_minmax(0,1fr)]">
      <Card>
        <CardHeader>
          <CardTitle>模板导入与注册</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex gap-2">
            <Button
              onClick={() => setMode("overleaf")}
              size="sm"
              variant={mode === "overleaf" ? "default" : "outline"}
            >
              Overleaf ZIP 向导
            </Button>
            <Button
              onClick={() => setMode("standard")}
              size="sm"
              variant={mode === "standard" ? "default" : "outline"}
            >
              标准模板 Artifact
            </Button>
          </div>
          {mode === "standard" ? (
            <>
              <Input
                aria-label="Artifact ID"
                placeholder="Artifact UUID"
                value={artifactId}
                onChange={(event) => setArtifactId(event.target.value)}
              />
              <Input
                aria-label="Version ID"
                placeholder="Version UUID"
                value={versionId}
                onChange={(event) => setVersionId(event.target.value)}
              />
              {(
                [
                  "name",
                  "version",
                  "entrypoint",
                  "output",
                  "content_target",
                  "bibliography_target",
                ] as const
              ).map((key) => (
                <Input
                  aria-label={key}
                  key={key}
                  placeholder={key}
                  value={manifest[key]}
                  onChange={(event) =>
                    setManifest((current) => ({
                      ...current,
                      [key]: event.target.value,
                    }))
                  }
                />
              ))}
              <Button
                className="w-full"
                disabled={
                  !canManage ||
                  !artifactId.trim() ||
                  !versionId.trim() ||
                  register.isPending
                }
                onClick={() => register.mutate()}
              >
                校验并注册
              </Button>
            </>
          ) : (
            <>
              <p className="rounded-md border bg-muted/30 p-3 text-xs text-muted-foreground">
                mmdash 默认论文模板由 Core
                自动、幂等地安装并验证；无需在浏览器生成或上传模板。下面的向导仅用于自定义
                Overleaf 模板。
              </p>
              <label className="flex cursor-pointer items-center justify-center gap-2 rounded-md border border-dashed p-5 text-sm">
                <FileUp className="size-4" />
                选择普通 Overleaf ZIP
                <input
                  accept=".zip,application/zip"
                  className="sr-only"
                  onChange={(event) => void choose(event.target.files?.[0])}
                  type="file"
                />
              </label>
              {source ? (
                <p className="truncate text-xs">
                  {source.name} · {inspection}
                </p>
              ) : null}
              {candidates.length ? (
                <select
                  aria-label="TeX 主文件"
                  className="h-9 w-full rounded-md border bg-background px-3 text-sm"
                  onChange={(event) => setEntrypoint(event.target.value)}
                  value={entrypoint}
                >
                  {candidates.map((item) => (
                    <option key={item}>{item}</option>
                  ))}
                </select>
              ) : null}
              <Input
                aria-label="导入模板名称"
                onChange={(event) =>
                  setManifest((current) => ({
                    ...current,
                    name: event.target.value,
                  }))
                }
                placeholder="模板名称"
                value={manifest.name}
              />
              <Input
                aria-label="导入模板版本"
                onChange={(event) =>
                  setManifest((current) => ({
                    ...current,
                    version: event.target.value,
                  }))
                }
                placeholder="版本"
                value={manifest.version}
              />
              <Button
                className="w-full"
                disabled={
                  !canManage ||
                  !source ||
                  !entrypoint ||
                  importTemplate.isPending
                }
                onClick={() => importTemplate.mutate()}
              >
                转换、上传、验证并测试构建
              </Button>
              {importTemplate.error ? (
                <p className="text-sm text-destructive">
                  {importTemplate.error.message}
                </p>
              ) : null}
            </>
          )}
          <p className="text-xs text-muted-foreground">
            向导把普通 ZIP 的主文档转换为 Markdown 内容插槽并写入 Template
            Spec；Core 固定 Artifact Version，Worker
            随后执行安全校验与真实测试构建。
          </p>
        </CardContent>
      </Card>
      <div className="space-y-3">
        {data.templates.map((template) => {
          const testBuild = data.builds.find(
            (build) =>
              build.build_kind === "template_test" &&
              build.template_id === template.template_id,
          );
          return (
            <Card key={template.template_id}>
              <CardContent className="pt-6">
                <div className="flex items-center gap-2">
                  <Badge>{template.status}</Badge>
                  <span className="font-medium">{template.manifest.name}</span>
                  <span className="text-xs text-muted-foreground">
                    {template.manifest.version}
                  </span>
                </div>
                <p className="mt-2 font-mono text-xs text-muted-foreground">
                  {template.manifest.entrypoint} → {template.manifest.output}
                </p>
                {template.error_code ? (
                  <p className="mt-2 text-sm text-destructive">
                    {template.error_code}
                  </p>
                ) : null}
                <Button
                  className="mt-3"
                  disabled={!canManage || copyTemplate.isPending}
                  onClick={() => copyTemplate.mutate(template)}
                  size="sm"
                  variant="outline"
                >
                  <Copy className="size-3.5" />
                  复制为可自定义 Artifact
                </Button>
                {template.status === "rejected" && testBuild ? (
                  <Button
                    className="ml-2 mt-3"
                    disabled={!canManage || retryTemplateTest.isPending}
                    onClick={() => retryTemplateTest.mutate(testBuild.build_id)}
                    size="sm"
                    variant="outline"
                  >
                    <RefreshCw className="size-3.5" />
                    重新测试模板
                  </Button>
                ) : null}
                {retryTemplateTest.error &&
                retryTemplateTest.variables === testBuild?.build_id ? (
                  <p className="mt-2 text-xs text-destructive">
                    {retryTemplateTest.error.message}
                  </p>
                ) : null}
                {copyTemplate.error &&
                copyTemplate.variables?.template_id === template.template_id ? (
                  <p className="mt-2 text-xs text-destructive">
                    {copyTemplate.error.message}
                  </p>
                ) : null}
              </CardContent>
            </Card>
          );
        })}
        {!data.templates.length ? <Empty label="尚无 Article 模板" /> : null}
      </div>
    </div>
  );
}

function OutputButton({
  output,
  projectId,
}: Readonly<{ output: ArticleBuildOutput; projectId: string }>) {
  const [pending, setPending] = useState(false);
  return (
    <Button
      disabled={pending}
      onClick={async () => {
        setPending(true);
        try {
          await downloadArtifactOutput(projectId, output);
        } finally {
          setPending(false);
        }
      }}
      size="sm"
      variant="outline"
    >
      <Download className="size-3.5" />
      {outputLabel(output.role)}
    </Button>
  );
}

function SyncBadge({
  connection,
  pending,
}: Readonly<{ connection: ConnectionState; pending: number }>) {
  const label =
    connection === "synced" && pending === 0
      ? "已同步"
      : connection === "offline"
        ? `离线 · ${pending} 项待传`
        : connection === "failed"
          ? "同步失败"
          : connection === "syncing"
            ? `同步中 · ${pending}`
            : connection === "connected"
              ? "已连接"
              : "连接中";
  return (
    <Badge className="gap-1.5">
      {connection === "synced" && pending === 0 ? (
        <CheckCircle2 className="size-3.5" />
      ) : (
        <LoaderCircle className="size-3.5 animate-spin" />
      )}
      {label}
    </Badge>
  );
}

function BuildStatus({ status }: Readonly<{ status: ArticleBuild["status"] }>) {
  return (
    <Badge className="gap-1">
      {["queued", "running"].includes(status) ? (
        <LoaderCircle className="size-3 animate-spin" />
      ) : status === "succeeded" ? (
        <CheckCircle2 className="size-3" />
      ) : status === "failed" ? (
        <CircleAlert className="size-3" />
      ) : (
        <Clock3 className="size-3" />
      )}
      {status}
    </Badge>
  );
}

const buildStageLabels: Record<ArticleBuild["progress_stage"], string> = {
  queued: "等待 Worker",
  preparing: "准备模板",
  resources: "整理图片与引用",
  converting: "生成 TeX 目录",
  compiling: "编译 PDF",
  packaging: "打包 TeX 源码",
  uploading: "归档构建产物",
  completed: "构建完成",
  failed: "构建失败",
  superseded: "已被新预览取代",
};

export function BuildProgress({ build }: Readonly<{ build: ArticleBuild }>) {
  const value = Math.max(0, Math.min(100, build.progress_percent));
  return (
    <div className="mt-3 space-y-1.5" data-article-build-progress>
      <div className="flex items-center justify-between gap-3 text-xs">
        <span className="text-muted-foreground">
          {buildStageLabels[build.progress_stage] ?? build.progress_stage}
        </span>
        <span className="font-mono tabular-nums">{value}%</span>
      </div>
      <div
        aria-label="论文编译进度"
        aria-valuemax={100}
        aria-valuemin={0}
        aria-valuenow={value}
        className="h-2 overflow-hidden rounded-full bg-muted"
        role="progressbar"
      >
        <div
          className={`h-full rounded-full transition-[width] duration-500 ${build.status === "failed" ? "bg-destructive" : "bg-primary"}`}
          style={{ width: `${value}%` }}
        />
      </div>
    </div>
  );
}

export function outputLabel(role: ArticleBuildOutput["role"]): string {
  if (role === "pdf") return "PDF";
  if (role === "source_zip") return "TeX 源码 ZIP";
  if (role === "tex_source") return "主 TeX 文件";
  if (role === "build_report") return "构建报告";
  if (role === "synctex") return "SyncTeX 映射";
  return "构建日志";
}

function articleBlockFromCollaborationEvent(
  payload: string,
): ArticleBlock | undefined {
  try {
    const event = JSON.parse(payload) as Record<string, unknown>;
    if (event.type !== "article.block.reviewed") return;
    const block = event.block as Record<string, unknown> | undefined;
    if (
      !block ||
      typeof block.block_id !== "string" ||
      typeof block.content_fingerprint !== "string" ||
      ![
        "ai_draft",
        "human_draft",
        "ai_revision",
        "human_revision",
        "reviewed",
      ].includes(block.tag as ArticleBlock["tag"]) ||
      typeof block.node_type !== "string" ||
      typeof block.ordinal !== "number" ||
      typeof block.text !== "string" ||
      typeof block.attrs !== "object" ||
      block.attrs === null ||
      typeof block.provenance !== "object" ||
      block.provenance === null ||
      typeof block.updated_at !== "string"
    ) {
      return;
    }
    return block as ArticleBlock;
  } catch {
    return;
  }
}

function updateArticleDraftCache(
  queryClient: QueryClient,
  projectId: string,
  draft: ArticleDraft,
) {
  queryClient.setQueryData<ArticleAggregate>(
    ["article", projectId],
    (current) =>
      current
        ? {
            ...current,
            draft,
            unreviewed_blocks: draft.blocks.filter(
              (block) => block.tag !== "reviewed",
            ).length,
          }
        : current,
  );
}

function updateArticleBlockCache(
  queryClient: QueryClient,
  projectId: string,
  reviewed: ArticleBlock,
) {
  queryClient.setQueryData<ArticleAggregate>(
    ["article", projectId],
    (current) => {
      if (!current) return current;
      const blocks = current.draft.blocks.map((block) =>
        block.block_id === reviewed.block_id ? reviewed : block,
      );
      return {
        ...current,
        draft: { ...current.draft, blocks },
        unreviewed_blocks: blocks.filter((block) => block.tag !== "reviewed")
          .length,
      };
    },
  );
}

function Empty({ label }: Readonly<{ label: string }>) {
  return (
    <div className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
      {label}
    </div>
  );
}
function LoadingState({ label }: Readonly<{ label: string }>) {
  return (
    <div className="flex min-h-64 items-center justify-center gap-2 text-sm text-muted-foreground">
      <LoaderCircle className="size-4 animate-spin" />
      {label}
    </div>
  );
}
function ErrorState({ message }: Readonly<{ message: string }>) {
  return (
    <div className="flex min-h-64 items-center justify-center gap-2 text-sm text-destructive">
      <CircleAlert className="size-4" />
      {message}
    </div>
  );
}
function colorFor(value: string) {
  let hash = 0;
  for (const char of value)
    hash = ((hash << 5) - hash + char.charCodeAt(0)) | 0;
  return `hsl(${Math.abs(hash) % 360} 72% 42%)`;
}
function formatBytes(value: number) {
  return value < 1024 * 1024
    ? `${Math.ceil(value / 1024)} KiB`
    : `${(value / (1024 * 1024)).toFixed(1)} MiB`;
}
function outputIcon(role: ArticleBuildOutput["role"]) {
  if (role === "pdf") return <FileText className="size-3.5" />;
  if (role === "source_zip") return <FileArchive className="size-3.5" />;
  if (role === "tex_source" || role === "synctex")
    return <FileCode2 className="size-3.5" />;
  return <Boxes className="size-3.5" />;
}
