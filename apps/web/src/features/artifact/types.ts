export type ArtifactKind =
  | "problem"
  | "attachment"
  | "experiment_result"
  | "model_file"
  | "article_build"
  | "other";

export type PublicArtifactKind = "problem" | "attachment" | "other";

export type ArtifactSource =
  "user_upload" | "experiment" | "model" | "article" | "system";

export type ArtifactStatus = "pending_upload" | "available" | "trashed";

export type Artifact = {
  artifact_id: string;
  project_id: string;
  kind: ArtifactKind;
  source: ArtifactSource;
  source_object_id: string | null;
  tags: string[];
  name: string;
  description: string | null;
  recommended_usage: string[];
  current_version_id: string | null;
  status: ArtifactStatus;
  created_by: string;
  trashed_at: string | null;
  created_at: string;
  updated_at: string;
};

export type ArtifactVersion = {
  version_id: string;
  artifact_id: string;
  version_no: number;
  storage_class: "object" | "git";
  filename: string;
  sha256: string;
  mime_type: string;
  size_bytes: number;
  status: "pending_upload" | "verifying" | "available" | "failed";
  available_at: string | null;
  git_reference: {
    repository_id: string;
    workspace: "result";
    commit_sha: string;
    path: string;
  } | null;
  created_by: string;
  created_at: string;
};

export type ArtifactDetail = {
  artifact: Artifact;
  current_version: ArtifactVersion | null;
};

export type ArtifactPage = {
  items: ArtifactDetail[];
  has_more: boolean;
  next_cursor: string | null;
};

export type UploadPart = {
  part_number: number;
  size_bytes: number;
  etag: string;
  completed_at: string;
};

export type UploadSession = {
  upload_id: string;
  artifact_id: string;
  version_id: string;
  upload_mode: "multipart" | "deduplicated";
  transfer_mode: "direct" | "local_proxy" | "none";
  status:
    | "initialized"
    | "uploading"
    | "completing"
    | "verifying"
    | "completed"
    | "aborted"
    | "expired"
    | "failed";
  size_bytes: number;
  sha256: string;
  part_size_bytes: number;
  part_count: number;
  completed_parts: UploadPart[];
  expires_at: string;
  created_at: string;
  updated_at: string;
};

export type TransferGrant = {
  method: "GET" | "PUT";
  url: string;
  headers: Record<string, string>;
  expires_at: string;
};

export type PartGrant = {
  part_number: number;
  size_bytes: number;
  transfer: TransferGrant;
};

export type DownloadGrant = {
  artifact_id: string;
  version_id: string;
  filename: string;
  mime_type: string;
  size_bytes: number;
  transfer: TransferGrant;
};

export type ArtifactPreview = {
  preview_id: string;
  version_id: string;
  preview_type: "image" | "pdf" | "csv" | "json" | "text" | "thumbnail";
  status: "queued" | "processing" | "available" | "failed" | "unsupported";
  structural_summary: Record<string, unknown> | null;
  error_code: string | null;
  transfer: TransferGrant | null;
  created_at: string;
  updated_at: string;
};

export type ArtifactListFilters = {
  cursor?: string;
  kind?: ArtifactKind;
  limit?: number;
  source?: ArtifactSource;
  status?: ArtifactStatus;
  tag?: string;
};

export type InitializeArtifactUpload = {
  description?: string;
  filename: string;
  idempotency_key: string;
  kind: PublicArtifactKind;
  mime_type?: string;
  name?: string;
  sha256: string;
  size_bytes: number;
  tags?: string[];
};

export type UpdateArtifact = {
  description?: string | null;
  kind?: PublicArtifactKind;
  name?: string;
  tags?: string[];
};
