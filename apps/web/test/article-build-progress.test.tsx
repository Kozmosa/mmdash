import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import {
  BuildProgress,
  latestSuccessfulArticlePreview,
  outputLabel,
} from "@/features/article/article-workbench";
import type { ArticleBuild } from "@/features/article/types";

const build: ArticleBuild = {
  build_report_artifact_version_id: null,
  commit_id: "00000000-0000-4000-8000-000000000101",
  created_at: "2026-08-26T00:00:00Z",
  error_message: null,
  finished_at: null,
  id: "00000000-0000-4000-8000-000000000102",
  kind: "formal",
  log_artifact_version_id: null,
  outputs: [],
  pdf_artifact_version_id: null,
  progress_percent: 55,
  progress_stage: "compiling",
  project_id: "00000000-0000-4000-8000-000000000103",
  source_zip_artifact_version_id: null,
  started_at: "2026-08-26T00:00:01Z",
  status: "running",
  superseded_at: null,
  synctex_artifact_version_id: null,
  template_version_id: "00000000-0000-4000-8000-000000000104",
  tex_source_artifact_version_id: null,
  updated_at: "2026-08-26T00:00:02Z",
};

describe("Article build progress", () => {
  it("shows the current PDF compilation stage and percentage", () => {
    render(<BuildProgress build={build} />);

    expect(screen.getByText("编译 PDF")).toBeInTheDocument();
    expect(screen.getByText("55%")).toBeInTheDocument();
    expect(
      screen.getByRole("progressbar", { name: "论文编译进度" }),
    ).toHaveAttribute("aria-valuenow", "55");
  });

  it("uses a clear download label for the release source archive", () => {
    expect(outputLabel("source_zip")).toBe("TeX 源码 ZIP");
  });

  it("only exposes a succeeded preview with a PDF output", () => {
    const failed = {
      ...build,
      build_kind: "preview" as const,
      status: "failed" as const,
    };
    const succeeded = {
      ...build,
      build_kind: "preview" as const,
      status: "succeeded" as const,
      outputs: [
        {
          role: "pdf" as const,
          artifact_id: "artifact-pdf",
          version_id: "version-pdf",
          filename: "paper.pdf",
          mime_type: "application/pdf",
          sha256: "a".repeat(64),
          size_bytes: 10,
        },
      ],
    };
    expect(latestSuccessfulArticlePreview([failed, succeeded])).toBe(succeeded);
    expect(latestSuccessfulArticlePreview([failed])).toBeUndefined();
  });
});
