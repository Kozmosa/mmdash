// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type { HocuspocusProvider } from "@hocuspocus/provider";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import * as Y from "yjs";

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

describe("WritingWorkspace sidebar header", () => {
  it("hides scrollbar, supports horizontal wheel scrolling, and keeps collapse button fixed", () => {
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

    const tabContainer = container.querySelector(".overflow-x-auto");
    expect(tabContainer).toBeInTheDocument();
    expect(tabContainer?.className).toContain("[scrollbar-width:none]");
    expect(tabContainer?.className).toContain("[&::-webkit-scrollbar]:hidden");

    // Check that collapse button is outside the tab container
    const collapseBtn = screen.getByRole("button", { name: "折叠左栏" });
    expect(collapseBtn).toBeInTheDocument();
    expect(tabContainer?.contains(collapseBtn)).toBe(false);

    // Test mouse wheel event
    Object.defineProperty(tabContainer, "scrollLeft", {
      value: 0,
      writable: true,
    });
    fireEvent.wheel(tabContainer!, { deltaY: 50 });
    expect(tabContainer?.scrollLeft).toBe(50);
  });

  it("renders outline resize handle between panel and TOC and supports resizing", () => {
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

    renderWithClient(
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

    const outlineResizeHandle = screen.getByRole("separator", {
      name: "拖动调整目录高度",
    });
    expect(outlineResizeHandle).toBeInTheDocument();

    const tocNav = screen.getByRole("navigation", { name: "论文目录" });
    expect(tocNav).toBeInTheDocument();
    expect(tocNav).toHaveStyle({ height: "220px" });

    // Keyboard resize
    fireEvent.keyDown(outlineResizeHandle, { key: "ArrowUp" });
    expect(tocNav).toHaveStyle({ height: "236px" });

    fireEvent.keyDown(outlineResizeHandle, { key: "ArrowDown" });
    expect(tocNav).toHaveStyle({ height: "220px" });
  });
});
