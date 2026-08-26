import { mergeAttributes, Node } from "@tiptap/core";
import { Table, TableView } from "@tiptap/extension-table";
import { ReactNodeViewRenderer } from "@tiptap/react";
import type { Node as ProseMirrorNode } from "@tiptap/pm/model";
import type { EditorView } from "@tiptap/pm/view";
import { createElement } from "react";

import { ArticleArtifactNodeView } from "./article-artifact-node-view";
import {
  articleImageGroupGridTemplateColumns,
  normalizeArticleImageGroupColumns,
} from "./article-image-group";
import { ArticleImageNodeView } from "./article-image-node-view";
import { ArticleImageGroupNodeView } from "./article-image-group-node-view";
import { ArticleZoteroCitationNodeView } from "./article-zotero-citation-node-view";
import {
  imageAlignmentStyle,
  normalizeAlignment,
  normalizeImageWidth,
} from "./article-image-utils";

export const Citation = Node.create({
  name: "citation",
  group: "inline",
  inline: true,
  atom: true,
  addAttributes() {
    return { citationKey: { default: "citation" } };
  },
  parseHTML() {
    return [{ tag: "span[data-citation-key]" }];
  },
  renderHTML({ HTMLAttributes }) {
    const key = String(HTMLAttributes.citationKey ?? "citation");
    return [
      "span",
      mergeAttributes(HTMLAttributes, {
        "data-citation-key": key,
        class: "rounded bg-muted px-1 font-mono text-xs",
      }),
      `[@${key}]`,
    ];
  },
});

export const ZoteroCitation = Node.create({
  name: "zoteroCitation",
  group: "inline",
  inline: true,
  atom: true,
  addAttributes() {
    return {
      citationKey: { default: "citation" },
      itemKey: { default: "" },
      referenceId: { default: "" },
      title: { default: "" },
      version: { default: 0 },
    };
  },
  parseHTML() {
    return [{ tag: "span[data-zotero-citation]" }];
  },
  renderHTML({ HTMLAttributes }) {
    const key = String(HTMLAttributes.citationKey ?? "citation");
    return [
      "span",
      mergeAttributes(HTMLAttributes, {
        "data-zotero-citation": key,
        class: "rounded bg-muted px-1 font-mono text-xs",
        title: String(HTMLAttributes.title ?? ""),
      }),
      `[@${key}]`,
    ];
  },
  addNodeView() {
    return ReactNodeViewRenderer(ArticleZoteroCitationNodeView);
  },
});

export const MathInline = Node.create({
  name: "mathInline",
  group: "inline",
  inline: true,
  atom: true,
  addAttributes() {
    return { latex: { default: "x" } };
  },
  parseHTML() {
    return [{ tag: "span[data-math-inline]" }];
  },
  renderHTML({ HTMLAttributes }) {
    const latex = String(HTMLAttributes.latex ?? "x");
    return [
      "span",
      mergeAttributes(HTMLAttributes, {
        "data-math-inline": latex,
        class: "rounded border bg-background px-1 font-serif italic",
      }),
      `$${latex}$`,
    ];
  },
});

export const MathBlock = Node.create({
  name: "mathBlock",
  group: "block",
  atom: true,
  addAttributes() {
    return { latex: { default: "\\int_0^1 x^2\\,dx" } };
  },
  parseHTML() {
    return [{ tag: "div[data-math-block]" }];
  },
  renderHTML({ HTMLAttributes }) {
    const latex = String(HTMLAttributes.latex ?? "");
    return [
      "div",
      mergeAttributes(HTMLAttributes, {
        "data-math-block": latex,
        class: "my-4 rounded-lg border bg-muted/30 p-4 text-center font-serif",
      }),
      `$$${latex}$$`,
    ];
  },
});

function versionedReference(name: string, dataAttribute: string) {
  return Node.create({
    name,
    group: "block",
    atom: true,
    draggable: true,
    addOptions() {
      return { projectId: "" };
    },
    addAttributes() {
      return {
        referenceId: { default: "" },
        alt: { default: "" },
        align: { default: "center" },
        artifactId: { default: "" },
        experimentId: { default: "" },
        mimeType: { default: "" },
        objectId: { default: "" },
        caption: { default: "" },
        title: { default: "固定版本引用" },
        versionId: { default: "" },
        width: { default: 100 },
      };
    },
    parseHTML() {
      return [
        { tag: `aside[${dataAttribute}]` },
        { tag: `figure[${dataAttribute}]` },
      ];
    },
    renderHTML({ HTMLAttributes }) {
      const title = String(HTMLAttributes.title ?? "固定版本引用");
      const version = String(HTMLAttributes.versionId ?? "未指定版本");
      const caption = String(HTMLAttributes.caption ?? "").trim();
      const mimeType = String(HTMLAttributes.mimeType ?? "");
      const isImage = mimeType.startsWith("image/");
      return [
        "figure",
        {
          [dataAttribute]: String(HTMLAttributes.objectId ?? ""),
          ...(isImage ? { "data-article-artifact-image": "true" } : {}),
          class: "article-reference my-3",
        },
        [
          "aside",
          mergeAttributes(HTMLAttributes, {
            [dataAttribute]: String(HTMLAttributes.objectId ?? ""),
            class: "rounded-lg border border-dashed bg-muted/20 p-3 text-sm",
          }),
          isImage ? `${title} · 图片预览` : `${title} · ${version}`,
        ],
        ...(caption
          ? [
              [
                "figcaption",
                { class: "mt-2 text-center text-sm text-muted-foreground" },
                caption,
              ],
            ]
          : []),
      ];
    },
    addNodeView() {
      if (name !== "artifactReference") return null;
      const projectId = String(this.options.projectId ?? "");
      return ReactNodeViewRenderer(
        (props) =>
          createElement(ArticleArtifactNodeView, { ...props, projectId }),
        {
          stopEvent({ event }) {
            const target = event.target as HTMLElement | null;
            const isInput =
              target?.tagName === "INPUT" ||
              target?.tagName === "BUTTON" ||
              target?.tagName === "SELECT" ||
              target?.tagName === "TEXTAREA";
            if (isInput) return true;
            return event.type.startsWith("drag") || event.type === "drop";
          },
        },
      );
    },
  });
}

export const ArtifactReference = versionedReference(
  "artifactReference",
  "data-artifact-reference",
);
export const ExperimentResult = versionedReference(
  "experimentResult",
  "data-experiment-result",
);
export const ModelReference = versionedReference(
  "modelReference",
  "data-model-reference",
);

export const ArticleImage = Node.create({
  name: "articleImage",
  group: "block",
  atom: true,
  draggable: true,
  addAttributes() {
    return {
      alt: { default: "" },
      align: { default: "center" },
      caption: { default: "" },
      src: { default: "" },
      width: { default: 100 },
    };
  },
  parseHTML() {
    return [{ tag: "figure[data-article-image]" }];
  },
  renderHTML({ HTMLAttributes }) {
    const src = String(HTMLAttributes.src ?? "");
    const alt = String(HTMLAttributes.alt ?? "");
    const caption = String(HTMLAttributes.caption ?? "").trim();
    const align = normalizeAlignment(HTMLAttributes.align);
    const width = normalizeImageWidth(HTMLAttributes.width);
    const imageMargins = imageAlignmentStyle(align);
    return [
      "figure",
      {
        "data-article-image": "true",
        class: "article-figure my-4",
        style: `text-align: ${align}`,
      },
      [
        "img",
        {
          alt,
          class: "max-h-[36rem] max-w-full object-contain",
          src,
          style: `margin-left: ${imageMargins.marginLeft}; margin-right: ${imageMargins.marginRight}; width: ${width}%;`,
        },
      ],
      ...(caption
        ? [
            [
              "figcaption",
              { class: "mt-2 text-center text-sm text-muted-foreground" },
              caption,
            ],
          ]
        : []),
    ];
  },
  addNodeView() {
    return ReactNodeViewRenderer(ArticleImageNodeView, {
      stopEvent({ event }) {
        const target = event.target as HTMLElement | null;
        const isInput =
          target?.tagName === "INPUT" ||
          target?.tagName === "BUTTON" ||
          target?.tagName === "SELECT" ||
          target?.tagName === "TEXTAREA";
        if (isInput) return true;
        return event.type.startsWith("drag") || event.type === "drop";
      },
    });
  },
});

export const ArticleImageGroup = Node.create({
  name: "articleImageGroup",
  group: "block",
  content: "(articleImage | artifactReference){2,16}",
  defining: true,
  isolating: true,
  addAttributes() {
    return {
      caption: { default: "" },
      columns: { default: 2 },
    };
  },
  parseHTML() {
    return [{ tag: "figure[data-article-image-group]" }];
  },
  renderHTML({ HTMLAttributes }) {
    const caption = String(HTMLAttributes.caption ?? "").trim();
    const columns = normalizeArticleImageGroupColumns(HTMLAttributes.columns);
    return [
      "figure",
      mergeAttributes(HTMLAttributes, {
        "data-article-image-group": "true",
        class: "article-image-group my-4",
      }),
      [
        "div",
        {
          "data-article-image-group-content": "true",
          style: `--article-image-group-columns: ${articleImageGroupGridTemplateColumns(columns)}; --article-image-group-columns-count: ${columns}; --article-image-group-item-basis: calc((100% - (${columns} - 1) * 0.75rem) / ${columns} - 1px);`,
        },
        0,
      ],
      ...(caption
        ? [
            [
              "figcaption",
              { class: "mt-3 text-center text-sm font-medium" },
              caption,
            ],
          ]
        : []),
    ];
  },
  addNodeView() {
    return ReactNodeViewRenderer(ArticleImageGroupNodeView, {
      stopEvent({ event }) {
        const target = event.target as HTMLElement | null;
        const isInput =
          target?.tagName === "INPUT" ||
          target?.tagName === "BUTTON" ||
          target?.tagName === "SELECT" ||
          target?.tagName === "TEXTAREA";
        if (isInput) return true;
        return event.type.startsWith("drag") || event.type === "drop";
      },
    });
  },
});

class ArticleTableView extends TableView {
  private readonly caption: HTMLDivElement;

  constructor(
    node: ProseMirrorNode,
    cellMinWidth: number,
    view?: EditorView,
    HTMLAttributes: Record<string, unknown> = {},
  ) {
    super(node, cellMinWidth, view, HTMLAttributes);
    this.caption = document.createElement("div");
    this.caption.className = "article-table-caption mb-2 text-sm";
    this.caption.dataset.tableCaption = "true";
    this.caption.setAttribute("aria-live", "polite");
    this.dom.insertBefore(this.caption, this.table);
    this.syncCaption(node);
  }

  override update(node: ProseMirrorNode) {
    const updated = super.update(node);
    if (updated) this.syncCaption(node);
    return updated;
  }

  private syncCaption(node: ProseMirrorNode) {
    const caption = String(node.attrs.caption ?? "").trim();
    this.caption.textContent = caption;
    this.caption.hidden = !caption;
  }
}

export const ArticleTable = Table.extend({
  addAttributes() {
    return {
      ...this.parent?.(),
      caption: { default: "" },
    };
  },
  renderHTML({ HTMLAttributes, node }) {
    const caption = String(node.attrs.caption ?? "").trim();
    return [
      "table",
      mergeAttributes(HTMLAttributes, { class: "article-table" }),
      ...(caption
        ? [["caption", { class: "article-table-caption" }, caption]]
        : []),
      ["tbody", 0],
    ];
  },
  addNodeView() {
    if (this.options.resizable && this.editor.isEditable) return null;
    return (props) =>
      new ArticleTableView(
        props.node,
        this.options.cellMinWidth,
        props.view,
        mergeAttributes(this.options.HTMLAttributes, props.HTMLAttributes),
      );
  },
});

export const TableCaption = Node.create({
  name: "tableCaption",
  group: "block",
  atom: true,
  addAttributes() {
    return { caption: { default: "表注" } };
  },
  parseHTML() {
    return [{ tag: "div[data-table-caption]" }];
  },
  renderHTML({ HTMLAttributes }) {
    return [
      "div",
      mergeAttributes(HTMLAttributes, {
        "data-table-caption": "true",
        class: "article-table-caption my-2 text-sm",
      }),
      String(HTMLAttributes.caption ?? "表注"),
    ];
  },
});

export function createArticleNodes(projectId: string) {
  return [
    Citation,
    ZoteroCitation,
    MathInline,
    MathBlock,
    ArticleImage,
    ArticleImageGroup,
    ArticleTable.configure({ resizable: true, View: ArticleTableView }),
    TableCaption,
    ArtifactReference.configure({ projectId }),
    ExperimentResult,
    ModelReference,
  ];
}
