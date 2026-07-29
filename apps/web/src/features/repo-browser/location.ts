import type { RepoWorkspaceKind } from "@/features/repo/types";

const fullShaPattern = /^[0-9a-f]{40}([0-9a-f]{24})?$/;

export type RepoLocation = {
  path: string;
  revision: string | null;
  workspace: RepoWorkspaceKind;
};

export function parseRepoLocation(search: URLSearchParams): RepoLocation {
  const rawWorkspace = search.get("workspace");
  const workspace: RepoWorkspaceKind =
    rawWorkspace === "article" || rawWorkspace === "result"
      ? rawWorkspace
      : "code";
  const revision = search.get("revision");
  const path = search.get("path") ?? "";
  return {
    path: validRepoPath(path) ? path : "",
    revision: revision && fullShaPattern.test(revision) ? revision : null,
    workspace,
  };
}

export function repoLocationQuery(location: RepoLocation): string {
  const query = new URLSearchParams({ workspace: location.workspace });
  if (location.revision) {
    query.set("revision", location.revision);
  }
  if (location.path) {
    query.set("path", location.path);
  }
  return query.toString();
}

function validRepoPath(path: string): boolean {
  return (
    path.length <= 4_096 &&
    !path.startsWith("/") &&
    !path.includes("\\") &&
    path.split("/").every((segment) => segment !== "" && segment !== "..")
  );
}
