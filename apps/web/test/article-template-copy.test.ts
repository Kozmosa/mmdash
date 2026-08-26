import { describe, expect, it, vi } from "vitest";

import {
  copiedTemplateManifest,
  copyArticleTemplateToArtifact,
} from "@/features/article/article-template-copy";
import type { ArticleTemplate } from "@/features/article/types";

const template: ArticleTemplate = {
  artifact_id: "artifact-default",
  created_at: "2026-01-01T00:00:00Z",
  created_by: "system",
  manifest: {
    bibliography_target: ".mmdash/references.bib",
    bibliography_tool: "none",
    content_target: ".mmdash/generated-content.tex",
    engine: "xelatex",
    entrypoint: "main.tex",
    name: "mmdash 默认论文模板",
    output: "main.pdf",
    schema_version: "1.0",
    version: "1.0.0",
  },
  project_id: "project-1",
  status: "ready",
  template_id: "template-12345678",
  updated_at: "2026-01-01T00:00:00Z",
  version_id: "version-default",
};

describe("Article template copying", () => {
  it("copies immutable bytes into an ordinary customizable Artifact", async () => {
    const detail = {
      artifact: { artifact_id: "artifact-copy" },
      current_version: { version_id: "version-copy" },
    } as never;
    const upload = vi.fn(async (...args: [File, string, string]) => {
      void args;
      return detail;
    });
    const download = vi.fn(async () => ({
      artifact_id: "artifact-default",
      filename: "mmdash-default-template.zip",
      mime_type: "application/zip",
      size_bytes: 3,
      transfer: {
        expires_at: "2026-01-01T00:05:00Z",
        headers: { Authorization: "temporary" },
        method: "GET" as const,
        url: "https://download.test/template",
      },
      version_id: "version-default",
    }));
    const fetch = vi.fn(
      async () => new Response(new Blob(["zip"]), { status: 200 }),
    );

    await expect(
      copyArticleTemplateToArtifact("project-1", template, {
        download,
        fetch,
        upload,
      }),
    ).resolves.toBe(detail);
    expect(download).toHaveBeenCalledWith(
      "project-1",
      "artifact-default",
      "version-default",
    );
    expect(fetch).toHaveBeenCalledWith("https://download.test/template", {
      headers: { Authorization: "temporary" },
      method: "GET",
    });
    const [file, name, projectId] = upload.mock.calls[0]!;
    expect(file).toBeInstanceOf(File);
    expect(file.name).toBe("article-template-copy-template.zip");
    expect(name).toBe("mmdash 默认论文模板 副本");
    expect(projectId).toBe("project-1");
  });

  it("prepares a distinct manifest for registration", () => {
    expect(copiedTemplateManifest(template)).toMatchObject({
      name: "mmdash 默认论文模板 副本",
      version: "1.0.0-copy",
    });
  });
});
