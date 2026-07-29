export type RepoWorkspaceKind = "code" | "article" | "result";

export type RepoWorkspace = {
  head_commit_sha: string | null;
  local_branch: "mmdash/code" | "mmdash/article" | "mmdash/result";
  remote_branch: string;
  status: "pending" | "ready" | "missing" | "dirty" | "error";
  tree_sha: string | null;
  updated_at: string;
  workspace: RepoWorkspaceKind;
};

export type Repository = {
  created_at: string;
  default_branch: string;
  display_name: string;
  last_error_code: string | null;
  last_error_message: string | null;
  last_synced_at: string | null;
  project_id: string;
  provider: "github" | "local";
  remote_url: string | null;
  repository_id: string;
  settings_version: number;
  status:
    | "pending"
    | "cloning"
    | "configuring"
    | "ready"
    | "syncing"
    | "error"
    | "disconnected";
  updated_at: string;
  webhook: {
    hook_id: string;
    public_url: string;
    secret?: string;
    secret_configured: boolean;
  };
  workspaces: RepoWorkspace[];
};

export type RepoConnectionTestResult = {
  branches: string[];
  checked_at: string;
  checks: {
    message?: string;
    name: string;
    status: "passed" | "failed";
  }[];
  default_branch: string;
  status: "passed" | "failed";
};

export type RepoBranch = {
  commit_sha: string;
  default: boolean;
  name: string;
  workspace: RepoWorkspaceKind | null;
};

export type RepoGitIdentity = {
  email: string;
  name: string;
  time: string;
};

export type RepoCommit = {
  author: RepoGitIdentity;
  changes: {
    path: string;
    previous_path?: string;
    status: string;
  }[];
  commit_sha: string;
  committer: RepoGitIdentity;
  first_seen_at: string;
  message: string;
  parent_shas: string[];
  repository_id: string;
  source: "connect" | "webhook" | "sync" | "mmdash" | "reference";
  tree_sha: string;
};

export type RepoCommitPage = {
  branch: string;
  has_more: boolean;
  items: RepoCommit[];
  next_cursor: string | null;
  resolved_revision: string;
  workspace: RepoWorkspaceKind;
};

export type RepoTreeEntry = {
  kind: "file" | "directory" | "symlink" | "submodule";
  mode: string;
  name: string;
  object_id: string;
  path: string;
  size: number | null;
};

export type RepoTreePage = {
  branch: string;
  has_more: boolean;
  items: RepoTreeEntry[];
  next_cursor: string | null;
  path: string;
  resolved_revision: string;
  workspace: RepoWorkspaceKind;
};

export type RepoFileContent = {
  branch: string;
  content: string | null;
  encoding: "utf-8" | null;
  kind: "file" | "symlink" | "submodule";
  mode: string;
  object_id: string;
  path: string;
  preview_status:
    | "text"
    | "binary"
    | "too_large"
    | "lfs_not_materialized"
    | "symlink"
    | "submodule";
  resolved_revision: string;
  size: number;
  workspace: RepoWorkspaceKind;
};

export type RepoSetting = {
  scope: "project";
  scope_id: string;
  type_key: "repo.connection";
  updated_at: string;
  updated_by: string;
  values: Record<string, unknown>;
  version: number;
};

export type ProjectPermissions = {
  permissions: string[];
  role: string;
};
