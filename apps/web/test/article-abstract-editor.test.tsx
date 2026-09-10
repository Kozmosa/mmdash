// @vitest-environment jsdom

import { cleanup, render } from "@testing-library/react";
import type { HocuspocusProvider } from "@hocuspocus/provider";
import * as Y from "yjs";
import { afterEach, describe, expect, it } from "vitest";

import { ArticleAbstractEditor } from "@/features/article/article-abstract-editor";

afterEach(cleanup);

describe("Article abstract editor", () => {
  it("mounts with the shared table schema", () => {
    const document = new Y.Doc();
    const provider = { document } as HocuspocusProvider;

    expect(() =>
      render(
        <ArticleAbstractEditor
          canEdit
          projectId="project-1"
          provider={provider}
        />,
      ),
    ).not.toThrow();

    document.destroy();
  });
});
