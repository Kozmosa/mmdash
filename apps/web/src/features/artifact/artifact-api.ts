import { apiClient } from "@/lib/api-client";

import type {
  ArtifactDetail,
  ArtifactFolder,
  ArtifactFolderTree,
  ArtifactListFilters,
  ArtifactPage,
  ArtifactPreview,
  ArtifactVersion,
  DownloadGrant,
  InitializeArtifactUpload,
  PartGrant,
  UpdateArtifact,
  UploadPart,
  UploadSession,
} from "./types";

function projectPath(projectId: string): string {
  return `/projects/${encodeURIComponent(projectId)}/artifacts`;
}

export async function ensureArtifactRootFolder(
  projectId: string,
  name: string,
): Promise<ArtifactFolder> {
  const normalizedName = name.trim().toLocaleLowerCase();
  const findExisting = (tree: ArtifactFolderTree) =>
    tree.items.find(
      (folder) => folder.name.trim().toLocaleLowerCase() === normalizedName,
    );
  const current = await artifactApi.listFolders(projectId);
  const existing = findExisting(current);
  if (existing) return existing;
  try {
    return await artifactApi.createFolder(projectId, name.trim(), null);
  } catch (error) {
    const raced = findExisting(await artifactApi.listFolders(projectId));
    if (raced) return raced;
    throw error;
  }
}

export const artifactApi = {
  createFolder(
    projectId: string,
    name: string,
    parentFolderId: string | null = null,
  ) {
    return apiClient.request<ArtifactFolder>(
      `${projectPath(projectId)}/folders`,
      {
        body: { name, parent_folder_id: parentFolderId },
        method: "POST",
      },
    );
  },

  deleteFolder(projectId: string, folderId: string, recursive = false) {
    return apiClient.request<void>(
      `${projectPath(projectId)}/folders/${encodeURIComponent(folderId)}?recursive=${recursive}`,
      { method: "DELETE" },
    );
  },

  listFolders(projectId: string) {
    return apiClient.request<ArtifactFolderTree>(
      `${projectPath(projectId)}/folders`,
    );
  },

  moveArtifact(
    projectId: string,
    artifactId: string,
    folderId: string | null,
    placement?: { attempt: number; uploadId: string },
  ) {
    return apiClient.request<ArtifactDetail>(
      `${projectPath(projectId)}/${encodeURIComponent(artifactId)}/folder`,
      {
        body: { folder_id: folderId },
        headers: placement
          ? {
              "x-mmdash-placement-attempt": String(placement.attempt),
              "x-mmdash-upload-id": placement.uploadId,
            }
          : undefined,
        method: "PUT",
      },
    );
  },

  moveFolder(
    projectId: string,
    folderId: string,
    parentFolderId: string | null,
    position?: number,
  ) {
    return apiClient.request<ArtifactFolder>(
      `${projectPath(projectId)}/folders/${encodeURIComponent(folderId)}/move`,
      {
        body: { parent_folder_id: parentFolderId, position },
        method: "POST",
      },
    );
  },

  renameFolder(projectId: string, folderId: string, name: string) {
    return apiClient.request<ArtifactFolder>(
      `${projectPath(projectId)}/folders/${encodeURIComponent(folderId)}`,
      { body: { name }, method: "PATCH" },
    );
  },

  abortUpload(projectId: string, uploadId: string) {
    return apiClient.request<void>(
      `${projectPath(projectId)}/uploads/${encodeURIComponent(uploadId)}`,
      { method: "DELETE" },
    );
  },

  confirmUpload(projectId: string, uploadId: string, parts: UploadPart[]) {
    return apiClient.request<ArtifactDetail>(
      `${projectPath(projectId)}/uploads/${encodeURIComponent(uploadId)}/confirm`,
      {
        body: {
          parts: parts
            .map(({ etag, part_number }) => ({ etag, part_number }))
            .sort((left, right) => left.part_number - right.part_number),
        },
        method: "POST",
      },
    );
  },

  download(projectId: string, artifactId: string, versionId?: string) {
    const version = versionId
      ? `/versions/${encodeURIComponent(versionId)}`
      : "";
    return apiClient.request<DownloadGrant>(
      `${projectPath(projectId)}/${encodeURIComponent(artifactId)}${version}/download`,
      { method: "POST" },
    );
  },

  get(projectId: string, artifactId: string) {
    return apiClient.request<ArtifactDetail>(
      `${projectPath(projectId)}/${encodeURIComponent(artifactId)}`,
    );
  },

  getUpload(projectId: string, uploadId: string) {
    return apiClient.request<UploadSession>(
      `${projectPath(projectId)}/uploads/${encodeURIComponent(uploadId)}`,
    );
  },

  initializeUpload(projectId: string, input: InitializeArtifactUpload) {
    return apiClient.request<UploadSession>(
      `${projectPath(projectId)}/uploads`,
      { body: input, method: "POST" },
    );
  },

  initializeVersionUpload(
    projectId: string,
    artifactId: string,
    input: Pick<
      InitializeArtifactUpload,
      "filename" | "idempotency_key" | "mime_type" | "sha256" | "size_bytes"
    >,
  ) {
    return apiClient.request<UploadSession>(
      `${projectPath(projectId)}/${encodeURIComponent(artifactId)}/versions/uploads`,
      { body: input, method: "POST" },
    );
  },

  list(projectId: string, filters: ArtifactListFilters = {}) {
    return apiClient.request<ArtifactPage>(projectPath(projectId), {
      query: filters,
    });
  },

  listPreviews(projectId: string, artifactId: string, versionId: string) {
    return apiClient.request<{ items: ArtifactPreview[] }>(
      `${projectPath(projectId)}/${encodeURIComponent(artifactId)}/versions/${encodeURIComponent(versionId)}/previews`,
    );
  },

  listTrash(
    projectId: string,
    filters: Pick<
      ArtifactListFilters,
      "cursor" | "kind" | "limit" | "tag"
    > = {},
  ) {
    return apiClient.request<ArtifactPage>(`${projectPath(projectId)}/trash`, {
      query: filters,
    });
  },

  listVersions(projectId: string, artifactId: string) {
    return apiClient.request<{ items: ArtifactVersion[] }>(
      `${projectPath(projectId)}/${encodeURIComponent(artifactId)}/versions`,
    );
  },

  purge(projectId: string, artifactId: string) {
    return apiClient.request<void>(
      `${projectPath(projectId)}/${encodeURIComponent(artifactId)}/purge`,
      { method: "DELETE" },
    );
  },

  restore(projectId: string, artifactId: string) {
    return apiClient.request<ArtifactDetail>(
      `${projectPath(projectId)}/${encodeURIComponent(artifactId)}/restore`,
      { method: "POST" },
    );
  },

  restoreVersion(
    projectId: string,
    artifactId: string,
    versionId: string,
    idempotencyKey: string,
  ) {
    return apiClient.request<ArtifactDetail>(
      `${projectPath(projectId)}/${encodeURIComponent(artifactId)}/versions/${encodeURIComponent(versionId)}/restore`,
      {
        body: { idempotency_key: idempotencyKey },
        method: "POST",
      },
    );
  },

  signParts(projectId: string, uploadId: string, partNumbers: number[]) {
    return apiClient.request<{ items: PartGrant[] }>(
      `${projectPath(projectId)}/uploads/${encodeURIComponent(uploadId)}/parts/sign`,
      { body: { part_numbers: partNumbers }, method: "POST" },
    );
  },

  trash(projectId: string, artifactId: string) {
    return apiClient.request<void>(
      `${projectPath(projectId)}/${encodeURIComponent(artifactId)}`,
      { method: "DELETE" },
    );
  },

  update(projectId: string, artifactId: string, input: UpdateArtifact) {
    return apiClient.request<ArtifactDetail>(
      `${projectPath(projectId)}/${encodeURIComponent(artifactId)}`,
      { body: input, method: "PATCH" },
    );
  },
};
