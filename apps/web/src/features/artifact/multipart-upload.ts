import { artifactApi } from "./artifact-api";
import { hashFile } from "./sha256";
import type {
  ArtifactDetail,
  PublicArtifactKind,
  UploadPart,
  UploadSession,
} from "./types";

const storagePrefix = "mmdash.artifact-upload.v1.";

export type StoredArtifactUpload = {
  artifactId: string;
  createdAt: string;
  description?: string;
  fileLastModified: number;
  fileName: string;
  fileSize: number;
  folderId?: string;
  idempotencyKey: string;
  kind: PublicArtifactKind;
  name?: string;
  projectId: string;
  sha256: string;
  tags: string[];
  uploadId: string;
  versionId: string;
};

export type UploadTaskStatus =
  | "idle"
  | "hashing"
  | "initializing"
  | "uploading"
  | "paused"
  | "confirming"
  | "completed"
  | "cancelled"
  | "failed";

export type UploadTaskSnapshot = {
  completedBytes: number;
  error?: Error;
  fileName: string;
  progress: number;
  session?: UploadSession;
  status: UploadTaskStatus;
  totalBytes: number;
};

type UploadTaskListener = (snapshot: UploadTaskSnapshot) => void;

type UploadTaskOptions = {
  artifactId?: string;
  concurrency?: number;
  description?: string;
  file: File;
  folderId?: string | null;
  idempotencyKey?: string;
  kind?: PublicArtifactKind;
  name?: string;
  projectId: string;
  retryLimit?: number;
  stored?: StoredArtifactUpload;
  tags?: string[];
};

export class MultipartUploadTask {
  private readonly concurrency: number;
  private readonly listeners = new Set<UploadTaskListener>();
  private readonly retryLimit: number;
  private readonly uploadControllers = new Set<AbortController>();
  private cancelled = false;
  private file: File;
  private pauseWaiters: (() => void)[] = [];
  private paused = false;
  private session?: UploadSession;
  private snapshot: UploadTaskSnapshot;

  constructor(private readonly options: UploadTaskOptions) {
    this.file = options.file;
    this.concurrency = clamp(options.concurrency ?? 3, 1, 6);
    this.retryLimit = clamp(options.retryLimit ?? 3, 1, 5);
    this.snapshot = {
      completedBytes: 0,
      fileName: options.file.name,
      progress: 0,
      status: "idle",
      totalBytes: options.file.size,
    };
  }

  subscribe(listener: UploadTaskListener): () => void {
    this.listeners.add(listener);
    listener(this.getSnapshot());
    return () => this.listeners.delete(listener);
  }

  getSnapshot(): UploadTaskSnapshot {
    return { ...this.snapshot };
  }

  async start(): Promise<ArtifactDetail> {
    try {
      const sha256 = await this.prepareHash();
      this.assertActive();
      this.setStatus("initializing");
      this.session = this.options.stored
        ? await this.recoverStored(sha256)
        : await this.initialize(sha256);
      this.update({ session: this.session });
      if (
        this.session.upload_mode === "deduplicated" ||
        this.session.status === "completed"
      ) {
        removeStoredUpload(this.options.projectId, this.session.upload_id);
        const detail = await artifactApi.get(
          this.options.projectId,
          this.session.artifact_id,
        );
        const placed = await this.placeInFolder(detail);
        this.update({
          completedBytes: this.file.size,
          progress: 1,
          status: "completed",
        });
        return placed;
      }
      persistStoredUpload(this.toStored(sha256));
      const parts = await this.uploadParts(this.session);
      this.assertActive();
      this.setStatus("confirming");
      const detail = await artifactApi.confirmUpload(
        this.options.projectId,
        this.session.upload_id,
        parts,
      );
      removeStoredUpload(this.options.projectId, this.session.upload_id);
      this.update({
        completedBytes: this.file.size,
        progress: 1,
        status: "completed",
      });
      return this.placeInFolder(detail);
    } catch (error) {
      if (this.cancelled) {
        this.setStatus("cancelled");
      } else {
        const normalized =
          error instanceof Error ? error : new Error("Artifact upload failed");
        this.update({ error: normalized, status: "failed" });
      }
      throw error;
    }
  }

  pause(): void {
    if (this.snapshot.status !== "uploading") {
      return;
    }
    this.paused = true;
    for (const controller of this.uploadControllers) {
      controller.abort();
    }
    this.setStatus("paused");
  }

  resume(file?: File): void {
    if (file) {
      this.validateResumeFile(file);
      this.file = file;
    }
    if (!this.paused) {
      return;
    }
    this.paused = false;
    this.setStatus(this.session ? "uploading" : "hashing");
    for (const resolve of this.pauseWaiters.splice(0)) {
      resolve();
    }
  }

  async cancel(): Promise<void> {
    this.cancelled = true;
    this.paused = false;
    for (const controller of this.uploadControllers) {
      controller.abort();
    }
    for (const resolve of this.pauseWaiters.splice(0)) {
      resolve();
    }
    if (this.session) {
      await artifactApi.abortUpload(
        this.options.projectId,
        this.session.upload_id,
      );
      removeStoredUpload(this.options.projectId, this.session.upload_id);
    }
    this.setStatus("cancelled");
  }

  private async prepareHash(): Promise<string> {
    this.setStatus("hashing");
    const sha256 = await hashFile(this.file, {
      onProgress: (completed, total) => {
        if (!this.session) {
          this.update({
            completedBytes: completed,
            progress: total === 0 ? 1 : completed / total,
          });
        }
      },
    });
    if (this.options.stored && sha256 !== this.options.stored.sha256) {
      throw new Error("所选文件内容与待恢复上传不一致");
    }
    return sha256;
  }

  private async initialize(sha256: string): Promise<UploadSession> {
    const input = {
      filename: this.file.name,
      idempotency_key: this.options.idempotencyKey ?? createIdempotencyKey(),
      mime_type: this.file.type || "application/octet-stream",
      sha256,
      size_bytes: this.file.size,
    };
    if (this.options.artifactId) {
      return artifactApi.initializeVersionUpload(
        this.options.projectId,
        this.options.artifactId,
        input,
      );
    }
    return artifactApi.initializeUpload(this.options.projectId, {
      ...input,
      description: this.options.description,
      kind: this.options.kind ?? "attachment",
      name: this.options.name || this.file.name,
      tags: this.options.tags ?? [],
    });
  }

  private async recoverStored(sha256: string): Promise<UploadSession> {
    const stored = this.options.stored!;
    if (
      stored.fileName !== this.file.name ||
      stored.fileSize !== this.file.size ||
      stored.sha256 !== sha256
    ) {
      throw new Error("所选文件与待恢复上传的文件指纹不一致");
    }
    return artifactApi.getUpload(stored.projectId, stored.uploadId);
  }

  private async uploadParts(session: UploadSession): Promise<UploadPart[]> {
    const uploaded = new Map(
      session.completed_parts.map((part) => [part.part_number, part]),
    );
    this.updateProgress(session, uploaded);
    const pending = Array.from(
      { length: session.part_count },
      (_, index) => index + 1,
    ).filter((partNumber) => !uploaded.has(partNumber));
    this.setStatus("uploading");

    const worker = async () => {
      while (pending.length > 0) {
        await this.waitWhilePaused();
        this.assertActive();
        const partNumber = pending.shift();
        if (partNumber === undefined) {
          return;
        }
        try {
          const part = await this.uploadPart(session, partNumber);
          uploaded.set(part.part_number, part);
          this.updateProgress(session, uploaded);
        } catch (error) {
          if (this.paused && isAbortError(error)) {
            pending.unshift(partNumber);
            continue;
          }
          throw error;
        }
      }
    };
    await Promise.all(
      Array.from(
        { length: Math.min(this.concurrency, pending.length || 1) },
        () => worker(),
      ),
    );
    return [...uploaded.values()].sort(
      (left, right) => left.part_number - right.part_number,
    );
  }

  private async uploadPart(
    session: UploadSession,
    partNumber: number,
  ): Promise<UploadPart> {
    const start = (partNumber - 1) * session.part_size_bytes;
    const end = Math.min(this.file.size, start + session.part_size_bytes);
    const body = this.file.slice(start, end);
    let lastError: unknown;
    for (let attempt = 0; attempt < this.retryLimit; attempt += 1) {
      await this.waitWhilePaused();
      this.assertActive();
      const grants = await artifactApi.signParts(
        this.options.projectId,
        session.upload_id,
        [partNumber],
      );
      const grant = grants.items[0];
      if (!grant || grant.part_number !== partNumber) {
        throw new Error(`未收到分片 ${partNumber} 的上传授权`);
      }
      await this.waitWhilePaused();
      this.assertActive();
      const controller = new AbortController();
      this.uploadControllers.add(controller);
      try {
        const response = await fetch(grant.transfer.url, {
          body,
          headers: grant.transfer.headers,
          method: grant.transfer.method,
          signal: controller.signal,
        });
        if (!response.ok) {
          throw new Error(
            `分片 ${partNumber} 上传失败（HTTP ${response.status}）`,
          );
        }
        const etag = response.headers.get("etag")?.trim();
        if (!etag) {
          throw new Error(`分片 ${partNumber} 未返回 ETag`);
        }
        return {
          completed_at: new Date().toISOString(),
          etag,
          part_number: partNumber,
          size_bytes: body.size,
        };
      } catch (error) {
        lastError = error;
        if (this.paused && isAbortError(error)) {
          throw error;
        }
        if (attempt + 1 < this.retryLimit) {
          await delay(250 * 2 ** attempt);
        }
      } finally {
        this.uploadControllers.delete(controller);
      }
    }
    throw lastError ?? new Error(`分片 ${partNumber} 在重试后仍未能完成上传`);
  }

  private updateProgress(
    session: UploadSession,
    uploaded: Map<number, UploadPart>,
  ): void {
    let completedBytes = 0;
    for (const part of uploaded.values()) {
      completedBytes += part.size_bytes;
    }
    this.update({
      completedBytes,
      progress:
        session.size_bytes === 0 ? 1 : completedBytes / session.size_bytes,
    });
  }

  private toStored(sha256: string): StoredArtifactUpload {
    const session = this.session!;
    return {
      artifactId: session.artifact_id,
      createdAt: new Date().toISOString(),
      description: this.options.description,
      fileLastModified: this.file.lastModified,
      fileName: this.file.name,
      fileSize: this.file.size,
      folderId: this.options.folderId ?? this.options.stored?.folderId,
      idempotencyKey:
        this.options.idempotencyKey ??
        this.options.stored?.idempotencyKey ??
        session.upload_id,
      kind: this.options.kind ?? this.options.stored?.kind ?? "attachment",
      name: this.options.name,
      projectId: this.options.projectId,
      sha256,
      tags: this.options.tags ?? [],
      uploadId: session.upload_id,
      versionId: session.version_id,
    };
  }

  private async placeInFolder(detail: ArtifactDetail): Promise<ArtifactDetail> {
    if (this.options.artifactId) return detail;
    const folderId = this.options.folderId ?? this.options.stored?.folderId;
    if (!folderId || detail.artifact.folder_id === folderId) return detail;
    return artifactApi.moveArtifact(
      this.options.projectId,
      detail.artifact.artifact_id,
      folderId,
    );
  }

  private validateResumeFile(file: File): void {
    const stored = this.options.stored;
    if (
      stored &&
      (stored.fileName !== file.name || stored.fileSize !== file.size)
    ) {
      throw new Error("所选文件与待恢复上传的文件名、大小或修改时间不一致");
    }
  }

  private async waitWhilePaused(): Promise<void> {
    if (!this.paused) {
      return;
    }
    await new Promise<void>((resolve) => this.pauseWaiters.push(resolve));
  }

  private assertActive(): void {
    if (this.cancelled) {
      throw new DOMException("Upload was cancelled", "AbortError");
    }
  }

  private setStatus(status: UploadTaskStatus): void {
    this.update({ status });
  }

  private update(patch: Partial<UploadTaskSnapshot>): void {
    this.snapshot = { ...this.snapshot, ...patch };
    for (const listener of this.listeners) {
      listener(this.getSnapshot());
    }
  }
}

export function listStoredUploads(projectId: string): StoredArtifactUpload[] {
  if (typeof window === "undefined") {
    return [];
  }
  const uploads: StoredArtifactUpload[] = [];
  for (let index = 0; index < window.localStorage.length; index += 1) {
    const key = window.localStorage.key(index);
    if (!key?.startsWith(`${storagePrefix}${projectId}.`)) {
      continue;
    }
    try {
      const item = JSON.parse(
        window.localStorage.getItem(key) ?? "",
      ) as StoredArtifactUpload;
      if (item.projectId === projectId && item.uploadId) {
        uploads.push(item);
      }
    } catch {
      window.localStorage.removeItem(key);
    }
  }
  return uploads.sort((left, right) =>
    right.createdAt.localeCompare(left.createdAt),
  );
}

function persistStoredUpload(upload: StoredArtifactUpload): void {
  if (typeof window === "undefined") {
    return;
  }
  window.localStorage.setItem(
    `${storagePrefix}${upload.projectId}.${upload.uploadId}`,
    JSON.stringify(upload),
  );
}

export function removeStoredUpload(projectId: string, uploadId: string): void {
  if (typeof window !== "undefined") {
    window.localStorage.removeItem(`${storagePrefix}${projectId}.${uploadId}`);
  }
}

function createIdempotencyKey(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  return `web-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.max(minimum, Math.min(maximum, Math.trunc(value)));
}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}

function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, milliseconds));
}
