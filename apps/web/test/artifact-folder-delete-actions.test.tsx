import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ArtifactFolderDeleteActions } from "@/features/artifact/artifact-folder-delete-actions";

afterEach(cleanup);

describe("ArtifactFolderDeleteActions", () => {
  it("makes the non-recursive and recursive preservation choices explicit", () => {
    const onDelete = vi.fn();
    const { rerender } = render(
      <ArtifactFolderDeleteActions
        folderName="Results"
        onDelete={onDelete}
        pending={false}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "删除文件夹" }));
    expect(screen.getByRole("dialog")).toHaveTextContent("Artifact 不会被删除");
    fireEvent.click(screen.getByRole("button", { name: "仅删除当前文件夹" }));
    expect(onDelete).toHaveBeenLastCalledWith(false);

    rerender(
      <ArtifactFolderDeleteActions
        folderName="Results"
        onDelete={onDelete}
        pending={false}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "删除文件夹" }));
    fireEvent.click(screen.getByRole("button", { name: "递归移除文件夹结构" }));
    expect(onDelete).toHaveBeenLastCalledWith(true);
  });
});
