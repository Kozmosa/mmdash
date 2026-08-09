"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, RefreshCw, X } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

import type { AgentInstance } from "./types";
import {
  contextProposalApi,
  type ContextProposal,
  type ContextProposalDecision,
} from "./context-proposal-api";

export const contextProposalQueryKey = (projectId: string) =>
  ["context-proposals", projectId] as const;

export function ContextProposalReviewPanel({
  canReview,
  instances,
  projectId,
}: {
  canReview: boolean;
  instances: AgentInstance[];
  projectId: string;
}) {
  const queryClient = useQueryClient();
  const proposals = useQuery({
    enabled: canReview,
    queryFn: () => contextProposalApi.list(projectId),
    queryKey: contextProposalQueryKey(projectId),
  });
  const pendingAgentProposals = useMemo(
    () =>
      (proposals.data?.items ?? []).filter(
        (proposal) =>
          proposal.status === "pending" &&
          proposal.proposed_by_actor_kind === "agent",
      ),
    [proposals.data?.items],
  );
  const instanceNames = useMemo(
    () =>
      new Map(
        instances.map((instance) => [
          instance.agent_instance_id,
          instance.display_name,
        ]),
      ),
    [instances],
  );
  const review = useMutation({
    mutationFn: ({
      decision,
      proposalId,
      reviewNote,
    }: {
      decision: ContextProposalDecision;
      proposalId: string;
      reviewNote?: string;
    }) =>
      contextProposalApi.review(projectId, proposalId, decision, reviewNote),
    onError: (error) =>
      toast.error(
        error instanceof Error ? error.message : "Context Proposal 审核失败",
      ),
    onSuccess: (reviewed) => {
      queryClient.setQueryData<{ items: ContextProposal[] }>(
        contextProposalQueryKey(projectId),
        (current) =>
          current
            ? {
                items: current.items.map((proposal) =>
                  proposal.proposal_id === reviewed.proposal_id
                    ? reviewed
                    : proposal,
                ),
              }
            : current,
      );
      toast.success(
        reviewed.status === "accepted"
          ? "项目上下文已确认"
          : "Context Proposal 已拒绝",
      );
    },
  });

  return (
    <Card>
      <CardHeader>
        <div className="flex items-start justify-between gap-3">
          <div>
            <CardTitle className="text-base">待审项目上下文</CardTitle>
            <CardDescription>
              Agent 只能提交提议；接受或拒绝始终由有权限的人完成。
            </CardDescription>
          </div>
          {canReview ? (
            <Button
              aria-label="刷新待审项目上下文"
              disabled={proposals.isFetching}
              onClick={() => void proposals.refetch()}
              size="icon"
              variant="ghost"
            >
              <RefreshCw aria-hidden="true" className="size-4" />
            </Button>
          ) : null}
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {!canReview ? (
          <p className="text-xs text-muted-foreground">
            当前项目角色没有上下文审核权限。
          </p>
        ) : null}
        {canReview && proposals.isLoading ? (
          <p className="text-xs text-muted-foreground">正在读取待审提议…</p>
        ) : null}
        {canReview && proposals.isError ? (
          <div className="space-y-2 rounded-md border border-destructive/30 p-3">
            <p className="text-xs text-destructive">待审提议读取失败。</p>
            <Button
              onClick={() => void proposals.refetch()}
              size="sm"
              variant="outline"
            >
              重试
            </Button>
          </div>
        ) : null}
        {canReview &&
        !proposals.isLoading &&
        !proposals.isError &&
        pendingAgentProposals.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            当前没有 Agent 创建的待审 Context Proposal。
          </p>
        ) : null}
        {canReview
          ? pendingAgentProposals.map((proposal) => (
              <ContextProposalCard
                canReview={canReview}
                instanceName={instanceNames.get(
                  proposal.proposed_by_actor_id ?? proposal.proposed_by,
                )}
                key={proposal.proposal_id}
                onReview={(decision, reviewNote) =>
                  review.mutate({
                    decision,
                    proposalId: proposal.proposal_id,
                    reviewNote,
                  })
                }
                pending={
                  review.isPending &&
                  review.variables?.proposalId === proposal.proposal_id
                }
                proposal={proposal}
              />
            ))
          : null}
      </CardContent>
    </Card>
  );
}

function ContextProposalCard({
  canReview,
  instanceName,
  onReview,
  pending,
  proposal,
}: {
  canReview: boolean;
  instanceName?: string;
  onReview: (decision: ContextProposalDecision, reviewNote?: string) => void;
  pending: boolean;
  proposal: ContextProposal;
}) {
  const [reviewNote, setReviewNote] = useState("");
  const actorId = proposal.proposed_by_actor_id ?? proposal.proposed_by;

  return (
    <article className="space-y-3 rounded-lg border border-border p-3">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <h3 className="break-words text-sm font-medium">{proposal.title}</h3>
          <p className="mt-1 text-[11px] text-muted-foreground">
            {instanceName ?? "Agent"} · {formatTimestamp(proposal.created_at)}
          </p>
        </div>
        <Badge>{proposal.context_type}</Badge>
      </div>
      <p className="whitespace-pre-wrap break-words text-sm leading-5">
        {proposal.content}
      </p>
      {proposal.rationale ? (
        <p className="rounded-md bg-muted p-2 text-xs text-muted-foreground">
          依据：{proposal.rationale}
        </p>
      ) : null}
      <details className="text-xs text-muted-foreground">
        <summary className="cursor-pointer">
          查看 Agent / Session / Run 来源
        </summary>
        <dl className="mt-2 grid gap-2 rounded-md bg-muted p-2">
          <ProvenanceRow label="Agent" value={actorId} />
          {proposal.agent_session_id ? (
            <ProvenanceRow label="Session" value={proposal.agent_session_id} />
          ) : null}
          {proposal.agent_run_id ? (
            <ProvenanceRow label="Run" value={proposal.agent_run_id} />
          ) : null}
          {proposal.source_object_ids.length > 0 ? (
            <div>
              <dt className="font-medium text-foreground">来源对象</dt>
              <dd className="mt-1 space-y-1">
                {proposal.source_object_ids.map((sourceId) => (
                  <code className="block break-all" key={sourceId}>
                    {sourceId}
                  </code>
                ))}
              </dd>
            </div>
          ) : null}
        </dl>
      </details>
      {canReview ? (
        <div className="space-y-2 border-t border-border pt-3">
          <label className="block text-xs text-muted-foreground">
            审核备注（可选）
            <textarea
              aria-label={`审核备注：${proposal.title}`}
              className="mt-1 min-h-16 w-full resize-y rounded-md border border-input bg-background p-2 text-xs text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
              disabled={pending}
              maxLength={2_000}
              onChange={(event) => setReviewNote(event.target.value)}
              value={reviewNote}
            />
          </label>
          <div className="flex flex-wrap gap-2">
            <Button
              aria-label={`接受提议：${proposal.title}`}
              disabled={pending}
              onClick={() =>
                onReview("accepted", reviewNote.trim() || undefined)
              }
              size="sm"
            >
              <Check aria-hidden="true" className="size-3" />
              接受
            </Button>
            <Button
              aria-label={`拒绝提议：${proposal.title}`}
              disabled={pending}
              onClick={() =>
                onReview("rejected", reviewNote.trim() || undefined)
              }
              size="sm"
              variant="outline"
            >
              <X aria-hidden="true" className="size-3" />
              拒绝
            </Button>
          </div>
        </div>
      ) : (
        <p className="border-t border-border pt-3 text-xs text-muted-foreground">
          当前项目角色没有上下文审核权限。
        </p>
      )}
    </article>
  );
}

function ProvenanceRow({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="font-medium text-foreground">{label}</dt>
      <dd>
        <code className="break-all">{value}</code>
      </dd>
    </div>
  );
}

function formatTimestamp(value: string): string {
  const timestamp = new Date(value);
  return Number.isNaN(timestamp.getTime())
    ? value
    : timestamp.toLocaleString("zh-CN", { hour12: false });
}
