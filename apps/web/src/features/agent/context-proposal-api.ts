import { apiClient } from "@/lib/api-client";

export type ContextProposalDecision = "accepted" | "rejected";
export type ContextProposalStatus = "accepted" | "pending" | "rejected";

export type ContextProposal = {
  agent_run_id?: string;
  agent_session_id?: string;
  content: string;
  context_type: string;
  created_at: string;
  project_id: string;
  promoted_context_id?: string;
  proposal_id: string;
  proposed_by: string;
  proposed_by_actor_id?: string;
  proposed_by_actor_kind?: "agent" | "api" | "session";
  rationale: string;
  review_note: string;
  reviewed_at?: string;
  reviewed_by?: string;
  source_object_ids: string[];
  status: ContextProposalStatus;
  title: string;
  updated_at: string;
};

export const contextProposalApi = {
  list(projectId: string) {
    return apiClient.request<{ items: ContextProposal[] }>(
      `/projects/${encodeURIComponent(projectId)}/context/proposals`,
    );
  },
  review(
    projectId: string,
    proposalId: string,
    decision: ContextProposalDecision,
    reviewNote?: string,
  ) {
    return apiClient.request<ContextProposal>(
      `/projects/${encodeURIComponent(projectId)}/context/proposals/${encodeURIComponent(proposalId)}/review`,
      {
        body: {
          decision,
          ...(reviewNote ? { review_note: reviewNote } : {}),
        },
        method: "POST",
      },
    );
  },
};
