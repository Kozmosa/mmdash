import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import {
  articleActionMessage,
  CommitDialog,
} from "@/features/article/article-workbench";
import type { ArticleAggregate } from "@/features/article/types";
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
});
