import type { Editor } from "@tiptap/core";
import { Fragment, type Node as ProseMirrorNode } from "@tiptap/pm/model";
import { NodeSelection } from "@tiptap/pm/state";

// Keep a practical upper bound for collaborative documents while allowing a
// four-column gallery to wrap over several rows.
export const articleImageGroupLimit = 16;

export type ArticleImageGroupAction =
  "mergeBefore" | "mergeAfter" | "removeFromGroup" | "ungroup";

export type ArticleImageGroupContext = {
  canMergeAfter: boolean;
  canMergeBefore: boolean;
  inGroup: boolean;
};

type GroupLocation = {
  childIndex: number;
  group: ProseMirrorNode;
  groupPos: number;
};

export function isArticleImageNode(node?: ProseMirrorNode | null): boolean {
  return (
    node?.type.name === "articleImage" ||
    (node?.type.name === "artifactReference" &&
      String(node.attrs.mimeType ?? "").startsWith("image/"))
  );
}

function groupAtChildPosition(
  editor: Editor,
  position: number,
): GroupLocation | undefined {
  let location: GroupLocation | undefined;
  editor.state.doc.forEach((node, groupPos) => {
    if (
      location ||
      node.type.name !== "articleImageGroup" ||
      position <= groupPos ||
      position >= groupPos + node.nodeSize
    ) {
      return;
    }
    let childPos = groupPos + 1;
    node.forEach((child, _offset, childIndex) => {
      if (childPos === position) {
        location = { childIndex, group: node, groupPos };
      }
      childPos += child.nodeSize;
    });
  });
  return location;
}

function topLevelNeighbors(editor: Editor, position: number) {
  let index = -1;
  editor.state.doc.forEach((node, nodeOffset, nodeIndex) => {
    if (nodeOffset === position) index = nodeIndex;
  });
  if (index < 0) return;
  const node = editor.state.doc.child(index);
  const before = index > 0 ? editor.state.doc.child(index - 1) : undefined;
  const after =
    index < editor.state.doc.childCount - 1
      ? editor.state.doc.child(index + 1)
      : undefined;
  return {
    after,
    afterPos: position + node.nodeSize,
    before,
    beforePos: before ? position - before.nodeSize : undefined,
    node,
  };
}

function canJoin(node?: ProseMirrorNode): boolean {
  return (
    isArticleImageNode(node) ||
    (node?.type.name === "articleImageGroup" &&
      node.childCount < articleImageGroupLimit)
  );
}

export function articleImageGroupContext(
  editor: Editor,
  position: number,
): ArticleImageGroupContext {
  if (groupAtChildPosition(editor, position)) {
    return { canMergeAfter: false, canMergeBefore: false, inGroup: true };
  }
  const neighbors = topLevelNeighbors(editor, position);
  if (!neighbors || !isArticleImageNode(neighbors.node)) {
    return { canMergeAfter: false, canMergeBefore: false, inGroup: false };
  }
  return {
    canMergeAfter: canJoin(neighbors.after),
    canMergeBefore: canJoin(neighbors.before),
    inGroup: false,
  };
}

function childrenOf(node: ProseMirrorNode): ProseMirrorNode[] {
  const children: ProseMirrorNode[] = [];
  node.forEach((child) => children.push(child));
  return children;
}

export function mergeArticleImageWithNeighbor(
  editor: Editor,
  position: number,
  direction: "before" | "after",
): boolean {
  const neighbors = topLevelNeighbors(editor, position);
  if (!neighbors || !isArticleImageNode(neighbors.node)) return false;
  const neighbor = direction === "before" ? neighbors.before : neighbors.after;
  const neighborPos =
    direction === "before" ? neighbors.beforePos : neighbors.afterPos;
  if (!neighbor || neighborPos === undefined || !canJoin(neighbor))
    return false;

  const groupType = editor.schema.nodes.articleImageGroup;
  if (!groupType) return false;
  const neighborChildren =
    neighbor.type.name === "articleImageGroup"
      ? childrenOf(neighbor)
      : [neighbor];
  const children =
    direction === "before"
      ? [...neighborChildren, neighbors.node]
      : [neighbors.node, ...neighborChildren];
  if (children.length > articleImageGroupLimit) return false;

  const start = Math.min(position, neighborPos);
  const end = Math.max(
    position + neighbors.node.nodeSize,
    neighborPos + neighbor.nodeSize,
  );
  const attrs =
    neighbor.type.name === "articleImageGroup"
      ? { ...neighbor.attrs }
      : { caption: "" };
  const group = groupType.create(attrs, Fragment.fromArray(children));
  const transaction = editor.state.tr.replaceWith(start, end, group);
  transaction.setSelection(NodeSelection.create(transaction.doc, start));
  editor.view.dispatch(transaction);
  editor.view.focus();
  return true;
}

export function ungroupArticleImages(
  editor: Editor,
  groupPos: number,
): boolean {
  const group = editor.state.doc.nodeAt(groupPos);
  if (group?.type.name !== "articleImageGroup") return false;
  editor.view.dispatch(
    editor.state.tr.replaceWith(
      groupPos,
      groupPos + group.nodeSize,
      group.content,
    ),
  );
  editor.view.focus();
  return true;
}

export function removeArticleImageFromGroup(
  editor: Editor,
  childPos: number,
): boolean {
  const location = groupAtChildPosition(editor, childPos);
  if (!location) return false;
  const children = childrenOf(location.group);
  const [removed] = children.splice(location.childIndex, 1);
  if (!removed) return false;
  if (children.length < 2)
    return ungroupArticleImages(editor, location.groupPos);

  const group = location.group.type.create(
    { ...location.group.attrs },
    Fragment.fromArray(children),
  );
  editor.view.dispatch(
    editor.state.tr.replaceWith(
      location.groupPos,
      location.groupPos + location.group.nodeSize,
      Fragment.fromArray([group, removed]),
    ),
  );
  editor.view.focus();
  return true;
}

export function deleteArticleImageNode(
  editor: Editor,
  position: number,
): boolean {
  const location = groupAtChildPosition(editor, position);
  if (!location) {
    const node = editor.state.doc.nodeAt(position);
    if (!node || !isArticleImageNode(node)) return false;
    editor.view.dispatch(
      editor.state.tr.delete(position, position + node.nodeSize),
    );
    return true;
  }

  const children = childrenOf(location.group);
  children.splice(location.childIndex, 1);
  const replacement =
    children.length === 1
      ? children[0]!
      : location.group.type.create(
          { ...location.group.attrs },
          Fragment.fromArray(children),
        );
  editor.view.dispatch(
    editor.state.tr.replaceWith(
      location.groupPos,
      location.groupPos + location.group.nodeSize,
      replacement,
    ),
  );
  return true;
}
