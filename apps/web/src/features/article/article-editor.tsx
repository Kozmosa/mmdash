"use client";

import type { HocuspocusProvider } from "@hocuspocus/provider";
import Collaboration from "@tiptap/extension-collaboration";
import CollaborationCaret from "@tiptap/extension-collaboration-caret";
import DragHandle from "@tiptap/extension-drag-handle-react";
import { Mathematics } from "@tiptap/extension-mathematics";
import { TableKit } from "@tiptap/extension-table";
import UniqueID from "@tiptap/extension-unique-id";
import { EditorContent, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import {
  Bold,
  Code2,
  GripVertical,
  Heading2,
  Italic,
  Save,
  Sigma,
  Table2,
  Undo2,
} from "lucide-react";
import { useEffect, useState, type DragEvent, type ReactNode } from "react";

import { Button } from "@/components/ui/button";

import { articleNodes } from "./article-nodes";
import { SlashCommand } from "./slash-command";

export const articleArtifactMime =
  "application/vnd.mmdash.article-artifact+json";

export type ArticleArtifactDrop = {
  artifactId: string;
  filename: string;
  mimeType: string;
  title: string;
  versionId: string;
};

export function parseArticleArtifactDrop(raw: string): ArticleArtifactDrop {
  const value = JSON.parse(raw) as Partial<ArticleArtifactDrop>;
  if (
    ![
      value.artifactId,
      value.versionId,
      value.title,
      value.filename,
      value.mimeType,
    ].every((item) => typeof item === "string" && item.length > 0)
  ) {
    throw new Error("Artifact 拖拽数据不完整");
  }
  return value as ArticleArtifactDrop;
}

type Collaborator = { color: string; name: string };

export function ArticleEditor({
  canEdit,
  collaborator,
  onFlush,
  onInsertArtifact,
  provider,
}: Readonly<{
  canEdit: boolean;
  collaborator: Collaborator;
  onFlush: () => void;
  onInsertArtifact: (
    artifact: ArticleArtifactDrop,
  ) => Promise<{ reference_id: string }>;
  provider: HocuspocusProvider;
}>) {
  const [dropError, setDropError] = useState<string>();
  const editor = useEditor(
    {
      editable: canEdit,
      extensions: [
        StarterKit.configure({ undoRedo: false }),
        ...articleNodes,
        Mathematics.configure({ katexOptions: { throwOnError: false } }),
        TableKit.configure({ table: { resizable: true } }),
        SlashCommand,
        UniqueID.configure({
          types: [
            "paragraph",
            "heading",
            "blockquote",
            "codeBlock",
            "mathBlock",
            "blockMath",
            "table",
            "artifactReference",
            "experimentResult",
          ],
        }),
        Collaboration.configure({
          document: provider.document,
          field: "default",
        }),
        CollaborationCaret.configure({ provider, user: collaborator }),
      ],
      immediatelyRender: false,
    },
    [provider],
  );

  useEffect(() => {
    editor?.setEditable(canEdit);
  }, [canEdit, editor]);

  useEffect(() => {
    const save = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "s") {
        event.preventDefault();
        onFlush();
      }
    };
    window.addEventListener("keydown", save);
    return () => window.removeEventListener("keydown", save);
  }, [onFlush]);

  useEffect(() => {
    if (!editor || !canEdit) return;
    const migrate = ({ state }: { state: boolean }) => {
      if (!state) return;
      const replacements: {
        attrs: Record<string, unknown>;
        pos: number;
        type: "blockMath" | "inlineMath";
      }[] = [];
      editor.state.doc.descendants((node, pos) => {
        if (node.type.name === "mathBlock")
          replacements.push({ attrs: node.attrs, pos, type: "blockMath" });
        if (node.type.name === "mathInline")
          replacements.push({ attrs: node.attrs, pos, type: "inlineMath" });
      });
      if (!replacements.length) return;
      const transaction = editor.state.tr;
      for (const replacement of replacements.reverse()) {
        transaction.setNodeMarkup(
          replacement.pos,
          editor.schema.nodes[replacement.type],
          replacement.attrs,
        );
      }
      transaction.setMeta("addToHistory", false);
      editor.view.dispatch(transaction);
    };
    provider.on("synced", migrate);
    if (provider.synced) migrate({ state: true });
    return () => {
      provider.off("synced", migrate);
    };
  }, [canEdit, editor, provider]);

  if (!editor) return null;

  const insertMath = () => {
    const latex = window.prompt("输入 LaTeX 公式", "\\int_0^1 x^2\\,dx");
    if (latex) editor.chain().focus().insertBlockMath({ latex }).run();
  };

  const dropArtifact = async (event: DragEvent<HTMLDivElement>) => {
    if (!canEdit || !event.dataTransfer.types.includes(articleArtifactMime))
      return;
    event.preventDefault();
    setDropError(undefined);
    try {
      const payload = parseArticleArtifactDrop(
        event.dataTransfer.getData(articleArtifactMime),
      );
      const reference = await onInsertArtifact(payload);
      const target = editor.view.posAtCoords({
        left: event.clientX,
        top: event.clientY,
      });
      editor
        .chain()
        .focus()
        .insertContentAt(target?.pos ?? editor.state.selection.to, {
          attrs: {
            artifactId: payload.artifactId,
            objectId: payload.artifactId,
            referenceId: reference.reference_id,
            title: payload.title,
            versionId: payload.versionId,
          },
          type: "artifactReference",
        })
        .run();
    } catch (error) {
      setDropError(
        error instanceof Error ? error.message : "Artifact 插入失败",
      );
    }
  };

  return (
    <div className="overflow-hidden rounded-xl border bg-background shadow-sm">
      <div className="flex flex-wrap items-center gap-1 border-b bg-muted/30 p-2">
        <EditorButton
          label="加粗"
          onClick={() => editor.chain().focus().toggleBold().run()}
        >
          <Bold />
        </EditorButton>
        <EditorButton
          label="斜体"
          onClick={() => editor.chain().focus().toggleItalic().run()}
        >
          <Italic />
        </EditorButton>
        <EditorButton
          label="二级标题"
          onClick={() =>
            editor.chain().focus().toggleHeading({ level: 2 }).run()
          }
        >
          <Heading2 />
        </EditorButton>
        <EditorButton
          label="代码块"
          onClick={() => editor.chain().focus().toggleCodeBlock().run()}
        >
          <Code2 />
        </EditorButton>
        <EditorButton label="公式块" onClick={insertMath}>
          <Sigma />
        </EditorButton>
        <EditorButton
          label="插入表格"
          onClick={() =>
            editor
              .chain()
              .focus()
              .insertTable({ rows: 3, cols: 3, withHeaderRow: true })
              .run()
          }
        >
          <Table2 />
        </EditorButton>
        <EditorButton
          label="撤销"
          onClick={() => editor.chain().focus().undo().run()}
        >
          <Undo2 />
        </EditorButton>
        <Button
          className="ml-auto"
          onClick={onFlush}
          size="sm"
          variant="outline"
        >
          <Save className="size-3.5" />
          保存同步
        </Button>
      </div>
      {!canEdit ? (
        <p className="border-b bg-muted/20 px-4 py-2 text-xs text-muted-foreground">
          你当前拥有只读权限；协同光标与远端更新仍会保持同步。
        </p>
      ) : null}
      {dropError ? (
        <p className="border-b bg-destructive/5 px-4 py-2 text-xs text-destructive">
          {dropError}
        </p>
      ) : null}
      <div
        className="relative pl-9"
        onDragOver={(event) => {
          if (canEdit && event.dataTransfer.types.includes(articleArtifactMime))
            event.preventDefault();
        }}
        onDrop={(event) => void dropArtifact(event)}
      >
        {canEdit ? (
          <DragHandle editor={editor}>
            <button
              aria-label="拖动当前块排序"
              className="flex size-7 cursor-grab items-center justify-center rounded border bg-background text-muted-foreground shadow-sm active:cursor-grabbing"
              type="button"
            >
              <GripVertical className="size-4" />
            </button>
          </DragHandle>
        ) : null}
        <EditorContent
          className="[&_.ProseMirror]:min-h-[42rem] [&_.ProseMirror]:p-8 [&_.ProseMirror]:text-[15px] [&_.ProseMirror]:leading-7 [&_.ProseMirror]:outline-none [&_.ProseMirror_blockquote]:border-l-2 [&_.ProseMirror_blockquote]:pl-4 [&_.ProseMirror_h1]:mb-4 [&_.ProseMirror_h1]:text-3xl [&_.ProseMirror_h1]:font-bold [&_.ProseMirror_h2]:mb-3 [&_.ProseMirror_h2]:mt-8 [&_.ProseMirror_h2]:text-2xl [&_.ProseMirror_h2]:font-semibold [&_.ProseMirror_p]:my-3 [&_.ProseMirror_table]:my-4 [&_.ProseMirror_table]:w-full [&_.ProseMirror_table]:border-collapse [&_.ProseMirror_td]:border [&_.ProseMirror_td]:p-2 [&_.ProseMirror_th]:border [&_.ProseMirror_th]:bg-muted [&_.ProseMirror_th]:p-2"
          editor={editor}
        />
      </div>
    </div>
  );
}

function EditorButton({
  children,
  label,
  onClick,
}: Readonly<{ children: ReactNode; label: string; onClick: () => void }>) {
  return (
    <Button aria-label={label} onClick={onClick} size="icon" variant="ghost">
      <span className="[&>svg]:size-4">{children}</span>
    </Button>
  );
}
