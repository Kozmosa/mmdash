import type { ResultBundle } from "@/features/experiment/types";

import type { ArticleVersionedReferenceInsert } from "./article-editor";
import type { ArticleReference } from "./types";

export function immutableExperimentVersion(
  result?: Pick<
    ResultBundle,
    "execution_bundle" | "files" | "result_commit_sha"
  >,
): string | undefined {
  return (
    result?.result_commit_sha ??
    result?.execution_bundle?.version_id ??
    result?.files.find((file) => file.artifact_version_id)?.artifact_version_id
  );
}

export function versionedReferenceInsert(
  reference: ArticleReference,
): ArticleVersionedReferenceInsert | undefined {
  if (
    reference.reference_type !== "model_snapshot" &&
    reference.reference_type !== "experiment_result" &&
    reference.reference_type !== "problem"
  )
    return undefined;
  return {
    mimeType:
      typeof reference.metadata.mime_type === "string"
        ? reference.metadata.mime_type
        : undefined,
    objectId: reference.source_object_id,
    referenceId: reference.reference_id,
    referenceType: reference.reference_type,
    title: reference.title,
    versionId: reference.source_version_id,
  };
}
