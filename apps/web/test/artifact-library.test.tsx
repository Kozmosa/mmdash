import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ArtifactLibrary } from "@/features/artifact/artifact-library";

const mocks = vi.hoisted(() => ({
  apiRequest: vi.fn(),
  createFolder: vi.fn(),
  list: vi.fn(),
  listFolders: vi.fn(),
  listTrash: vi.fn(),
  moveArtifact: vi.fn(),
  moveFolder: vi.fn(),
  renameFolder: vi.fn(),
  deleteFolder: vi.fn(),
  push: vi.fn(),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: mocks.push }),
  useSearchParams: () => new URLSearchParams("setup=1"),
}));

vi.mock("@/components/providers/project-provider", () => ({
  useCurrentProject: () => ({
    id: "00000000-0000-4000-8000-000000000001",
    name: "Artifact Project",
    role: "owner",
  }),
}));

vi.mock("@/features/artifact/artifact-api", () => ({
  artifactApi: {
    createFolder: mocks.createFolder,
    deleteFolder: mocks.deleteFolder,
    download: vi.fn(),
    list: mocks.list,
    listFolders: mocks.listFolders,
    listTrash: mocks.listTrash,
    moveArtifact: mocks.moveArtifact,
    moveFolder: mocks.moveFolder,
    renameFolder: mocks.renameFolder,
  },
}));

vi.mock("@/features/artifact/artifact-detail-drawer", () => ({
  ArtifactDetailDrawer: ({ artifactId }: { artifactId?: string }) =>
    artifactId ? <div>详情 {artifactId}</div> : null,
}));

vi.mock("@/features/artifact/artifact-uploader", () => ({
  ArtifactUploader: () => null,
  formatBytes: (value: number) => `${value} B`,
}));

vi.mock("@/lib/api-client", () => ({
  apiClient: { request: mocks.apiRequest },
}));

const artifactId = "00000000-0000-4000-8000-000000000002";
const detail = {
  artifact: {
    artifact_id: artifactId,
    created_at: "2026-07-30T00:00:00Z",
    created_by: "user-1",
    current_version_id: "00000000-0000-4000-8000-000000000003",
    description: null,
    folder_id: null,
    kind: "problem",
    name: "Problem statement",
    project_id: "00000000-0000-4000-8000-000000000001",
    recommended_usage: [],
    source: "user_upload",
    source_object_id: null,
    status: "available",
    tags: ["source"],
    trashed_at: null,
    updated_at: "2026-07-30T00:00:00Z",
  },
  current_version: {
    artifact_id: artifactId,
    available_at: "2026-07-30T00:00:00Z",
    created_at: "2026-07-30T00:00:00Z",
    created_by: "user-1",
    filename: "problem.pdf",
    git_reference: null,
    mime_type: "application/pdf",
    sha256: "a".repeat(64),
    size_bytes: 42,
    status: "available",
    storage_class: "object",
    version_id: "00000000-0000-4000-8000-000000000003",
    version_no: 1,
  },
};

function Providers({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: { queries: { retry: false } },
        })
      }
    >
      {children}
    </QueryClientProvider>
  );
}

describe("Artifact library selector", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.list.mockResolvedValue({
      has_more: false,
      items: [detail],
      next_cursor: null,
    });
    mocks.listTrash.mockResolvedValue({
      has_more: false,
      items: [],
      next_cursor: null,
    });
    mocks.listFolders.mockResolvedValue({
      items: [
        {
          children: [],
          folder_id: "00000000-0000-4000-8000-000000000010",
          name: "资料",
          parent_folder_id: null,
          position: 0,
          project_id: "00000000-0000-4000-8000-000000000001",
        },
      ],
    });
    mocks.moveArtifact.mockResolvedValue(detail);
    mocks.createFolder.mockResolvedValue({
      children: [],
      folder_id: "00000000-0000-4000-8000-000000000011",
      name: "新文件夹",
      parent_folder_id: null,
      position: 1,
      project_id: "00000000-0000-4000-8000-000000000001",
    });
    mocks.apiRequest.mockImplementation(
      (_path: string, options?: { method?: string }) =>
        Promise.resolve(
          options?.method === "PATCH"
            ? { id: "00000000-0000-4000-8000-000000000001" }
            : {
                id: "00000000-0000-4000-8000-000000000001",
                source_artifact_ids: [],
              },
        ),
    );
  });

  afterEach(cleanup);

  it("selects an available source file and persists the two-step Project binding", async () => {
    render(<ArtifactLibrary />, { wrapper: Providers });

    fireEvent.click(
      await screen.findByRole("checkbox", {
        name: "选择 Problem statement",
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "保存并返回首页" }));

    await waitFor(() =>
      expect(mocks.apiRequest).toHaveBeenCalledWith(
        "/projects/00000000-0000-4000-8000-000000000001",
        {
          body: { source_artifact_ids: [artifactId] },
          method: "PATCH",
        },
      ),
    );
    expect(mocks.push).toHaveBeenCalledWith(
      "/projects/00000000-0000-4000-8000-000000000001",
    );
  });

  it("can remove a bound source that is absent from the active library", async () => {
    const unavailableId = "00000000-0000-4000-8000-000000000099";
    mocks.apiRequest.mockImplementation(
      (_path: string, options?: { method?: string }) =>
        Promise.resolve(
          options?.method === "PATCH"
            ? { id: "00000000-0000-4000-8000-000000000001" }
            : {
                id: "00000000-0000-4000-8000-000000000001",
                source_artifact_ids: [unavailableId],
              },
        ),
    );
    render(<ArtifactLibrary />, { wrapper: Providers });

    fireEvent.click(
      await screen.findByRole("button", {
        name: `移除 ${unavailableId}`,
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "保存并返回首页" }));

    await waitFor(() =>
      expect(mocks.apiRequest).toHaveBeenCalledWith(
        "/projects/00000000-0000-4000-8000-000000000001",
        {
          body: { source_artifact_ids: [] },
          method: "PATCH",
        },
      ),
    );
  });

  it("filters by fixed source and opens the shared detail drawer", async () => {
    render(<ArtifactLibrary />, { wrapper: Providers });
    await screen.findByText("Problem statement");

    fireEvent.change(screen.getByLabelText("来源"), {
      target: { value: "user_upload" },
    });
    await waitFor(() =>
      expect(mocks.list).toHaveBeenLastCalledWith(
        "00000000-0000-4000-8000-000000000001",
        expect.objectContaining({ source: "user_upload" }),
      ),
    );

    fireEvent.click(await screen.findByText("Problem statement"));
    expect(screen.getByText(`详情 ${artifactId}`)).toBeInTheDocument();
  });

  it("uses the shared folder API instead of browser-local mappings", async () => {
    render(<ArtifactLibrary />, { wrapper: Providers });
    const artifactTitle = await screen.findByText("Problem statement");
    const artifactCard = artifactTitle.closest("[draggable='true']");
    const folderTitle = screen.getByRole("button", { name: "资料" });
    const folderCard = folderTitle.closest("[draggable='true']");
    expect(artifactCard).not.toBeNull();
    expect(folderCard).not.toBeNull();
    const values = new Map<string, string>();
    const dataTransfer = {
      dropEffect: "none",
      effectAllowed: "none",
      getData: (type: string) => values.get(type) ?? "",
      setData: (type: string, value: string) => values.set(type, value),
      types: [] as string[],
    };
    fireEvent.dragStart(artifactCard!, { dataTransfer });
    dataTransfer.types = [...values.keys()];
    fireEvent.dragOver(folderCard!, { dataTransfer });
    fireEvent.drop(folderCard!, { dataTransfer });
    await waitFor(() =>
      expect(mocks.moveArtifact).toHaveBeenCalledWith(
        "00000000-0000-4000-8000-000000000001",
        artifactId,
        "00000000-0000-4000-8000-000000000010",
      ),
    );
    expect(window.localStorage.getItem("mmdash-artifact-folders")).toBeNull();
  });
});
