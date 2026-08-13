"use client";

import type { HocuspocusProvider } from "@hocuspocus/provider";
import Collaboration from "@tiptap/extension-collaboration";
import CollaborationCaret from "@tiptap/extension-collaboration-caret";
import UniqueID from "@tiptap/extension-unique-id";
import { EditorContent, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import { Bold, Code2, Heading2, Italic, Save, Sigma, Undo2 } from "lucide-react";
import { useEffect, type ReactNode } from "react";

import { Button } from "@/components/ui/button";

import { articleNodes } from "./article-nodes";

type Collaborator = { color: string; name: string };

export function ArticleEditor({
  canEdit,
  collaborator,
  onFlush,
  provider,
}: Readonly<{
  canEdit: boolean;
  collaborator: Collaborator;
  onFlush: () => void;
  provider: HocuspocusProvider;
}>) {
  const editor = useEditor(
    {
      editable: canEdit,
      extensions: [
        StarterKit.configure({ undoRedo: false }),
        ...articleNodes,
        UniqueID.configure({
          types: [
            "paragraph",
            "heading",
            "blockquote",
            "codeBlock",
            "mathBlock",
            "artifactReference",
            "experimentResult",
          ],
        }),
        Collaboration.configure({ document: provider.document, field: "default" }),
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

  if (!editor) return null;

  const insertMath = () => {
    const latex = window.prompt("输入 LaTeX 公式", "\\int_0^1 x^2\\,dx");
    if (latex) editor.chain().focus().insertContent({ attrs: { latex }, type: "mathBlock" }).run();
  };

  return (
    <div className="overflow-hidden rounded-xl border bg-background shadow-sm">
      <div className="flex flex-wrap items-center gap-1 border-b bg-muted/30 p-2">
        <EditorButton label="加粗" onClick={() => editor.chain().focus().toggleBold().run()}><Bold /></EditorButton>
        <EditorButton label="斜体" onClick={() => editor.chain().focus().toggleItalic().run()}><Italic /></EditorButton>
        <EditorButton label="二级标题" onClick={() => editor.chain().focus().toggleHeading({ level: 2 }).run()}><Heading2 /></EditorButton>
        <EditorButton label="代码块" onClick={() => editor.chain().focus().toggleCodeBlock().run()}><Code2 /></EditorButton>
        <EditorButton label="公式块" onClick={insertMath}><Sigma /></EditorButton>
        <EditorButton label="撤销" onClick={() => editor.chain().focus().undo().run()}><Undo2 /></EditorButton>
        <Button className="ml-auto" onClick={onFlush} size="sm" variant="outline">
          <Save className="size-3.5" />保存同步
        </Button>
      </div>
      {!canEdit ? (
        <p className="border-b bg-muted/20 px-4 py-2 text-xs text-muted-foreground">
          你当前拥有只读权限；协同光标与远端更新仍会保持同步。
        </p>
      ) : null}
      <EditorContent
        className="[&_.ProseMirror]:min-h-[42rem] [&_.ProseMirror]:p-8 [&_.ProseMirror]:text-[15px] [&_.ProseMirror]:leading-7 [&_.ProseMirror]:outline-none [&_.ProseMirror_blockquote]:border-l-2 [&_.ProseMirror_blockquote]:pl-4 [&_.ProseMirror_h1]:mb-4 [&_.ProseMirror_h1]:text-3xl [&_.ProseMirror_h1]:font-bold [&_.ProseMirror_h2]:mb-3 [&_.ProseMirror_h2]:mt-8 [&_.ProseMirror_h2]:text-2xl [&_.ProseMirror_h2]:font-semibold [&_.ProseMirror_p]:my-3"
        editor={editor}
      />
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
