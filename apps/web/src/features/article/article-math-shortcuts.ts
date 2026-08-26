import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";

export type ArticleMathKind = "block" | "inline";

export type ArticleMathShortcutsOptions = {
  delay: number;
  onCreate?: (kind: ArticleMathKind, pos: number) => void;
};

export const ArticleMathShortcuts =
  Extension.create<ArticleMathShortcutsOptions>({
    name: "articleMathShortcuts",

    addOptions() {
      return { delay: 240, onCreate: undefined };
    },

    addProseMirrorPlugins() {
      let timer: ReturnType<typeof setTimeout> | undefined;
      const clearTimer = () => {
        if (timer !== undefined) clearTimeout(timer);
        timer = undefined;
      };
      const convertRun = (expectedEnd: number) => {
        timer = undefined;
        const { doc, tr } = this.editor.state;
        if (expectedEnd > doc.content.size) return;
        let count = 0;
        for (let pos = expectedEnd; pos > 0 && count < 4; pos -= 1) {
          if (doc.textBetween(pos - 1, pos, "", "") !== "$") break;
          count += 1;
        }
        if (count !== 2 && count !== 4) return;
        const start = expectedEnd - count;
        const $start = doc.resolve(start);
        const $end = doc.resolve(expectedEnd);
        if (!$start.sameParent($end) || !$start.parent.isTextblock) return;
        const kind: ArticleMathKind = count === 4 ? "block" : "inline";
        const nodeType =
          this.editor.schema.nodes[
            kind === "block" ? "blockMath" : "inlineMath"
          ];
        if (!nodeType) return;
        let insertedAt = start;
        if (kind === "block") {
          const consumesTextBlock =
            start === $start.start() &&
            expectedEnd === $end.end() &&
            $start.parent.textContent === "$$$$";
          if (!consumesTextBlock) return;
          insertedAt = $start.before();
          tr.replaceWith(
            $start.before(),
            $end.after(),
            nodeType.create({ latex: "x" }),
          );
        } else {
          tr.replaceWith(start, expectedEnd, nodeType.create({ latex: "x" }));
        }
        this.editor.view.dispatch(tr.scrollIntoView());
        this.options.onCreate?.(kind, insertedAt);
      };

      return [
        new Plugin({
          key: new PluginKey("articleMathShortcuts"),
          props: {
            handleTextInput: (_view, from, _to, text) => {
              if (text !== "$") return false;
              clearTimer();
              const expectedEnd = from + 1;
              timer = setTimeout(
                () => convertRun(expectedEnd),
                this.options.delay,
              );
              return false;
            },
          },
          view: () => ({ destroy: clearTimer }),
        }),
      ];
    },
  });
