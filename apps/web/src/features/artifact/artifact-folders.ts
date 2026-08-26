import type { ArtifactFolder } from "./types";

export type ArtifactFolderOption = {
  depth: number;
  folder: ArtifactFolder;
  path: string;
};

export function flattenArtifactFolders(
  folders: ArtifactFolder[],
  depth = 0,
  parentPath = "",
): ArtifactFolderOption[] {
  return folders.flatMap((folder) => {
    const path = parentPath ? `${parentPath}/${folder.name}` : folder.name;
    return [
      { depth, folder, path },
      ...flattenArtifactFolders(folder.children, depth + 1, path),
    ];
  });
}

export function artifactFolderDescendantIds(
  folder: ArtifactFolder,
): Set<string> {
  const ids = new Set<string>();
  const visit = (children: ArtifactFolder[]) => {
    for (const child of children) {
      ids.add(child.folder_id);
      visit(child.children);
    }
  };
  visit(folder.children);
  return ids;
}

export function findArtifactFolder(
  folders: ArtifactFolder[],
  folderId: string | null,
): ArtifactFolder | undefined {
  if (!folderId) return undefined;
  for (const folder of folders) {
    if (folder.folder_id === folderId) return folder;
    const nested = findArtifactFolder(folder.children, folderId);
    if (nested) return nested;
  }
}

export function artifactFolderPath(
  folders: ArtifactFolder[],
  folderId: string | null,
): ArtifactFolder[] {
  if (!folderId) return [];
  for (const folder of folders) {
    if (folder.folder_id === folderId) return [folder];
    const nested = artifactFolderPath(folder.children, folderId);
    if (nested.length > 0) return [folder, ...nested];
  }
  return [];
}
