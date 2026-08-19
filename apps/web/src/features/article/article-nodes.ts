import { mergeAttributes, Node } from "@tiptap/core";

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
    addAttributes() {
      return {
        referenceId: { default: "" },
        artifactId: { default: "" },
        experimentId: { default: "" },
        objectId: { default: "" },
        title: { default: "固定版本引用" },
        versionId: { default: "" },
      };
    },
    parseHTML() {
      return [{ tag: `aside[${dataAttribute}]` }];
    },
    renderHTML({ HTMLAttributes }) {
      const title = String(HTMLAttributes.title ?? "固定版本引用");
      const version = String(HTMLAttributes.versionId ?? "未指定版本");
      return [
        "aside",
        mergeAttributes(HTMLAttributes, {
          [dataAttribute]: String(HTMLAttributes.objectId ?? ""),
          class: "my-3 rounded-lg border border-dashed bg-muted/20 p-3 text-sm",
        }),
        `${title} · ${version}`,
      ];
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

export const articleNodes = [
  Citation,
  MathInline,
  MathBlock,
  ArtifactReference,
  ExperimentResult,
];
