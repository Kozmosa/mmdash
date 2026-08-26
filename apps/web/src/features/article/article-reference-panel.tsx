"use client";

import { useMutation, useQuery } from "@tanstack/react-query";
import {
  ArrowLeft,
  ChevronRight,
  FileArchive,
  GitCommitHorizontal,
  Link2,
  ListTree,
  Pin,
  Plus,
} from "lucide-react";
import Image from "next/image";
import { useEffect, useState, type ReactNode } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ArtifactDetailDrawer } from "@/features/artifact/artifact-detail-drawer";
import { artifactApi } from "@/features/artifact/artifact-api";
import { experimentApi } from "@/features/experiment/api";
import {
  ResultFilePreview,
  ResultFileTree,
} from "@/features/experiment/experiment-ui";
import { ModelDocument } from "@/features/model/model-question-page";
import type {
  ModelOverview,
  ModelQuestionDetail,
  ModelSnapshot,
} from "@/features/model/types";
import { apiClient } from "@/lib/api-client";
import { optionalRequest } from "@/features/repo/optional-request";

import { articleApi } from "./api";
import {
  immutableExperimentVersion,
  versionedReferenceInsert,
} from "./article-reference";
import {
  articleInsertReferenceEvent,
  type ArticleVersionedReferenceInsert,
} from "./article-editor";
import type {
  ArticleAggregate,
  ArticleReference,
  ArticleRenderTheme,
} from "./types";

type ReferenceKind = "experiment_result" | "model_snapshot" | "problem";

export function ArticleReferencePanel({
  canEdit,
  data,
  initialKind = "model_snapshot",
  onRefresh,
}: Readonly<{
  canEdit: boolean;
  data: ArticleAggregate;
  initialKind?: ReferenceKind;
  onRefresh: () => Promise<void>;
}>) {
  const [kind, setKind] = useState<ReferenceKind>(initialKind);
  const [questionId, setQuestionId] = useState("");
  const [snapshotId, setSnapshotId] = useState("");
  const [experimentId, setExperimentId] = useState("");
  const [problemArtifactId, setProblemArtifactId] = useState("");
  const projectId = data.draft.project_id;
  useEffect(() => setKind(initialKind), [initialKind]);
  const renderingSetting = useQuery({
    queryFn: () =>
      optionalRequest<{ values: Record<string, unknown> }>(
        apiClient,
        `/projects/${encodeURIComponent(projectId)}/settings/article.rendering`,
      ),
    queryKey: ["article-rendering-setting", projectId],
    retry: false,
  });
  const renderTheme: ArticleRenderTheme =
    renderingSetting.data?.values.theme === "latex" ? "latex" : "md";
  const items = data.references.filter((item) => item.reference_type === kind);
  const remove = useMutation({
    mutationFn: (id: string) => articleApi.removeReference(projectId, id),
    onSuccess: onRefresh,
  });
  const pin = useMutation({
    mutationFn: (
      input: Omit<
        ArticleReference,
        "created_at" | "created_by" | "project_id" | "reference_id"
      >,
    ) => articleApi.addReference(projectId, input),
    onSuccess: onRefresh,
  });

  const openPinned = (reference: ArticleReference) => {
    setKind(reference.reference_type as ReferenceKind);
    if (reference.reference_type === "model_snapshot") {
      setQuestionId(reference.source_object_id);
      setSnapshotId(reference.source_version_id);
    }
    if (reference.reference_type === "experiment_result")
      setExperimentId(reference.source_object_id);
    if (reference.reference_type === "problem")
      setProblemArtifactId(reference.source_object_id);
  };

  return (
    <div className="flex h-full min-h-0 flex-col gap-3">
      <div className="grid shrink-0 grid-cols-3 gap-1 rounded-lg bg-muted/40 p-1">
        {(
          [
            ["model_snapshot", "模型"],
            ["experiment_result", "Experiments"],
            ["problem", "题目"],
          ] as const
        ).map(([value, label]) => (
          <Button
            key={value}
            onClick={() => setKind(value)}
            size="sm"
            variant={kind === value ? "secondary" : "ghost"}
          >
            {label}
          </Button>
        ))}
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        {kind === "model_snapshot" ? (
          <ModelReferenceBrowser
            canEdit={canEdit}
            onPin={(input) => pin.mutate(input)}
            onQuestionChange={setQuestionId}
            onSnapshotChange={setSnapshotId}
            projectId={projectId}
            questionId={questionId}
            references={data.references}
            renderTheme={renderTheme}
            snapshotId={snapshotId}
          />
        ) : null}
        {kind === "experiment_result" ? (
          <ExperimentReferenceBrowser
            canEdit={canEdit}
            experimentId={experimentId}
            onExperimentChange={setExperimentId}
            onPin={(input) => pin.mutate(input)}
            projectId={projectId}
            references={data.references}
          />
        ) : null}
        {kind === "problem" ? (
          <ProblemReferenceBrowser
            artifactId={problemArtifactId}
            canEdit={canEdit}
            onArtifactChange={setProblemArtifactId}
            onPin={(input) => pin.mutate(input)}
            projectId={projectId}
            references={data.references}
          />
        ) : null}

        {items.length ? (
          <div className="space-y-1 border-t pt-3">
            <p className="text-[11px] font-medium text-muted-foreground">
              已固定参考
            </p>
            {items.map((reference) => (
              <PinnedReference
                canEdit={canEdit}
                key={reference.reference_id}
                onOpen={() => openPinned(reference)}
                onRemove={() => remove.mutate(reference.reference_id)}
                reference={reference}
              />
            ))}
          </div>
        ) : null}
      </div>
    </div>
  );
}

function ProblemReferenceBrowser({
  artifactId,
  canEdit,
  onArtifactChange,
  onPin,
  projectId,
  references,
}: Readonly<{
  artifactId: string;
  canEdit: boolean;
  onArtifactChange: (id: string) => void;
  onPin: (
    input: Omit<
      ArticleReference,
      "created_at" | "created_by" | "project_id" | "reference_id"
    >,
  ) => void;
  projectId: string;
  references: ArticleReference[];
}>) {
  const artifacts = useQuery({
    queryFn: () =>
      artifactApi.list(projectId, {
        kind: "problem",
        limit: 50,
        status: "available",
      }),
    queryKey: ["article-reference-problems", projectId],
  });
  const selected = artifacts.data?.items.find(
    (item) => item.artifact.artifact_id === artifactId,
  );
  const version = selected?.current_version;
  const previews = useQuery({
    enabled: Boolean(artifactId && version?.version_id),
    queryFn: () =>
      artifactApi.listPreviews(projectId, artifactId, version!.version_id),
    queryKey: [
      "article-reference-problem-preview",
      projectId,
      artifactId,
      version?.version_id,
    ],
  });
  const preview = previews.data?.items.find(
    (item) =>
      item.status === "available" &&
      item.transfer &&
      ["thumbnail", "image", "pdf"].includes(item.preview_type),
  );
  const download = useQuery({
    enabled: Boolean(artifactId && version?.version_id),
    queryFn: () =>
      artifactApi.download(projectId, artifactId, version!.version_id),
    queryKey: [
      "article-reference-problem-download",
      projectId,
      artifactId,
      version?.version_id,
    ],
  });
  const existing = references.find(
    (item) =>
      item.reference_type === "problem" &&
      item.source_object_id === artifactId &&
      item.source_version_id === version?.version_id,
  );

  if (!artifactId) {
    return (
      <BrowserList label="选择题目源 Artifact">
        {(artifacts.data?.items ?? []).map((item) => (
          <button
            className="flex w-full items-center gap-2 rounded-md border p-2 text-left hover:bg-muted/60"
            key={item.artifact.artifact_id}
            onClick={() => onArtifactChange(item.artifact.artifact_id)}
            type="button"
          >
            <FileArchive className="size-4 shrink-0 text-muted-foreground" />
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm font-medium">
                {item.artifact.name}
              </span>
              <span className="block truncate text-[11px] text-muted-foreground">
                {item.current_version?.filename ?? "暂无可用版本"}
              </span>
            </span>
            <ChevronRight className="size-4 text-muted-foreground" />
          </button>
        ))}
        {!artifacts.data?.items.length ? (
          <EmptyReference label="项目尚未绑定可用的题目 Artifact。" />
        ) : null}
      </BrowserList>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      <PathBar
        onBack={() => onArtifactChange("")}
        parts={[
          "题目",
          selected?.artifact.name ?? artifactId,
          version ? `v${version.version_no}` : "无版本",
        ]}
      />
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-md border bg-background">
        {preview?.transfer && preview.preview_type !== "pdf" ? (
          <div className="relative min-h-0 flex-1 bg-muted/30">
            <Image
              alt={selected?.artifact.name ?? "题目预览"}
              className="object-contain"
              fill
              sizes="320px"
              src={preview.transfer.url}
              unoptimized
            />
          </div>
        ) : preview?.transfer ? (
          <iframe
            className="min-h-0 flex-1 w-full"
            src={preview.transfer.url}
            title={`${selected?.artifact.name ?? "题目"} PDF 预览`}
          />
        ) : (
          <div className="p-4 text-xs text-muted-foreground">
            暂无内嵌预览；可打开冻结版本查看。
          </div>
        )}
        <div className="border-t p-3">
          <p className="truncate text-sm font-medium">
            {selected?.artifact.name}
          </p>
          <p className="truncate text-[11px] text-muted-foreground">
            {version?.filename} · {version?.mime_type}
          </p>
          {download.data ? (
            <a
              className="mt-2 inline-block text-xs underline"
              href={download.data.transfer.url}
              rel="noreferrer"
              target="_blank"
            >
              打开冻结版本
            </a>
          ) : null}
        </div>
      </div>
      {existing ? (
        <div className="shrink-0">
          <InsertReferenceButton canEdit={canEdit} reference={existing} />
        </div>
      ) : (
        <Button
          disabled={!canEdit || !version}
          onClick={() =>
            onPin({
              metadata: {
                artifact_id: artifactId,
                filename: version!.filename,
                mime_type: version!.mime_type,
                sha256: version!.sha256,
              },
              reference_type: "problem",
              source_object_id: artifactId,
              source_version_id: version!.version_id,
              title: selected!.artifact.name,
            })
          }
          size="sm"
          variant="outline"
        >
          <Pin className="size-3.5" />
          固定题目版本
        </Button>
      )}
    </div>
  );
}

function ModelReferenceBrowser({
  canEdit,
  onPin,
  onQuestionChange,
  onSnapshotChange,
  projectId,
  questionId,
  references,
  renderTheme,
  snapshotId,
}: Readonly<{
  canEdit: boolean;
  onPin: (
    input: Omit<
      ArticleReference,
      "created_at" | "created_by" | "project_id" | "reference_id"
    >,
  ) => void;
  onQuestionChange: (id: string) => void;
  onSnapshotChange: (id: string) => void;
  projectId: string;
  questionId: string;
  references: ArticleReference[];
  renderTheme: ArticleRenderTheme;
  snapshotId: string;
}>) {
  const [selectedArtifactId, setSelectedArtifactId] = useState<string>();
  const overview = useQuery({
    queryFn: () =>
      apiClient.request<ModelOverview>(
        `/projects/${encodeURIComponent(projectId)}/models`,
      ),
    queryKey: ["article-reference-models", projectId],
  });
  const detail = useQuery({
    enabled: Boolean(questionId),
    queryFn: () =>
      apiClient.request<ModelQuestionDetail>(
        `/projects/${encodeURIComponent(projectId)}/models/questions/${encodeURIComponent(questionId)}`,
      ),
    queryKey: ["article-reference-model-question", projectId, questionId],
  });
  const snapshot = useQuery({
    enabled: Boolean(questionId && snapshotId),
    queryFn: () =>
      apiClient.request<ModelSnapshot>(
        `/projects/${encodeURIComponent(projectId)}/models/questions/${encodeURIComponent(questionId)}/snapshots/${encodeURIComponent(snapshotId)}`,
      ),
    queryKey: [
      "article-reference-model-snapshot",
      projectId,
      questionId,
      snapshotId,
    ],
  });
  const question = overview.data?.questions.find(
    (item) => item.question_id === questionId,
  );
  const selectedSummary = detail.data?.snapshots.find(
    (item) => item.snapshot_id === snapshotId,
  );
  const existing = references.find(
    (item) =>
      item.reference_type === "model_snapshot" &&
      item.source_object_id === questionId &&
      item.source_version_id === snapshotId,
  );

  if (!questionId) {
    return (
      <BrowserList label="选择模型问题">
        {(overview.data?.questions ?? []).map((item) => (
          <button
            className="flex w-full items-center gap-2 rounded-md border p-2 text-left hover:bg-muted/60"
            key={item.question_id}
            onClick={() => onQuestionChange(item.question_id)}
            type="button"
          >
            <span className="flex size-8 shrink-0 items-center justify-center rounded bg-primary/10 text-xs font-semibold text-primary">
              {item.code}
            </span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm font-medium">
                {item.title}
              </span>
              <span className="text-[11px] text-muted-foreground">
                {item.snapshot_count} 个版本
              </span>
            </span>
            <ChevronRight className="size-4 text-muted-foreground" />
          </button>
        ))}
      </BrowserList>
    );
  }

  if (!snapshotId) {
    return (
      <div className="space-y-2">
        <PathBar
          onBack={() => onQuestionChange("")}
          parts={["模型", question?.code ?? "Q?"]}
        />
        <BrowserList label="选择对应版本">
          {(detail.data?.snapshots ?? []).map((item, index, all) => (
            <button
              className="w-full rounded-md border p-2 text-left hover:bg-muted/60"
              key={item.snapshot_id}
              onClick={() => onSnapshotChange(item.snapshot_id)}
              type="button"
            >
              <span className="block text-sm font-medium">
                版本 {all.length - index} · {item.title}
              </span>
              <span className="text-[11px] text-muted-foreground">
                {new Date(item.captured_at).toLocaleString()}
              </span>
            </button>
          ))}
        </BrowserList>
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      <PathBar
        onBack={() => onSnapshotChange("")}
        parts={[
          "模型",
          question?.code ?? "Q?",
          selectedSummary
            ? `版本 ${Math.max(1, (detail.data?.snapshots.length ?? 1) - detail.data!.snapshots.indexOf(selectedSummary))}`
            : snapshotId,
        ]}
      />
      {snapshot.data ? (
        <div
          className={`article-rendered-document min-h-0 flex-1 overflow-auto rounded-md border bg-background px-4 py-5 ${renderTheme === "latex" ? "article-rendered-document-latex" : ""}`}
        >
          <ModelDocument
            assets={snapshot.data.assets}
            blocks={snapshot.data.blocks}
            onArtifactOpen={setSelectedArtifactId}
            projectId={projectId}
          />
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">正在读取模型版本…</p>
      )}
      {snapshot.data ? (
        <div className="flex shrink-0 gap-2">
          {existing ? (
            <InsertReferenceButton canEdit={canEdit} reference={existing} />
          ) : (
            <Button
              disabled={!canEdit}
              onClick={() =>
                onPin({
                  metadata: {
                    captured_at: snapshot.data!.captured_at,
                    content_hash: snapshot.data!.content_hash,
                  },
                  reference_type: "model_snapshot",
                  source_object_id: questionId,
                  source_version_id: snapshotId,
                  title: snapshot.data!.title,
                })
              }
              size="sm"
              variant="outline"
            >
              <Pin className="size-3.5" />
              固定此模型版本
            </Button>
          )}
        </div>
      ) : null}
      <ArtifactDetailDrawer
        artifactId={selectedArtifactId}
        onClose={() => setSelectedArtifactId(undefined)}
        projectId={projectId}
        trashView={false}
      />
    </div>
  );
}

function ExperimentReferenceBrowser({
  canEdit,
  experimentId,
  onExperimentChange,
  onPin,
  projectId,
  references,
}: Readonly<{
  canEdit: boolean;
  experimentId: string;
  onExperimentChange: (id: string) => void;
  onPin: (
    input: Omit<
      ArticleReference,
      "created_at" | "created_by" | "project_id" | "reference_id"
    >,
  ) => void;
  projectId: string;
  references: ArticleReference[];
}>) {
  const [selectedPath, setSelectedPath] = useState<string>();
  const experiments = useQuery({
    queryFn: () => experimentApi.list(projectId),
    queryKey: ["article-reference-experiments", projectId],
  });
  const current = experiments.data?.items.find(
    (item) => item.experiment_id === experimentId,
  );
  const result = useQuery({
    enabled: Boolean(experimentId),
    queryFn: () => experimentApi.result(projectId, experimentId),
    queryKey: ["article-reference-result", projectId, experimentId],
  });
  const selectedFile = result.data?.files.find(
    (file) => file.path === selectedPath,
  );
  const versionId = immutableExperimentVersion(result.data);
  const existing = references.find(
    (item) =>
      item.reference_type === "experiment_result" &&
      item.source_object_id === experimentId &&
      item.source_version_id === versionId,
  );

  if (!experimentId) {
    return (
      <BrowserList label="选择实验记录">
        {(experiments.data?.items ?? []).map((item) => (
          <button
            className="w-full rounded-md border p-2 text-left hover:bg-muted/60"
            key={item.experiment_id}
            onClick={() => {
              onExperimentChange(item.experiment_id);
              setSelectedPath(undefined);
            }}
            type="button"
          >
            <span className="flex items-center gap-2">
              <span className="min-w-0 flex-1 truncate text-sm font-medium">
                {item.name}
              </span>
              <Badge>{item.execution_status}</Badge>
            </span>
            <span className="mt-1 flex items-center gap-1 truncate text-[11px] text-muted-foreground">
              <GitCommitHorizontal className="size-3" />
              {item.source_commit.slice(0, 12)} ·{" "}
              {new Date(item.created_at).toLocaleString()}
            </span>
          </button>
        ))}
      </BrowserList>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      <PathBar
        onBack={() => onExperimentChange("")}
        parts={["experiments", current?.name ?? experimentId, "结果"]}
      />
      {current && result.data ? (
        result.data.files.length ? (
          <div className="grid min-h-0 flex-1 gap-2 rounded-md border p-2 xl:grid-cols-[12rem_minmax(0,1fr)]">
            <ResultFileTree
              files={result.data.files}
              onSelect={setSelectedPath}
              selectedPath={selectedFile?.path}
              storageKey={`article-reference-tree:${projectId}:${experimentId}`}
            />
            <div className="min-h-0 min-w-0 overflow-auto rounded border bg-background p-2">
              {selectedFile ? (
                <ResultFilePreview
                  current={current}
                  file={selectedFile}
                  projectId={projectId}
                />
              ) : (
                <p className="p-3 text-xs text-muted-foreground">
                  从文件树选择一个文件预览。
                </p>
              )}
            </div>
          </div>
        ) : (
          <EmptyReference label="该实验结果没有可展示的文件。" />
        )
      ) : (
        <p className="text-xs text-muted-foreground">正在读取实验结果…</p>
      )}
      {current && result.data ? (
        existing ? (
          <InsertReferenceButton canEdit={canEdit} reference={existing} />
        ) : (
          <Button
            disabled={!canEdit || !versionId}
            onClick={() =>
              onPin({
                metadata: {
                  execution_bundle: result.data!.execution_bundle,
                  files: result.data!.files,
                  result_commit_sha: result.data!.result_commit_sha,
                  result_manifest_sha256: result.data!.result_manifest_sha256,
                },
                reference_type: "experiment_result",
                source_object_id: experimentId,
                source_version_id: versionId!,
                title: current.name,
              })
            }
            size="sm"
            variant="outline"
          >
            <Pin className="size-3.5" />
            固定此实验结果
          </Button>
        )
      ) : null}
      {result.data && !versionId ? (
        <p className="text-xs text-destructive">
          此实验尚未绑定 result commit、Execution Bundle Version 或文件 Artifact
          Version，不能作为不可变参考。
        </p>
      ) : null}
    </div>
  );
}

function PathBar({
  onBack,
  parts,
}: Readonly<{ onBack: () => void; parts: string[] }>) {
  return (
    <div className="flex min-w-0 items-center gap-1 rounded-md border bg-muted/30 px-2 py-1.5 font-mono text-[11px]">
      <button
        aria-label="返回上一级"
        className="rounded p-1 hover:bg-muted"
        onClick={onBack}
        type="button"
      >
        <ArrowLeft className="size-3.5" />
      </button>
      {parts.map((part, index) => (
        <span className="contents" key={`${part}-${index}`}>
          {index ? <span className="text-muted-foreground">/</span> : null}
          <span className="truncate">{part}</span>
        </span>
      ))}
    </div>
  );
}

function BrowserList({
  children,
  label,
}: Readonly<{ children: ReactNode; label: string }>) {
  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      <p className="flex shrink-0 items-center gap-1 text-[11px] font-medium text-muted-foreground">
        <ListTree className="size-3.5" />
        {label}
      </p>
      <div className="min-h-0 flex-1 space-y-1 overflow-auto">{children}</div>
    </div>
  );
}

function InsertReferenceButton({
  canEdit,
  reference,
}: Readonly<{ canEdit: boolean; reference: ArticleReference }>) {
  return (
    <Button
      disabled={!canEdit}
      onClick={() => insertReference(reference)}
      size="sm"
      variant="outline"
    >
      <Plus className="size-3.5" />
      插入编辑器
    </Button>
  );
}

function insertReference(reference: ArticleReference) {
  const detail = versionedReferenceInsert(reference);
  if (!detail) return;
  window.dispatchEvent(
    new CustomEvent<ArticleVersionedReferenceInsert>(
      articleInsertReferenceEvent,
      { detail },
    ),
  );
}

function PinnedReference({
  canEdit,
  onOpen,
  onRemove,
  reference,
}: Readonly<{
  canEdit: boolean;
  onOpen: () => void;
  onRemove: () => void;
  reference: ArticleReference;
}>) {
  return (
    <div className="rounded-md border p-2">
      <button
        className="flex w-full items-center gap-2 text-left"
        onClick={onOpen}
        type="button"
      >
        <Link2 className="size-3.5 shrink-0" />
        <span className="min-w-0 flex-1 truncate text-sm font-medium">
          {reference.title}
        </span>
        <ChevronRight className="size-3.5 text-muted-foreground" />
      </button>
      <p className="mt-1 truncate font-mono text-[10px] text-muted-foreground">
        {reference.source_version_id}
      </p>
      <div className="mt-2 flex gap-1">
        <InsertReferenceButton canEdit={canEdit} reference={reference} />
        {canEdit ? (
          <Button onClick={onRemove} size="sm" variant="ghost">
            移除
          </Button>
        ) : null}
      </div>
    </div>
  );
}

function EmptyReference({ label }: Readonly<{ label: string }>) {
  return (
    <div className="rounded-md border border-dashed p-4 text-center text-xs text-muted-foreground">
      <FileArchive className="mx-auto mb-2 size-5" />
      {label}
    </div>
  );
}
