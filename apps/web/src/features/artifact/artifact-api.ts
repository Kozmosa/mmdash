import { apiClient } from "@/lib/api-client";

import type {
  ArtifactDetail,
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

export const artifactApi = {
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
