import { cleanup, fireEvent, render, screen } from "@testing-library/react";
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

vi.mock("next/image", () => ({
  default: ({ unoptimized, ...properties }: Record<string, unknown>) => {
    void unoptimized;
    return <img {...properties} />;
  },
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

  it("renders CSV with a table mode", () => {
    render(
      <ContentPreview
        content={{
          ...fixture,
          content: "name,value\nalpha,1\nbeta,2",
          encoding: "utf-8",
          path: "summary.csv",
          preview_status: "text",
        }}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "表格" }));
    expect(screen.getByText("alpha")).toBeInTheDocument();
    expect(screen.getByText("beta")).toBeInTheDocument();
  });

  it("renders Markdown with a rendered mode", () => {
    render(
      <ContentPreview
        content={{
          ...fixture,
          content: "# Summary\n\nRendered content",
          encoding: "utf-8",
          path: "README.md",
          preview_status: "text",
        }}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "渲染" }));
    expect(
      screen.getByRole("heading", { name: "Summary" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Rendered content")).toBeInTheDocument();
  });

  it("renders an image through the fixed repository object URL", () => {
    render(
      <ContentPreview
        content={{
          ...fixture,
          path: "figures/dependency-plot.png",
        }}
        projectId="project-1"
      />,
    );

    const image = screen.getByRole("img", {
      name: "figures/dependency-plot.png",
    });
    expect(image).toHaveAttribute(
      "src",
      "/api/projects/project-1/repository/raw?path=figures%2Fdependency-plot.png&revision=" +
        "b".repeat(40) +
        "&workspace=code",
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
