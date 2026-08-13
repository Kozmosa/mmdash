import { EventEmitter } from "node:events";

import { HocuspocusProvider } from "@hocuspocus/provider";
import { CoreClient, CoreClientError } from "@mmdash/core-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import WebSocket from "ws";
import * as Y from "yjs";

import { buildApp } from "../src/app.js";
import { ArticleCollaboration } from "../src/article/collaboration.js";
import { signedSessionCookie, testConfig } from "./helpers.js";

const apps: ReturnType<typeof buildApp>[] = [];
const projectId = "00000000-0000-4000-8000-000000000001";

afterEach(async () => {
  await Promise.all(apps.splice(0).map((app) => app.close()));
});

describe("Article browser routes", () => {
  it("flushes the collaborative draft before creating a commit", async () => {
    let revision = 4;
    const fetchImplementation = vi.fn<typeof fetch>(async (input, init) => {
      const url = String(input);
      if (url.endsWith("/permissions")) {
        return Response.json({
          permissions: ["project.article.read", "project.article.edit"],
          project_id: projectId,
          role: "editor",
        });
      }
      if (url.endsWith("/article/draft") && init?.method === "GET") {
        return Response.json(draft(revision));
      }
      if (url.endsWith("/article/draft/flush") && init?.method === "PUT") {
        const body = JSON.parse(String(init.body));
        expect(body.expected_revision).toBe(4);
        revision = 5;
        return Response.json(draft(revision));
      }
      if (url.endsWith("/article/commits") && init?.method === "POST") {
        const body = JSON.parse(String(init.body));
        expect(body).toEqual({ draft_revision: 5, message: "checkpoint" });
        return Response.json({ commit_id: "commit-1" }, { status: 201 });
      }
      throw new Error(`unexpected Core request: ${init?.method} ${url}`);
    });
    const app = buildApp({ config: testConfig, fetchImplementation, logger: false });
    apps.push(app);
    const cookie = await signedSessionCookie(app);

    const response = await app.inject({
      headers: { cookie },
      method: "POST",
      payload: { draft_revision: 1, message: "checkpoint" },
      url: `/api/projects/${projectId}/article/commits`,
    });

    expect(response.statusCode, response.body).toBe(201);
    expect(fetchImplementation.mock.calls.map(([url]) => String(url))).toEqual([
      `http://core.test/v1/projects/${projectId}/permissions`,
      `http://core.test/v1/projects/${projectId}/article/draft`,
      `http://core.test/v1/projects/${projectId}/article/draft/flush`,
      `http://core.test/v1/projects/${projectId}/article/draft`,
      `http://core.test/v1/projects/${projectId}/article/commits`,
    ]);
  });
});

describe("Article collaboration", () => {
  it("merges two real WebSocket clients, restores on reconnect, and keeps a Viewer read-only", async () => {
    let revision = 0;
    let yjsUpdate = "";
    const fetchImplementation = vi.fn<typeof fetch>(async (input, init) => {
      const url = String(input);
      if (url.endsWith("/permissions")) {
        const token = new Headers(init?.headers).get("authorization");
        return Response.json({
          permissions: token === "Bearer viewer-token" ? ["project.article.read"] : ["project.article.read", "project.article.edit"],
          project_id: projectId,
          role: token === "Bearer viewer-token" ? "viewer" : "editor",
        });
      }
      if (url.endsWith("/article/draft") && init?.method === "GET") {
        return Response.json({ ...draft(revision), yjs_update: yjsUpdate });
      }
      if (url.endsWith("/article/draft/flush") && init?.method === "PUT") {
        const body = JSON.parse(String(init.body)) as { expected_revision: number; yjs_update: string };
        if (body.expected_revision !== revision) return Response.json({ code: "ARTICLE_CONFLICT" }, { status: 409 });
        revision += 1; yjsUpdate = body.yjs_update;
        return Response.json({ ...draft(revision), yjs_update: yjsUpdate });
      }
      throw new Error(`unexpected Core request: ${init?.method} ${url}`);
    });
    const app = buildApp({ config: testConfig, fetchImplementation, logger: false });
    apps.push(app);
    const editorCookie = await signedSessionCookie(app, { access_token: "editor-token" });
    const viewerCookie = await signedSessionCookie(app, { access_token: "viewer-token", session_id: "viewer-session", user_id: "viewer-user" });
    await app.listen({ host: "127.0.0.1", port: 0 });
    const address = app.server.address();
    if (typeof address === "string" || address === null) throw new Error("Expected BFF TCP address");
    const url = `ws://127.0.0.1:${address.port}/api/projects/${projectId}/article/collaboration`;
    const firstDoc = new Y.Doc(); const secondDoc = new Y.Doc();
    const first = realProvider(url, editorCookie, firstDoc); const second = realProvider(url, editorCookie, secondDoc);
    await Promise.all([providerSynced(first), providerSynced(second)]);
    firstDoc.getText("shared").insert(0, "alpha");
    secondDoc.getText("shared").insert(0, "beta ");
    await waitUntil(() => firstDoc.getText("shared").toString() === secondDoc.getText("shared").toString() && firstDoc.getText("shared").length === 10);
    const merged = firstDoc.getText("shared").toString();
    second.destroy(); secondDoc.destroy();
    await waitUntil(() => revision > 0);
    const reconnectDoc = new Y.Doc(); const reconnect = realProvider(url, editorCookie, reconnectDoc);
    await providerSynced(reconnect);
    expect(reconnectDoc.getText("shared").toString()).toBe(merged);
    const viewerDoc = new Y.Doc(); const viewer = realProvider(url, viewerCookie, viewerDoc);
    await providerSynced(viewer);
    viewerDoc.getText("shared").insert(0, "forbidden ");
    await new Promise((resolve) => setTimeout(resolve, 150));
    expect(firstDoc.getText("shared").toString()).toBe(merged);
    viewer.destroy(); viewerDoc.destroy(); reconnect.destroy(); reconnectDoc.destroy(); first.destroy(); firstDoc.destroy();
  }, 15_000);

  it("merges the latest Core update and retries one CAS conflict", async () => {
    const requests: { body?: unknown; method?: string; path: string }[] = [];
    let getCount = 0;
    let putCount = 0;
    const coreClient = {
      async request(path: string, options: { body?: unknown; method?: string }) {
        requests.push({ body: options.body, method: options.method, path });
        if (options.method === "GET") {
          getCount += 1;
          return draft(getCount === 1 ? 1 : 3);
        }
        putCount += 1;
        if (putCount === 1) {
          throw new CoreClientError(409, { code: "ARTICLE_DRAFT_CONFLICT" });
        }
        return draft(3);
      },
    } as CoreClient;
    const collaboration = new ArticleCollaboration(coreClient);

    const result = await collaboration.flush(context());
    await collaboration.destroy();

    expect(result.draft_revision).toBe(3);
    const expectedRevisions = requests
      .filter((request) => request.method === "PUT")
      .map((request) => (request.body as { expected_revision: number }).expected_revision);
    expect(expectedRevisions).toEqual([1, 3]);
  });

  it("caps each project at 32 live browser connections", async () => {
    const collaboration = new ArticleCollaboration({} as CoreClient);
    const handleConnection = vi
      .spyOn(collaboration.hocuspocus, "handleConnection")
      .mockReturnValue({ handleClose: vi.fn(), handleMessage: vi.fn() } as never);
    const sockets = Array.from({ length: 33 }, () => new FakeSocket());

    for (const socket of sockets) {
      collaboration.connect(socket as never, new Request("http://bff.test"), context());
    }

    expect(handleConnection).toHaveBeenCalledTimes(32);
    expect(sockets[32]!.close).toHaveBeenCalledWith(
      1013,
      "Article collaboration connection limit reached",
    );
    sockets[0]!.emit("close", 1000, Buffer.from("done"));
    collaboration.connect(new FakeSocket() as never, new Request("http://bff.test"), context());
    expect(handleConnection).toHaveBeenCalledTimes(33);
    await collaboration.destroy();
  });
});

class FakeSocket extends EventEmitter {
  close = vi.fn();
}

function context() {
  return {
    accessToken: "access-token",
    canEdit: true,
    projectId: "project-1",
    requestId: "request-1",
    sessionId: "session-1",
    userId: "user-1",
  };
}

function draft(revision: number) {
  return {
    draft_revision: revision,
    state_vector: "",
    tiptap_json: { content: [], type: "doc" },
    yjs_update: "",
  };
}

function realProvider(url: string, cookie: string, document: Y.Doc) {
  class CookieWebSocket extends WebSocket {
    constructor(target: string | URL) { super(target, { headers: { cookie } }); }
  }
  const provider = new HocuspocusProvider({ document, name: `article:${projectId}`, sessionAwareness: true, token: "browser-session", url, WebSocketPolyfill: CookieWebSocket });
  return provider;
}

function providerSynced(provider: HocuspocusProvider): Promise<void> {
  if (provider.synced) return Promise.resolve();
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => reject(new Error(`provider did not sync (authenticated=${provider.isAuthenticated}, socket=${provider.configuration.websocketProvider.status})`)), 5_000);
    provider.on("synced", ({ state }: { state: boolean }) => { if (state) { clearTimeout(timeout); resolve(); } });
    provider.on("authenticationFailed", ({ reason }: { reason: string }) => { clearTimeout(timeout); reject(new Error(reason)); });
  });
}

async function waitUntil(predicate: () => boolean) {
  const deadline = Date.now() + 5_000;
  while (!predicate()) {
    if (Date.now() > deadline) throw new Error("condition timed out");
    await new Promise((resolve) => setTimeout(resolve, 20));
  }
}
