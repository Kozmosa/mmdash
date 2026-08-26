import { Hocuspocus, type Document } from "@hocuspocus/server";
import type { CoreClient } from "@mmdash/core-client";
import type { FastifyInstance } from "fastify";
import { randomUUID } from "node:crypto";
import {
  prosemirrorJSONToYXmlFragment,
  yXmlFragmentToProsemirrorJSON,
} from "y-prosemirror";
import WebSocket, { type RawData } from "ws";
import * as Y from "yjs";

import { articleDocumentSchema } from "./document-schema.js";

type RoomContext = {
  accessToken: string;
  canEdit: boolean;
  projectId: string;
  requestId: string;
  sessionId: string;
  userId: string;
};

type RoomState = RoomContext & { revision: number };

type Draft = {
  draft_revision: number;
  state_vector: string;
  tiptap_json: Record<string, unknown>;
  yjs_update: string;
};

export class ArticleCollaboration {
  private static readonly maxConnectionsPerProject = 32;
  private readonly connectionCounts = new Map<string, number>();
  private readonly rooms = new Map<string, RoomState>();
  private readonly stores = new Map<string, Promise<void>>();
  readonly hocuspocus: Hocuspocus<RoomContext>;

  constructor(private readonly coreClient: CoreClient) {
    this.hocuspocus = new Hocuspocus<RoomContext>({
      debounce: 750,
      maxDebounce: 5_000,
      maxPendingDocuments: 1,
      maxUnauthenticatedQueueMessages: 100,
      maxUnauthenticatedQueueSize: 4 * 1024 * 1024,
      timeout: 30_000,
      unloadImmediately: true,
      async onAuthenticate({ context, documentName, token }) {
        if (
          token !== "browser-session" ||
          documentName !== roomName(context.projectId)
        ) {
          throw new Error("Article collaboration permission denied");
        }
        return context;
      },
      async onConnect({ connectionConfig, context, documentName }) {
        if (documentName !== roomName(context.projectId)) {
          throw new Error("Article room does not match project context");
        }
        connectionConfig.readOnly = !context.canEdit;
        return context;
      },
      onLoadDocument: async ({ context, document, documentName }) => {
        const draft = await this.getDraft(context);
        if (draft.yjs_update) {
          Y.applyUpdate(
            document,
            Buffer.from(draft.yjs_update, "base64"),
            "core-load",
          );
        } else if (hasDocumentContent(draft.tiptap_json)) {
          // v0.1 drafts created before collaboration was enabled may contain
          // authoritative Tiptap JSON but no Yjs update. Populate the room
          // before any browser can send an empty state and overwrite it.
          prosemirrorJSONToYXmlFragment(
            articleDocumentSchema,
            draft.tiptap_json,
            document.getXmlFragment("default"),
          );
        }
        this.rooms.set(documentName, {
          ...context,
          revision: draft.draft_revision,
        });
      },
      onStoreDocument: async ({ document, documentName, lastContext }) => {
        const context = lastContext ?? this.rooms.get(documentName);
        if (!context) {
          throw new Error("Article room context is unavailable");
        }
        await this.serializeStore(documentName, () =>
          this.persistDocument(documentName, document, context),
        );
      },
    });
  }

  connect(
    socket: WebSocket,
    request: Request,
    context: RoomContext,
    pendingMessages: RawData[] = [],
  ): void {
    const count = this.connectionCounts.get(context.projectId) ?? 0;
    if (count >= ArticleCollaboration.maxConnectionsPerProject) {
      socket.close(1013, "Article collaboration connection limit reached");
      return;
    }
    this.connectionCounts.set(context.projectId, count + 1);
    let released = false;
    const release = () => {
      if (released) return;
      released = true;
      const remaining = (this.connectionCounts.get(context.projectId) ?? 1) - 1;
      if (remaining > 0)
        this.connectionCounts.set(context.projectId, remaining);
      else this.connectionCounts.delete(context.projectId);
    };
    let client;
    try {
      client = this.hocuspocus.handleConnection(socket, request, context);
    } catch (error) {
      release();
      throw error;
    }
    socket.on("message", (raw: RawData) =>
      client.handleMessage(toUint8Array(raw)),
    );
    socket.on("close", (code, reason) => {
      release();
      client.handleClose({ code, reason: reason.toString() });
    });
    socket.on("error", () => {
      release();
      client.handleClose({ code: 1011, reason: "socket error" });
    });
    for (const raw of pendingMessages) client.handleMessage(toUint8Array(raw));
  }

  async flush(context: RoomContext): Promise<Draft> {
    const name = roomName(context.projectId);
    const connection = await this.hocuspocus.openDirectConnection(
      name,
      context,
    );
    await connection.disconnect();
    await this.stores.get(name);
    return this.getDraft(context);
  }

  async destroy(): Promise<void> {
    this.hocuspocus.flushPendingStores();
    this.hocuspocus.closeConnections();
    await Promise.allSettled(this.stores.values());
  }

  private async persistDocument(
    documentName: string,
    document: Document,
    context: RoomContext,
  ): Promise<void> {
    const state = this.rooms.get(documentName) ?? { ...context, revision: 0 };
    for (let attempt = 0; attempt < 2; attempt += 1) {
      const update = Buffer.from(Y.encodeStateAsUpdate(document)).toString(
        "base64",
      );
      const vector = Buffer.from(Y.encodeStateVector(document)).toString(
        "base64",
      );
      let tiptap: Record<string, unknown> = { content: [], type: "doc" };
      const fragment = document.getXmlFragment("default");
      if (fragment.length > 0) tiptap = yXmlFragmentToProsemirrorJSON(fragment);
      try {
        const draft = await this.coreClient.request<Draft>(
          `/v1/projects/${encodeURIComponent(context.projectId)}/article/draft/flush`,
          {
            body: {
              actor_kind: "human",
              expected_revision: state.revision,
              provenance: {
                session_id: context.sessionId,
                user_id: context.userId,
              },
              state_vector: vector,
              tiptap_json: tiptap,
              yjs_update: update,
            },
            method: "PUT",
          },
          coreContext(context),
        );
        this.rooms.set(documentName, {
          ...context,
          revision: draft.draft_revision,
        });
        return;
      } catch (error) {
        if (attempt > 0) throw error;
        const latest = await this.getDraft(context);
        state.revision = latest.draft_revision;
        this.rooms.set(documentName, {
          ...context,
          revision: latest.draft_revision,
        });
        if (latest.yjs_update) {
          Y.applyUpdate(
            document,
            Buffer.from(latest.yjs_update, "base64"),
            "core-cas-merge",
          );
        }
      }
    }
  }

  private serializeStore(
    documentName: string,
    work: () => Promise<void>,
  ): Promise<void> {
    const pending = (this.stores.get(documentName) ?? Promise.resolve())
      .catch(() => undefined)
      .then(work);
    this.stores.set(documentName, pending);
    void pending.finally(() => {
      if (this.stores.get(documentName) === pending)
        this.stores.delete(documentName);
    });
    return pending;
  }

  private getDraft(context: RoomContext): Promise<Draft> {
    return this.coreClient.request(
      `/v1/projects/${encodeURIComponent(context.projectId)}/article/draft`,
      { method: "GET" },
      coreContext(context),
    );
  }
}

function hasDocumentContent(value: Record<string, unknown>): boolean {
  return (
    value.type === "doc" &&
    Array.isArray(value.content) &&
    value.content.length > 0
  );
}

export function registerArticleCollaboration(
  app: FastifyInstance,
  coreClient: CoreClient,
  collaboration = new ArticleCollaboration(coreClient),
): ArticleCollaboration {
  app.get(
    "/api/projects/:projectId/article/collaboration",
    { config: { auth: "required", project: "required" }, websocket: true },
    (socket, request) => {
      // Hocuspocus sends its authentication frame immediately after the HTTP
      // upgrade. Buffer frames while the project permission lookup is in
      // flight, otherwise a low-latency client can authenticate before the
      // collaboration handler exists and remain disconnected forever.
      const pendingMessages: RawData[] = [];
      let pendingBytes = 0;
      const bufferMessage = (raw: RawData) => {
        const bytes = rawDataSize(raw);
        pendingBytes += bytes;
        if (pendingMessages.length >= 100 || pendingBytes > 4 * 1024 * 1024) {
          socket.off("message", bufferMessage);
          socket.close(
            1009,
            "Article collaboration authentication queue exceeded",
          );
          return;
        }
        pendingMessages.push(raw);
      };
      socket.on("message", bufferMessage);
      void roomContext(coreClient, request)
        .then((context) => {
          socket.off("message", bufferMessage);
          const url = new URL(request.url, "http://web-bff.local");
          collaboration.connect(
            socket,
            new Request(url),
            context,
            pendingMessages,
          );
        })
        .catch((error) => {
          request.log.warn(
            { err: error },
            "article collaboration authorization failed",
          );
          socket.close(1008, "Article collaboration permission denied");
        });
    },
  );
  app.addHook("onClose", async () => collaboration.destroy());
  return collaboration;
}

export async function roomContext(
  coreClient: CoreClient,
  request: {
    browserIdentity?: {
      accessToken: string;
      sessionId: string;
      userId: string;
    };
    currentProjectId?: string;
    id: string;
  },
): Promise<RoomContext> {
  const identity = request.browserIdentity!;
  const projectId = request.currentProjectId!;
  const permissions = await coreClient.getProjectPermissions(projectId, {
    accessToken: identity.accessToken,
    projectId,
    requestId: request.id,
    userId: identity.userId,
  });
  return {
    accessToken: identity.accessToken,
    canEdit: permissions.permissions.includes("project.article.edit"),
    projectId,
    requestId: request.id || randomUUID(),
    sessionId: identity.sessionId,
    userId: identity.userId,
  };
}

function coreContext(context: RoomContext) {
  return {
    accessToken: context.accessToken,
    projectId: context.projectId,
    requestId: context.requestId,
    userId: context.userId,
  };
}

function roomName(projectId: string): string {
  return `article:${projectId}`;
}

function toUint8Array(raw: RawData): Uint8Array {
  if (raw instanceof ArrayBuffer) return new Uint8Array(raw);
  if (Array.isArray(raw)) return new Uint8Array(Buffer.concat(raw));
  return new Uint8Array(raw.buffer, raw.byteOffset, raw.byteLength);
}

function rawDataSize(raw: RawData): number {
  if (raw instanceof ArrayBuffer) return raw.byteLength;
  if (Array.isArray(raw))
    return raw.reduce((total, item) => total + item.byteLength, 0);
  return raw.byteLength;
}
