export type AgentManagementMode = "manual" | "auto";
export type AgentInstanceStatus =
  "setup_pending" | "configuring" | "active" | "degraded" | "disabled";
export type AgentManagementPath =
  "direct" | "cloudflare_access" | "unreachable" | "unsupported_auth";
export type AgentSessionType = "main" | "progress" | "experiment";
export type AgentSessionStatus = "active" | "ended";
export type AgentRunStatus =
  | "queued"
  | "running"
  | "waiting_for_approval"
  | "stopping"
  | "completed"
  | "failed"
  | "stopped";
export type AgentApprovalChoice = "once" | "session" | "always" | "deny";

export type AgentProjectAccessCapabilities = {
  configure: boolean;
  rotate: boolean;
  verify: boolean;
};

export type AgentCapabilities = {
  jobs: boolean;
  message_history: boolean;
  project_access: AgentProjectAccessCapabilities;
  run_approval: boolean;
  run_events: boolean;
  run_status: boolean;
  run_stop: boolean;
  runs: boolean;
  session_chat_stream: boolean;
  session_fork: boolean;
  sessions: boolean;
};

export type AgentCheck = {
  checked_at: string;
  code: string;
  kind:
    | "runtime"
    | "authentication"
    | "capabilities"
    | "sessions"
    | "messages"
    | "sse"
    | "runs"
    | "jobs"
    | "management"
    | "project_access";
  message?: string;
  status: "passed" | "failed" | "unsupported" | "not_configured";
};

export type AgentCredential = {
  activated_at?: string;
  agent_instance_id: string;
  allowed_tools: string[];
  created_at: string;
  expires_at?: string;
  grant_id: string;
  id: string;
  last_used_at?: string;
  name: string;
  project_id: string;
  replaces_token_id?: string;
  revoked_at?: string;
  status: "pending" | "active" | "revoked";
};

export type AgentProjectGrant = {
  agent_instance_id: string;
  allowed_tools: string[];
  created_at: string;
  default_session_id?: string;
  grant_id: string;
  last_access_at?: string;
  project_access_status?:
    "pending" | "verified" | "failed" | "rotation_failed" | "revoked";
  project_id: string;
  revoked_at?: string;
  status: "active" | "revoked";
  updated_at: string;
  version: number;
};

export type AgentInstance = {
  adapter_type: "hermes";
  agent_instance_id: string;
  capabilities: AgentCapabilities;
  created_at: string;
  created_by: string;
  credentials: AgentCredential[];
  disabled_at?: string;
  display_name: string;
  grant: AgentProjectGrant;
  management_check?: AgentCheck;
  management_mode: AgentManagementMode;
  management_path: AgentManagementPath;
  management_url?: string;
  profile?: string;
  project_access_check?: AgentCheck;
  project_id: string;
  request_timeout_seconds: number;
  runtime_check?: AgentCheck;
  runtime_url: string;
  secrets: {
    cloudflare_access_configured: boolean;
    dashboard_session_token_configured: boolean;
    hermes_api_key_configured: boolean;
  };
  status: AgentInstanceStatus;
  updated_at: string;
  version: number;
};

export type OneTimeAgentToken = {
  credential: AgentCredential;
  mcp_endpoint: string;
  server_name?: string;
  token: string;
};

export type AgentInstanceProvisioningResult = {
  instance: AgentInstance;
  one_time_credential?: OneTimeAgentToken;
};

export type AgentChecksResult = {
  checked_at: string;
  checks: AgentCheck[];
  instance: AgentInstance;
  status: "passed" | "failed" | "partial";
};

export type AgentProjectAccessVerificationResult = {
  check: AgentCheck;
  checked_at: string;
  instance: AgentInstance;
  verified: boolean;
};

export type AgentTokenRotationResult = {
  credential: AgentCredential;
  old_credential_remains_active: boolean;
  one_time_credential?: OneTimeAgentToken;
  rotation_status:
    | "awaiting_user"
    | "configuring"
    | "verifying"
    | "completed"
    | "failed"
    | "cancelled";
  safe_error_code?: string;
};

export type AgentPrompt = {
  agent_instance_id: string;
  custom: boolean;
  custom_prompt?: string;
  default_prompt: string;
  effective_prompt: string;
  project_id: string;
  updated_at?: string;
  updated_by?: string;
  version: number;
};

export type AgentSession = {
  agent_instance_id: string;
  created_at: string;
  default: boolean;
  ended_at?: string;
  end_reason?: string;
  last_message_at?: string;
  last_run_at?: string;
  last_run_id?: string;
  parent_session_id?: string;
  project_id: string;
  remote_session_id: string;
  session_id: string;
  session_type: AgentSessionType;
  status: AgentSessionStatus;
  title: string;
  updated_at: string;
  version: number;
};

export type AgentReasoningEffort =
  "none" | "minimal" | "low" | "medium" | "high" | "xhigh" | "max" | "ultra";

export type AgentToolCall = {
  completed_at?: string;
  input_summary?: string;
  name: string;
  output_summary?: string;
  safe_error_code?: string;
  started_at?: string;
  status: "queued" | "running" | "completed" | "failed";
  tool_call_id: string;
};

export type AgentMessage = {
  attachments?: AgentChatAttachment[];
  content: string;
  created_at?: string;
  message_id: string;
  role: "user" | "assistant" | "tool" | "system";
  tool_call_id?: string;
  tool_calls?: AgentToolCall[];
};

export type AgentChatAttachment = {
  artifact_id: string;
  version_id: string;
  run_id: string;
  direction: "input" | "output";
  name: string;
  filename: string;
  mime_type: string;
  size_bytes: number;
  created_at: string;
  local_url?: string;
};

export type AgentRun = {
  completed_at?: string;
  created_at: string;
  remote_run_id: string;
  run_id: string;
  safe_error_code?: string;
  safe_error_message?: string;
  session_id: string;
  source: "message" | "regenerate" | "rerun" | "progress_evaluation";
  source_run_id?: string;
  started_at?: string;
  status: AgentRunStatus;
  tool_calls: AgentToolCall[];
  updated_at: string;
  usage?: {
    input_tokens?: number;
    output_tokens?: number;
    total_tokens?: number;
  };
  version: number;
};

export type AgentRunLaunch = {
  run: AgentRun;
  session: AgentSession;
};

export type AgentApproval = {
  approval_id: string;
  choices: AgentApprovalChoice[];
};

export type AgentStreamEventType =
  | "run.started"
  | "run.status"
  | "message.started"
  | "message.delta"
  | "message.completed"
  | "reasoning.available"
  | "tool.progress"
  | "tool.started"
  | "tool.completed"
  | "tool.failed"
  | "approval.required"
  | "approval.responded"
  | "subagent.started"
  | "subagent.completed"
  | "run.completed"
  | "run.failed"
  | "run.stopped"
  | "heartbeat"
  | "done"
  | "error";

export type AgentStreamEvent = {
  approval?: AgentApproval;
  delta?: string;
  event: AgentStreamEventType;
  event_id: string;
  message_id?: string;
  occurred_at: string;
  run?: AgentRun;
  run_id: string;
  safe_error_code?: string;
  safe_error_message?: string;
  sequence: number;
  session_id: string;
  tool_call?: AgentToolCall;
};

export type AgentInstanceInput = {
  adapter_type?: "hermes";
  allowed_tools: string[];
  cloudflare_access_client_id?: string;
  cloudflare_access_client_secret?: string;
  dashboard_session_token?: string;
  display_name: string;
  hermes_api_key: string;
  management_mode: AgentManagementMode;
  management_url?: string;
  profile?: string;
  request_timeout_seconds?: number;
  runtime_url: string;
};

export type AgentInstanceUpdate = Partial<AgentInstanceInput>;
