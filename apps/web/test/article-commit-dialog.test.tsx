import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import {
  articleActionMessage,
  ArticleOperationStatus,
  CommitDialog,
} from "@/features/article/article-workbench";
import type {
  ArticleAggregate,
  ArticleCommitOperation,
} from "@/features/article/types";
import { ApiError } from "@/lib/api-client";

const aggregate: ArticleAggregate = {
  chapter_tags: [],
  builds: [],
  commits: [],
  draft: {
    blocks: [],
    draft_revision: 1,
    markdown: "",
    project_id: "00000000-0000-4000-8000-000000000041",
    state_vector: "",
    sync_status: "synced",
    tiptap_json: {},
    updated_at: "2026-08-19T00:00:00Z",
    yjs_update: "",
  },
  references: [],
  releases: [],
  section_completion: 0,
  templates: [],
  unreviewed_blocks: 0,
  warnings: [],
};

describe("Article commit readiness", () => {
  it("turns repository conflicts into actionable guidance", () => {
    const error = new ApiError({
      code: "ARTICLE_REPOSITORY_NOT_CONFIGURED",
      message: "conflict",
      status: 409,
    });
    expect(articleActionMessage(error)).toContain("项目设置连接 Repo");
  });

  it("shows a real empty template state instead of an empty select", () => {
    const openTemplates = vi.fn();
    render(
      <QueryClientProvider client={new QueryClient()}>
        <CommitDialog
          canBuild
          canRelease
          data={aggregate}
          onClose={vi.fn()}
          onOpenTemplates={openTemplates}
          onRefresh={vi.fn()}
        />
      </QueryClientProvider>,
    );
    expect(
      screen.getByRole("option", { name: "没有已就绪模板" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "提交并发布" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "打开模板页" }));
    expect(openTemplates).toHaveBeenCalledOnce();
  });

  it("keeps an asynchronous publication visible after the dialog closes", () => {
    const operation: ArticleCommitOperation = {
      attempts: 1,
      commit_id: "commit-1",
      created_at: "2026-08-19T00:00:00Z",
      draft_revision: 2,
      max_attempts: 10,
      next_attempt_at: "2026-08-19T00:00:00Z",
      operation_id: "operation-1",
      operation_kind: "publication",
      project_id: aggregate.draft.project_id,
      stage: "committing",
      status: "running",
      updated_at: "2026-08-19T00:00:00Z",
    };
    const view = render(<ArticleOperationStatus operations={[operation]} />);

    const indicator = view.getByRole("button", {
      name: "有 Commit / Release 操作正在执行",
    });
    const operationStatus = within(view.container);
    expect(operationStatus.queryByRole("dialog")).not.toBeInTheDocument();
    fireEvent.click(indicator);
    expect(
      operationStatus.getByRole("dialog", { name: "Commit / Release 队列" }),
    ).toHaveTextContent("提交并发布");
    expect(operationStatus.getByText(/固定 Commit 中/)).toBeInTheDocument();
  });

  it("shows a red failure state and exposes the failure log in the queue", () => {
    const operation: ArticleCommitOperation = {
      attempts: 10,
      commit_id: "commit-1",
      created_at: "2026-08-19T00:00:00Z",
      draft_revision: 7,
      error_code: "ARTICLE_COMMIT_PERSIST_FAILED",
      finished_at: "2026-08-19T00:02:00Z",
      max_attempts: 10,
      next_attempt_at: "2026-08-19T00:01:00Z",
      operation_id: "operation-failed",
      operation_kind: "commit",
      project_id: aggregate.draft.project_id,
      stage: "failed",
      status: "failed",
      updated_at: "2026-08-19T00:02:00Z",
    };
    render(<ArticleOperationStatus operations={[operation]} />);

    fireEvent.click(
      screen.getByRole("button", {
        name: "最近的 Commit / Release 操作失败",
      }),
    );
    fireEvent.click(screen.getByText("查看失败日志"));
    expect(
      screen.getByText("ARTICLE_COMMIT_PERSIST_FAILED"),
    ).toBeInTheDocument();
    expect(screen.getByText("operation-failed")).toBeInTheDocument();
  });
});
