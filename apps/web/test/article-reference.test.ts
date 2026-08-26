import { describe, expect, it } from "vitest";

import {
  immutableExperimentVersion,
  versionedReferenceInsert,
} from "@/features/article/article-reference";
import type { ArticleReference } from "@/features/article/types";

describe("Article immutable references", () => {
  it("uses a result commit instead of the mutable experiment id", () => {
    expect(
      immutableExperimentVersion({
        execution_bundle: {
          artifact_id: "bundle",
          filename: "execution-bundle.zip",
          sha256: "a".repeat(64),
          size_bytes: 10,
          version_id: "bundle-version",
        },
        files: [],
        result_commit_sha: "commit-sha",
      }),
    ).toBe("commit-sha");
    expect(
      immutableExperimentVersion({
        execution_bundle: {
          artifact_id: "bundle",
          filename: "execution-bundle.zip",
          sha256: "a".repeat(64),
          size_bytes: 10,
          version_id: "bundle-version",
        },
        files: [],
      }),
    ).toBe("bundle-version");
    expect(immutableExperimentVersion({ files: [] })).toBeUndefined();
  });

  it("inserts only supported frozen references with their stored version", () => {
    const reference: ArticleReference = {
      created_at: "2026-08-24T00:00:00Z",
      created_by: "user-1",
      metadata: { mime_type: "application/pdf" },
      project_id: "project-1",
      reference_id: "reference-1",
      reference_type: "problem",
      source_object_id: "artifact-1",
      source_version_id: "version-4",
      title: "题目",
    };
    expect(versionedReferenceInsert(reference)).toEqual({
      mimeType: "application/pdf",
      objectId: "artifact-1",
      referenceId: "reference-1",
      referenceType: "problem",
      title: "题目",
      versionId: "version-4",
    });
    expect(
      versionedReferenceInsert({
        ...reference,
        reference_type: "zotero",
      }),
    ).toBeUndefined();
  });
});
