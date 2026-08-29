"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Check, Clock3, Copy, ExternalLink, FileArchive, GitCompareArrows, ListTree, Save, Tag } from "lucide-react";
import katex from "katex";
import dynamic from "next/dynamic";
import Image from "next/image";
import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

import { useCurrentProject } from "@/components/providers/project-provider";
import { EmptyState } from "@/components/states/empty-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/cn";
import { apiClient } from "@/lib/api-client";
import { artifactApi } from "@/features/artifact/artifact-api";

import { RefreshIcon } from "./refresh-icon";
import type { ModelBlock, ModelDiff, ModelQuestionDetail, ModelRichText, ModelSnapshot } from "./types";

const builtInTags = ["初稿", "修订中", "最终版"];
const ArtifactDetailDrawer = dynamic(
  () => import("@/features/artifact/artifact-detail-drawer").then((module) => module.ArtifactDetailDrawer),
  { ssr: false },
);

export function ModelQuestionPage({ questionId }: { questionId: string }) {
  const project = useCurrentProject();
  const queryClient = useQueryClient();
  const base = `/projects/${encodeURIComponent(project.id)}/models/questions/${encodeURIComponent(questionId)}`;
  const [selectedId, setSelectedId] = useState<string>();
  const [selectedArtifactId, setSelectedArtifactId] = useState<string>();
  const [showDiff, setShowDiff] = useState(false);
  const [diffFromId, setDiffFromId] = useState<string>();
  const [diffToId, setDiffToId] = useState<string>();
  const detail = useQuery({
    queryFn: () => apiClient.request<ModelQuestionDetail>(base),
    queryKey: ["model-question", project.id, questionId],
    refetchInterval: (query) => ["queued", "running"].includes((query.state.data as ModelQuestionDetail | undefined)?.question.sync_status ?? "") ? 1_500 : false,
  });
  const activeId = selectedId ?? detail.data?.question.latest_snapshot_id;
  const selectedSummary = detail.data?.snapshots.find((item) => item.snapshot_id === activeId);
  const snapshot = useQuery({
    enabled: Boolean(activeId && activeId !== detail.data?.latest_snapshot?.snapshot_id),
    queryFn: () => apiClient.request<ModelSnapshot>(`${base}/snapshots/${encodeURIComponent(activeId!)}`),
    queryKey: ["model-snapshot", project.id, questionId, activeId],
  });
  const activeSnapshot = detail.data && activeId === detail.data.latest_snapshot?.snapshot_id ? detail.data.latest_snapshot : snapshot.data;
  const diff = useQuery({
    enabled: Boolean(showDiff && diffFromId && diffToId && diffFromId !== diffToId),
    queryFn: () => apiClient.request<ModelDiff>(`${base}/diff`, { query: { from_snapshot_id: diffFromId, to_snapshot_id: diffToId } }),
    queryKey: ["model-diff", project.id, questionId, diffFromId, diffToId],
  });
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ["model-question", project.id, questionId] });
  const sync = useMutation({ mutationFn: () => apiClient.request(`${base}/sync`, { method: "POST" }), onSuccess: async () => { setSelectedId(undefined); await invalidate(); toast.success("模型同步已进入队列，自动同步倒计时已重置"); } });

  if (detail.isLoading) return <p className="text-sm text-muted-foreground">正在读取模型版本…</p>;
  if (detail.error || !detail.data) return <p className="text-sm text-destructive">{detail.error?.message ?? "模型不存在"}</p>;
  const question = detail.data.question;
  const toggleDiff = () => {
    if (showDiff) {
      setShowDiff(false);
      return;
    }
    const to = activeId ?? detail.data.question.latest_snapshot_id ?? detail.data.snapshots[0]?.snapshot_id;
    const from = selectedSummary?.previous_snapshot_id ?? detail.data.snapshots.find((item) => item.snapshot_id !== to)?.snapshot_id;
    setDiffFromId(from);
    setDiffToId(to);
    setShowDiff(true);
  };

  return (
    <>
    <section className="space-y-4" aria-labelledby="model-question-title">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div><Link className="mb-3 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground" href={`/projects/${encodeURIComponent(project.id)}/models`}><ArrowLeft className="size-4" />全部题号</Link><div className="flex flex-wrap items-center gap-2"><h1 className="text-2xl font-semibold tracking-tight" id="model-question-title">{question.code} · {question.title}</h1><Badge>{question.sync_status}</Badge></div><a className="mt-1 inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground" href={question.notion_page_url} rel="noreferrer" target="_blank">打开 Notion 页面<ExternalLink className="size-3" /></a></div>
        <Button disabled={sync.isPending} onClick={() => sync.mutate()}><RefreshIcon spinning={sync.isPending} />同步</Button>
      </header>
      {question.last_error_message ? <p className="rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">{question.last_error_message}</p> : null}
      {!detail.data.snapshots.length ? <EmptyState description="点击右上角同步；Worker 读取绑定的 Notion 子页面后会生成第一个不可变快照。" title="尚无模型版本" /> : (
        <div className="grid items-start gap-4 xl:grid-cols-[minmax(10rem,1fr)_minmax(0,6fr)_minmax(16rem,3fr)]">
          <aside className="space-y-2 xl:sticky xl:top-4"><h2 className="px-1 text-xs font-semibold uppercase tracking-wider text-muted-foreground">版本时间线</h2>{detail.data.snapshots.map((item, index) => <button className={`w-full rounded-lg border p-3 text-left transition-colors ${item.snapshot_id === activeId ? "border-primary bg-primary/5" : "border-border hover:bg-muted/50"}`} key={item.snapshot_id} onClick={() => { setSelectedId(item.snapshot_id); setShowDiff(false); }} type="button"><span className="block text-sm font-medium">版本 {detail.data.snapshots.length - index}</span><span className="mt-1 block text-xs text-muted-foreground">{new Date(item.captured_at).toLocaleString()}</span>{item.tags.length ? <span className="mt-2 flex flex-wrap gap-1">{item.tags.map((value) => <Badge key={value}>{value}</Badge>)}</span> : null}</button>)}</aside>
          <main className="min-w-0"><Card className="overflow-hidden"><CardHeader className="space-y-3 border-b border-border"><div className="flex flex-wrap items-center justify-between gap-2"><div><CardTitle>{activeSnapshot?.title ?? selectedSummary?.title}</CardTitle><p className="mt-1 text-xs text-muted-foreground">与 Notion 对齐的无行号文档视图</p></div><Button disabled={detail.data.snapshots.length < 2} onClick={toggleDiff} size="sm" variant={showDiff ? "secondary" : "outline"}><GitCompareArrows className="size-4" />{showDiff ? "查看文档" : "比较版本"}</Button></div>{showDiff ? <DiffVersionPicker fromId={diffFromId} onFromChange={setDiffFromId} onToChange={setDiffToId} snapshots={detail.data.snapshots} toId={diffToId} /> : null}</CardHeader><CardContent className="p-0"><article className="mx-auto max-w-3xl px-8 py-10 sm:px-12">{showDiff ? <DiffDocument diff={diff.data} invalid={!diffFromId || !diffToId || diffFromId === diffToId} loading={diff.isLoading} /> : activeSnapshot ? <ModelDocument assets={activeSnapshot.assets} blocks={activeSnapshot.blocks} onArtifactOpen={setSelectedArtifactId} projectId={project.id} /> : <p className="text-sm text-muted-foreground">正在读取版本…</p>}</article></CardContent></Card></main>
          <SnapshotInfo base={base} onSaved={async () => { await invalidate(); if (activeId) await queryClient.invalidateQueries({ queryKey: ["model-snapshot", project.id, questionId, activeId] }); }} snapshot={activeSnapshot} />
        </div>
      )}
    </section>
    <ArtifactDetailDrawer artifactId={selectedArtifactId} onClose={() => setSelectedArtifactId(undefined)} projectId={project.id} trashView={false} />
    </>
  );
}

function SnapshotInfo({ base, onSaved, snapshot }: { base: string; onSaved: () => Promise<void>; snapshot?: ModelSnapshot }) {
  const [tags, setTags] = useState("");
  const [note, setNote] = useState("");
  useEffect(() => { setTags(snapshot?.tags.join(", ") ?? ""); setNote(snapshot?.version_note ?? ""); }, [snapshot?.snapshot_id, snapshot?.tags, snapshot?.version_note]);
  const save = useMutation({ mutationFn: () => apiClient.request(`${base}/snapshots/${encodeURIComponent(snapshot!.snapshot_id)}`, { body: { tags: tags.split(/[,，]/).map((value) => value.trim()).filter(Boolean), version_note: note }, method: "PATCH" }), onSuccess: async () => { await onSaved(); toast.success("版本 Tag 与说明已保存"); } });
  if (!snapshot) return <aside />;
  return <aside className="space-y-4 xl:sticky xl:top-4"><Card><CardHeader><CardTitle className="text-base">文档信息</CardTitle></CardHeader><CardContent className="space-y-4"><InfoRow icon={Clock3} label="捕获时间" value={new Date(snapshot.captured_at).toLocaleString()} /><InfoRow icon={FileArchive} label="内容 Hash" value={snapshot.content_hash.slice(0, 12)} /><InfoRow icon={FileArchive} label="模型文件" value={`${snapshot.assets.length} 个`} /><div className="space-y-2"><label className="text-xs font-medium" htmlFor="model-tags">Tags</label><Input id="model-tags" onChange={(event) => setTags(event.target.value)} placeholder="初稿, 自定义标签" value={tags} /><div className="flex flex-wrap gap-1">{builtInTags.map((value) => <Button key={value} onClick={() => setTags((current) => [...current.split(/[,，]/).map((item) => item.trim()).filter(Boolean), value].filter((item, index, all) => all.indexOf(item) === index).join(", "))} size="sm" type="button" variant="outline"><Tag className="size-3" />{value}</Button>)}</div></div><div className="space-y-2"><label className="text-xs font-medium" htmlFor="version-note">版本说明</label><textarea className="min-h-28 w-full rounded-md border border-input bg-background px-3 py-2 text-sm" id="version-note" onChange={(event) => setNote(event.target.value)} placeholder="记录这次版本的人工说明" value={note} /></div>{save.error ? <p className="text-xs text-destructive">{save.error.message}</p> : null}<Button className="w-full" disabled={save.isPending} onClick={() => save.mutate()}><Save className="size-4" />保存信息</Button></CardContent></Card><DocumentOutline outline={snapshot.outline} /></aside>;
}

function InfoRow({ icon: Icon, label, value }: { icon: typeof Clock3; label: string; value: string }) { return <div className="flex items-start gap-2 text-sm"><Icon className="mt-0.5 size-4 text-muted-foreground" /><div><p className="text-xs text-muted-foreground">{label}</p><p className="break-all font-medium">{value}</p></div></div>; }

function DocumentOutline({ outline }: { outline?: ModelSnapshot["outline"] }) {
  const items = (outline ?? []).filter((item) => item.level >= 1 && item.level <= 3 && item.title.trim());
  return <Card className="flex max-h-[calc(100vh-2rem)] flex-col overflow-hidden"><CardHeader><CardTitle className="flex items-center gap-2 text-base"><ListTree className="size-4" />文档目录</CardTitle></CardHeader><CardContent className="min-h-0 overflow-y-auto">{items.length ? <nav aria-label="文档目录"><ol className="space-y-1">{items.map((item) => <li key={item.block_id}><a className="block truncate rounded px-2 py-1.5 text-sm text-muted-foreground hover:bg-muted hover:text-foreground" href={`#${headingAnchor(item.block_id)}`} style={{ paddingLeft: `${0.5 + (item.level - 1) * 0.75}rem` }}>{item.title}</a></li>)}</ol></nav> : <p className="text-sm text-muted-foreground">当前版本没有一至三级标题。</p>}</CardContent></Card>;
}

function DiffVersionPicker({ fromId, onFromChange, onToChange, snapshots, toId }: { fromId?: string; onFromChange: (value: string) => void; onToChange: (value: string) => void; snapshots: ModelQuestionDetail["snapshots"]; toId?: string }) {
  const optionLabel = (index: number, capturedAt: string) => `版本 ${snapshots.length - index} · ${new Date(capturedAt).toLocaleString()}`;
  return <div className="grid gap-3 rounded-lg bg-muted/50 p-3 sm:grid-cols-[1fr_auto_1fr]"><label className="space-y-1 text-xs font-medium"><span>基准版本</span><select aria-label="Diff 基准版本" className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm" onChange={(event) => onFromChange(event.target.value)} value={fromId ?? ""}><option disabled value="">选择版本</option>{snapshots.map((item, index) => <option disabled={item.snapshot_id === toId} key={item.snapshot_id} value={item.snapshot_id}>{optionLabel(index, item.captured_at)}</option>)}</select></label><span className="self-end pb-2 text-center text-xs text-muted-foreground">比较到</span><label className="space-y-1 text-xs font-medium"><span>目标版本</span><select aria-label="Diff 目标版本" className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm" onChange={(event) => onToChange(event.target.value)} value={toId ?? ""}><option disabled value="">选择版本</option>{snapshots.map((item, index) => <option disabled={item.snapshot_id === fromId} key={item.snapshot_id} value={item.snapshot_id}>{optionLabel(index, item.captured_at)}</option>)}</select></label></div>;
}

type ModelAsset = ModelSnapshot["assets"][number];

export function ModelDocument({ assets, blocks, onArtifactOpen, projectId }: { assets: ModelAsset[]; blocks: ModelBlock[]; onArtifactOpen: (artifactId: string) => void; projectId: string }) {
  const assetsByBlock = useMemo(() => new Map(assets.map((asset) => [asset.source_block_id, asset])), [assets]);
  return <ModelBlocks assetsByBlock={assetsByBlock} blocks={blocks} onArtifactOpen={onArtifactOpen} projectId={projectId} />;
}

function ModelBlocks({ assetsByBlock, blocks, onArtifactOpen, projectId }: { assetsByBlock: Map<string, ModelAsset>; blocks: ModelBlock[]; onArtifactOpen: (artifactId: string) => void; projectId: string }) { return <div className="space-y-1">{blocks.map((block) => <ModelBlockView assetsByBlock={assetsByBlock} block={block} key={block.block_id} onArtifactOpen={onArtifactOpen} projectId={projectId} />)}</div>; }

export function extractModelBlockText(block: ModelBlock): string {
  if (block.type === "equation") {
    return `$$${block.expression ?? block.text ?? ""}$$`;
  }
  if (block.type === "code") {
    return block.text ?? "";
  }
  if (block.type === "table") {
    if (block.children && block.children.length > 0) {
      const rows = block.children.map((child) => {
        if (child.cells) {
          return child.cells
            .map((cell) => cell.map((p) => p.text).join(""))
            .join("\t");
        }
        if (child.rows?.[0]) {
          return child.rows[0].join("\t");
        }
        return child.text;
      });
      return rows.join("\n");
    }
    return block.text ?? "";
  }
  if (["bookmark", "link_preview"].includes(block.type)) {
    return block.url || block.text || block.caption || "";
  }
  if (["file", "pdf", "image"].includes(block.type)) {
    return block.caption || block.text || "";
  }
  if (block.rich_text && block.rich_text.length > 0) {
    return block.rich_text
      .map((part) => (part.expression ? `$${part.expression}$` : part.text))
      .join("");
  }
  return block.text ?? "";
}

function escapeHtml(str: string): string {
  return str
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function extractRichTextHtml(parts: ModelRichText[] = []): string {
  return parts
    .map((part) => {
      if (part.expression) {
        return `<span data-type="inlineMath" data-latex="${escapeHtml(part.expression)}">$${escapeHtml(part.expression)}$</span>`;
      }
      let content = escapeHtml(part.text);
      if (part.code) content = `<code>${content}</code>`;
      if (part.bold) content = `<strong>${content}</strong>`;
      if (part.italic) content = `<em>${content}</em>`;
      if (part.underline) content = `<u>${content}</u>`;
      if (part.strikethrough) content = `<s>${content}</s>`;
      if (part.href) content = `<a href="${escapeHtml(part.href)}">${content}</a>`;
      return content;
    })
    .join("");
}

export function modelBlockToClipboardData(
  block: ModelBlock,
  asset?: ModelAsset,
  displayName?: string,
): {
  artifact?: {
    artifactId: string;
    versionId: string;
    filename: string;
    mimeType: string;
    title: string;
  };
  html: string;
  text: string;
} {
  const artifactId = block.artifact_id ?? asset?.artifact_id;
  const versionId = block.artifact_version_id ?? asset?.artifact_version_id;

  if (block.type === "image" && artifactId && versionId) {
    const title = displayName || block.caption || asset?.filename || "模型图片";
    const filename = asset?.filename || "image.png";
    const mimeType = asset?.mime_type || "image/png";
    const artifact = {
      artifactId,
      filename,
      mimeType,
      title,
      versionId,
    };
    return {
      artifact,
      html: `<figure data-artifact-reference="${escapeHtml(artifactId)}" data-article-artifact-image="true" data-artifact-id="${escapeHtml(artifactId)}" data-version-id="${escapeHtml(versionId)}" data-mime-type="${escapeHtml(mimeType)}" data-title="${escapeHtml(title)}" data-filename="${escapeHtml(filename)}" class="article-reference my-3"><aside data-artifact-reference="${escapeHtml(artifactId)}" class="rounded-lg border border-dashed bg-muted/20 p-3 text-sm">${escapeHtml(title)} · 图片预览</aside></figure>`,
      text: `![${title}](artifact://${artifactId}?version=${versionId})`,
    };
  }

  if (block.type === "equation") {
    const latex = block.expression ?? block.text ?? "";
    return {
      html: `<div data-type="blockMath" data-latex="${escapeHtml(latex)}">$$${escapeHtml(latex)}$$</div>`,
      text: `$$${latex}$$`,
    };
  }
  if (block.type === "code") {
    const code = block.text ?? "";
    return {
      html: `<pre><code>${escapeHtml(code)}</code></pre>`,
      text: code,
    };
  }
  if (block.type === "quote") {
    const text = extractModelBlockText(block);
    const innerHtml = extractRichTextHtml(block.rich_text) || escapeHtml(text);
    return {
      html: `<blockquote><p>${innerHtml}</p></blockquote>`,
      text,
    };
  }
  if (block.type.startsWith("heading_")) {
    const level = block.level ?? 1;
    const tag = level === 1 ? "h1" : level === 2 ? "h2" : "h3";
    const text = extractModelBlockText(block);
    const innerHtml = extractRichTextHtml(block.rich_text) || escapeHtml(text);
    return {
      html: `<${tag}>${innerHtml}</${tag}>`,
      text,
    };
  }
  const text = extractModelBlockText(block);
  const innerHtml = extractRichTextHtml(block.rich_text) || escapeHtml(text);
  return {
    html: `<p>${innerHtml}</p>`,
    text,
  };
}

export function ModelBlockCopyButton({
  asset,
  block,
  displayName,
}: {
  asset?: ModelAsset;
  block: ModelBlock;
  displayName?: string;
}) {
  const [copied, setCopied] = useState(false);

  const copy = async (event: React.MouseEvent<HTMLButtonElement>) => {
    event.stopPropagation();
    const { artifact, html, text } = modelBlockToClipboardData(
      block,
      asset,
      displayName,
    );
    if (!text && !html && !artifact) return;
    try {
      if (typeof ClipboardItem !== "undefined" && navigator.clipboard?.write) {
        const itemData: Record<string, Blob> = {
          "text/html": new Blob([html], { type: "text/html" }),
          "text/plain": new Blob([text], { type: "text/plain" }),
        };
        if (artifact) {
          itemData["application/json"] = new Blob([JSON.stringify(artifact)], {
            type: "application/json",
          });
        }
        const item = new ClipboardItem(itemData);
        await navigator.clipboard.write([item]);
      } else if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text);
      }
      setCopied(true);
      toast.success("已复制内容");
      setTimeout(() => setCopied(false), 2000);
    } catch {
      try {
        await navigator.clipboard.writeText(text);
        setCopied(true);
        toast.success("已复制内容");
        setTimeout(() => setCopied(false), 2000);
      } catch {
        toast.error("复制失败");
      }
    }
  };

  return (
    <button
      aria-label="复制块内容"
      className={cn(
        "absolute right-1 top-1 z-10 flex size-6 items-center justify-center rounded border border-border/70 bg-background/90 text-muted-foreground shadow-2xs transition-opacity duration-[1ms] hover:bg-muted hover:text-foreground focus-visible:opacity-100",
        copied
          ? "opacity-100 text-green-600"
          : "opacity-0 group-hover:opacity-100",
      )}
      onClick={copy}
      title={copied ? "已复制" : "复制"}
      type="button"
    >
      {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
    </button>
  );
}

function ModelBlockView({ assetsByBlock, block, onArtifactOpen, projectId }: { assetsByBlock: Map<string, ModelAsset>; block: ModelBlock; onArtifactOpen: (artifactId: string) => void; projectId: string }) {
  const content = <>{block.rich_text.length ? block.rich_text.map((part, index) => <RichTextView key={`${block.block_id}-${index}`} part={part} />) : block.text}</>;
  const asset = assetsByBlock.get(block.block_id);
  const artifactId = block.artifact_id ?? asset?.artifact_id;
  const artifactVersionId = block.artifact_version_id ?? asset?.artifact_version_id;
  const detail = useQuery({
    enabled: Boolean(artifactId),
    queryFn: () => artifactApi.get(projectId, artifactId!),
    queryKey: ["artifact", projectId, artifactId, "active"],
  });
  // The Artifact 展示名称 is live metadata; once it differs from the imported
  // Notion filename the document should display the user's rename instead of
  // the immutable snapshot caption/filename.
  const renamed = Boolean(detail.data?.artifact?.name && asset?.filename && detail.data.artifact.name !== asset.filename);
  const displayName = renamed ? detail.data!.artifact.name : undefined;
  const documentTitle = displayName ?? (block.caption || asset?.filename || (block.type === "pdf" ? "Notion PDF" : "Notion 文件"));
  let node;
  if (block.type.startsWith("heading_")) { const level = block.level ?? 1; const id = headingAnchor(block.block_id); node = level === 1 ? <h1 className="mb-2 mt-7 scroll-mt-6 text-3xl font-bold" id={id}>{content}</h1> : level === 2 ? <h2 className="mb-1 mt-6 scroll-mt-6 text-2xl font-semibold" id={id}>{content}</h2> : <h3 className="mb-1 mt-5 scroll-mt-6 text-xl font-semibold" id={id}>{content}</h3>; }
  else if (block.type === "bulleted_list_item") node = <div className="flex gap-3 py-1"><span>•</span><p>{content}</p></div>;
  else if (block.type === "numbered_list_item") node = <div className="flex gap-3 py-1"><span>1.</span><p>{content}</p></div>;
  else if (block.type === "to_do") node = <label className="flex gap-3 py-1"><input checked={Boolean(block.checked)} readOnly type="checkbox" /><span>{content}</span></label>;
  else if (block.type === "quote") node = <blockquote className="my-2 border-l-4 border-foreground pl-4">{content}</blockquote>;
  else if (block.type === "code") node = <pre className="my-3 overflow-x-auto rounded-lg bg-muted p-4 text-sm"><code>{block.text}</code></pre>;
  else if (block.type === "equation") node = <MathExpression displayMode expression={block.expression ?? block.text} />;
  else if (block.type === "table") { const rows = block.children.map((child) => child.cells ?? (child.rows?.[0] ?? []).map((text) => [{ text }])); node = <div className="my-4 overflow-x-auto"><table className="w-full border-collapse text-sm"><tbody>{rows.map((row, rowIndex) => <tr key={`${block.block_id}-row-${rowIndex}`}>{row.map((cell, cellIndex) => <td className="border border-border px-3 py-2 align-top" key={`${block.block_id}-${rowIndex}-${cellIndex}`}>{cell.map((part, partIndex) => <RichTextView key={`${block.block_id}-${rowIndex}-${cellIndex}-${partIndex}`} part={part} />)}</td>)}</tr>)}</tbody></table></div>; }
  else if (["bookmark", "link_preview"].includes(block.type) && block.url) node = <a className="my-3 flex items-center justify-between gap-4 rounded-lg border border-border p-4 hover:bg-muted/50" href={block.url} rel="noreferrer" target="_blank"><span className="min-w-0"><span className="block truncate font-medium">{block.caption || bookmarkTitle(block.url)}</span><span className="mt-1 block truncate text-xs text-muted-foreground">{block.url}</span></span><ExternalLink className="size-4 shrink-0 text-muted-foreground" /></a>;
  else if (block.type === "synced_block") node = block.children.length ? null : <p className="my-3 rounded-lg border border-dashed border-border p-4 text-sm text-muted-foreground">同步区块尚未读取到内容，请重新同步。</p>;
  else if (block.type === "image") node = <ModelArtifactImage artifactId={artifactId} artifactVersionId={artifactVersionId} onOpen={onArtifactOpen} projectId={projectId} title={displayName ?? (block.caption || asset?.filename || "Notion 模型图片")} />;
  else if (["file", "pdf"].includes(block.type)) node = <button className="my-3 flex w-full items-center gap-3 rounded-lg border border-border p-4 text-left hover:bg-muted/50" disabled={!artifactId} onClick={() => artifactId && onArtifactOpen(artifactId)} type="button"><FileArchive className="size-5 shrink-0" /><span className="min-w-0"><span className="block truncate font-medium">{documentTitle}</span>{block.caption && asset?.filename && block.caption !== asset.filename ? <span className="block truncate text-xs text-muted-foreground">{asset.filename}</span> : null}</span></button>;
  else if (block.type === "divider") node = <hr className="my-5 border-border" />;
  else node = <p className="min-h-6 whitespace-pre-wrap py-1 leading-7">{content}</p>;

  const canCopy = block.type !== "divider" && !(block.type === "synced_block" && !block.children.length);

  return (
    <div className="group relative" data-block-id={block.block_id}>
      <div className="min-w-0 flex-1 pr-6">{node}</div>
      {canCopy ? <ModelBlockCopyButton asset={asset} block={block} displayName={displayName} /> : null}
      {block.type !== "table" && block.children.length ? (
        <div className={block.type === "synced_block" ? "" : "ml-6 border-l border-border pl-4"}>
          <ModelBlocks assetsByBlock={assetsByBlock} blocks={block.children} onArtifactOpen={onArtifactOpen} projectId={projectId} />
        </div>
      ) : null}
    </div>
  );
}

function bookmarkTitle(rawUrl: string) {
  try {
    const parsed = new URL(rawUrl);
    const path = parsed.pathname.replace(/^\//, "").replace(/\/$/, "");
    return path ? `${parsed.hostname} · ${path}` : parsed.hostname;
  } catch {
    return rawUrl;
  }
}

function headingAnchor(blockId: string) { return `model-heading-${blockId}`; }

function ModelArtifactImage({ artifactId, artifactVersionId, onOpen, projectId, title }: { artifactId?: string; artifactVersionId?: string; onOpen: (artifactId: string) => void; projectId: string; title: string }) {
  const download = useQuery({
    enabled: Boolean(artifactId),
    queryFn: () => artifactApi.download(projectId, artifactId!, artifactVersionId),
    queryKey: ["model-artifact-image", projectId, artifactId, artifactVersionId],
    refetchOnWindowFocus: false,
    staleTime: 4 * 60 * 1_000,
  });
  if (!artifactId) return <p className="my-3 rounded-lg border border-dashed border-border p-4 text-sm text-muted-foreground">图片尚未完成 Artifact 转存</p>;
  return <figure className="my-4"><button aria-label={`查看图片 Artifact：${title}`} className="block w-full overflow-hidden rounded-lg border border-border bg-muted/20 hover:border-foreground/30" onClick={() => onOpen(artifactId)} type="button">{download.data?.transfer.url ? <Image alt={title} className="h-auto max-h-[42rem] w-full object-contain" height={900} src={download.data.transfer.url} unoptimized width={1200} /> : <span className="flex min-h-40 items-center justify-center p-6 text-sm text-muted-foreground">{download.error ? "图片加载失败，点击查看 Artifact 信息" : "正在加载模型图片…"}</span>}</button>{title ? <figcaption className="mt-2 text-center text-sm text-muted-foreground">{title}</figcaption> : null}</figure>;
}

function RichTextView({ part }: { part: ModelRichText }) {
  const className = `${part.bold ? "font-semibold" : ""} ${part.italic ? "italic" : ""} ${part.strikethrough ? "line-through" : ""} ${part.underline ? "underline" : ""} ${part.code ? "rounded bg-muted px-1 font-mono text-[0.9em]" : ""}`;
  if (part.expression) return <span className={className}><MathExpression expression={part.expression} /></span>;
  return part.href ? <a className={`${className} underline-offset-2 hover:underline`} href={part.href} rel="noreferrer" target="_blank">{part.text}</a> : <span className={className}>{part.text}</span>;
}

function MathExpression({ displayMode = false, expression }: { displayMode?: boolean; expression: string }) {
  let html: string;
  try {
    html = katex.renderToString(expression, { displayMode, strict: false, throwOnError: false, trust: false });
  } catch {
    return displayMode ? <div className="my-3 overflow-x-auto text-center font-mono text-sm">{expression}</div> : <code>{expression}</code>;
  }
  const markup = { __html: html };
  return displayMode
    ? <div className="my-3 overflow-x-auto py-2 text-center" data-model-equation="block" dangerouslySetInnerHTML={markup} />
    : <span className="inline-block max-w-full align-middle" data-model-equation="inline" dangerouslySetInnerHTML={markup} />;
}

type ModelDiffBlock = ModelDiff["blocks"][number];
type ModelDiffOperation = ModelDiffBlock["operations"][number];

function DiffDocument({ diff, invalid, loading }: { diff?: ModelDiff; invalid: boolean; loading: boolean }) {
  if (invalid) return <p className="text-sm text-muted-foreground">请选择两个不同的模型版本。</p>;
  if (loading) return <p className="text-sm text-muted-foreground">正在生成字符级差异…</p>;
  if (!diff) return <p className="text-sm text-muted-foreground">正在读取版本差异…</p>;
  return <div className="space-y-1">{diff.blocks.map((block) => <DiffBlockView diffBlock={block} key={block.block_id} />)}</div>;
}

function DiffBlockView({ diffBlock }: { diffBlock: ModelDiffBlock }) {
  const block = diffBlock.block;
  const content = <DiffRuns blockId={block.block_id} operations={diffBlock.operations} />;
  const changeClass = diffBlock.change === "added" ? "border-l-2 border-blue-400 bg-blue-50/40 pl-3 dark:bg-blue-950/20" : diffBlock.change === "deleted" ? "border-l-2 border-pink-300 bg-pink-50/40 pl-3 dark:bg-pink-950/20" : "";
  let node;
  if (block.type.startsWith("heading_")) { const level = block.level ?? 1; node = level === 1 ? <h1 className="mb-2 mt-7 text-3xl font-bold">{content}</h1> : level === 2 ? <h2 className="mb-1 mt-6 text-2xl font-semibold">{content}</h2> : <h3 className="mb-1 mt-5 text-xl font-semibold">{content}</h3>; }
  else if (block.type === "bulleted_list_item") node = <div className="flex gap-3 py-1"><span>•</span><p>{content}</p></div>;
  else if (block.type === "numbered_list_item") node = <div className="flex gap-3 py-1"><span>1.</span><p>{content}</p></div>;
  else if (block.type === "to_do") node = <div className="flex gap-3 py-1"><input checked={Boolean(block.checked)} readOnly type="checkbox" /><span>{content}</span></div>;
  else if (block.type === "quote") node = <blockquote className="my-2 border-l-4 border-foreground pl-4">{content}</blockquote>;
  else if (block.type === "code") node = <pre className="my-3 overflow-x-auto rounded-lg bg-muted p-4 text-sm"><code>{content}</code></pre>;
  else if (block.type === "equation") node = <div><MathExpression displayMode expression={block.expression ?? block.text} />{diffBlock.change !== "unchanged" ? <div className="overflow-x-auto rounded-md bg-muted/50 p-2 font-mono text-xs">{content}</div> : null}</div>;
  else if (block.type === "table") return null;
  else if (block.type === "table_row") { const cells = splitDiffCells(diffBlock.operations); node = <div className="overflow-x-auto"><table className="w-full border-collapse text-sm"><tbody><tr>{cells.map((operations, index) => <td className="border border-border px-3 py-2 align-top" key={`${block.block_id}-cell-${index}`}><DiffRuns blockId={`${block.block_id}-${index}`} operations={operations} /></td>)}</tr></tbody></table></div>; }
  else if (["bookmark", "link_preview"].includes(block.type) && block.url) node = <a className="my-3 flex items-center justify-between gap-4 rounded-lg border border-border p-4 hover:bg-muted/50" href={block.url} rel="noreferrer" target="_blank"><span className="min-w-0"><span className="block truncate font-medium">{block.caption || bookmarkTitle(block.url)}</span><span className="mt-1 block truncate text-xs text-muted-foreground">{content}</span></span><ExternalLink className="size-4 shrink-0 text-muted-foreground" /></a>;
  else if (["image", "file", "pdf"].includes(block.type)) node = <div className="my-3 flex items-center gap-3 rounded-lg border border-border p-4"><FileArchive className="size-5" /><span>{block.caption || (block.type === "image" ? "模型图片" : "模型文件")}</span></div>;
  else if (block.type === "divider") node = <hr className="my-5 border-border" />;
  else if (block.type === "synced_block") return null;
  else node = <p className="min-h-6 whitespace-pre-wrap py-1 leading-7">{content}</p>;
  return <div className={changeClass} data-diff-change={diffBlock.change}>{node}</div>;
}

function DiffRuns({ blockId, operations }: { blockId: string; operations: ModelDiffOperation[] }) {
  return <>{operations.map((operation, index) => <span className={operation.kind === "added" ? "rounded-sm bg-blue-100 px-0.5 text-blue-950 dark:bg-blue-900/60 dark:text-blue-100" : operation.kind === "deleted" ? "rounded-sm bg-pink-100 px-0.5 text-pink-700/60 line-through dark:bg-pink-900/40 dark:text-pink-200/60" : ""} data-diff-kind={operation.kind} key={`${blockId}-${index}`}>{operation.text}</span>)}</>;
}

function splitDiffCells(operations: ModelDiffOperation[]) {
  const cells: ModelDiffOperation[][] = [[]];
  for (const operation of operations) {
    const parts = operation.text.split("\t");
    parts.forEach((text, index) => {
      if (text) cells[cells.length - 1].push({ ...operation, text });
      if (index < parts.length - 1) cells.push([]);
    });
  }
  return cells;
}
