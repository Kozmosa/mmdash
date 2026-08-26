import {
  Schema,
  type Attrs,
  type MarkSpec,
  type NodeSpec,
} from "@tiptap/pm/model";

const attributes = (...names: string[]): Record<string, { default: unknown }> =>
  Object.fromEntries(names.map((name) => [name, { default: null }]));

const blockAttrs = attributes("id", "tag", "provenance");
const referenceAttrs = {
  ...blockAttrs,
  ...attributes(
    "referenceId",
    "alt",
    "align",
    "artifactId",
    "experimentId",
    "mimeType",
    "objectId",
    "caption",
    "title",
    "versionId",
    "width",
  ),
};

const nodes: Record<string, NodeSpec> = {
  doc: { content: "block*" },
  text: { group: "inline" },
  paragraph: { attrs: blockAttrs, content: "inline*", group: "block" },
  heading: {
    attrs: { ...blockAttrs, level: { default: 1 } },
    content: "inline*",
    defining: true,
    group: "block",
  },
  blockquote: {
    attrs: blockAttrs,
    content: "block+",
    defining: true,
    group: "block",
  },
  codeBlock: {
    attrs: { ...blockAttrs, language: { default: null } },
    code: true,
    content: "text*",
    defining: true,
    group: "block",
    marks: "",
  },
  bulletList: { attrs: blockAttrs, content: "listItem+", group: "block" },
  orderedList: {
    attrs: { ...blockAttrs, start: { default: 1 }, type: { default: null } },
    content: "listItem+",
    group: "block",
  },
  listItem: { attrs: blockAttrs, content: "paragraph block*", defining: true },
  horizontalRule: { attrs: blockAttrs, group: "block" },
  hardBreak: { group: "inline", inline: true, selectable: false },
  image: {
    attrs: attributes("id", "src", "alt", "title", "width", "height"),
    atom: true,
    group: "block",
  },
  articleImage: {
    attrs: {
      ...blockAttrs,
      ...attributes("src", "alt", "caption", "align", "width", "height"),
    },
    atom: true,
    group: "block",
  },
  articleImageGroup: {
    attrs: {
      ...blockAttrs,
      caption: { default: "" },
      columns: { default: 2 },
    },
    content: "(articleImage | artifactReference){2,16}",
    defining: true,
    group: "block",
    isolating: true,
  },
  mathBlock: {
    attrs: { ...blockAttrs, latex: { default: "" } },
    atom: true,
    group: "block",
  },
  blockMath: {
    attrs: { ...blockAttrs, latex: { default: "" } },
    atom: true,
    group: "block",
  },
  mathInline: {
    attrs: { latex: { default: "" } },
    atom: true,
    group: "inline",
    inline: true,
  },
  inlineMath: {
    attrs: { latex: { default: "" } },
    atom: true,
    group: "inline",
    inline: true,
  },
  citation: {
    attrs: attributes("citationKey"),
    atom: true,
    group: "inline",
    inline: true,
  },
  zoteroCitation: {
    attrs: attributes(
      "citationKey",
      "itemKey",
      "referenceId",
      "title",
      "version",
    ),
    atom: true,
    group: "inline",
    inline: true,
  },
  artifactReference: { attrs: referenceAttrs, atom: true, group: "block" },
  experimentResult: { attrs: referenceAttrs, atom: true, group: "block" },
  modelReference: { attrs: referenceAttrs, atom: true, group: "block" },
  tableCaption: {
    attrs: { ...blockAttrs, caption: { default: "" } },
    atom: true,
    group: "block",
  },
  table: {
    attrs: { ...blockAttrs, caption: { default: "" } },
    content: "tableRow+",
    group: "block",
    isolating: true,
    tableRole: "table",
  },
  tableRow: { content: "(tableCell | tableHeader)+", tableRole: "row" },
  tableCell: {
    attrs: {
      colspan: { default: 1 },
      rowspan: { default: 1 },
      colwidth: { default: null },
      backgroundColor: { default: null },
    },
    content: "block+",
    isolating: true,
    tableRole: "cell",
  },
  tableHeader: {
    attrs: {
      colspan: { default: 1 },
      rowspan: { default: 1 },
      colwidth: { default: null },
      backgroundColor: { default: null },
    },
    content: "block+",
    isolating: true,
    tableRole: "header_cell",
  },
};

const marks: Record<string, MarkSpec> = {
  bold: {},
  italic: {},
  strike: {},
  code: { code: true, excludes: "_" },
  link: {
    attrs: attributes("href", "target", "rel", "class") as Attrs,
    inclusive: false,
  },
};

export const articleDocumentSchema = new Schema({ marks, nodes });
