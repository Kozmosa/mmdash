import { artifactApi } from "@/features/artifact/artifact-api";
import { MultipartUploadTask } from "@/features/artifact/multipart-upload";
import type { ArtifactDetail } from "@/features/artifact/types";

import type { ArticleTemplate } from "./types";

type CopyTemplateDependencies = {
  download: typeof artifactApi.download;
  fetch: typeof globalThis.fetch;
  upload: (
    file: File,
    name: string,
    projectId: string,
  ) => Promise<ArtifactDetail>;
};

const defaultDependencies: CopyTemplateDependencies = {
  download: artifactApi.download,
  fetch: globalThis.fetch.bind(globalThis),
  upload: (file, name, projectId) =>
    new MultipartUploadTask({
      description:
        "从不可变 Article 模板复制，可下载修改后作为新 Version 再次注册",
      file,
      idempotencyKey: crypto.randomUUID(),
      kind: "attachment",
      name,
      projectId,
      tags: ["article-template", "template-copy"],
    }).start(),
};

export async function copyArticleTemplateToArtifact(
  projectId: string,
  template: ArticleTemplate,
  dependencies: CopyTemplateDependencies = defaultDependencies,
): Promise<ArtifactDetail> {
  const grant = await dependencies.download(
    projectId,
    template.artifact_id,
    template.version_id,
  );
  const response = await dependencies.fetch(grant.transfer.url, {
    headers: grant.transfer.headers,
    method: grant.transfer.method,
  });
  if (!response.ok) throw new Error("模板文件读取失败，未创建副本");
  const blob = await response.blob();
  const filename = `article-template-copy-${template.template_id.slice(0, 8)}.zip`;
  const file = new File([blob], filename, {
    lastModified: Date.now(),
    type: grant.mime_type || "application/zip",
  });
  return dependencies.upload(file, `${template.manifest.name} 副本`, projectId);
}

export function copiedTemplateManifest(template: ArticleTemplate) {
  return {
    ...template.manifest,
    name: `${template.manifest.name} 副本`,
    version: `${template.manifest.version}-copy`,
  } satisfies ArticleTemplate["manifest"];
}
