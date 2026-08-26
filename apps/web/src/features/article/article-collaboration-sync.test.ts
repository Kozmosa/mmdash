import { describe, expect, it, vi } from "vitest";

import {
  flushArticleCollaboration,
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
});
