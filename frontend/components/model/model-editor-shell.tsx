"use client";

import { forwardRef, useEffect, useImperativeHandle, useMemo, useRef, useState } from "react";
import CodeMirror from "@uiw/react-codemirror";
import { markdown as markdownLanguage } from "@codemirror/lang-markdown";
import { autocompletion, type CompletionContext } from "@codemirror/autocomplete";
import { Prec } from "@codemirror/state";
import { EditorView, keymap, type ViewUpdate } from "@codemirror/view";
import { FileCode2, Pilcrow } from "lucide-react";

import MarkdownRenderer from "@/components/model/markdown-renderer";

export interface CollaboratorCursor {
  id: string;
  name: string;
  color: string;
  cursorIndex: number;
  selectionStart?: number;
  selectionEnd?: number;
}

export interface ModelEditorCursor {
  cursorIndex: number;
  selectionStart: number;
  selectionEnd: number;
}

export interface ModelEditorShellHandle {
  focus: () => void;
  insertMarkdown: (markdown: string, cursorOffset?: number) => void;
}

interface ModelEditorShellProps {
  value: string;
  onChange: (markdown: string) => void;
  onCursorChange?: (cursor: ModelEditorCursor) => void;
  collaborators?: CollaboratorCursor[];
}

interface SlashCommand {
  id: string;
  label: string;
  aliases: string[];
  description: string;
  template: string;
  cursorOffset?: number;
}

const slashCommands: SlashCommand[] = [
  {
    id: "formula",
    label: "公式",
    aliases: ["gs"],
    description: "插入 LaTeX 公式块",
    template: "$$\n\n$$",
    cursorOffset: 3,
  },
  {
    id: "table",
    label: "表格",
    aliases: ["table", "tb"],
    description: "插入 Markdown 表格",
    template: "| 项目 | 说明 |\n| --- | --- |\n|  |  |",
    cursorOffset: 31,
  },
  {
    id: "quote",
    label: "引用",
    aliases: ["quote", "qt"],
    description: "插入引用块",
    template: "> 引用内容",
    cursorOffset: 2,
  },
  {
    id: "hr",
    label: "分割线",
    aliases: ["hr"],
    description: "插入水平分割线",
    template: "---",
  },
  {
    id: "image",
    label: "插入图片",
    aliases: ["img"],
    description: "插入图片链接",
    template: "![图片描述](图片地址)",
    cursorOffset: 4,
  },
  {
    id: "file",
    label: "插入文件",
    aliases: ["file"],
    description: "插入文件链接",
    template: "[文件名称](文件地址)",
    cursorOffset: 1,
  },
  {
    id: "code",
    label: "代码块",
    aliases: ["code", "cd"],
    description: "插入代码块",
    template: "```\n\n```",
    cursorOffset: 4,
  },
];

function filterSlashCommands(query: string) {
  if (!query) return slashCommands;
  return slashCommands
    .filter((command) => {
      const haystack = [command.id, command.label, ...command.aliases, command.description]
        .join(" ")
        .toLowerCase();
      return haystack.includes(query);
    })
    .sort((left, right) => Number(isExactSlashCommand(right, query)) - Number(isExactSlashCommand(left, query)));
}

function isExactSlashCommand(command: SlashCommand, query: string) {
  const normalizedQuery = query.toLowerCase();
  return (
    command.id.toLowerCase() === normalizedQuery ||
    command.label.toLowerCase() === normalizedQuery ||
    command.aliases.some((alias) => alias.toLowerCase() === normalizedQuery)
  );
}

function findExactSlashCommand(query: string) {
  return slashCommands.find((command) => isExactSlashCommand(command, query));
}

function applySlashCommand(view: EditorView, command: SlashCommand, from: number, to: number) {
  const cursor = from + (command.cursorOffset ?? command.template.length);
  view.dispatch({
    changes: { from, to, insert: command.template },
    selection: { anchor: cursor },
    scrollIntoView: true,
  });
  view.focus();
}

function slashCompletion(context: CompletionContext) {
  const line = context.state.doc.lineAt(context.pos);
  const beforeCursor = line.text.slice(0, context.pos - line.from);
  const match = beforeCursor.match(/\/([A-Za-z0-9\u4e00-\u9fa5_]*)$/);
  if (!match) return null;

  const query = match[1].toLowerCase();
  const from = context.pos - match[0].length;
  const commands = filterSlashCommands(query);
  if (commands.length === 0) return null;

  return {
    from,
    to: context.pos,
    filter: false,
    validFor: /^\/[A-Za-z0-9\u4e00-\u9fa5_]*$/,
    options: commands.map((command) => ({
      label: command.label,
      detail: command.aliases.map((alias) => `/${alias}`).join(" "),
      info: command.description,
      type: "keyword",
      apply(view: EditorView, _completion: unknown, completionFrom: number, completionTo: number) {
        applySlashCommand(view, command, completionFrom, completionTo);
      },
    })),
  };
}

const slashCommandKeymap = Prec.highest(
  keymap.of([
    {
      key: "Enter",
      run(view) {
        const selection = view.state.selection.main;
        if (!selection.empty) return false;

        const line = view.state.doc.lineAt(selection.from);
        const beforeCursor = line.text.slice(0, selection.from - line.from);
        const match = beforeCursor.match(/\/([A-Za-z0-9\u4e00-\u9fa5_]*)$/);
        if (!match?.[1]) return false;

        const command = findExactSlashCommand(match[1]);
        if (!command) return false;

        applySlashCommand(view, command, selection.from - match[0].length, selection.from);
        return true;
      },
    },
  ])
);

export const ModelEditorShell = forwardRef<ModelEditorShellHandle, ModelEditorShellProps>(
  function ModelEditorShell({ value, onChange, onCursorChange, collaborators = [] }, ref) {
    const editorViewRef = useRef<EditorView | null>(null);
    const [draft, setDraft] = useState(value);

    useEffect(() => {
      if (value !== draft) setDraft(value);
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [value]);

    const extensions = useMemo(
      () => [
        markdownLanguage(),
        slashCommandKeymap,
        autocompletion({
          override: [slashCompletion],
          activateOnTyping: true,
          icons: false,
          maxRenderedOptions: 8,
        }),
        EditorView.lineWrapping,
        EditorView.theme({
          "&": {
            height: "100%",
            background: "transparent",
            color: "hsl(var(--foreground))",
          },
          ".cm-scroller": {
            fontFamily:
              "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, \"Liberation Mono\", \"Courier New\", monospace",
            fontSize: "15px",
            lineHeight: "1.65",
          },
          ".cm-content": {
            minHeight: "100%",
            padding: "20px 24px",
            textDecoration: "none",
          },
          ".cm-content *": {
            textDecoration: "none",
            textDecorationLine: "none",
          },
          ".cm-line": {
            padding: "0",
            textDecoration: "none",
          },
          ".cm-focused": {
            outline: "none",
          },
          ".cm-gutters": {
            display: "none",
          },
          ".cm-tooltip": {
            border: "1px solid hsl(var(--border))",
            borderRadius: "12px",
            boxShadow: "0 18px 45px rgb(15 23 42 / 0.14)",
            overflow: "hidden",
            background: "hsl(var(--background))",
          },
          ".cm-tooltip-autocomplete ul": {
            fontFamily: "inherit",
            padding: "4px",
          },
          ".cm-tooltip-autocomplete ul li": {
            borderRadius: "8px",
            padding: "8px 10px",
          },
          ".cm-tooltip-autocomplete ul li[aria-selected]": {
            background: "hsl(var(--muted))",
            color: "hsl(var(--foreground))",
          },
          ".cm-completionDetail": {
            color: "hsl(var(--muted-foreground))",
            marginLeft: "8px",
          },
          ".cm-completionInfo": {
            border: "1px solid hsl(var(--border))",
            borderRadius: "10px",
            padding: "8px 10px",
            background: "hsl(var(--background))",
          },
        }),
        EditorView.contentAttributes.of({
          spellcheck: "false",
          autocorrect: "off",
          autocapitalize: "off",
        }),
      ],
      []
    );

    const reportCursor = (viewUpdate: ViewUpdate) => {
      if (!onCursorChange) return;
      const selection = viewUpdate.state.selection.main;
      onCursorChange({
        cursorIndex: selection.to,
        selectionStart: selection.from,
        selectionEnd: selection.to,
      });
    };

    useImperativeHandle(ref, () => ({
      focus: () => editorViewRef.current?.focus(),
      insertMarkdown: (markdown: string, cursorOffset?: number) => {
        const view = editorViewRef.current;
        if (!view) {
          const nextValue = `${draft}${markdown}`;
          setDraft(nextValue);
          onChange(nextValue);
          return;
        }

        const selection = view.state.selection.main;
        const cursor = selection.from + (cursorOffset ?? markdown.length);
        view.dispatch({
          changes: { from: selection.from, to: selection.to, insert: markdown },
          selection: { anchor: cursor },
          scrollIntoView: true,
        });
        view.focus();
      },
    }));

    return (
      <div className="grid min-h-[calc(100vh-12rem)] w-full overflow-hidden rounded-xl border bg-background shadow-sm lg:grid-cols-2">
        <section className="relative flex min-h-[520px] flex-col border-b lg:border-b-0 lg:border-r">
          <div className="flex h-12 items-center justify-between border-b px-4 text-sm">
            <div className="flex items-center gap-2 font-medium">
              <FileCode2 className="size-4" />
              Markdown 编辑
            </div>
            <div className="text-xs text-muted-foreground">输入 / 唤出命令</div>
          </div>
          <div className="min-h-0 flex-1">
            <CodeMirror
              value={draft}
              height="100%"
              basicSetup={{
                lineNumbers: false,
                foldGutter: false,
                highlightActiveLine: false,
                highlightActiveLineGutter: false,
                autocompletion: false,
              }}
              extensions={extensions}
              placeholder={"# 模型\n\n输入 /gs 插入公式，/table 插入表格。"}
              onCreateEditor={(view) => {
                editorViewRef.current = view;
              }}
              onChange={(nextValue) => {
                setDraft(nextValue);
                onChange(nextValue);
              }}
              onUpdate={(viewUpdate) => {
                if (viewUpdate.selectionSet || viewUpdate.docChanged) reportCursor(viewUpdate);
              }}
            />
          </div>
          {collaborators.length > 0 && (
            <div className="absolute bottom-3 right-3 flex flex-wrap justify-end gap-2">
              {collaborators.map((member) => (
                <span
                  key={member.id}
                  className="rounded-full border bg-background px-2 py-1 text-xs shadow-sm"
                  style={{ borderColor: member.color }}
                >
                  <Pilcrow className="mr-1 inline size-3" />
                  {member.name}
                </span>
              ))}
            </div>
          )}
        </section>

        <section className="flex min-h-[520px] flex-col overflow-hidden">
          <div className="flex h-12 items-center justify-between border-b px-4 text-sm">
            <div className="font-medium">渲染预览</div>
            <div className="text-xs text-muted-foreground">实时 Markdown</div>
          </div>
          <div className="min-h-0 flex-1 overflow-auto px-8 py-6">
            {draft.trim() ? (
              <MarkdownRenderer markdown={draft} />
            ) : (
              <div className="flex h-full min-h-80 items-center justify-center rounded-lg border border-dashed text-center text-sm text-muted-foreground">
                左侧输入 Markdown 后，这里会实时显示渲染结果。
              </div>
            )}
          </div>
        </section>
      </div>
    );
  }
);
