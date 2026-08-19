"use client";

import { HocuspocusProvider, WebSocketStatus } from "@hocuspocus/provider";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { gunzipSync, strFromU8, unzipSync } from "fflate";
import {
  BookOpen,
  Boxes,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  CircleAlert,
  Clock3,
  Download,
  FileArchive,
  FileCode2,
  FilePenLine,
  FileUp,
  FileText,
  Link2,
  LoaderCircle,
  RefreshCw,
  Search,
  Users,
} from "lucide-react";
import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type DragEvent,
} from "react";
import * as Y from "yjs";

import { useCurrentProject } from "@/components/providers/project-provider";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { artifactApi } from "@/features/artifact/artifact-api";
import { MultipartUploadTask } from "@/features/artifact/multipart-upload";
import type { ArtifactDetail } from "@/features/artifact/types";
import type { ProjectPermissions } from "@/features/repo/types";
import { apiClient } from "@/lib/api-client";
import { ApiError } from "@/lib/api-client";

import { articleApi } from "./api";
import {
  ArticleEditor,
  articleArtifactMime,
  type ArticleArtifactDrop,
} from "./article-editor";
import { ArticleTagPanel } from "./article-tag-panel";
import { convertOverleafZip, inspectOverleafZip } from "./overleaf-import";
import {
  forwardSyncPoint,
  parseSyncTex,
  reverseSyncPoint,
  type SyncTexPoint,
} from "./synctex";
import type {
  ArticleAggregate,
  ArticleBuild,
  ArticleBuildOutput,
  ArticleRelease,
  ArticleTemplateManifest,
  ZoteroItem,
} from "./types";
import { openArtifactLibraryEvent } from "./slash-command";

type WorkspaceTab = "write" | "history" | "templates" | "zotero";
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
    refetchInterval: (query) =>
      query.state.data?.builds.some((build) =>
        ["queued", "running"].includes(build.status),
      )
        ? 2_000
        : 15_000,
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
  const canManageZotero =
    permissions.data?.permissions.includes("project.article.zotero.manage") ??
    false;
  const collaborator = useMemo(
    () => ({
      color: colorFor(project.id),
      name: project.role ? `${project.role} · 当前用户` : "当前用户",
    }),
    [project.id, project.role],
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
    const offline = () => setConnection("offline");
    const online = () => {
      setConnection(WebSocketStatus.Connecting);
      next.connect();
    };
    window.addEventListener("offline", offline);
    window.addEventListener("online", online);
    setProvider(next);
    return () => {
      window.removeEventListener("offline", offline);
      window.removeEventListener("online", online);
      next.destroy();
      document.destroy();
      setProvider(undefined);
    };
  }, [project.id]);

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
      <ErrorState message={aggregate.error?.message ?? "论文工作区不可用"} />
    );
  }

  const data = aggregate.data;
  return (
    <section className="space-y-5" aria-labelledby="article-title">
      <header className="flex flex-wrap items-start gap-4">
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
        className="flex flex-wrap gap-2 border-b pb-3"
      >
        {(
          [
            ["write", "写作"],
            ["history", "版本历史"],
            ["templates", "模板"],
            ["zotero", "Zotero"],
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
        <div className="flex items-center gap-2 rounded-lg border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">
          <CircleAlert className="size-4" />
          {error}
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
      {tab === "zotero" ? (
        <ZoteroWorkspace
          canManage={canManageZotero}
          data={data}
          onRefresh={refresh}
          projectId={project.id}
        />
      ) : null}
    </section>
  );
}

function WritingWorkspace({
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
  onOpenTemplates: () => void;
  onRefresh: () => Promise<void>;
  provider?: HocuspocusProvider;
  synced: boolean;
}>) {
  const [panel, setPanel] = useState<
    "reference" | "artifact" | "zotero" | "pdf"
  >("reference");
  const [collapsed, setCollapsed] = useState(false);
  const [width, setWidth] = useState(320);
  const [commitOpen, setCommitOpen] = useState(false);
  const projectId = data.draft.project_id;
  useEffect(() => {
    const openArtifact = () => {
      setCollapsed(false);
      setPanel("artifact");
    };
    window.addEventListener(openArtifactLibraryEvent, openArtifact);
    return () =>
      window.removeEventListener(openArtifactLibraryEvent, openArtifact);
  }, []);
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
    async (blockId: string) => {
      await articleApi.flush(projectId);
      await articleApi.reviewBlock(projectId, blockId);
      await onRefresh();
    },
    [onRefresh, projectId],
  );
  return (
    <>
      <div className="flex min-h-[44rem] gap-3">
        <aside
          className={`shrink-0 overflow-hidden rounded-lg border bg-card ${collapsed ? "w-12" : ""}`}
          style={collapsed ? undefined : { width }}
        >
          <div className="flex items-center gap-1 border-b p-2">
            {!collapsed
              ? (
                  [
                    ["reference", "参考"],
                    ["artifact", "Artifact"],
                    ["zotero", "Zotero"],
                    ["pdf", "PDF"],
                  ] as const
                ).map(([value, label]) => (
                  <Button
                    key={value}
                    onClick={() => setPanel(value)}
                    size="sm"
                    variant={panel === value ? "secondary" : "ghost"}
                  >
                    {label}
                  </Button>
                ))
              : null}
            <Button
              aria-label={collapsed ? "展开左栏" : "折叠左栏"}
              className="ml-auto"
              onClick={() => setCollapsed((value) => !value)}
              size="sm"
              variant="ghost"
            >
              {collapsed ? (
                <ChevronRight className="size-4" />
              ) : (
                <ChevronLeft className="size-4" />
              )}
            </Button>
          </div>
          {!collapsed ? (
            <div className="max-h-[48rem] overflow-auto p-3">
              {panel === "reference" ? (
                <ReferencePanel
                  canEdit={canEdit}
                  data={data}
                  onRefresh={onRefresh}
                />
              ) : null}
              {panel === "artifact" ? (
                <ArtifactPanel
                  canEdit={canEdit}
                  data={data}
                  onRefresh={onRefresh}
                  projectId={projectId}
                />
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
          ) : null}
          {!collapsed ? (
            <label className="block border-t p-2 text-[11px] text-muted-foreground">
              左栏宽度
              <input
                aria-label="左栏宽度"
                className="ml-2 w-28 align-middle"
                max={520}
                min={260}
                onChange={(event) => setWidth(Number(event.target.value))}
                type="range"
                value={width}
              />
            </label>
          ) : null}
        </aside>
        <main className="min-w-0 flex-1 space-y-3">
          <div className="flex flex-wrap items-center gap-2 rounded-lg border bg-card p-3">
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
          <ArticleTagPanel
            blocks={data.draft.blocks}
            canEdit={canEdit}
            onReview={reviewBlock}
          />
          {provider && synced ? (
            <ArticleEditor
              canEdit={canEdit}
              collaborator={collaborator}
              onFlush={onFlush}
              onInsertArtifact={insertArtifact}
              provider={provider}
            />
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

function ReferencePanel({
  canEdit,
  data,
  onRefresh,
}: Readonly<{
  canEdit: boolean;
  data: ArticleAggregate;
  onRefresh: () => Promise<void>;
}>) {
  const [kind, setKind] = useState<
    "problem" | "model_snapshot" | "experiment_result"
  >("problem");
  const remove = useMutation({
    mutationFn: (id: string) =>
      articleApi.removeReference(data.draft.project_id, id),
    onSuccess: onRefresh,
  });
  const items = data.references.filter((item) => item.reference_type === kind);
  return (
    <div className="space-y-3">
      <div className="grid grid-cols-3 gap-1">
        {(["problem", "model_snapshot", "experiment_result"] as const).map(
          (value) => (
            <Button
              key={value}
              onClick={() => setKind(value)}
              size="sm"
              variant={kind === value ? "secondary" : "ghost"}
            >
              {value === "problem"
                ? "Problem"
                : value === "model_snapshot"
                  ? "Model"
                  : "Experiment"}
            </Button>
          ),
        )}
      </div>
      {items.map((reference) => (
        <PinnedReference
          canEdit={canEdit}
          key={reference.reference_id}
          onRemove={() => remove.mutate(reference.reference_id)}
          reference={reference}
        />
      ))}
      {!items.length ? <Empty label="此类引用尚未固定版本" /> : null}
    </div>
  );
}

function ArtifactPanel({
  canEdit,
  data,
  onRefresh,
  projectId,
}: Readonly<{
  canEdit: boolean;
  data: ArticleAggregate;
  onRefresh: () => Promise<void>;
  projectId: string;
}>) {
  const artifacts = useQuery({
    queryFn: () =>
      artifactApi.list(projectId, { limit: 50, status: "available" }),
    queryKey: ["article-artifacts", projectId],
  });
  const add = useMutation({
    mutationFn: (item: ArtifactDetail) => {
      if (!item.current_version) throw new Error("Artifact 没有可用版本");
      return articleApi.addReference(projectId, {
        metadata: {
          filename: item.current_version.filename,
          mime_type: item.current_version.mime_type,
        },
        reference_type: "artifact",
        source_object_id: item.artifact.artifact_id,
        source_version_id: item.current_version.version_id,
        title: item.artifact.name,
      });
    },
    onSuccess: onRefresh,
  });
  const remove = useMutation({
    mutationFn: (id: string) => articleApi.removeReference(projectId, id),
    onSuccess: onRefresh,
  });
  return (
    <div className="space-y-2">
      <p className="text-xs text-muted-foreground">
        加入时固定当前不可变 Version；正式 Build 不追随新版本。
      </p>
      {artifacts.data?.items.map((item) => {
        const pinned = data.references.find(
          (reference) =>
            reference.reference_type === "artifact" &&
            reference.source_object_id === item.artifact.artifact_id &&
            reference.source_version_id === item.current_version?.version_id,
        );
        const drag = (event: DragEvent<HTMLDivElement>) => {
          if (!canEdit || !item.current_version) return;
          const payload: ArticleArtifactDrop = {
            artifactId: item.artifact.artifact_id,
            filename: item.current_version.filename,
            mimeType: item.current_version.mime_type,
            title: item.artifact.name,
            versionId: item.current_version.version_id,
          };
          event.dataTransfer.effectAllowed = "copy";
          event.dataTransfer.setData(
            articleArtifactMime,
            JSON.stringify(payload),
          );
        };
        return (
          <div
            className="cursor-grab rounded-md border p-3 active:cursor-grabbing"
            draggable={canEdit && Boolean(item.current_version)}
            key={item.artifact.artifact_id}
            onDragStart={drag}
          >
            <p className="truncate text-sm font-medium">{item.artifact.name}</p>
            <p className="truncate text-[11px] text-muted-foreground">
              {item.current_version?.filename} · {item.artifact.kind}
            </p>
            <p className="mt-1 text-[11px] text-muted-foreground">
              拖到右侧编辑器可固定版本并插入块
            </p>
            <Button
              className="mt-2"
              disabled={!canEdit || add.isPending || remove.isPending}
              onClick={() =>
                pinned ? remove.mutate(pinned.reference_id) : add.mutate(item)
              }
              size="sm"
              variant="outline"
            >
              {pinned ? "移除固定引用" : "固定并加入"}
            </Button>
          </div>
        );
      })}
      {artifacts.isPending ? <LoadingState label="正在读取 Artifact…" /> : null}
      {artifacts.error ? (
        <p className="text-sm text-destructive">{artifacts.error.message}</p>
      ) : null}
    </div>
  );
}

function WritingZoteroPanel({
  canEdit,
  data,
  onRefresh,
  projectId,
}: Readonly<{
  canEdit: boolean;
  data: ArticleAggregate;
  onRefresh: () => Promise<void>;
  projectId: string;
}>) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<ZoteroItem[]>([]);
  const search = useMutation({
    mutationFn: () => articleApi.searchZotero(projectId, query.trim()),
    onSuccess: (value) => setResults(value.items),
  });
  const freeze = useMutation({
    mutationFn: (item: ZoteroItem) =>
      articleApi.addReference(projectId, {
        citation_key: item.citation_key,
        metadata: item.raw,
        reference_type: "zotero",
        source_object_id: item.item_key,
        source_version_id: String(item.version),
        title: item.title,
      }),
    onSuccess: onRefresh,
  });
  return (
    <div className="space-y-3">
      <div className="flex gap-2">
        <Input
          aria-label="左栏搜索 Zotero"
          onChange={(event) => setQuery(event.target.value)}
          placeholder="作者、标题、关键词"
          value={query}
        />
        <Button
          disabled={!query.trim() || search.isPending}
          onClick={() => search.mutate()}
          size="sm"
        >
          <Search className="size-4" />
        </Button>
      </div>
      {results.map((item) => {
        const pinned = data.references.some(
          (reference) =>
            reference.reference_type === "zotero" &&
            reference.source_object_id === item.item_key &&
            reference.source_version_id === String(item.version),
        );
        return (
          <div className="rounded-md border p-3" key={item.item_key}>
            <p className="text-sm font-medium">{item.title}</p>
            <p className="mt-1 text-[11px] text-muted-foreground">
              {item.authors.join(", ")} · @{item.citation_key}
            </p>
            <Button
              className="mt-2"
              disabled={!canEdit || pinned || freeze.isPending}
              onClick={() => freeze.mutate(item)}
              size="sm"
              variant="outline"
            >
              {pinned ? "已固定此版本" : "固定引用"}
            </Button>
          </div>
        );
      })}
      {!results.length ? <Empty label="搜索只读 Zotero Library" /> : null}
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
  const pdf = latest?.outputs.find((item) => item.role === "pdf");
  const [url, setURL] = useState<string>();
  useEffect(() => {
    if (!pdf) {
      setURL(undefined);
      return;
    }
    void artifactApi
      .download(projectId, pdf.artifact_id, pdf.version_id)
      .then((grant) => setURL(grant.transfer.url));
  }, [pdf, projectId]);
  const preview = useMutation({
    mutationFn: () => {
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
    <div className="space-y-3">
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
      {latest ? <BuildStatus status={latest.status} /> : null}
      {url ? (
        <iframe
          className="h-[34rem] w-full rounded-md border"
          src={url}
          title="最新草稿 PDF 预览"
        />
      ) : (
        <Empty label="尚无可用草稿 PDF" />
      )}
    </div>
  );
}

function PinnedReference({
  canEdit,
  onRemove,
  reference,
}: Readonly<{
  canEdit: boolean;
  onRemove: () => void;
  reference: ArticleAggregate["references"][number];
}>) {
  return (
    <div className="rounded-md border p-3">
      <div className="flex items-center gap-2">
        <Link2 className="size-3.5" />
        <span className="truncate text-sm font-medium">{reference.title}</span>
      </div>
      <p className="mt-1 truncate font-mono text-[11px] text-muted-foreground">
        {reference.source_version_id}
      </p>
      {canEdit ? (
        <Button className="mt-2" onClick={onRemove} size="sm" variant="ghost">
          移除
        </Button>
      ) : null}
    </div>
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
      await onRefresh();
      onClose();
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
      await onRefresh();
      onClose();
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
    mutationFn: (kind: "preview" | "formal") =>
      kind === "preview"
        ? articleApi.createPreview(projectId, {
            bibliography_tool: "auto",
            draft_revision: data.draft.draft_revision,
            engine: "auto",
            template_id: templateId,
          })
        : articleApi.createBuild(projectId, {
            bibliography_tool: "auto",
            commit_id: commitId,
            engine: "auto",
            idempotency_key: crypto.randomUUID(),
            template_id: templateId,
          }),
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
                    {outputIcon(role)}下载 {role}
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
        {data.templates.map((template) => (
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
            </CardContent>
          </Card>
        ))}
        {!data.templates.length ? <Empty label="尚无 Article 模板" /> : null}
      </div>
    </div>
  );
}

function ZoteroWorkspace({
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
  const binding = useQuery({
    queryFn: () => articleApi.getZotero(projectId),
    queryKey: ["article-zotero", projectId],
    retry: false,
  });
  const [libraryType, setLibraryType] = useState<"user" | "group">("user");
  const [libraryId, setLibraryId] = useState("");
  const [collectionKey, setCollectionKey] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<ZoteroItem[]>([]);
  const save = useMutation({
    mutationFn: () =>
      articleApi.updateZotero(projectId, {
        api_key: apiKey,
        collection_key: collectionKey || undefined,
        library_id: libraryId,
        library_type: libraryType,
      }),
    onSuccess: () => binding.refetch(),
  });
  const search = useMutation({
    mutationFn: () => articleApi.searchZotero(projectId, query),
    onSuccess: (value) => setResults(value.items),
  });
  const freeze = useMutation({
    mutationFn: (item: ZoteroItem) =>
      articleApi.addReference(projectId, {
        citation_key: item.citation_key,
        metadata: item.raw,
        reference_type: "zotero",
        source_object_id: item.item_key,
        source_version_id: String(item.version),
        title: item.title,
      }),
    onSuccess: onRefresh,
  });
  return (
    <div className="grid gap-5 xl:grid-cols-[23rem_minmax(0,1fr)]">
      <Card>
        <CardHeader>
          <CardTitle>Zotero 只读绑定</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <p className="text-xs text-muted-foreground">
            {binding.data?.api_key_configured
              ? `已配置 ${binding.data.library_type}/${binding.data.library_id}`
              : "尚未配置"}
          </p>
          <select
            className="h-9 w-full rounded-md border bg-background px-3 text-sm"
            value={libraryType}
            onChange={(event) =>
              setLibraryType(event.target.value as "user" | "group")
            }
          >
            <option value="user">User library</option>
            <option value="group">Group library</option>
          </select>
          <Input
            aria-label="Library ID"
            placeholder="Library ID"
            value={libraryId}
            onChange={(event) => setLibraryId(event.target.value)}
          />
          <Input
            aria-label="Collection key"
            placeholder="Collection key（可选）"
            value={collectionKey}
            onChange={(event) => setCollectionKey(event.target.value)}
          />
          <Input
            aria-label="Zotero API key"
            placeholder="只读 API Key"
            type="password"
            value={apiKey}
            onChange={(event) => setApiKey(event.target.value)}
          />
          <Button
            className="w-full"
            disabled={!canManage || !libraryId || !apiKey || save.isPending}
            onClick={() => save.mutate()}
          >
            保存加密凭据
          </Button>
        </CardContent>
      </Card>
      <div className="space-y-4">
        <Card>
          <CardContent className="flex gap-2 pt-6">
            <Input
              aria-label="搜索 Zotero"
              placeholder="标题、作者或 DOI"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
            <Button
              disabled={!query.trim() || search.isPending}
              onClick={() => search.mutate()}
            >
              <Search className="size-4" />
              搜索
            </Button>
          </CardContent>
        </Card>
        {results.map((item) => (
          <Card key={item.item_key}>
            <CardContent className="pt-6">
              <p className="font-medium">{item.title}</p>
              <p className="mt-1 text-xs text-muted-foreground">
                {item.authors.join(", ")} · {item.year} · @{item.citation_key}
              </p>
              <Button
                className="mt-3"
                disabled={
                  freeze.isPending ||
                  data.references.some(
                    (reference) =>
                      reference.source_object_id === item.item_key &&
                      reference.source_version_id === String(item.version),
                  )
                }
                onClick={() => freeze.mutate(item)}
                size="sm"
                variant="outline"
              >
                <BookOpen className="size-3.5" />
                固定此版本
              </Button>
            </CardContent>
          </Card>
        ))}
        {!results.length ? (
          <Empty label="搜索 Zotero 后可将条目版本固定到论文" />
        ) : null}
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
      {output.role}
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
  const colors = ["#2563eb", "#7c3aed", "#059669", "#dc2626", "#d97706"];
  let hash = 0;
  for (const char of value)
    hash = ((hash << 5) - hash + char.charCodeAt(0)) | 0;
  return colors[Math.abs(hash) % colors.length];
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
