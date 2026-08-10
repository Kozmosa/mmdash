import { apiClient } from "@/lib/api-client";

import type { Box, Experiment, ExperimentLog, ResourceLimits } from "./types";

const path = (projectId: string) => `/projects/${encodeURIComponent(projectId)}`;

export const experimentApi = {
  list(projectId: string, status?: string) { return apiClient.request<{ items: Experiment[]; has_more: boolean }>(`${path(projectId)}/experiments`, { query: { status } }); },
  create(projectId: string, input: { name: string; source_commit: string; entrypoint: string; parameters: Record<string, unknown>; environment: Record<string, string>; inputs: Record<string, unknown>; runtime: string; limits: ResourceLimits; idempotency_key: string }) { return apiClient.request<Experiment>(`${path(projectId)}/experiments`, { body: input, method: "POST" }); },
  get(projectId: string, experimentId: string) { return apiClient.request<Experiment>(`${path(projectId)}/experiments/${encodeURIComponent(experimentId)}`); },
  run(projectId: string, experimentId: string) { return apiClient.request<Experiment>(`${path(projectId)}/experiments/${encodeURIComponent(experimentId)}/run`, { method: "POST" }); },
  cancel(projectId: string, experimentId: string) { return apiClient.request<Experiment>(`${path(projectId)}/experiments/${encodeURIComponent(experimentId)}/cancel`, { method: "POST" }); },
  logs(projectId: string, experimentId: string) { return apiClient.request<{ items: ExperimentLog[] }>(`${path(projectId)}/experiments/${encodeURIComponent(experimentId)}/logs`); },
  result(projectId: string, experimentId: string) { return apiClient.request<Experiment["result"]>(`${path(projectId)}/experiments/${encodeURIComponent(experimentId)}/result`); },
  boxes(projectId: string) { return apiClient.request<{ items: Box[] }>(`${path(projectId)}/boxes`); },
  bindBox(projectId: string, boxId: string) { return apiClient.request<Box>(`${path(projectId)}/box`, { body: { box_id: boxId }, method: "PUT" }); },
  unbindBox(projectId: string) { return apiClient.request<void>(`${path(projectId)}/box`, { method: "DELETE" }); },
};
