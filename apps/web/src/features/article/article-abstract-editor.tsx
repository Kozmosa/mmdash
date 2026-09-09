import type { HocuspocusProvider } from "@hocuspocus/provider";
import Collaboration from "@tiptap/extension-collaboration";
import { Mathematics } from "@tiptap/extension-mathematics";
import StarterKit from "@tiptap/starter-kit";
import { EditorContent, useEditor } from "@tiptap/react";
import { TableKit } from "@tiptap/extension-table";

import { createArticleNodes } from "./article-nodes";

// The abstract is an independent collaborative Markdown document, not a body
// block: it deliberately omits block review, slash commands, and drag
// handles. Formulas, formatting, and Zotero citations come from the shared
// article node set. TableKit must match the body editor (table: false):
// the shared ArticleTable node declares a `tableRow+` content expression,
// so the row/cell/header nodes have to exist here too or schema creation
// throws and takes down the whole workbench.
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
        TableKit.configure({ table: false }),
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
