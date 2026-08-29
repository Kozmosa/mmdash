export type ResourceLimits = {
  cpu_millis: number;
  memory_bytes: number;
  timeout_seconds: number;
  disk_bytes: number;
  pids: number;
  network: "disabled" | "restricted" | "enabled";
};

export type RuntimePolicy = "auto" | "e2b" | "local-docker";
export type ExperimentType = "box" | "box-re" | "self";
export type ExperimentStatus =
  | "created"
  | "queued"
  | "preparing"
  | "running"
  | "uploading"
  | "processing_result"
  | "awaiting_result"
  | "verifying_result"
  | "succeeded"
  | "failed"
  | "canceled"
  | "timed_out"
  | "archived";

export type ExperimentFailure = {
  stage: string;
  code: string;
  message: string;
  failed_at: string;
  box_id?: string;
  runtime?: "local-docker" | "e2b";
  attempt: number;
  retryable: boolean;
  cleanup_result: Record<string, unknown>;
};

export type ExperimentRetry = {
  retry_of_experiment_id?: string;
  root_experiment_id: string;
  superseded_by_experiment_id?: string;
  latest_experiment_id: string;
  retry_sequence: number;
  warning_code?: "EXPERIMENT_HAS_NEWER_RETRY";
};

export type ExperimentResultContract = {
  branch: string;
  directory: string;
  manifest_schema: string;
  git_large_file_threshold_bytes: number;
  artifact_pointer_path: ".mmdash/artifacts.json";
  push_required: true;
  bind_tool: "experiment.result.bind";
  instructions: string;
};

export type ArtifactPointer = {
  artifact_id: string;
  version_id: string;
  filename: "execution-bundle.zip";
  sha256: string;
  size_bytes: number;
};

export type Experiment = {
  experiment_id: string;
  project_id: string;
  name: string;
  experiment_type: ExperimentType;
  execution_status: ExperimentStatus;
  connectivity_status?: "box_online" | "box_offline";
  created_by: string;
  source_commit: string;
  entrypoint: string;
  parameters: Record<string, unknown>;
  environment: Record<string, string>;
  inputs: Record<string, unknown>;
  requested_runtime_policy: RuntimePolicy;
  requested_box_id?: string;
  actual_runtime?: "local-docker" | "e2b";
  runtime_version?: string;
  limits: ResourceLimits;
  box_id?: string;
  task_id?: string;
  exit_code?: number;
  failure?: ExperimentFailure;
  resource_usage?: Record<string, unknown>;
  summary?: string;
  project_timezone: string;
  result_directory: string;
  result_commit_sha?: string;
  execution_bundle?: ArtifactPointer;
  result_manifest_sha256?: string;
  result_contract?: ExperimentResultContract;
  retry: ExperimentRetry;
  logs_truncated: boolean;
  logs_truncated_at?: string;
  progress: number;
  created_at: string;
  started_at?: string;
  finished_at?: string;
  updated_at: string;
};

export type ProjectBoxBinding = {
  project_id: string;
  box_id: string;
  assigned_by: string;
  assigned_at: string;
  force_unbound_at?: string;
};

export type Box = {
  box_id: string;
  owner_user_id: string;
  name: string;
  status:
    | "online"
    | "offline"
    | "draining"
    | "revoked"
    | "legacy_reauthorization_required";
  version: string;
  capabilities: { name: string; version: string; features?: string[] }[];
  runtimes: { name: string; version: string; image?: string }[];
  limits: ResourceLimits;
  load: {
    running_tasks: number;
    capacity: number;
    cpu_millis: number;
    memory_bytes: number;
  };
  project_assignments: ProjectBoxBinding[];
  last_heartbeat_at?: string;
  offline_since?: string;
  drain_requested_at?: string;
  revoked_at?: string;
  legacy_reauthorization_required: boolean;
  created_at: string;
  updated_at: string;
};

export type BoxRelease = {
  platform: "windows" | "linux";
  version: string;
  artifact_id: string;
  version_id: string;
  filename: string;
  sha256: string;
  size_bytes: number;
  download: {
    method: "GET" | "PUT";
    url: string;
    headers: Record<string, string>;
    expires_at: string;
  };
  install_command: string;
  instructions: string;
};

export type ExperimentLog = {
  log_id: string;
  experiment_id: string;
  sequence: number;
  stream: "stdout" | "stderr" | "system";
  message: string;
  fields?: Record<string, unknown>;
  occurred_at: string;
  received_at: string;
  late_after_failure: boolean;
};

export type ResultFile = {
  path: string;
  storage_kind: "git" | "artifact";
  sha256: string;
  size_bytes: number;
  media_type: string;
  artifact_id?: string;
  artifact_version_id?: string;
  repository_path?: string;
};

export type ResultBundle = {
  experiment_id: string;
  execution_status: ExperimentStatus;
  result_directory: string;
  result_commit_sha?: string;
  result_manifest_sha256?: string;
  manifest?: Record<string, unknown>;
  execution_bundle?: ArtifactPointer;
  files: ResultFile[];
  retry: ExperimentRetry;
  summary?: string;
  analysis?: string;
};

export type ExperimentSettings = {
  project_id: string;
  timezone: string;
  default_runtime_policy: RuntimePolicy;
  default_limits: ResourceLimits;
  git_large_file_threshold_bytes: number;
  updated_by: string;
  updated_at: string;
};
