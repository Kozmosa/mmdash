import { describe, expect, it, vi } from "vitest";

import {
  flushArticleCollaboration,
  registerArticleAbstractCollaborationProvider,
  registerArticleCollaborationProvider,
  type ArticleCollaborationSyncProvider,
} from "./article-collaboration-sync";

describe("Article collaboration snapshot sync", () => {
  it("flushes the provider batching window before continuing", async () => {
    let unsynced = true;
    const provider: ArticleCollaborationSyncProvider = {
      flushPendingUpdates: vi.fn(() => {
        unsynced = false;
      }),
      get hasUnsyncedChanges() {
        return unsynced;
      },
    };
    const unregister = registerArticleCollaborationProvider(
      "project-1",
      provider,
    );

    try {
      await flushArticleCollaboration("project-1");
      expect(provider.flushPendingUpdates).toHaveBeenCalledTimes(1);
      expect(provider.hasUnsyncedChanges).toBe(false);
    } finally {
      unregister();
    }
  });

  it("waits for the server acknowledgement of an outstanding update", async () => {
    let unsynced = true;
    const provider: ArticleCollaborationSyncProvider = {
      flushPendingUpdates: vi.fn(() => {
        setTimeout(() => {
          unsynced = false;
        }, 5);
      }),
      get hasUnsyncedChanges() {
        return unsynced;
      },
    };
    const unregister = registerArticleCollaborationProvider(
      "project-2",
      provider,
    );

    try {
      await flushArticleCollaboration("project-2");
      expect(provider.hasUnsyncedChanges).toBe(false);
    } finally {
      unregister();
    }
  });

  it("is a no-op when the Article provider is not mounted", async () => {
    await expect(
      flushArticleCollaboration("project-without-provider"),
    ).resolves.toBeUndefined();
  });

  it("waits for both the body and abstract rooms before a commit barrier", async () => {
    const flushed: string[] = [];
    const makeProvider = (name: string): ArticleCollaborationSyncProvider => ({
      flushPendingUpdates: vi.fn(() => flushed.push(name)),
      get hasUnsyncedChanges() {
        return false;
      },
    });
    const body = makeProvider("body");
    const abstract = makeProvider("abstract");
    const unregisterBody = registerArticleCollaborationProvider(
      "project-dual",
      body,
    );
    const unregisterAbstract = registerArticleAbstractCollaborationProvider(
      "project-dual",
      abstract,
    );

    try {
      await flushArticleCollaboration("project-dual");
      expect(flushed).toEqual(["body", "abstract"]);
    } finally {
      unregisterBody();
      unregisterAbstract();
    }
  });
});
