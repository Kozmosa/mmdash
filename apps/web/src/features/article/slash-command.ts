import { Extension, type Editor, type Range } from "@tiptap/core";
import { isChangeOrigin } from "@tiptap/extension-collaboration";
import Suggestion, { type SuggestionProps } from "@tiptap/suggestion";

export const openArtifactLibraryEvent = "mmdash:article-open-artifact";
export const openArticleImageUploadEvent = "mmdash:article-upload-image";
export const openArticleSidebarEvent = "mmdash:article-open-sidebar";

type SlashItem = {
  action: (editor: Editor) => void;
  keywords: string;
  label: string;
};

const slashItems: SlashItem[] = [
  {
    label: "正文",
    keywords: "paragraph text 正文 段落",
    action: (editor) => {
      editor.chain().focus().setParagraph().run();
    },
  },
  {
    label: "一级标题",
    keywords: "heading title h1 标题",
    action: (editor) => {
      editor.chain().focus().toggleHeading({ level: 1 }).run();
    },
  },
  {
    label: "二级标题",
    keywords: "heading title h2 标题",
    action: (editor) => {
      editor.chain().focus().toggleHeading({ level: 2 }).run();
    },
  },
  {
    label: "无序列表",
    keywords: "bullet list 列表",
    action: (editor) => {
      editor.chain().focus().toggleBulletList().run();
    },
  },
  {
    label: "有序列表",
    keywords: "ordered list 列表",
    action: (editor) => {
      editor.chain().focus().toggleOrderedList().run();
    },
  },
  {
    label: "引用",
    keywords: "quote blockquote 引用",
    action: (editor) => {
      editor.chain().focus().toggleBlockquote().run();
    },
  },
  {
    label: "代码块",
    keywords: "code block 代码",
    action: (editor) => {
      editor.chain().focus().toggleCodeBlock().run();
    },
  },
  {
    label: "行内公式",
    keywords: "inline math latex 公式",
    action: (editor) => {
      editor.chain().focus().insertInlineMath({ latex: "x" }).run();
    },
  },
  {
    label: "公式块",
    keywords: "block math latex 公式",
    action: (editor) => {
      editor
        .chain()
        .focus()
        .insertBlockMath({ latex: "\\int_0^1 x^2\\,dx" })
        .run();
    },
  },
  {
    label: "表格",
    keywords: "table 表格",
    action: (editor) => {
      editor
        .chain()
        .focus()
        .insertTable({ rows: 3, cols: 3, withHeaderRow: true })
        .run();
    },
  },
  {
    label: "图片",
    keywords: "image upload paste 图片 上传",
    action: () => {
      window.dispatchEvent(new CustomEvent(openArticleImageUploadEvent));
    },
  },
  {
    label: "Artifact 引用",
    keywords: "artifact file image 附件 图片",
    action: () => {
      window.dispatchEvent(new CustomEvent(openArtifactLibraryEvent));
    },
  },
  {
    label: "Model 引用",
    keywords: "model snapshot 模型 快照 引用",
    action: () => {
      window.dispatchEvent(
        new CustomEvent(openArticleSidebarEvent, {
          detail: { panel: "reference", referenceKind: "model_snapshot" },
        }),
      );
    },
  },
  {
    label: "Experiment 引用",
    keywords: "experiment result 实验 结果 引用",
    action: () => {
      window.dispatchEvent(
        new CustomEvent(openArticleSidebarEvent, {
          detail: { panel: "reference", referenceKind: "experiment_result" },
        }),
      );
    },
  },
  {
    label: "Zotero 引用",
    keywords: "zotero citation reference 文献 引用",
    action: () => {
      window.dispatchEvent(
        new CustomEvent(openArticleSidebarEvent, {
          detail: { panel: "zotero" },
        }),
      );
    },
  },
];

export const SlashCommand = Extension.create({
  name: "articleSlashCommand",
  addProseMirrorPlugins() {
    return [
      Suggestion<SlashItem, SlashItem>({
        editor: this.editor,
        char: "/",
        startOfLine: true,
        shouldShow: ({ transaction }) => !isChangeOrigin(transaction),
        items: ({ query }) => {
          const normalized = query.trim().toLowerCase();
          return slashItems.filter(
            (item) =>
              !normalized ||
              `${item.label} ${item.keywords}`
                .toLowerCase()
                .includes(normalized),
          );
        },
        command: ({ editor, range, props }) => {
          editor.chain().focus().deleteRange(range).run();
          props.action(editor);
        },
        render: renderSlashMenu,
      }),
    ];
  },
});

function renderSlashMenu() {
  let root: HTMLDivElement | undefined;
  let unmount: (() => void) | undefined;
  let selected = 0;
  let current: SuggestionProps<SlashItem, SlashItem> | undefined;

  const draw = (props: SuggestionProps<SlashItem, SlashItem>) => {
    current = props;
    if (!root) return;
    selected = Math.min(selected, Math.max(0, props.items.length - 1));
    root.replaceChildren();
    root.setAttribute("aria-label", "插入块");
    root.setAttribute("role", "listbox");
    for (const [index, item] of props.items.entries()) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = `block w-full rounded px-3 py-2 text-left text-sm ${index === selected ? "bg-accent text-accent-foreground" : "hover:bg-muted"}`;
      button.textContent = item.label;
      button.setAttribute("role", "option");
      button.setAttribute("aria-selected", String(index === selected));
      button.addEventListener("mousedown", (event) => {
        event.preventDefault();
        props.command(item);
      });
      root.append(button);
    }
    if (!props.items.length) {
      const empty = document.createElement("p");
      empty.className = "px-3 py-2 text-sm text-muted-foreground";
      empty.textContent = "没有匹配的块";
      root.append(empty);
    }
  };

  return {
    onStart(props: SuggestionProps<SlashItem, SlashItem>) {
      root = document.createElement("div");
      root.className =
        "z-50 max-h-80 w-60 overflow-auto rounded-lg border bg-popover p-1 text-popover-foreground shadow-lg";
      draw(props);
      unmount = props.mount(root);
    },
    onUpdate: draw,
    onKeyDown({ event }: { event: KeyboardEvent }) {
      if (!current?.items.length) return event.key === "Escape";
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        selected =
          (selected +
            (event.key === "ArrowDown" ? 1 : -1) +
            current.items.length) %
          current.items.length;
        draw(current);
        return true;
      }
      if (event.key === "Enter") {
        current.command(current.items[selected]!);
        return true;
      }
      return event.key === "Escape";
    },
    onExit() {
      unmount?.();
      unmount = undefined;
      root = undefined;
      current = undefined;
      selected = 0;
    },
  };
}

export function runSlashItemForTest(
  label: string,
  editor: Editor,
  range: Range,
): void {
  const item = slashItems.find((candidate) => candidate.label === label);
  if (!item) throw new Error(`Unknown slash item: ${label}`);
  editor.chain().focus().deleteRange(range).run();
  item.action(editor);
}
