import type { HocuspocusProvider } from "@hocuspocus/provider";
import Collaboration from "@tiptap/extension-collaboration";
import { Mathematics } from "@tiptap/extension-mathematics";
import StarterKit from "@tiptap/starter-kit";
import { EditorContent, useEditor } from "@tiptap/react";

import { createArticleNodes } from "./article-nodes";

// The abstract is an independent collaborative Markdown document, not a body
// block: it deliberately omits block review, slash commands, and drag
// handles. Formulas, formatting, and Zotero citations come from the shared
// article node set.
export function ArticleAbstractEditor({
  canEdit,
  projectId,
  provider,
}: Readonly<{
  canEdit: boolean;
  projectId: string;
  provider: HocuspocusProvider;
}>) {
  const editor = useEditor(
    {
      editable: canEdit,
      extensions: [
        StarterKit.configure({ dropcursor: false, undoRedo: false }),
        ...createArticleNodes(projectId),
        Mathematics.configure({ katexOptions: { throwOnError: false } }),
        Collaboration.configure({
          document: provider.document,
          field: "default",
        }),
      ],
    },
    [provider],
  );
  return (
    <EditorContent
      className="mx-auto min-h-0 w-full max-w-3xl flex-1 overflow-y-auto rounded-md border bg-card p-6"
      editor={editor}
    />
  );
}
