import { Editor } from "@tiptap/core";
import { Mathematics } from "@tiptap/extension-mathematics";
import StarterKit from "@tiptap/starter-kit";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ArticleMathShortcuts } from "@/features/article/article-math-shortcuts";

const editors: Editor[] = [];

afterEach(() => {
  editors.splice(0).forEach((editor) => editor.destroy());
});

function createEditor(onCreate = vi.fn()) {
  const editor = new Editor({
    content: "<p></p>",
    extensions: [
      StarterKit,
      Mathematics.configure({ katexOptions: { throwOnError: false } }),
      ArticleMathShortcuts.configure({ delay: 0, onCreate }),
    ],
  });
  editors.push(editor);
  return editor;
}

function typeCharacter(editor: Editor, character: string) {
  const { from, to } = editor.state.selection;
  let handled = false;
  editor.view.someProp("handleTextInput", (handler) => {
    if (handler(editor.view, from, to, character, () => editor.state.tr)) {
      handled = true;
      return true;
    }
    return false;
  });
  if (!handled) editor.view.dispatch(editor.state.tr.insertText(character));
}

describe("ArticleMathShortcuts", () => {
  it("turns two consecutive dollars into inline math", async () => {
    const onCreate = vi.fn();
    const editor = createEditor(onCreate);
    editor.commands.insertContent("text ");
    typeCharacter(editor, "$");
    typeCharacter(editor, "$");

    await new Promise((resolve) => setTimeout(resolve, 10));
    expect(editor.getJSON().content?.[0]?.content?.at(-1)).toMatchObject({
      attrs: { latex: "x" },
      type: "inlineMath",
    });
    expect(onCreate).toHaveBeenCalledWith("inline", 6);
  });

  it("turns four consecutive dollars in an empty block into centered block math", async () => {
    const onCreate = vi.fn();
    const editor = createEditor(onCreate);
    typeCharacter(editor, "$");
    typeCharacter(editor, "$");
    typeCharacter(editor, "$");
    typeCharacter(editor, "$");

    await new Promise((resolve) => setTimeout(resolve, 10));
    expect(editor.getJSON().content?.[0]).toMatchObject({
      attrs: { latex: "x" },
      type: "blockMath",
    });
    expect(onCreate).toHaveBeenCalledWith("block", 0);
  });
});
