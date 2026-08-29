import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { WritingWorkspace } from "@/features/article/article-workbench";
import { apiClient } from "@/lib/api-client";
import type {
  ArticleAggregate,
  ZoteroCollection,
  ZoteroItem,
} from "@/features/article/types";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const mockCollections: ZoteroCollection[] = [
  {
    collection_key: "col-ml",
    name: "Machine Learning",
    num_collections: 1,
    num_items: 5,
    parent_collection_key: null,
  },
  {
    collection_key: "col-dl",
    name: "Deep Learning",
    num_collections: 0,
    num_items: 2,
    parent_collection_key: "col-ml",
  },
  {
    collection_key: "col-math",
    name: "Math Modeling",
    num_collections: 0,
    num_items: 3,
    parent_collection_key: null,
  },
];

const mockItemsRoot: ZoteroItem[] = [
  {
    authors: ["Alice Smith", "Bob Jones"],
    citation_key: "Smith2026",
    doi: "10.1000/182",
    item_key: "item-1",
    item_type: "journalArticle",
    raw: { key: "item-1", title: "Foundations of Deep Learning" },
    title: "Foundations of Deep Learning",
    version: 1,
    year: "2026",
  },
  {
    authors: ["Charlie Brown"],
    citation_key: "Brown2025",
    item_key: "item-2",
    item_type: "book",
    raw: { key: "item-2", title: "Discrete Math Models" },
    title: "Discrete Math Models",
    version: 2,
    year: "2025",
  },
];

const mockItemsDL: ZoteroItem[] = [
  {
    authors: ["Alice Smith"],
    citation_key: "Smith2026DL",
    item_key: "item-3",
    item_type: "journalArticle",
    raw: { key: "item-3", title: "Transformer Architectures" },
    title: "Transformer Architectures",
    version: 1,
    year: "2026",
  },
];

describe("Article Writing Workspace Zotero panel", () => {
  it("renders Zotero category hierarchy, navigates collections, and allows dragging items without a pin button", async () => {
    vi.spyOn(apiClient, "request").mockImplementation(async (path, options) => {
      if (path.endsWith("/article/zotero/collections")) {
        return { items: mockCollections } as never;
      }
      if (path.includes("/article/zotero/items")) {
        const collection = options?.query?.collection;
        if (collection === "col-dl") {
          return { items: mockItemsDL } as never;
        }
        return { items: mockItemsRoot } as never;
      }
      if (path.endsWith("/article")) {
        return createMockAggregate() as never;
      }
      if (path.endsWith("/settings/article.rendering")) {
        return { values: { theme: "md" } } as never;
      }
      return {} as never;
    });

    render(
      <TestQueryProvider>
        <WritingWorkspace
          canBuild={false}
          canEdit
          canRelease={false}
          data={createMockAggregate()}
          onOpenHistory={vi.fn()}
          onOpenTemplates={vi.fn()}
          onRefresh={vi.fn()}
          projectId="project-1"
        />
      </TestQueryProvider>,
    );

    // Switch to Zotero tab
    const zoteroTab = screen.getByRole("button", { name: "Zotero" });
    fireEvent.click(zoteroTab);

    // Verify collections and items are rendered
    expect(await screen.findByText("Machine Learning")).toBeInTheDocument();
    expect(screen.getByText("Math Modeling")).toBeInTheDocument();
    expect(
      screen.getByText("Foundations of Deep Learning"),
    ).toBeInTheDocument();
    expect(screen.getByText("Discrete Math Models")).toBeInTheDocument();

    // Verify there is NO "固定引用" or "固定并置入" button
    expect(screen.queryByRole("button", { name: "固定引用" })).toBeNull();
    expect(screen.queryByRole("button", { name: "固定并置入" })).toBeNull();
    expect(screen.queryByRole("button", { name: "已固定此版本" })).toBeNull();

    // Verify dragging item card sets application/vnd.mmdash.zotero+json
    const itemCard = screen
      .getByText("Foundations of Deep Learning")
      .closest("[draggable='true']");
    expect(itemCard).toBeInTheDocument();

    const setData = vi.fn();
    fireEvent.dragStart(itemCard!, {
      dataTransfer: {
        setData,
        effectAllowed: "none",
      },
    });
    expect(setData).toHaveBeenCalledWith(
      "application/vnd.mmdash.zotero+json",
      expect.stringContaining("Smith2026"),
    );

    // Click into "Machine Learning" collection
    fireEvent.click(screen.getByText("Machine Learning"));

    // Subcollection "Deep Learning" should be visible
    expect(await screen.findByText("Deep Learning")).toBeInTheDocument();

    // Click into "Deep Learning"
    fireEvent.click(screen.getByText("Deep Learning"));

    // Should load items in "Deep Learning"
    expect(
      await screen.findByText("Transformer Architectures"),
    ).toBeInTheDocument();

    // Click breadcrumb "全部条目" to return to root
    fireEvent.click(screen.getByRole("button", { name: /全部条目/ }));
    expect(await screen.findByText("Machine Learning")).toBeInTheDocument();
  });
});

function createMockAggregate(): ArticleAggregate {
  return {
    builds: [],
    chapter_tags: [],
    commits: [],
    draft: {
      blocks: [],
      content: { type: "doc", content: [] },
      created_at: "2026-08-19T00:00:00Z",
      draft_id: "draft-1",
      draft_revision: 1,
      format: "latex",
      patch_count: 0,
      project_id: "project-1",
      schema_version: "1.0",
      unreviewed_patch_count: 0,
      updated_at: "2026-08-19T00:00:00Z",
    },
    references: [],
    releases: [],
    section_completion: 0,
    templates: [],
    unreviewed_blocks: 0,
    warnings: [],
  };
}

function TestQueryProvider({ children }: Readonly<{ children: ReactNode }>) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
