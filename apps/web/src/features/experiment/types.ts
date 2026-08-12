export type ResourceLimits = {
  cpu_millis: number;
  memory_bytes: number;
  timeout_seconds: number;
  disk_bytes: number;
  pids: number;
  network: "disabled" | "restricted" | "enabled";
};

export type Experiment = {
  experiment_id: string;
  project_id: string;
  name: string;
  status: string;
  created_by: string;
  source_commit: string;
  entrypoint: string;
  parameters: Record<string, unknown>;
  environment: Record<string, string>;
  inputs: Record<string, unknown>;
  runtime: "local-docker" | "e2b";
  limits: ResourceLimits;
  box_id?: string;
  task_id?: string;
  exit_code?: number;
  failure_code?: string;
  failure_message?: string;
  resource_usage?: Record<string, unknown>;
  summary?: string;
  result?: { manifest: Record<string, unknown>; artifact: Record<string, unknown>; summary?: string; analysis?: string };
  created_at: string;
  started_at?: string;
  finished_at?: string;
  updated_at: string;
};

export type Box = {
  box_id: string;
  project_id: string;
  name: string;
  status: string;
  version: string;
  capabilities: { name: string; version: string; features?: string[] }[];
  runtimes: { name: string; version: string; image?: string }[];
  limits: ResourceLimits;
  load: { running_tasks: number; capacity: number; cpu_millis: number; memory_bytes: number };
  last_heartbeat_at?: string;
  updated_at: string;
};

export type ExperimentLog = { log_id: string; experiment_id: string; level: string; message: string; occurred_at: string; fields?: Record<string, unknown> };
