export type ArticleRenderTheme = "md" | "latex";

export const ARTICLE_RENDER_THEME_EVENT = "mmdash:article-render-theme";

export type ArticleBlock = {
  block_id: string;
  node_type: string;
  ordinal: number;
  text: string;
  attrs: Record<string, unknown>;
  tag:
    "ai_draft" | "human_draft" | "ai_revision" | "human_revision" | "reviewed";
  provenance: Record<string, unknown>;
  updated_at: string;
};

export type ArticleChapterTag = {
  chapter_tag_id: string;
  project_id: string;
  heading_block_id: string;
  status: "unedited" | "unreviewed" | "reviewed" | "needs_review";
  heading_block_type: string;
  heading_fingerprint: string;
  stale_reason?: string;
  updated_by: string;
  updated_at: string;
  reviewed_by?: string;
  reviewed_at?: string;
};

export type ArticleDraft = {
  project_id: string;
  draft_revision: number;
  yjs_update: string;
  state_vector: string;
  tiptap_json: Record<string, unknown>;
  markdown: string;
  blocks: ArticleBlock[];
  sync_status: "synced" | "syncing" | "offline" | "failed";
  updated_at: string;
};

export type ArticleReference = {
  reference_id: string;
  project_id: string;
  reference_type:
    "problem" | "model_snapshot" | "experiment_result" | "artifact" | "zotero";
  source_object_id: string;
  source_version_id: string;
  title: string;
  citation_key?: string;
  metadata: Record<string, unknown>;
  created_by: string;
  created_at: string;
};

export type ArticleCommit = {
  commit_id: string;
  project_id: string;
  commit_sha: string;
  draft_revision: number;
  state_vector: string;
  manuscript_sha256: string;
  message: string;
  created_by: string;
  created_at: string;
};

export type ArticleCommitOperation = {
  operation_id: string;
  commit_id: string;
  project_id: string;
  operation_kind: "commit" | "publication";
  publication_id?: string;
  draft_revision: number;
  status: "queued" | "running" | "retry_wait" | "succeeded" | "failed";
  stage: "queued" | "committing" | "publishing" | "completed" | "failed";
  commit_sha?: string;
  error_code?: string;
  attempts: number;
  max_attempts: number;
  next_attempt_at: string;
  created_at: string;
  updated_at: string;
  finished_at?: string;
};

export type ArticleBuildOutput = {
  role:
    "pdf" | "tex_source" | "source_zip" | "build_report" | "log" | "synctex";
  artifact_id: string;
  version_id: string;
  filename: string;
  mime_type: string;
  sha256: string;
  size_bytes: number;
};

export type ArticleBuild = {
  build_id: string;
  project_id: string;
  build_kind: "preview" | "formal" | "template_test";
  status: "queued" | "running" | "succeeded" | "failed" | "superseded";
  draft_revision?: number;
  commit_id?: string;
  commit_sha?: string;
  job_id?: string;
  template_id: string;
  template_artifact_id: string;
  template_version_id: string;
  engine: "auto" | "pdflatex" | "xelatex" | "lualatex";
  bibliography_tool: "auto" | "bibtex" | "biber" | "none";
  toolchain?: Record<string, unknown>;
  outputs: ArticleBuildOutput[];
  progress_percent: number;
  progress_stage:
    | "queued"
    | "preparing"
    | "resources"
    | "converting"
    | "compiling"
    | "packaging"
    | "uploading"
    | "completed"
    | "failed"
    | "superseded";
  error_code?: string;
  error_message?: string;
  created_by: string;
  created_at: string;
  updated_at: string;
  finished_at?: string;
};

export type ArticleRelease = {
  release_id: string;
  project_id: string;
  commit_id: string;
  commit_sha: string;
  build_id: string;
  tag: string;
  title: string;
  notes: string;
  template_version_id: string;
  engine: string;
  toolchain: Record<string, unknown>;
  outputs: ArticleBuildOutput[];
  created_by: string;
  created_at: string;
};

export type ArticlePublication = {
  publication_id: string;
  project_id: string;
  commit_id: string;
  build_id: string;
  release_id?: string;
  status: "building" | "released" | "failed";
  tag: string;
  title: string;
  notes: string;
  error_code?: string;
  created_by: string;
  created_at: string;
  updated_at: string;
};

export type ArticleTemplateManifest = {
  schema_version: "1.0";
  name: string;
  version: string;
  entrypoint: string;
  output: string;
  content_target: string;
  bibliography_target: string;
  engine: "auto" | "pdflatex" | "xelatex" | "lualatex";
  bibliography_tool: "auto" | "bibtex" | "biber" | "none";
};

export type ArticleTemplate = {
  template_id: string;
  project_id: string;
  artifact_id: string;
  version_id: string;
  manifest: ArticleTemplateManifest;
  status: "validating" | "ready" | "rejected";
  error_code?: string;
  created_by: string;
  created_at: string;
  updated_at: string;
};

export type ArticleAggregate = {
  draft: ArticleDraft;
  chapter_tags: ArticleChapterTag[];
  references: ArticleReference[];
  commits: ArticleCommit[];
  builds: ArticleBuild[];
  releases: ArticleRelease[];
  templates: ArticleTemplate[];
  unreviewed_blocks: number;
  section_completion: number;
  warnings: {
    code: "ARTICLE_COMPONENT_UNAVAILABLE";
    component:
      | "references"
      | "commits"
      | "builds"
      | "releases"
      | "templates"
      | "chapter_tags"
      | "templates.bootstrap"
      | "chapter_tags.bootstrap";
    message: string;
  }[];
};

export type ZoteroBinding = {
  project_id: string;
  library_type: "user" | "group";
  library_id: string;
  collection_key?: string;
  api_key_configured: boolean;
  read_only: true;
};

export type ZoteroItem = {
  item_key: string;
  version: number;
  citation_key: string;
  title: string;
  authors: string[];
  item_type: string;
  year?: string;
  doi?: string;
  raw: Record<string, unknown>;
};

export type ZoteroCollection = {
  collection_key: string;
  name: string;
  num_collections: number;
  num_items: number;
  parent_collection_key?: string | null;
};
