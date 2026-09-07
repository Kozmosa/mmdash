import type { HocuspocusProvider } from "@hocuspocus/provider";

export type ArticleCollaborationSyncProvider = Pick<
  HocuspocusProvider,
  "flushPendingUpdates" | "hasUnsyncedChanges"
>;

// Both collaborative documents must finish syncing before a commit barrier:
// the body room and the independent abstract room.
const providers = new Map<string, ArticleCollaborationSyncProvider>();

const syncTimeoutMs = 5_000;
const syncPollIntervalMs = 20;

function bodyKey(projectId: string): string {
  return `article:${projectId}`;
}

function abstractKey(projectId: string): string {
  return `article-abstract:${projectId}`;
}

export function registerArticleCollaborationProvider(
  projectId: string,
  provider: ArticleCollaborationSyncProvider,
): () => void {
  providers.set(bodyKey(projectId), provider);
  return () => {
    if (providers.get(bodyKey(projectId)) === provider)
      providers.delete(bodyKey(projectId));
  };
}

export function registerArticleAbstractCollaborationProvider(
  projectId: string,
  provider: ArticleCollaborationSyncProvider,
): () => void {
  providers.set(abstractKey(projectId), provider);
  return () => {
    if (providers.get(abstractKey(projectId)) === provider)
      providers.delete(abstractKey(projectId));
  };
}

export async function flushArticleCollaboration(
  projectId: string,
): Promise<void> {
  const registered = [bodyKey(projectId), abstractKey(projectId)]
    .map((key) => providers.get(key))
    .filter((provider): provider is ArticleCollaborationSyncProvider =>
      Boolean(provider),
    );
  if (!registered.length) return;

  for (const provider of registered) provider.flushPendingUpdates();

  const deadline = Date.now() + syncTimeoutMs;
  while (registered.some((provider) => provider.hasUnsyncedChanges)) {
    if (Date.now() >= deadline) {
      throw new Error("草稿同步超时，请检查网络连接后重试");
    }
    await sleep(syncPollIntervalMs);
  }
}

function sleep(milliseconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}
