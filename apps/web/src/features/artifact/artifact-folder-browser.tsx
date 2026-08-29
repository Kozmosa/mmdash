"use client";

import { ChevronRight, Folder, FolderOpen, Home } from "lucide-react";
import { useState, type DragEvent, type ReactNode } from "react";

import { cn } from "@/lib/cn";

import { artifactFolderPath, findArtifactFolder } from "./artifact-folders";
import type { ArtifactFolder } from "./types";

export const artifactMoveMime = "application/vnd.mmdash.artifact-move+json";
export const artifactFolderMoveMime =
  "application/vnd.mmdash.artifact-folder-move+json";

type ArtifactMovePayload = { artifactId: string };
type FolderMovePayload = { folderId: string };

export function ArtifactFolderBrowser({
  canManage,
  compact = false,
  currentFolderId,
  folders,
  onMoveArtifact,
  onMoveFolder,
  onNavigate,
}: Readonly<{
  canManage: boolean;
  compact?: boolean;
  currentFolderId: string | null;
  folders: ArtifactFolder[];
  onMoveArtifact: (artifactId: string, folderId: string | null) => void;
  onMoveFolder: (folderId: string, parentFolderId: string | null) => void;
  onNavigate: (folderId: string | null) => void;
}>) {
  const [dropTarget, setDropTarget] = useState<string>();
  const current = findArtifactFolder(folders, currentFolderId);
  const children = current?.children ?? folders;
  const path = artifactFolderPath(folders, currentFolderId);

  const drop = (event: DragEvent, targetFolderId: string | null) => {
    if (!canManage) return;
    const artifact = readPayload<ArtifactMovePayload>(event, artifactMoveMime);
    const folder = readPayload<FolderMovePayload>(
      event,
      artifactFolderMoveMime,
    );
    if (!artifact && !folder) return;
    event.preventDefault();
    event.stopPropagation();
    setDropTarget(undefined);
    if (artifact?.artifactId)
      onMoveArtifact(artifact.artifactId, targetFolderId);
    if (folder?.folderId && folder.folderId !== targetFolderId) {
      onMoveFolder(folder.folderId, targetFolderId);
    }
  };

  const allowDrop = (event: DragEvent, key: string) => {
    if (
      !canManage ||
      (!event.dataTransfer.types.includes(artifactMoveMime) &&
        !event.dataTransfer.types.includes(artifactFolderMoveMime))
    ) {
      return;
    }
    event.preventDefault();
    event.dataTransfer.dropEffect = "move";
    setDropTarget(key);
  };

  return (
    <section
      aria-label="Artifact 文件夹浏览器"
      className={cn("rounded-lg border bg-muted/15", compact ? "p-2" : "p-3")}
    >
      <nav
        aria-label="Artifact 文件夹路径"
        className="flex min-h-8 flex-wrap items-center gap-0.5 rounded-md bg-background px-1.5 text-xs shadow-sm"
      >
        <PathButton
          active={!currentFolderId}
          dropActive={dropTarget === "root"}
          label="项目文件"
          onDragLeave={() => setDropTarget(undefined)}
          onDragOver={(event) => allowDrop(event, "root")}
          onDrop={(event) => drop(event, null)}
          onNavigate={() => onNavigate(null)}
        >
          <Home className="size-3.5" />
        </PathButton>
        {path.map((folder) => (
          <span className="contents" key={folder.folder_id}>
            <ChevronRight
              aria-hidden="true"
              className="size-3 text-muted-foreground"
            />
            <PathButton
              active={folder.folder_id === currentFolderId}
              dropActive={dropTarget === folder.folder_id}
              label={folder.name}
              onDragLeave={() => setDropTarget(undefined)}
              onDragOver={(event) => allowDrop(event, folder.folder_id)}
              onDrop={(event) => drop(event, folder.folder_id)}
              onNavigate={() => onNavigate(folder.folder_id)}
            />
          </span>
        ))}
      </nav>
      {children.length > 0 ? (
        <div
          className={cn(
            "mt-2 grid gap-2",
            compact ? "grid-cols-1" : "sm:grid-cols-2 lg:grid-cols-3",
          )}
        >
          {children.map((folder) => (
            <div
              className={cn(
                "group flex min-w-0 items-center gap-2 rounded-md border bg-background p-2 text-left shadow-sm transition hover:border-primary/40 hover:shadow",
                dropTarget === folder.folder_id
                  ? "border-primary bg-primary/5 ring-2 ring-primary/20"
                  : null,
              )}
              draggable={canManage}
              key={folder.folder_id}
              onDragEnd={() => setDropTarget(undefined)}
              onDragLeave={() => setDropTarget(undefined)}
              onDragOver={(event) => allowDrop(event, folder.folder_id)}
              onDragStart={(event) => {
                event.dataTransfer.effectAllowed = "move";
                event.dataTransfer.setData(
                  artifactFolderMoveMime,
                  JSON.stringify({ folderId: folder.folder_id }),
                );
              }}
              onDrop={(event) => drop(event, folder.folder_id)}
            >
              <Folder className="size-4 shrink-0 text-amber-500" />
              <button
                className="min-w-0 flex-1 truncate text-left text-xs font-medium"
                onClick={() => onNavigate(folder.folder_id)}
                title={folder.name}
                type="button"
              >
                {folder.name}
              </button>
              <FolderOpen className="size-3.5 shrink-0 text-muted-foreground opacity-0 transition group-hover:opacity-100" />
            </div>
          ))}
        </div>
      ) : (
        <p className="mt-2 px-1 text-[11px] text-muted-foreground">
          此目录没有子文件夹；可把 Artifact 拖到上方路径移动到父目录。
        </p>
      )}
    </section>
  );
}

function PathButton({
  active,
  children,
  dropActive,
  label,
  onDragLeave,
  onDragOver,
  onDrop,
  onNavigate,
}: Readonly<{
  active: boolean;
  children?: ReactNode;
  dropActive: boolean;
  label: string;
  onDragLeave: () => void;
  onDragOver: (event: DragEvent<HTMLButtonElement>) => void;
  onDrop: (event: DragEvent<HTMLButtonElement>) => void;
  onNavigate: () => void;
}>) {
  return (
    <button
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex h-7 min-w-0 items-center gap-1 rounded px-1.5 hover:bg-muted",
        active ? "font-medium text-foreground" : "text-muted-foreground",
        dropActive ? "bg-primary/10 text-primary ring-1 ring-primary/30" : null,
      )}
      onClick={onNavigate}
      onDragLeave={onDragLeave}
      onDragOver={onDragOver}
      onDrop={onDrop}
      title={label}
      type="button"
    >
      {children}
      <span className="max-w-36 truncate">{label}</span>
    </button>
  );
}

function readPayload<T>(event: DragEvent, mime: string): T | undefined {
  const raw = event.dataTransfer.getData(mime);
  if (!raw) return undefined;
  try {
    return JSON.parse(raw) as T;
  } catch {
    return undefined;
  }
}
