// @vitest-environment jsdom

import { Editor } from "@tiptap/core";
import { TableKit } from "@tiptap/extension-table";
import StarterKit from "@tiptap/starter-kit";
import type { Node as ProseMirrorNode } from "@tiptap/pm/model";
import type { NodeViewProps } from "@tiptap/react";
import { createEvent, fireEvent, render, screen } from "@testing-library/react";
import { createElement } from "react";
import { describe, expect, it, vi } from "vitest";

import {
  articleArtifactMime,
  articleInsertArtifactIntoGroupEvent,
} from "@/features/article/article-editor";
import { ArticleImageNodeView } from "@/features/article/article-image-node-view";
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

function makeEditor() {
  return new Editor({
    content: {
      content: [
        {
          attrs: { caption: "组合图片", columns: 2 },
          content: [image(1), image(2), image(3)],
          type: "articleImageGroup",
        },
      ],
      type: "doc",
    },
    extensions: [
      StarterKit,
      ...createArticleNodes("project-1"),
      TableKit.configure({ table: false }),
    ],
  });
}

function makeNodeViewProps(
  editor: Editor,
  node: ProseMirrorNode,
  getPos: () => number,
): NodeViewProps {
  return {
    decorations: [],
    deleteNode: () => {},
    editor,
    extension: editor.extensionManager.extensions[0]!,
    getPos,
    node,
    selected: false,
    updateAttributes: () => {},
  } as unknown as NodeViewProps;
}

describe("ArticleImageNodeView drag-and-drop", () => {
  it("renders drag handle badge and initiates drag with payload", () => {
    const editor = makeEditor();
    const groupNode = editor.state.doc.child(0);
    const firstChild = groupNode.child(0);

    render(
      createElement(
        ArticleImageNodeView,
        makeNodeViewProps(editor, firstChild, () => 1),
      ),
    );

    const dragHandle = screen.getByLabelText("拖拽调整顺序");
    expect(dragHandle).toBeInTheDocument();
    expect(dragHandle.getAttribute("data-drag-handle")).toBe("true");
    expect(dragHandle.getAttribute("draggable")).toBe("true");
    expect(dragHandle.textContent).toContain("1/3");

    const values = new Map<string, string>();
    const dataTransfer = {
      dropEffect: "none",
      effectAllowed: "none",
      getData: (type: string) => values.get(type) ?? "",
      setData: (type: string, val: string) => {
        values.set(type, val);
        dataTransfer.types = [...values.keys()];
      },
      types: [] as string[],
    };

    fireEvent.dragStart(dragHandle, { dataTransfer });
    expect(dataTransfer.effectAllowed).toBe("move");
    expect(
      dataTransfer.getData("application/vnd.mmdash.image-group-item"),
    ).toBe(JSON.stringify({ fromIndex: 0, groupPos: 0 }));
    expect(dataTransfer.getData("text/plain")).toBe(
      "mmdash-image-group-item:0:0",
    );

    editor.destroy();
  });

  it("handles dragover and drop on a target image to reorder the group", () => {
    const editor = makeEditor();
    const groupNode = editor.state.doc.child(0);
    const secondChild = groupNode.child(1);
    const secondChildPos = 1 + groupNode.child(0).nodeSize;

    const { container } = render(
      createElement(
        ArticleImageNodeView,
        makeNodeViewProps(editor, secondChild, () => secondChildPos),
      ),
    );

    const figure = container.querySelector("figure");
    expect(figure).toBeInTheDocument();

    const values = new Map<string, string>([
      [
        "application/vnd.mmdash.image-group-item",
        JSON.stringify({ fromIndex: 0, groupPos: 0 }),
      ],
      ["text/plain", "mmdash-image-group-item:0:0"],
    ]);
    const dataTransfer = {
      dropEffect: "move",
      effectAllowed: "move",
      getData: (type: string) => values.get(type) ?? "",
      setData: (type: string, val: string) => values.set(type, val),
      types: [...values.keys()],
    };

    figure!.getBoundingClientRect = () => ({
      bottom: 200,
      height: 200,
      left: 100,
      right: 300,
      top: 0,
      width: 200,
      x: 100,
      y: 0,
      toJSON: () => {},
    });

    const dragOverEvent = createEvent.dragOver(figure!, { clientX: 250 });
    Object.defineProperty(dragOverEvent, "dataTransfer", {
      value: dataTransfer,
    });
    Object.defineProperty(dragOverEvent, "clientX", { value: 250 });
    fireEvent(figure!, dragOverEvent);

    const dropEvent = createEvent.drop(figure!, { clientX: 250 });
    Object.defineProperty(dropEvent, "dataTransfer", {
      value: dataTransfer,
    });
    Object.defineProperty(dropEvent, "clientX", { value: 250 });
    fireEvent(figure!, dropEvent);

    const updatedGroup = editor.state.doc.child(0);
    expect(updatedGroup.child(0).attrs.caption).toBe("子题注 2");
    expect(updatedGroup.child(1).attrs.caption).toBe("子题注 1");
    expect(updatedGroup.child(2).attrs.caption).toBe("子题注 3");

    editor.destroy();
  });

  it("handles dragging right-to-left to move an image earlier", () => {
    const editor = makeEditor();
    const groupNode = editor.state.doc.child(0);
    const firstChild = groupNode.child(0);

    const { container } = render(
      createElement(
        ArticleImageNodeView,
        makeNodeViewProps(editor, firstChild, () => 1),
      ),
    );

    const figure = container.querySelector("figure");
    expect(figure).toBeInTheDocument();

    const values = new Map<string, string>([
      [
        "application/vnd.mmdash.image-group-item",
        JSON.stringify({ fromIndex: 2, groupPos: 0 }),
      ],
      ["text/plain", "mmdash-image-group-item:0:2"],
    ]);
    const dataTransfer = {
      dropEffect: "move",
      effectAllowed: "move",
      getData: (type: string) => values.get(type) ?? "",
      setData: (type: string, val: string) => values.set(type, val),
      types: [...values.keys()],
    };

    figure!.getBoundingClientRect = () => ({
      bottom: 200,
      height: 200,
      left: 100,
      right: 300,
      top: 0,
      width: 200,
      x: 100,
      y: 0,
      toJSON: () => {},
    });

    const dropEvent = createEvent.drop(figure!, { clientX: 120 });
    Object.defineProperty(dropEvent, "dataTransfer", {
      value: dataTransfer,
    });
    Object.defineProperty(dropEvent, "clientX", { value: 120 });
    fireEvent(figure!, dropEvent);

    const updatedGroup = editor.state.doc.child(0);
    expect(updatedGroup.child(0).attrs.caption).toBe("子题注 3");
    expect(updatedGroup.child(1).attrs.caption).toBe("子题注 1");
    expect(updatedGroup.child(2).attrs.caption).toBe("子题注 2");

    editor.destroy();
  });

  it("handles dropping an artifact onto an image group item and emits articleInsertArtifactIntoGroupEvent", () => {
    const editor = makeEditor();
    const groupNode = editor.state.doc.child(0);
    const secondChild = groupNode.child(1);
    const secondChildPos = 1 + groupNode.child(0).nodeSize;

    const { container } = render(
      createElement(
        ArticleImageNodeView,
        makeNodeViewProps(editor, secondChild, () => secondChildPos),
      ),
    );

    const figure = container.querySelector("figure");
    expect(figure).toBeInTheDocument();

    figure!.getBoundingClientRect = () => ({
      bottom: 200,
      height: 200,
      left: 100,
      right: 300,
      top: 0,
      width: 200,
      x: 100,
      y: 0,
      toJSON: () => {},
    });

    const payload = {
      artifactId: "art-1",
      filename: "chart.png",
      mimeType: "image/png",
      title: "图表",
      versionId: "ver-1",
    };

    const values = new Map<string, string>([
      [articleArtifactMime, JSON.stringify(payload)],
    ]);
    const dataTransfer = {
      dropEffect: "copy",
      effectAllowed: "copy",
      getData: (type: string) => values.get(type) ?? "",
      setData: (type: string, val: string) => values.set(type, val),
      types: [...values.keys()],
    };

    const handler = vi.fn();
    window.addEventListener(articleInsertArtifactIntoGroupEvent, handler);

    const dragOverEvent = createEvent.dragOver(figure!, { clientX: 250 });
    Object.defineProperty(dragOverEvent, "dataTransfer", {
      value: dataTransfer,
    });
    Object.defineProperty(dragOverEvent, "clientX", { value: 250 });
    fireEvent(figure!, dragOverEvent);

    const dropEvent = createEvent.drop(figure!, { clientX: 250 });
    Object.defineProperty(dropEvent, "dataTransfer", {
      value: dataTransfer,
    });
    Object.defineProperty(dropEvent, "clientX", { value: 250 });
    fireEvent(figure!, dropEvent);

    expect(handler).toHaveBeenCalledTimes(1);
    const eventDetail = (handler.mock.calls[0]![0] as CustomEvent).detail;
    expect(eventDetail.groupPos).toBe(0);
    expect(eventDetail.insertIndex).toBe(2);
    expect(eventDetail.payload).toEqual(payload);

    window.removeEventListener(articleInsertArtifactIntoGroupEvent, handler);
    editor.destroy();
  });
});
