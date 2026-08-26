// @vitest-environment jsdom

import { Editor } from "@tiptap/core";
import { TableKit } from "@tiptap/extension-table";
import { DOMSerializer } from "@tiptap/pm/model";
import StarterKit from "@tiptap/starter-kit";
import { describe, expect, it } from "vitest";

import {
  articleImageGroupContext,
  articleImageGroupGridTemplateColumns,
  deleteArticleImageNode,
  groupAtChildPosition,
  insertArticleImageIntoGroup,
  mergeArticleImageWithNeighbor,
  moveArticleImageInGroupDirection,
  normalizeArticleImageGroupColumns,
  removeArticleImageFromGroup,
  reorderArticleImageInGroup,
  ungroupArticleImages,
} from "@/features/article/article-image-group";
import { createArticleNodes } from "@/features/article/article-nodes";

function image(index: number) {
  return {
    attrs: {
      alt: `图片 ${index}`,
      caption: `子题注 ${index}`,
      src: `https://example.test/${index}.png`,
    },
    type: "articleImage",
  };
}

function makeEditor(count: number) {
  return new Editor({
    content: {
      content: Array.from({ length: count }, (_, index) => image(index + 1)),
      type: "doc",
    },
    extensions: [
      StarterKit,
      ...createArticleNodes("project-1"),
      TableKit.configure({ table: false }),
    ],
  });
}

describe("article image groups", () => {
  it("normalizes the row setting and preserves it in serialized layout", () => {
    expect(normalizeArticleImageGroupColumns("3.4")).toBe(3);
    expect(normalizeArticleImageGroupColumns(9)).toBe(4);
    expect(articleImageGroupGridTemplateColumns(3)).toBe(
      "repeat(3, minmax(0, 1fr))",
    );

    const editor = makeEditor(3);
    expect(mergeArticleImageWithNeighbor(editor, 0, "after")).toBe(true);
    expect(
      mergeArticleImageWithNeighbor(
        editor,
        editor.state.doc.child(0).nodeSize,
        "before",
      ),
    ).toBe(true);
    const groupPosition = 0;
    const group = editor.state.doc.child(0);
    editor.view.dispatch(
      editor.state.tr.setNodeMarkup(groupPosition, undefined, {
        ...group.attrs,
        columns: 3,
      }),
    );

    const dom = DOMSerializer.fromSchema(editor.schema).serializeNode(
      editor.state.doc.child(0),
    );
    const content =
      dom instanceof HTMLElement
        ? dom.querySelector("[data-article-image-group-content]")
        : null;
    expect(content?.getAttribute("style")).toContain(
      "--article-image-group-columns: repeat(3, minmax(0, 1fr));",
    );
    expect(content?.getAttribute("style")).toContain(
      "--article-image-group-columns-count: 3;",
    );
    expect(content?.getAttribute("style")).toContain(
      "--article-image-group-item-basis: calc((100% - (3 - 1) * 0.75rem) / 3 - 1px);",
    );
    editor.destroy();
  });

  it("merges adjacent images into a multi-row group without losing captions", () => {
    const editor = makeEditor(5);

    expect(mergeArticleImageWithNeighbor(editor, 0, "after")).toBe(true);
    for (let count = 2; count < 5; count += 1) {
      const nextImagePosition = editor.state.doc.child(0).nodeSize;
      expect(
        mergeArticleImageWithNeighbor(editor, nextImagePosition, "before"),
      ).toBe(true);
    }

    const group = editor.state.doc.child(0);
    expect(editor.state.doc.childCount).toBe(2);
    expect(group.type.name).toBe("articleImageGroup");
    expect(group.childCount).toBe(5);
    expect(group.attrs.columns).toBe(2);
    expect(
      Array.from(
        { length: group.childCount },
        (_, index) => group.child(index).attrs.caption,
      ),
    ).toEqual(["子题注 1", "子题注 2", "子题注 3", "子题注 4", "子题注 5"]);
    expect(articleImageGroupContext(editor, 1).inGroup).toBe(true);
    expect(articleImageGroupContext(editor, 1).canMoveEarlier).toBe(false);
    expect(articleImageGroupContext(editor, 1).canMoveLater).toBe(true);
    expect(editor.state.doc.child(1).type.name).toBe("paragraph");
    editor.destroy();
  });

  it("reorders images within a group directly or via direction helpers", () => {
    const editor = makeEditor(3);
    expect(mergeArticleImageWithNeighbor(editor, 0, "after")).toBe(true);
    const thirdPos = editor.state.doc.child(0).nodeSize;
    expect(mergeArticleImageWithNeighbor(editor, thirdPos, "before")).toBe(true);

    // Initial order: [1, 2, 3]
    let group = editor.state.doc.child(0);
    expect(group.child(0).attrs.caption).toBe("子题注 1");
    expect(group.child(1).attrs.caption).toBe("子题注 2");
    expect(group.child(2).attrs.caption).toBe("子题注 3");

    // Move first item (index 0) to end (index 2) -> [2, 3, 1]
    expect(reorderArticleImageInGroup(editor, 0, 0, 2)).toBe(true);
    group = editor.state.doc.child(0);
    expect(group.child(0).attrs.caption).toBe("子题注 2");
    expect(group.child(1).attrs.caption).toBe("子题注 3");
    expect(group.child(2).attrs.caption).toBe("子题注 1");

    // Now move the middle item (index 1: "子题注 3") earlier -> [3, 2, 1]
    // The position of the second child in the group is 1 + child(0).nodeSize = 1 + 1 = 2 (or child pos inside group)
    const secondChildPos = 1 + group.child(0).nodeSize;
    expect(moveArticleImageInGroupDirection(editor, secondChildPos, "earlier")).toBe(true);
    group = editor.state.doc.child(0);
    expect(group.child(0).attrs.caption).toBe("子题注 3");
    expect(group.child(1).attrs.caption).toBe("子题注 2");
    expect(group.child(2).attrs.caption).toBe("子题注 1");

    const lastChildPos = 1 + group.child(0).nodeSize + group.child(1).nodeSize;

    // Test groupAtChildPosition across positions and offsets
    expect(groupAtChildPosition(editor, 1)).toEqual({
      childIndex: 0,
      group,
      groupPos: 0,
    });
    expect(groupAtChildPosition(editor, secondChildPos)).toEqual({
      childIndex: 1,
      group,
      groupPos: 0,
    });
    expect(groupAtChildPosition(editor, lastChildPos)).toEqual({
      childIndex: 2,
      group,
      groupPos: 0,
    });

    // Schema draggable verification
    expect(editor.schema.nodes.articleImage.spec.draggable).toBe(true);
    expect(editor.schema.nodes.artifactReference.spec.draggable).toBe(true);

    // Context check for first item: cannot move earlier, can move later
    const firstContext = articleImageGroupContext(editor, 1);
    expect(firstContext.canMoveEarlier).toBe(false);
    expect(firstContext.canMoveLater).toBe(true);

    // Context check for last item: can move earlier, cannot move later
    const lastContext = articleImageGroupContext(editor, lastChildPos);
    expect(lastContext.canMoveEarlier).toBe(true);
    expect(lastContext.canMoveLater).toBe(false);

    editor.destroy();
  });

  it("removes, deletes, and ungroups child images without corrupting the document", () => {
    const editor = makeEditor(3);
    expect(mergeArticleImageWithNeighbor(editor, 0, "after")).toBe(true);
    const thirdPosition = editor.state.doc.child(0).nodeSize;
    expect(mergeArticleImageWithNeighbor(editor, thirdPosition, "before")).toBe(
      true,
    );

    expect(removeArticleImageFromGroup(editor, 2)).toBe(true);
    expect(editor.state.doc.child(0).type.name).toBe("articleImageGroup");
    expect(editor.state.doc.child(0).childCount).toBe(2);
    expect(editor.state.doc.child(1).attrs.caption).toBe("子题注 2");

    expect(deleteArticleImageNode(editor, 1)).toBe(true);
    expect(editor.state.doc.child(0).type.name).toBe("articleImage");
    expect(editor.state.doc.child(0).attrs.caption).toBe("子题注 3");

    editor.commands.setContent({
      content: [
        {
          attrs: { caption: "大题注", columns: 1 },
          content: [image(1), image(2)],
          type: "articleImageGroup",
        },
      ],
      type: "doc",
    });
    expect(ungroupArticleImages(editor, 0)).toBe(true);
    expect(editor.state.doc.childCount).toBe(3);
    expect(editor.state.doc.child(0).attrs.caption).toBe("子题注 1");
    expect(editor.state.doc.child(1).attrs.caption).toBe("子题注 2");
    expect(editor.state.doc.child(2).type.name).toBe("paragraph");
    editor.destroy();
  });

  it("inserts an image into an existing group at the specified index", () => {
    const editor = makeEditor(2);
    expect(mergeArticleImageWithNeighbor(editor, 0, "after")).toBe(true);
    expect(editor.state.doc.child(0).childCount).toBe(2);

    const newImageNode = editor.schema.nodes.articleImage.create({
      alt: "新图片",
      caption: "插入的子题注",
      src: "https://example.test/new.png",
    });

    expect(insertArticleImageIntoGroup(editor, 0, 1, newImageNode)).toBe(true);
    const group = editor.state.doc.child(0);
    expect(group.childCount).toBe(3);
    expect(group.child(0).attrs.caption).toBe("子题注 1");
    expect(group.child(1).attrs.caption).toBe("插入的子题注");
    expect(group.child(2).attrs.caption).toBe("子题注 2");
    editor.destroy();
  });
});
