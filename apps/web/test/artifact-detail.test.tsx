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

import { ArtifactDetailDrawer } from "@/features/artifact/artifact-detail-drawer";

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  listPreviews: vi.fn(),
  listVersions: vi.fn(),
  trash: vi.fn(),
  update: vi.fn(),
}));

vi.mock("@/components/providers/project-provider", () => ({
  useCurrentProject: () => ({
    id: "00000000-0000-4000-8000-000000000001",
    role: "maintainer",
  }),
}));

vi.mock("@/features/artifact/artifact-api", () => ({
  artifactApi: {
    get: mocks.get,
    listPreviews: mocks.listPreviews,
    listVersions: mocks.listVersions,
    trash: mocks.trash,
    update: mocks.update,
  },
}));

vi.mock("@/features/artifact/artifact-uploader", () => ({
  ArtifactUploader: () => null,
  formatBytes: (value: number) => `${value} B`,
}));

const projectId = "00000000-0000-4000-8000-000000000001";
const artifactId = "00000000-0000-4000-8000-000000000002";
const version = {
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
};
const detail = {
  artifact: {
    artifact_id: artifactId,
    created_at: "2026-07-30T00:00:00Z",
    created_by: "user-1",
    current_version_id: version.version_id,
    description: "Original",
    kind: "problem",
    name: "Problem statement",
    project_id: projectId,
    recommended_usage: [],
    source: "user_upload",
    source_object_id: null,
    status: "available",
    tags: ["source"],
    trashed_at: null,
    updated_at: "2026-07-30T00:00:00Z",
  },
  current_version: version,
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

describe("Artifact detail drawer", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.get.mockResolvedValue(detail);
    mocks.listPreviews.mockResolvedValue({ items: [] });
    mocks.listVersions.mockResolvedValue({ items: [version] });
    mocks.update.mockResolvedValue({
      ...detail,
      artifact: { ...detail.artifact, name: "Updated statement" },
    });
    mocks.trash.mockResolvedValue(undefined);
  });

  afterEach(cleanup);

  it("preserves a system kind while updating other metadata", async () => {
    const systemDetail = {
      ...detail,
      artifact: {
        ...detail.artifact,
        kind: "repository_source",
      },
    };
    render(
      <ArtifactDetailDrawer
        artifactId={artifactId}
        initialDetail={systemDetail as never}
        onClose={vi.fn()}
        projectId={projectId}
        trashView={false}
      />,
      { wrapper: Providers },
    );

    fireEvent.change(screen.getByLabelText("展示名称"), {
      target: { value: "Updated repository source" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存元数据" }));

    await waitFor(() => expect(mocks.update).toHaveBeenCalled());
    const update = mocks.update.mock.calls.at(-1)?.[2] as Record<
      string,
      unknown
    >;
    expect(update).toMatchObject({
      description: "Original",
      name: "Updated repository source",
      tags: ["source"],
    });
    expect(update).not.toHaveProperty("kind");
  });

  it("uses trash-list detail without active-only detail, preview, or download grants", async () => {
    render(
      <ArtifactDetailDrawer
        artifactId={artifactId}
        initialDetail={
          {
            ...detail,
            artifact: {
              ...detail.artifact,
              status: "trashed",
              trashed_at: "2026-07-30T01:00:00Z",
            },
          } as never
        }
        onClose={vi.fn()}
        projectId={projectId}
        trashView
      />,
      { wrapper: Providers },
    );

    await waitFor(() => expect(mocks.listVersions).toHaveBeenCalled());
    expect(mocks.get).not.toHaveBeenCalled();
    expect(mocks.listPreviews).not.toHaveBeenCalled();
    expect(
      screen.queryByRole("button", { name: /下载版本/ }),
    ).not.toBeInTheDocument();
  });

  it("edits safe metadata and moves the Artifact to recoverable trash", async () => {
    const onClose = vi.fn();
    render(
      <ArtifactDetailDrawer
        artifactId={artifactId}
        initialDetail={detail as never}
        onClose={onClose}
        projectId={projectId}
        trashView={false}
      />,
      { wrapper: Providers },
    );

    fireEvent.change(screen.getByLabelText("展示名称"), {
      target: { value: "Updated statement" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存元数据" }));
    await waitFor(() =>
      expect(mocks.update).toHaveBeenCalledWith(
        projectId,
        artifactId,
        expect.objectContaining({
          description: "Original",
          kind: "problem",
          name: "Updated statement",
          tags: ["source"],
        }),
      ),
    );

    fireEvent.click(screen.getByRole("button", { name: "移入回收站" }));
    await waitFor(() =>
      expect(mocks.trash).toHaveBeenCalledWith(projectId, artifactId),
    );
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });
});
