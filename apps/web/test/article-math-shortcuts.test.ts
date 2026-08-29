import { Editor } from "@tiptap/core";
import { Mathematics } from "@tiptap/extension-mathematics";
import StarterKit from "@tiptap/starter-kit";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ArticleMathShortcuts } from "@/features/article/article-math-shortcuts";
import { parseTextWithMath, transformMathInHtml } from "@/features/article/article-editor";

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

  it("transforms block math and inline math strings into blockMath and inlineMath HTML elements", () => {
    const blockInput = "<p>$$E = mc^2$$</p>";
    const blockOutput = transformMathInHtml(blockInput);
    expect(blockOutput).toContain('data-type="blockMath"');
    expect(blockOutput).toContain('data-latex="E = mc^2"');

    const inlineInput = "<p>质能方程 $E = mc^2$ 说明质量与能量等价</p>";
    const inlineOutput = transformMathInHtml(inlineInput);
    expect(inlineOutput).toContain('data-type="inlineMath"');
    expect(inlineOutput).toContain('data-latex="E = mc^2"');
  });

  it("parses pasted text wrapped in $$$$ into blockMath and $$ or $ into inlineMath nodes", () => {
    const editor = createEditor();
    const schema = editor.state.schema;

    // Single block math with $$$$
    const blockNodes4 = parseTextWithMath("$$$$E = mc^2$$$$", schema);
    expect(blockNodes4).toHaveLength(1);
    expect(blockNodes4[0].type.name).toBe("blockMath");
    expect(blockNodes4[0].attrs.latex).toBe("E = mc^2");

    // Single block math with $$
    const blockNodes2 = parseTextWithMath("$$E = mc^2$$", schema);
    expect(blockNodes2).toHaveLength(1);
    expect(blockNodes2[0].type.name).toBe("blockMath");
    expect(blockNodes2[0].attrs.latex).toBe("E = mc^2");

    // Single inline math with $
    const inlineNodes1 = parseTextWithMath("$x^2 + y^2 = r^2$", schema);
    expect(inlineNodes1).toHaveLength(1);
    expect(inlineNodes1[0].type.name).toBe("inlineMath");
    expect(inlineNodes1[0].attrs.latex).toBe("x^2 + y^2 = r^2");

    // Mixed text with block math and inline math
    const mixed = `公式说明：
$$$$\\int_0^1 x^2\\,dx$$$$
这里 $$x$$ 为自变量，$y$ 为因变量。`;

    const mixedNodes = parseTextWithMath(mixed, schema);
    expect(mixedNodes).toHaveLength(3);
    expect(mixedNodes[0].type.name).toBe("paragraph");
    expect(mixedNodes[1].type.name).toBe("blockMath");
    expect(mixedNodes[1].attrs.latex).toBe("\\int_0^1 x^2\\,dx");
    expect(mixedNodes[2].type.name).toBe("paragraph");
  });
});
