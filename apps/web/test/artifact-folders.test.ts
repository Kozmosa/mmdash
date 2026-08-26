import { describe, expect, it } from "vitest";

import {
  artifactFolderDescendantIds,
  flattenArtifactFolders,
} from "@/features/artifact/artifact-folders";
import type { ArtifactFolder } from "@/features/artifact/types";

const tree: ArtifactFolder[] = [
  {
    children: [
      {
        children: [],
        folder_id: "child",
        name: "数据",
        parent_folder_id: "root",
        position: 0,
        project_id: "project",
      },
    ],
    folder_id: "root",
    name: "实验",
    parent_folder_id: null,
    position: 0,
    project_id: "project",
  },
];

describe("Artifact folder tree", () => {
  it("flattens nested folders without losing their shared IDs", () => {
    expect(flattenArtifactFolders(tree)).toEqual([
      { depth: 0, folder: tree[0], path: "实验" },
      { depth: 1, folder: tree[0]!.children[0], path: "实验/数据" },
    ]);
  });

  it("identifies descendants that cannot become a folder parent", () => {
    expect([...artifactFolderDescendantIds(tree[0]!)].sort()).toEqual([
      "child",
    ]);
  });
});
