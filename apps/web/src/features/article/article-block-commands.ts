import type { Editor } from "@tiptap/core";
import {
  isNodeRangeSelection,
  NodeRangeSelection,
} from "@tiptap/extension-node-range";
import { Fragment } from "@tiptap/pm/model";
import { NodeSelection, TextSelection } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";

export type ArticleBlockConversion =
  | "paragraph"
  | "heading1"
  | "heading2"
  | "heading3"
  | "blockquote"
  | "codeBlock";

export type ArticleBlockMoveDirection = "up" | "down";
export type ArticleBlockInsertDirection = "before" | "after";

export type ArticleArtifactImageReplacement = {
  artifactId: string;
  mimeType: string;
  referenceId: string;
  title: string;
  versionId: string;
};

function isTopLevelBlock(editor: Editor, position: number) {
  return (
    Boolean(editor.state.doc.nodeAt(position)) &&
    editor.state.doc.resolve(position).depth === 0
  );
}

function placeTextCursor(editor: Editor, position: number) {
  const safePosition = Math.min(
    Math.max(0, position),
    editor.state.doc.content.size,
  );
  editor.view.dispatch(
    editor.state.tr
      .setSelection(
        TextSelection.near(editor.state.doc.resolve(safePosition), 1),
      )
      .scrollIntoView(),
  );
  editor.view.focus();
}

export function selectArticleBlock(
  editor: Editor,
  position: number,
  options: { scrollIntoView?: boolean } = {},
) {
  if (!isTopLevelBlock(editor, position)) return false;
  const transaction = editor.state.tr.setSelection(
    NodeSelection.create(editor.state.doc, position),
  );
  if (options.scrollIntoView !== false) transaction.scrollIntoView();
  editor.view.dispatch(transaction);
  editor.view.focus();
  return true;
}

export function replaceArticleImageWithArtifact(
  editor: Editor,
  position: number,
  input: ArticleArtifactImageReplacement,
) {
  const node = editor.state.doc.nodeAt(position);
  if (
    !node ||
    (node.type.name !== "articleImage" &&
      node.type.name !== "artifactReference")
  ) {
    return false;
  }
  const artifactReference = editor.schema.nodes.artifactReference;
  if (!artifactReference) return false;
  const replacement = artifactReference.create({
    ...node.attrs,
    alt: String(node.attrs.alt ?? "").trim() || input.title,
    artifactId: input.artifactId,
    mimeType: input.mimeType,
    objectId: input.artifactId,
    referenceId: input.referenceId,
    title: input.title,
    versionId: input.versionId,
  });
  editor.view.dispatch(
    editor.state.tr.replaceWith(
      position,
      position + node.nodeSize,
      replacement,
    ),
  );
  return true;
}

export function deleteArticleBlock(editor: Editor, position: number) {
  const node = editor.state.doc.nodeAt(position);
  if (!node || !isTopLevelBlock(editor, position)) return false;
  const transaction = editor.state.tr.delete(
    position,
    position + node.nodeSize,
  );
  if (transaction.doc.childCount === 0) {
    transaction.insert(0, editor.schema.nodes.paragraph.create());
  }
  editor.view.dispatch(transaction);
  placeTextCursor(editor, Math.min(position, editor.state.doc.content.size));
  return true;
}

export function deleteSelectedArticleBlock(view: EditorView) {
  const { selection } = view.state;
  if (
    !(selection instanceof NodeSelection) &&
    !isNodeRangeSelection(selection)
  ) {
    return false;
  }
  const from = selection.from;
  const to =
    selection instanceof NodeSelection
      ? from + (view.state.doc.nodeAt(from)?.nodeSize ?? 0)
      : selection.to;
  if (
    to <= from ||
    view.state.doc.resolve(from).depth !== 0 ||
    view.state.doc.resolve(to).depth !== 0
  ) {
    return false;
  }
  const transaction = view.state.tr.delete(from, to);
  if (transaction.doc.childCount === 0) {
    transaction.insert(0, view.state.schema.nodes.paragraph.create());
  }
  const cursor = Math.min(from, transaction.doc.content.size);
  transaction.setSelection(
    TextSelection.near(transaction.doc.resolve(cursor), 1),
  );
  view.dispatch(transaction.scrollIntoView());
  view.focus();
  return true;
}

export function duplicateArticleBlock(editor: Editor, position: number) {
  const node = editor.state.doc.nodeAt(position);
  if (!node || !isTopLevelBlock(editor, position)) return false;
  const copy = node.type.create(
    { ...node.attrs, id: undefined },
    node.content,
    node.marks,
  );
  const copyPosition = position + node.nodeSize;
  const transaction = editor.state.tr.insert(copyPosition, copy);
  transaction
    .setSelection(NodeSelection.create(transaction.doc, copyPosition))
    .scrollIntoView();
  editor.view.dispatch(transaction);
  editor.view.focus();
  return true;
}

export function insertArticleBlock(
  editor: Editor,
  position: number,
  direction: ArticleBlockInsertDirection,
) {
  const node = editor.state.doc.nodeAt(position);
  if (!node || !isTopLevelBlock(editor, position)) return false;
  const paragraph = editor.schema.nodes.paragraph.create();
  const insertionPosition =
    direction === "before" ? position : position + node.nodeSize;
  const transaction = editor.state.tr.insert(insertionPosition, paragraph);
  transaction
    .setSelection(
      TextSelection.near(transaction.doc.resolve(insertionPosition + 1), 1),
    )
    .scrollIntoView();
  editor.view.dispatch(transaction);
  editor.view.focus();
  return true;
}

export function moveArticleBlock(
  editor: Editor,
  position: number,
  direction: ArticleBlockMoveDirection,
) {
  const node = editor.state.doc.nodeAt(position);
  if (!node || !isTopLevelBlock(editor, position)) return false;
  const index = editor.state.doc.resolve(position).index(0);
  if (direction === "up" && index === 0) return false;
  if (direction === "down" && index >= editor.state.doc.childCount - 1)
    return false;

  if (direction === "up") {
    const previous = editor.state.doc.child(index - 1);
    const previousPosition = position - previous.nodeSize;
    const transaction = editor.state.tr.replaceWith(
      previousPosition,
      position + node.nodeSize,
      [node, previous],
    );
    transaction
      .setSelection(NodeSelection.create(transaction.doc, previousPosition))
      .scrollIntoView();
    editor.view.dispatch(transaction);
  } else {
    const next = editor.state.doc.child(index + 1);
    const nextPosition = position + node.nodeSize;
    const transaction = editor.state.tr.replaceWith(
      position,
      nextPosition + next.nodeSize,
      [next, node],
    );
    transaction
      .setSelection(
        NodeSelection.create(transaction.doc, position + next.nodeSize),
      )
      .scrollIntoView();
    editor.view.dispatch(transaction);
  }
  editor.view.focus();
  return true;
}

/**
 * Move a contiguous range of top-level blocks [rangeFrom, rangeTo) to a
 * target insertion position. Used by multi-block drag-and-drop.
 *
 * `targetPos` is the document offset where the blocks should be inserted
 * (before that position). Returns false if the move is a no-op or invalid.
 */
export function moveArticleBlockRange(
  editor: Editor,
  rangeFrom: number,
  rangeTo: number,
  targetPos: number,
) {
  const { doc } = editor.state;
  if (rangeFrom >= rangeTo) return false;
  if (rangeFrom < 0 || rangeTo > doc.content.size) return false;
  // No-op: target is inside the range being dragged
  if (targetPos >= rangeFrom && targetPos <= rangeTo) return false;

  // Collect the nodes in the range
  const nodes: import("@tiptap/pm/model").Node[] = [];
  doc.forEach((node, offset) => {
    if (offset >= rangeFrom && offset < rangeTo) {
      nodes.push(node);
    }
  });
  if (nodes.length === 0) return false;

  const fragment = Fragment.from(nodes);
  const rangeSize = rangeTo - rangeFrom;

  let tr = editor.state.tr;

  if (targetPos < rangeFrom) {
    // Moving up: insert first, then delete old range (which shifted right)
    tr = tr.insert(targetPos, fragment);
    // After insert, the old range shifted by the inserted size
    tr = tr.delete(rangeFrom + rangeSize, rangeTo + rangeSize);
    // Select the moved blocks at target
    tr = tr.setSelection(
      NodeRangeSelection.create(
        tr.doc,
        targetPos,
        targetPos + rangeSize,
        0,
      ),
    );
  } else {
    // Moving down: delete first, then insert at adjusted position
    tr = tr.delete(rangeFrom, rangeTo);
    // After delete, targetPos shifted left by the deleted size
    const adjustedTarget = targetPos - rangeSize;
    tr = tr.insert(adjustedTarget, fragment);
    tr = tr.setSelection(
      NodeRangeSelection.create(
        tr.doc,
        adjustedTarget,
        adjustedTarget + rangeSize,
        0,
      ),
    );
  }

  tr.scrollIntoView();
  editor.view.dispatch(tr);
  editor.view.focus();
  return true;
}

function conversionTarget(editor: Editor, conversion: ArticleBlockConversion) {
  if (
    conversion === "heading1" ||
    conversion === "heading2" ||
    conversion === "heading3"
  ) {
    return {
      node: editor.schema.nodes.heading,
      attrs: { level: Number(conversion.slice(-1)) },
    };
  }
  return { node: editor.schema.nodes[conversion], attrs: {} };
}

export function convertArticleBlock(
  editor: Editor,
  position: number,
  conversion: ArticleBlockConversion,
) {
  const node = editor.state.doc.nodeAt(position);
  if (!node || !isTopLevelBlock(editor, position)) return false;
  const target = conversionTarget(editor, conversion);
  if (!target.node) return false;
  const id = node.attrs.id;

  if (conversion === "blockquote") {
    const child = node.type.create(
      { ...node.attrs, id: undefined },
      node.content,
      node.marks,
    );
    const wrapper = target.node.create({ id }, child);
    const transaction = editor.state.tr.replaceWith(
      position,
      position + node.nodeSize,
      wrapper,
    );
    transaction
      .setSelection(NodeSelection.create(transaction.doc, position))
      .scrollIntoView();
    editor.view.dispatch(transaction);
    editor.view.focus();
    return true;
  }

  if (node.type.name === "blockquote") {
    if (node.childCount !== 1) return false;
    const child = node.firstChild;
    if (!child) return false;
    if (!target.node.validContent(child.content)) return false;
    const unwrapped = target.node.create(
      {
        id,
        ...(conversion === "codeBlock"
          ? { language: child.attrs.language ?? null }
          : {}),
        ...target.attrs,
      },
      child.content,
      child.marks,
    );
    const transaction = editor.state.tr.replaceWith(
      position,
      position + node.nodeSize,
      unwrapped,
    );
    transaction
      .setSelection(NodeSelection.create(transaction.doc, position))
      .scrollIntoView();
    editor.view.dispatch(transaction);
    editor.view.focus();
    return true;
  }

  const attrs = {
    id,
    ...(conversion === "codeBlock"
      ? { language: node.attrs.language ?? null }
      : {}),
    ...target.attrs,
  };
  if (!target.node.validContent(node.content)) return false;
  const transaction = editor.state.tr.setNodeMarkup(
    position,
    target.node,
    attrs,
  );
  transaction
    .setSelection(NodeSelection.create(transaction.doc, position))
    .scrollIntoView();
  editor.view.dispatch(transaction);
  editor.view.focus();
  return true;
}
