export type ProgressTask = {
  task_id: string;
  project_id: string;
  title: string;
  description: string;
  status: "todo" | "in_progress" | "blocked" | "done";
  work_state: "todo" | "in_progress" | "blocked";
  start_at?: string;
  due_at?: string;
  completed_at?: string;
  source: string;
  source_run_id?: string;
  source_evaluation_id?: string;
  manual_override_fields?: string[];
};

export type ProgressMilestone = {
  milestone_id: string;
  project_id: string;
  title: string;
  description: string;
  status: "planned" | "in_progress" | "completed";
  critical: boolean;
  start_at?: string;
  target_at?: string;
  target_has_time: boolean;
  completed_at?: string;
  source: string;
};

export type ProgressProposal = {
  proposal_id: string;
  proposal_type: string;
  target_id?: string;
  title: string;
  rationale: string;
  changes: Record<string, unknown>;
  status: string;
  source: string;
  source_run_id?: string;
  source_evaluation_id?: string;
  reviewed_at?: string;
  created_at: string;
};

export type ProgressSettings = {
  project_id: string;
  auto_task_changes: boolean;
  auto_tracking_enabled: boolean;
  event_triggers_enabled: boolean;
  cron_enabled: boolean;
  cron_schedule: string;
  debounce_seconds: number;
  min_interval_seconds: number;
  evaluator_mode: "core_agent" | "mock";
  agent_instance_id?: string;
  cron_remote_job_id?: string;
  cron_sync_status: "pending" | "syncing" | "ready" | "failed" | "disabled";
  cron_error_code?: string;
  cron_synced_at?: string;
  updated_by: string;
  updated_at: string;
};

export type ProgressTrackerState = {
  project_id: string;
  last_evaluation_id?: string;
  detected_stage: string;
  effective_stage: string;
  stage_overridden: boolean;
  summary: string;
  changes_since_last: string[];
  completed_items: string[];
  in_progress_items: string[];
  blockers: string[];
  pending_questions: string[];
  last_evaluated_at?: string;
  updated_at: string;
};

export type ProgressEvaluationTrigger = {
  trigger_id: string;
  trigger_type: string;
  source_event_id?: string;
  source_event_type?: string;
  source_resource_id?: string;
  source_version?: string;
  payload: Record<string, unknown>;
  occurred_at: string;
};

export type ProgressRisk = {
  risk_id: string;
  evaluation_id: string;
  project_id: string;
  risk_key: string;
  title: string;
  severity: "low" | "medium" | "high" | "critical";
  status: "open" | "mitigated" | "accepted";
  detail: string;
  created_at: string;
};

export type ProgressEvaluation = {
  evaluation_id: string;
  request_id: string;
  project_id: string;
  job_id?: string;
  status: "queued" | "running" | "succeeded" | "failed";
  input_version: string;
  input_snapshot: Record<string, unknown>;
  detected_stage: string;
  summary: string;
  changes_since_last: string[];
  completed_items: string[];
  in_progress_items: string[];
  blockers: string[];
  pending_questions: string[];
  source_event_ids: string[];
  trigger_kind: "event" | "manual" | "cron" | "retry";
  agent_instance_id?: string;
  agent_session_id?: string;
  agent_run_id?: string;
  evaluator_mode: "core_agent" | "mock";
  attempts: number;
  error_code?: string;
  error_message?: string;
  requested_by: string;
  created_at: string;
  started_at?: string;
  completed_at?: string;
  updated_at: string;
  triggers: ProgressEvaluationTrigger[];
  risks: ProgressRisk[];
};

export type ProgressAggregate = {
  milestones: ProgressMilestone[];
  tasks: ProgressTask[];
  today: ProgressTask[];
  overdue: ProgressTask[];
  blocked: ProgressTask[];
  proposals: ProgressProposal[];
  reminders: { reminder_id: string; note: string; status: string; remind_at: string }[];
  board: {
    todo: ProgressTask[];
    in_progress: ProgressTask[];
    blocked: ProgressTask[];
    done: ProgressTask[];
  };
  gantt: { id: string; kind: string; title: string; target_at?: string; status: string }[];
  settings: ProgressSettings;
  tracking: ProgressTrackerState;
  latest_evaluation?: ProgressEvaluation;
};

export type ProgressEvaluationPage = {
  items: ProgressEvaluation[];
  has_more: boolean;
  next_cursor?: string;
};
