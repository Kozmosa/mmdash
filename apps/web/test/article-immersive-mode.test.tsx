// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { HocuspocusProvider } from "@hocuspocus/provider";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import * as Y from "yjs";

import { ArticleEditor } from "@/features/article/article-editor";
import { WritingWorkspace } from "@/features/article/article-workbench";
import type { ArticleAggregate } from "@/features/article/types";

afterEach(cleanup);

function renderWithClient(ui: React.ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });
  return render(
    <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>,
  );
}

function makeFakeProvider(): HocuspocusProvider {
  const ydoc = new Y.Doc();
  return {
    awareness: {
      clientID: 1,
      doc: ydoc,
      getLocalState: () => ({
        user: { color: "#3b82f6", name: "Alice" },
      }),
      getStates: () => new Map(),
      off: () => {},
      on: () => {},
      setLocalState: () => {},
      setLocalStateField: () => {},
      states: new Map(),
    },
    document: ydoc,
    off: () => {},
    on: () => {},
    status: "connected",
  } as unknown as HocuspocusProvider;
}

describe("Article Editor Immersive Mode", () => {
  it("renders immersive mode toggle button in toolbar and triggers onToggleImmersive", () => {
    const onToggleImmersive = vi.fn();
    const onOpenCommit = vi.fn();

    renderWithClient(
      <ArticleEditor
        blocks={[]}
        canCommit={true}
        canEdit={true}
        chapterTags={[]}
        collaborator={{ color: "#3b82f6", name: "Alice" }}
        draftRevision={3}
        immersive={false}
        onFlush={() => {}}
        onInsertArtifact={async () => ({ reference_id: "ref-1" })}
        onInsertZotero={async () => ({ reference_id: "ref-2" })}
        onOpenCommit={onOpenCommit}
        onOutlineChange={() => {}}
        onReviewBlock={async () => {}}
        onReviewChapter={async () => {}}
        onToggleImmersive={onToggleImmersive}
        projectId="project-1"
        provider={makeFakeProvider()}
      />,
    );

    const toggleBtn = screen.getByRole("button", { name: "进入沉浸编辑模式" });
    expect(toggleBtn).toBeInTheDocument();
    expect(toggleBtn).toHaveTextContent("沉浸模式");

    fireEvent.click(toggleBtn);
    expect(onToggleImmersive).toHaveBeenCalledOnce();
  });

  it("displays draft badge, commit button, and exit button in toolbar when immersive is active", () => {
    const onToggleImmersive = vi.fn();
    const onOpenCommit = vi.fn();

    renderWithClient(
      <ArticleEditor
        blocks={[]}
        canCommit={true}
        canEdit={true}
        chapterTags={[]}
        collaborator={{ color: "#3b82f6", name: "Alice" }}
        draftRevision={7}
        immersive={true}
        onFlush={() => {}}
        onInsertArtifact={async () => ({ reference_id: "ref-1" })}
        onInsertZotero={async () => ({ reference_id: "ref-2" })}
        onOpenCommit={onOpenCommit}
        onOutlineChange={() => {}}
        onReviewBlock={async () => {}}
        onReviewChapter={async () => {}}
        onToggleImmersive={onToggleImmersive}
        projectId="project-1"
        provider={makeFakeProvider()}
      />,
    );

    expect(screen.getByText("草稿 r7")).toBeInTheDocument();

    const commitBtn = screen.getByRole("button", { name: "Commit…" });
    expect(commitBtn).toBeInTheDocument();
    fireEvent.click(commitBtn);
    expect(onOpenCommit).toHaveBeenCalledOnce();

    const exitBtn = screen.getByRole("button", { name: "退出沉浸模式 (Esc)" });
    expect(exitBtn).toBeInTheDocument();
    expect(exitBtn).toHaveTextContent("退出沉浸");

    fireEvent.click(exitBtn);
    expect(onToggleImmersive).toHaveBeenCalledOnce();
  });

  it("toggles immersive mode in WritingWorkspace and exits with Escape key", () => {
    const fakeData: ArticleAggregate = {
      builds: [],
      chapter_tags: [],
      commits: [],
      draft: {
        blocks: [],
        draft_revision: 1,
        project_id: "project-1",
        updated_at: new Date().toISOString(),
      },
      references: [],
      releases: [],
      section_completion: 0.8,
      templates: [],
      unreviewed_blocks: 2,
      warnings: [],
    };

    const { container } = renderWithClient(
      <WritingWorkspace
        canBuild={true}
        canEdit={true}
        canRelease={true}
        collaborator={{ color: "#3b82f6", name: "Alice" }}
        data={fakeData}
        onFlush={() => {}}
        onOpenHistory={() => {}}
        onOpenTemplates={() => {}}
        onRefresh={async () => {}}
        provider={makeFakeProvider()}
        synced={true}
      />,
    );

    const normalDraftBar = screen.getByText("2 个未审阅块 · 完成度 80%");
    expect(normalDraftBar).toBeInTheDocument();

    const enterBtn = screen.getByRole("button", {
      name: "进入沉浸编辑模式",
    });
    expect(enterBtn).toBeInTheDocument();
    fireEvent.click(enterBtn);

    expect(screen.queryByText("2 个未审阅块 · 完成度 80%")).toBeNull();
    const immersiveContainer = container.querySelector(".fixed.inset-0.z-50");
    expect(immersiveContainer).toBeInTheDocument();

    fireEvent.keyDown(window, { key: "Escape" });
    expect(container.querySelector(".fixed.inset-0.z-50")).toBeNull();
    expect(screen.getByText("2 个未审阅块 · 完成度 80%")).toBeInTheDocument();
  });
});
