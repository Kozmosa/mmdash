import { apiClient } from "@/lib/api-client";

import type {
  Box,
  Experiment,
  ExperimentLog,
  ExperimentSettings,
  ExperimentType,
  ResourceLimits,
  ResultBundle,
  RuntimePolicy,
} from "./types";

const path = (projectId: string) =>
  `/projects/${encodeURIComponent(projectId)}`;
const experimentPath = (projectId: string, experimentId: string) =>
  `${path(projectId)}/experiments/${encodeURIComponent(experimentId)}`;

export type CreateExperimentInput = {
  name: string;
  experiment_type: Exclude<ExperimentType, "box-re">;
  source_commit: string;
  entrypoint: string;
  parameters: Record<string, unknown>;
  environment: Record<string, string>;
  inputs: Record<string, unknown>;
  runtime_policy?: RuntimePolicy;
  requested_box_id?: string;
  limits_override?: ResourceLimits;
  idempotency_key: string;
};

export const experimentApi = {
  list(projectId: string, status?: string) {
    return apiClient.request<{
      items: Experiment[];
      has_more: boolean;
      next_cursor?: string;
    }>(`${path(projectId)}/experiments`, { query: { status } });
  },
  create(projectId: string, input: CreateExperimentInput) {
    return apiClient.request<Experiment>(`${path(projectId)}/experiments`, {
      body: input,
      method: "POST",
    });
  },
  get(projectId: string, experimentId: string) {
    return apiClient.request<Experiment>(
      experimentPath(projectId, experimentId),
    );
  },
  run(
    projectId: string,
    experimentId: string,
    idempotencyKey = crypto.randomUUID(),
  ) {
    return apiClient.request<Experiment>(
      `${experimentPath(projectId, experimentId)}/run`,
      {
        body: { idempotency_key: idempotencyKey },
        method: "POST",
      },
    );
  },
  rerun(
    projectId: string,
    experimentId: string,
    input: Partial<Omit<CreateExperimentInput, "experiment_type">>,
  ) {
    return apiClient.request<Experiment>(
      `${experimentPath(projectId, experimentId)}/rerun`,
      {
        body: {
          ...input,
          idempotency_key: input.idempotency_key ?? crypto.randomUUID(),
        },
        method: "POST",
      },
    );
  },
  cancel(projectId: string, experimentId: string) {
    return apiClient.request<Experiment>(
      `${experimentPath(projectId, experimentId)}/cancel`,
      { method: "POST" },
    );
  },
  async logs(projectId: string, experimentId: string) {
    const items: ExperimentLog[] = [];
    let cursor: string | undefined;
    for (;;) {
      const page = await apiClient.request<{
        items: ExperimentLog[];
        has_more: boolean;
        next_cursor?: string;
      }>(`${experimentPath(projectId, experimentId)}/logs`, {
        query: { cursor, limit: 500 },
      });
      items.push(...page.items);
      if (!page.has_more || !page.next_cursor) {
        return { has_more: false, items };
      }
      cursor = page.next_cursor;
    }
  },
  result(projectId: string, experimentId: string) {
    return apiClient.request<ResultBundle>(
      `${experimentPath(projectId, experimentId)}/result`,
    );
  },
  compare(projectId: string, experimentIds: string[]) {
    return apiClient.request<{ items: Experiment[] }>(
      `${path(projectId)}/experiments/compare`,
      {
        query: { experiment_id: experimentIds.join(",") },
      },
    );
  },
  settings(projectId: string) {
    return apiClient.request<ExperimentSettings>(
      `${path(projectId)}/experiments/settings`,
    );
  },
  updateSettings(
    projectId: string,
    input: Omit<ExperimentSettings, "project_id" | "updated_by" | "updated_at">,
  ) {
    return apiClient.request<ExperimentSettings>(
      `${path(projectId)}/experiments/settings`,
      {
        body: input,
        method: "PATCH",
      },
    );
  },
  projectBoxes(projectId: string) {
    return apiClient.request<{ items: Box[] }>(`${path(projectId)}/boxes`);
  },
  personalBoxes() {
    return apiClient.request<{ items: Box[] }>("/users/me/boxes");
  },
  renameBox(boxId: string, name: string) {
    return apiClient.request<Box>(
      `/users/me/boxes/${encodeURIComponent(boxId)}`,
      {
        body: { name },
        method: "PATCH",
      },
    );
  },
  revokeBox(boxId: string, mode: "drain" | "force") {
    return apiClient.request<{
      box: Box;
      mode: "drain" | "force";
      active_experiments: number;
    }>(`/users/me/boxes/${encodeURIComponent(boxId)}/revoke`, {
      body: { mode },
      method: "POST",
    });
  },
  assignBox(projectId: string, boxId: string) {
    return apiClient.request<Box>(
      `${path(projectId)}/boxes/${encodeURIComponent(boxId)}`,
      { method: "PUT" },
    );
  },
  removeBox(projectId: string, boxId: string, force = false) {
    return apiClient.request<void>(
      `${path(projectId)}/boxes/${encodeURIComponent(boxId)}`,
      {
        method: "DELETE",
        query: { force },
      },
    );
  },
};
