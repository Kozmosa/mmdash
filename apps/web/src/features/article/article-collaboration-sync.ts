import type { HocuspocusProvider } from "@hocuspocus/provider";

export type ArticleCollaborationSyncProvider = Pick<
  HocuspocusProvider,
  "flushPendingUpdates" | "hasUnsyncedChanges"
>;

const providers = new Map<string, ArticleCollaborationSyncProvider>();

const syncTimeoutMs = 5_000;
const syncPollIntervalMs = 20;

export function registerArticleCollaborationProvider(
  projectId: string,
  provider: ArticleCollaborationSyncProvider,
): () => void {
  providers.set(projectId, provider);

  return () => {
    if (providers.get(projectId) === provider) {
      providers.delete(projectId);
    }
  };
}

export async function flushArticleCollaboration(
  projectId: string,
): Promise<void> {
  const provider = providers.get(projectId);
  if (!provider) return;

  provider.flushPendingUpdates();

  const deadline = Date.now() + syncTimeoutMs;
  while (provider.hasUnsyncedChanges) {
    if (Date.now() >= deadline) {
      throw new Error("草稿同步超时，请检查网络连接后重试");
    }
    await sleep(syncPollIntervalMs);
  }
}

function sleep(milliseconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}
