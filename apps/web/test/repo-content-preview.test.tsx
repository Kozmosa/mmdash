import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/components/ui/code-editor", () => ({
  CodeEditor: ({
    onChange,
    value,
  }: {
    onChange?: (value: string) => void;
    value: string;
  }) => <pre data-read-only={String(onChange === undefined)}>{value}</pre>,
}));

import { ContentPreview } from "@/features/repo-browser/content-preview";
import type { RepoFileContent } from "@/features/repo/types";

const fixture: RepoFileContent = {
  branch: "main",
  content: null,
  encoding: null,
  kind: "file",
  mode: "100644",
  object_id: "a".repeat(40),
  path: "fixture.bin",
  preview_status: "binary",
  resolved_revision: "b".repeat(40),
  size: 42,
  workspace: "code",
};

afterEach(cleanup);

describe("Repo safe content states", () => {
  it("renders text in the editor without an edit callback", () => {
    render(
      <ContentPreview
        content={{
          ...fixture,
          content: "package main",
          encoding: "utf-8",
          path: "main.go",
          preview_status: "text",
        }}
      />,
    );

    expect(screen.getByText("package main")).toHaveAttribute(
      "data-read-only",
      "true",
    );
  });

  it.each([
    ["binary", "Binary 文件不可预览"],
    ["too_large", "文件过大"],
    ["lfs_not_materialized", "Git LFS 内容未物化"],
    ["symlink", "Symbolic link"],
    ["submodule", "Submodule"],
  ] as const)(
    "renders %s as metadata instead of an editor",
    (status, title) => {
      render(
        <ContentPreview
          content={{
            ...fixture,
            kind:
              status === "symlink" || status === "submodule" ? status : "file",
            preview_status: status,
          }}
        />,
      );
      expect(screen.getByRole("status")).toHaveTextContent(title);
      expect(screen.getByText("42 bytes")).toBeInTheDocument();
    },
  );
});
