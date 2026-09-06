import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  listStoredUploads,
  MultipartUploadTask,
} from "@/features/artifact/multipart-upload";
import { ApiError } from "@/lib/api-client";

const mocks = vi.hoisted(() => ({
  abortUpload: vi.fn(),
  confirmUpload: vi.fn(),
  get: vi.fn(),
  getUpload: vi.fn(),
  initializeUpload: vi.fn(),
  initializeVersionUpload: vi.fn(),
  moveArtifact: vi.fn(),
  signParts: vi.fn(),
}));

vi.mock("@/features/artifact/artifact-api", () => ({
  artifactApi: mocks,
}));

const projectId = "00000000-0000-4000-8000-000000000001";
const uploadId = "00000000-0000-4000-8000-000000000002";

function uploadSession(overrides: Record<string, unknown> = {}) {
  return {
    artifact_id: "00000000-0000-4000-8000-000000000003",
    completed_parts: [],
    created_at: "2026-07-30T00:00:00Z",
    expires_at: "2026-07-30T01:00:00Z",
    part_count: 3,
    part_size_bytes: 2,
    sha256: "bef57ec7f53a6d40beb640a780a639c83bc29ac8a9816f1fc6c5c6dcd93c4721",
    size_bytes: 6,
    status: "initialized",
    transfer_mode: "local_proxy",
    updated_at: "2026-07-30T00:00:00Z",
    upload_id: uploadId,
    upload_mode: "multipart",
    version_id: "00000000-0000-4000-8000-000000000004",
    ...overrides,
  };
}

function artifactDetail(folderId: string | null = null) {
  return {
    artifact: {
      artifact_id: "00000000-0000-4000-8000-000000000003",
      folder_id: folderId,
    },
    current_version: {
      filename: "source.txt",
      mime_type: "text/plain",
      status: "available",
      version_id: "00000000-0000-4000-8000-000000000004",
    },
  };
}

function storedUpload(folderId: string) {
  return {
    artifactId: "00000000-0000-4000-8000-000000000003",
    createdAt: "2026-07-30T00:00:00Z",
    fileLastModified: 100,
    fileName: "source.txt",
    fileSize: 6,
    folderId,
    idempotencyKey: "upload-key",
    kind: "attachment" as const,
    projectId,
    sha256: "bef57ec7f53a6d40beb640a780a639c83bc29ac8a9816f1fc6c5c6dcd93c4721",
    tags: [],
    uploadId,
    versionId: "00000000-0000-4000-8000-000000000004",
  };
}

describe("Artifact multipart upload task", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    const storage = memoryStorage();
    vi.stubGlobal("localStorage", storage);
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: storage,
    });
    mocks.initializeUpload.mockResolvedValue(uploadSession());
    mocks.signParts.mockImplementation(
      (_projectId: string, _uploadId: string, [partNumber]: number[]) =>
        Promise.resolve({
          items: [
            {
              part_number: partNumber,
              size_bytes: 2,
              transfer: {
                expires_at: "2026-07-30T00:01:00Z",
                headers: {},
                method: "PUT",
                url: `/api/artifact-transfers/token-${partNumber}`,
              },
            },
          ],
        }),
    );
    mocks.confirmUpload.mockResolvedValue({
      artifact: {
        artifact_id: "00000000-0000-4000-8000-000000000003",
      },
    });
    mocks.moveArtifact.mockImplementation(
      (_projectId: string, _artifactId: string, folderId: string) =>
        Promise.resolve(artifactDetail(folderId)),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("uploads with bounded concurrency, retries one part, and confirms in order", async () => {
    let active = 0;
    let maximumActive = 0;
    const attempts = new Map<number, number>();
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const partNumber = Number(String(input).split("-").at(-1));
        const attempt = (attempts.get(partNumber) ?? 0) + 1;
        attempts.set(partNumber, attempt);
        active += 1;
        maximumActive = Math.max(maximumActive, active);
        return new Promise<Response>((resolve) => {
          window.setTimeout(() => {
            active -= 1;
            resolve(
              partNumber === 2 && attempt === 1
                ? new Response(null, { status: 503 })
                : new Response(null, {
                    headers: { etag: `"etag-${partNumber}"` },
                    status: 204,
                  }),
            );
          }, 5);
        });
      }),
    );
    const file = new File(["abcdef"], "source.txt", {
      lastModified: 100,
      type: "text/plain",
    });
    const task = new MultipartUploadTask({
      concurrency: 2,
      file,
      kind: "problem",
      projectId,
      retryLimit: 2,
    });

    await task.start();

    expect(maximumActive).toBeLessThanOrEqual(2);
    expect(attempts.get(2)).toBe(2);
    expect(mocks.signParts).toHaveBeenCalledTimes(4);
    expect(mocks.confirmUpload).toHaveBeenCalledWith(
      projectId,
      uploadId,
      expect.arrayContaining([
        expect.objectContaining({ part_number: 1 }),
        expect.objectContaining({ part_number: 2 }),
        expect.objectContaining({ part_number: 3 }),
      ]),
    );
    expect(task.getSnapshot()).toMatchObject({
      completedBytes: 6,
      progress: 1,
      status: "completed",
    });
    expect(listStoredUploads(projectId)).toEqual([]);
  });

  it("assigns the target folder atomically during upload initialization", async () => {
    const folderId = "00000000-0000-4000-8000-000000000005";
    mocks.confirmUpload.mockResolvedValue(artifactDetail(folderId));
    vi.stubGlobal(
      "fetch",
      vi.fn(() =>
        Promise.resolve(
          new Response(null, {
            headers: { etag: '"etag"' },
            status: 204,
          }),
        ),
      ),
    );

    const task = new MultipartUploadTask({
      file: new File(["abcdef"], "source.txt", { type: "text/plain" }),
      folderId,
      projectId,
    });
    await task.start();

    expect(mocks.initializeUpload).toHaveBeenCalledWith(
      projectId,
      expect.objectContaining({ folder_id: folderId }),
    );
    expect(mocks.moveArtifact).not.toHaveBeenCalled();
    expect(task.getSnapshot()).toMatchObject({ status: "completed" });
  });

  it("reconciles and retries a transient folder assignment for a legacy upload", async () => {
    const folderId = "00000000-0000-4000-8000-000000000005";
    const stored = storedUpload(folderId);
    localStorage.setItem(
      `mmdash.artifact-upload.v1.${projectId}.${uploadId}`,
      JSON.stringify(stored),
    );
    mocks.getUpload.mockResolvedValue(
      uploadSession({ status: "completed", upload_mode: "multipart" }),
    );
    mocks.get.mockResolvedValue(artifactDetail());
    mocks.moveArtifact
      .mockRejectedValueOnce(
        new ApiError({
          message: "Core service is temporarily unavailable",
          status: 502,
        }),
      )
      .mockResolvedValueOnce(artifactDetail(folderId));

    const task = new MultipartUploadTask({
      file: new File(["abcdef"], "source.txt", {
        lastModified: 100,
        type: "text/plain",
      }),
      projectId,
      retryLimit: 2,
      stored,
    });
    const detail = await task.start();

    expect(mocks.moveArtifact).toHaveBeenCalledTimes(2);
    expect(detail.artifact.folder_id).toBe(folderId);
    expect(task.getSnapshot()).toMatchObject({ status: "completed" });
    expect(task.getSnapshot().placementError).toBeUndefined();
    expect(listStoredUploads(projectId)).toEqual([]);
  });

  it("keeps a confirmed legacy upload recoverable when folder assignment stays unavailable", async () => {
    const folderId = "00000000-0000-4000-8000-000000000005";
    const stored = storedUpload(folderId);
    localStorage.setItem(
      `mmdash.artifact-upload.v1.${projectId}.${uploadId}`,
      JSON.stringify(stored),
    );
    mocks.getUpload.mockResolvedValue(
      uploadSession({ status: "completed", upload_mode: "multipart" }),
    );
    mocks.get.mockResolvedValue(artifactDetail());
    mocks.moveArtifact.mockRejectedValue(
      new ApiError({
        message: "Core service is temporarily unavailable",
        status: 502,
      }),
    );

    const task = new MultipartUploadTask({
      file: new File(["abcdef"], "source.txt", {
        lastModified: 100,
        type: "text/plain",
      }),
      projectId,
      retryLimit: 2,
      stored,
    });
    const detail = await task.start();

    expect(detail.artifact.folder_id).toBeNull();
    expect(task.getSnapshot()).toMatchObject({ status: "completed" });
    expect(task.getSnapshot().placementError?.message).toContain(
      "文件已上传，但暂未归档到目标文件夹",
    );
    expect(listStoredUploads(projectId)).toEqual([stored]);
  });

  it("recovers completed provider parts after a refresh and uploads only missing parts", async () => {
    const file = new File(["abcdef"], "source.txt", {
      lastModified: 200,
      type: "text/plain",
    });
    const stored = {
      artifactId: "00000000-0000-4000-8000-000000000003",
      createdAt: "2026-07-30T00:00:00Z",
      fileLastModified: 100,
      fileName: "source.txt",
      fileSize: 6,
      idempotencyKey: "upload-key",
      kind: "problem" as const,
      projectId,
      sha256:
        "bef57ec7f53a6d40beb640a780a639c83bc29ac8a9816f1fc6c5c6dcd93c4721",
      tags: [],
      uploadId,
      versionId: "00000000-0000-4000-8000-000000000004",
    };
    localStorage.setItem(
      `mmdash.artifact-upload.v1.${projectId}.${uploadId}`,
      JSON.stringify(stored),
    );
    mocks.getUpload.mockResolvedValue(
      uploadSession({
        completed_parts: [
          {
            completed_at: "2026-07-30T00:00:10Z",
            etag: '"etag-1"',
            part_number: 1,
            size_bytes: 2,
          },
        ],
        status: "uploading",
      }),
    );
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) =>
        Promise.resolve(
          new Response(null, {
            headers: {
              etag: `"etag-${String(input).split("-").at(-1)}"`,
            },
            status: 204,
          }),
        ),
      ),
    );

    await new MultipartUploadTask({
      concurrency: 2,
      file,
      projectId,
      stored,
    }).start();

    expect(mocks.initializeUpload).not.toHaveBeenCalled();
    expect(mocks.getUpload).toHaveBeenCalledWith(projectId, uploadId);
    expect(mocks.signParts.mock.calls.map((call) => call[2][0]).sort()).toEqual(
      [2, 3],
    );
    expect(listStoredUploads(projectId)).toEqual([]);
  });

  it("pauses an in-flight part, resumes with a fresh grant, and completes", async () => {
    mocks.initializeUpload.mockResolvedValue(
      uploadSession({ part_count: 1, part_size_bytes: 6 }),
    );
    let firstSignal: AbortSignal | undefined;
    let attempt = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
        attempt += 1;
        if (attempt === 1) {
          firstSignal = init?.signal ?? undefined;
          return new Promise<Response>((_resolve, reject) => {
            firstSignal?.addEventListener("abort", () =>
              reject(new DOMException("paused", "AbortError")),
            );
          });
        }
        return Promise.resolve(
          new Response(null, {
            headers: { etag: '"etag-resumed"' },
            status: 204,
          }),
        );
      }),
    );
    const task = new MultipartUploadTask({
      concurrency: 1,
      file: new File(["abcdef"], "source.txt", {
        lastModified: 100,
        type: "text/plain",
      }),
      projectId,
    });
    const completion = task.start();
    await vi.waitFor(() => expect(firstSignal).toBeDefined());

    task.pause();
    expect(firstSignal?.aborted).toBe(true);
    expect(task.getSnapshot().status).toBe("paused");
    task.resume();
    await completion;

    expect(attempt).toBe(2);
    expect(mocks.signParts).toHaveBeenCalledTimes(2);
    expect(task.getSnapshot().status).toBe("completed");
  });

  it("cancels provider state and removes the refresh record", async () => {
    mocks.initializeUpload.mockResolvedValue(
      uploadSession({ part_count: 1, part_size_bytes: 6 }),
    );
    let requestStarted = false;
    vi.stubGlobal(
      "fetch",
      vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
        requestStarted = true;
        return new Promise<Response>((_resolve, reject) => {
          init?.signal?.addEventListener("abort", () =>
            reject(new DOMException("cancelled", "AbortError")),
          );
        });
      }),
    );
    const task = new MultipartUploadTask({
      concurrency: 1,
      file: new File(["abcdef"], "source.txt", {
        lastModified: 100,
        type: "text/plain",
      }),
      projectId,
    });
    const completion = task.start();
    await vi.waitFor(() => expect(requestStarted).toBe(true));

    await task.cancel();
    await expect(completion).rejects.toMatchObject({ name: "AbortError" });

    expect(mocks.abortUpload).toHaveBeenCalledWith(projectId, uploadId);
    expect(task.getSnapshot().status).toBe("cancelled");
    expect(listStoredUploads(projectId)).toEqual([]);
  });
});

function memoryStorage(): Storage {
  const values = new Map<string, string>();
  return {
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    key: (index) => [...values.keys()][index] ?? null,
    get length() {
      return values.size;
    },
    removeItem: (key) => {
      values.delete(key);
    },
    setItem: (key, value) => {
      values.set(key, value);
    },
  };
}
