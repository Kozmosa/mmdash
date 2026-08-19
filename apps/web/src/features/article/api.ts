import { apiClient } from "@/lib/api-client";

import type {
  ArticleAggregate,
  ArticleBlock,
  ArticleBuild,
  ArticleCommit,
  ArticleDraft,
  ArticleReference,
  ArticleRelease,
  ArticlePublication,
  ArticleTemplate,
  ArticleTemplateManifest,
  ZoteroBinding,
  ZoteroItem,
} from "./types";

const base = (projectId: string) =>
  `/projects/${encodeURIComponent(projectId)}/article`;

export const articleApi = {
  aggregate(projectId: string) {
    return apiClient.request<ArticleAggregate>(base(projectId));
  },
  flush(projectId: string) {
    return apiClient.request<ArticleDraft>(`${base(projectId)}/draft/flush`, {
      method: "POST",
    });
  },
  createCommit(projectId: string, draftRevision: number, message: string) {
    return apiClient.request<ArticleCommit>(`${base(projectId)}/commits`, {
      body: { draft_revision: draftRevision, message },
      method: "POST",
    });
  },
  reviewBlock(projectId: string, blockId: string) {
    return apiClient.request<ArticleBlock>(
      `${base(projectId)}/blocks/${encodeURIComponent(blockId)}/review`,
      { method: "POST" },
    );
  },
  restoreCommit(projectId: string, commitId: string) {
    return apiClient.request<ArticleDraft>(
      `${base(projectId)}/commits/${encodeURIComponent(commitId)}/restore`,
      { method: "POST" },
    );
  },
  createPreview(
    projectId: string,
    input: {
      bibliography_tool: ArticleBuild["bibliography_tool"];
      draft_revision: number;
      engine: ArticleBuild["engine"];
      template_id: string;
    },
  ) {
    return apiClient.request<ArticleBuild>(
      `${base(projectId)}/preview-builds`,
      {
        body: input,
        method: "POST",
      },
    );
  },
  createBuild(
    projectId: string,
    input: {
      bibliography_tool: ArticleBuild["bibliography_tool"];
      commit_id: string;
      engine: ArticleBuild["engine"];
      idempotency_key: string;
      template_id: string;
    },
  ) {
    return apiClient.request<ArticleBuild>(`${base(projectId)}/builds`, {
      body: input,
      method: "POST",
    });
  },
  retryBuild(projectId: string, buildId: string) {
    return apiClient.request<ArticleBuild>(
      `${base(projectId)}/builds/${encodeURIComponent(buildId)}/retry`,
      { method: "POST" },
    );
  },
  createRelease(
    projectId: string,
    input: {
      build_id: string;
      commit_id: string;
      notes: string;
      tag: string;
      title: string;
    },
  ) {
    return apiClient.request<ArticleRelease>(`${base(projectId)}/releases`, {
      body: input,
      method: "POST",
    });
  },
  createPublication(
    projectId: string,
    input: {
      bibliography_tool: ArticleBuild["bibliography_tool"];
      draft_revision: number;
      engine: ArticleBuild["engine"];
      idempotency_key: string;
      message: string;
      notes: string;
      tag: string;
      template_id: string;
      title: string;
    },
  ) {
    return apiClient.request<ArticlePublication>(
      `${base(projectId)}/publications`,
      {
        body: input,
        method: "POST",
      },
    );
  },
  addReference(
    projectId: string,
    input: Omit<
      ArticleReference,
      "created_at" | "created_by" | "project_id" | "reference_id"
    >,
  ) {
    return apiClient.request<ArticleReference>(
      `${base(projectId)}/references`,
      {
        body: input,
        method: "POST",
      },
    );
  },
  removeReference(projectId: string, referenceId: string) {
    return apiClient.request<void>(
      `${base(projectId)}/references/${encodeURIComponent(referenceId)}`,
      { method: "DELETE" },
    );
  },
  registerTemplate(
    projectId: string,
    artifactId: string,
    versionId: string,
    manifest: ArticleTemplateManifest,
  ) {
    return apiClient.request<ArticleTemplate>(`${base(projectId)}/templates`, {
      body: { artifact_id: artifactId, manifest, version_id: versionId },
      method: "POST",
    });
  },
  getZotero(projectId: string) {
    return apiClient.request<ZoteroBinding>(`${base(projectId)}/zotero`);
  },
  updateZotero(
    projectId: string,
    input: {
      api_key: string;
      collection_key?: string;
      library_id: string;
      library_type: "user" | "group";
    },
  ) {
    return apiClient.request<ZoteroBinding>(`${base(projectId)}/zotero`, {
      body: input,
      method: "PUT",
    });
  },
  searchZotero(projectId: string, query: string) {
    return apiClient.request<{ items: ZoteroItem[] }>(
      `${base(projectId)}/zotero/search`,
      { query: { q: query } },
    );
  },
};
