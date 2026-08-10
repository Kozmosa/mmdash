export type ModelSyncStatus =
  | "idle"
  | "queued"
  | "running"
  | "succeeded"
  | "unchanged"
  | "failed"
  | "cancelled"
  | "timed_out";

export type ModelSource = {
  source_id: string;
  project_id: string;
  notion_root_page_id: string;
  notion_root_page_url: string;
  notion_root_title: string;
  auto_sync_enabled: boolean;
  auto_sync_interval_seconds: number;
  next_sync_at?: string;
  countdown_seconds?: number;
  sync_status: ModelSyncStatus;
  discovered_page_count: number;
  last_synced_at?: string;
  last_error_message?: string;
};

export type ModelSourcePage = {
  notion_page_id: string;
  parent_page_id?: string;
  title: string;
  url: string;
  depth: number;
  has_children: boolean;
  bound_question_id?: string;
};

export type ModelQuestion = {
  question_id: string;
  project_id: string;
  code: string;
  title: string;
  notion_page_id: string;
  notion_page_url: string;
  position: number;
  latest_snapshot_id?: string;
  snapshot_count: number;
  sync_status: ModelSyncStatus;
  last_synced_at?: string;
  last_error_message?: string;
  created_at: string;
  updated_at: string;
};

export type ModelOverview = {
  project_id: string;
  generated_at: string;
  configured: boolean;
  source?: ModelSource;
  discovered_pages: ModelSourcePage[];
  questions: ModelQuestion[];
};

export type ModelRichText = {
  text: string;
  expression?: string;
  bold?: boolean;
  italic?: boolean;
  strikethrough?: boolean;
  underline?: boolean;
  code?: boolean;
  color?: string;
  href?: string;
};

export type ModelBlock = {
  block_id: string;
  type: string;
  text: string;
  level?: number;
  rich_text: ModelRichText[];
  language?: string;
  expression?: string;
  checked?: boolean;
  rows?: string[][];
  cells?: ModelRichText[][];
  url?: string;
  artifact_id?: string;
  artifact_version_id?: string;
  caption?: string;
  children: ModelBlock[];
};

export type ModelSnapshotSummary = {
  snapshot_id: string;
  question_id: string;
  previous_snapshot_id?: string;
  title: string;
  content_hash: string;
  summary: string;
  tags: string[];
  version_note?: string;
  captured_at: string;
  triggered_by: string;
  created_at: string;
  metadata_updated_at: string;
};

export type ModelSnapshot = ModelSnapshotSummary & {
  project_id: string;
  notion_page_id: string;
  notion_page_url: string;
  outline: { block_id: string; title: string; level: number }[];
  blocks: ModelBlock[];
  content_markdown: string;
  content_text: string;
  assets: {
    source_block_id: string;
    artifact_id: string;
    artifact_version_id: string;
    filename: string;
    mime_type: string;
  }[];
};

export type ModelQuestionDetail = {
  question: ModelQuestion;
  latest_snapshot?: ModelSnapshot;
  snapshots: ModelSnapshotSummary[];
};

export type ModelDiff = {
  question_id: string;
  from_snapshot_id: string;
  to_snapshot_id: string;
  granularity: "character";
  blocks: {
    block_id: string;
    type: string;
    change: "unchanged" | "added" | "deleted" | "modified";
    block: ModelBlock;
    operations: { kind: "unchanged" | "added" | "deleted"; text: string }[];
  }[];
};
