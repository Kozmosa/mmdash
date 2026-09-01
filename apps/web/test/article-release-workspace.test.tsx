import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { articleApi } from "@/features/article/api";
import { ReleaseWorkspace } from "@/features/article/article-workbench";
import type {
  ArticleAggregate,
  ArticleBuild,
  ArticleCommit,
  ArticleRelease,
} from "@/features/article/types";

const projectId = "00000000-0000-4000-8000-000000000001";
const commitOneId = "00000000-0000-4000-8000-000000000101";
const commitTwoId = "00000000-0000-4000-8000-000000000102";
const buildOneId = "00000000-0000-4000-8000-000000000201";
const buildTwoId = "00000000-0000-4000-8000-000000000202";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("Article Release workspace", () => {
  it("selects an existing Commit before choosing one of its successful Builds", async () => {
    const createRelease = vi
      .spyOn(articleApi, "createRelease")
      .mockResolvedValue(releaseFor(commitTwoId, buildTwoId));
    renderWorkspace(aggregate());

    const commit = screen.getByLabelText("Release Commit");
    const build = screen.getByLabelText("Release Build");
    expect(commit).toHaveValue(commitOneId);
    expect(build).toHaveValue(buildOneId);

    fireEvent.change(commit, { target: { value: commitTwoId } });
    expect(commit).toHaveValue(commitTwoId);
    expect(build).toHaveValue(buildTwoId);
    expect(
      within(build).queryByRole("option", { name: /aaaaaaaaaaaa/ }),
    ).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "发布" }));
    await waitFor(() =>
      expect(createRelease).toHaveBeenCalledWith(
        projectId,
        expect.objectContaining({
          build_id: buildTwoId,
          commit_id: commitTwoId,
        }),
      ),
    );
  });

  it("keeps Commits selectable and guides the user when no successful Build exists", () => {
    const onOpenCommits = vi.fn();
    const data = aggregate();
    data.builds = [];
    renderWorkspace(data, onOpenCommits);

    expect(screen.getByLabelText("Release Commit")).toHaveValue(commitOneId);
    expect(
      screen.getByRole("option", { name: "该 Commit 暂无成功正式构建" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "发布" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "前往 Commits" }));
    expect(onOpenCommits).toHaveBeenCalledOnce();
  });
});

function renderWorkspace(data: ArticleAggregate, onOpenCommits = vi.fn()) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <ReleaseWorkspace
        canRelease
        data={data}
        onOpenCommits={onOpenCommits}
        onRefresh={vi.fn().mockResolvedValue(undefined)}
        projectId={projectId}
      />
    </QueryClientProvider>,
  );
}

function aggregate(): ArticleAggregate {
  const commits = [
    commit(commitOneId, "a", "First checkpoint"),
    commit(commitTwoId, "b", "Second checkpoint"),
  ];
  return {
    builds: [build(buildOneId, commits[0]), build(buildTwoId, commits[1])],
    chapter_tags: [],
    commits,
    draft: {
      blocks: [],
      draft_revision: 2,
      markdown: "",
      project_id: projectId,
      state_vector: "",
      sync_status: "synced",
      tiptap_json: {},
      updated_at: "2026-09-01T00:00:00Z",
      yjs_update: "",
    },
    references: [],
    releases: [],
    section_completion: 0,
    templates: [],
    unreviewed_blocks: 0,
    warnings: [],
  };
}

function commit(
  id: string,
  shaCharacter: string,
  message: string,
): ArticleCommit {
  return {
    commit_id: id,
    commit_sha: shaCharacter.repeat(40),
    created_at: "2026-09-01T00:00:00Z",
    created_by: "user-1",
    draft_revision: 1,
    manuscript_sha256: "c".repeat(64),
    message,
    project_id: projectId,
    state_vector: "",
  };
}

function build(id: string, source: ArticleCommit): ArticleBuild {
  return {
    bibliography_tool: "none",
    build_id: id,
    build_kind: "formal",
    commit_id: source.commit_id,
    commit_sha: source.commit_sha,
    created_at: "2026-09-01T00:00:00Z",
    created_by: "user-1",
    engine: "xelatex",
    outputs: [],
    progress_percent: 100,
    progress_stage: "completed",
    project_id: projectId,
    status: "succeeded",
    template_artifact_id: "00000000-0000-4000-8000-000000000301",
    template_id: "00000000-0000-4000-8000-000000000302",
    template_version_id: "00000000-0000-4000-8000-000000000303",
    updated_at: "2026-09-01T00:00:00Z",
  };
}

function releaseFor(commitId: string, buildId: string): ArticleRelease {
  return {
    build_id: buildId,
    commit_id: commitId,
    commit_sha: "b".repeat(40),
    created_at: "2026-09-01T00:00:00Z",
    created_by: "user-1",
    engine: "xelatex",
    notes: "",
    outputs: [],
    project_id: projectId,
    release_id: "00000000-0000-4000-8000-000000000401",
    tag: "v0.1.1",
    template_version_id: "00000000-0000-4000-8000-000000000303",
    title: "论文版本",
    toolchain: {},
  };
}
