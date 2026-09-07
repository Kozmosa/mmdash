import { describe, expect, it } from "vitest";

import {
  CUMCM_CHAPTERS,
  CUMCM_ENVIRONMENTS,
  cumcmChapterNodes,
  cumcmEnvironmentNode,
  cumcmEnvironmentSlashItems,
  cumcmSkeletonNodes,
} from "./article-cumcm";
import {
  clearSlashFeatureItems,
  nextSlashSelection,
  setSlashFeatureItems,
} from "./slash-command";

type TiptapNode = Record<string, unknown>;

const asNode = (value: unknown): TiptapNode => value as TiptapNode;

describe("article-cumcm preset", () => {
  it("builds a skeleton in texfile order with every H1 chapter", () => {
    const skeleton = cumcmSkeletonNodes();
    const headings = skeleton
      .filter(
        (node) =>
          node.type === "heading" &&
          (node.attrs as { level: number }).level === 1,
      )
      .map((node) => (node.content as TiptapNode[])[0].text);
    expect(headings).toEqual([
      "问题重述",
      "问题分析",
      "模型假设",
      "符号说明",
      "模型建立与求解",
      "结果分析与检验",
      "模型的评价与推广",
      "AI 使用说明",
      "附录",
    ]);
    expect(CUMCM_CHAPTERS.map((chapter) => chapter.key)).toEqual([
      "restatement",
      "analysis",
      "assumptions",
      "modeling",
      "evaluation",
      "ai_statement",
      "appendix",
    ]);
    // 参考文献 is generated from Zotero citations at build time.
    expect(headings).not.toContain("参考文献");
  });

  it("emits raw latexBlock nodes for cls environments", () => {
    const node = asNode(cumcmEnvironmentNode("assumption"));
    expect(node).toBeDefined();
    const text = (node.content as TiptapNode[])[0].text as string;
    expect(text).toContain("\\begin{assumption}");
    expect(text).toContain("\\label{asu:1}");
    expect(text).toContain("\\end{assumption}");
    expect(CUMCM_ENVIRONMENTS.map((item) => item.env)).toContain("problem");
  });

  it("returns no nodes for unknown keys", () => {
    expect(cumcmChapterNodes("missing")).toEqual([]);
    expect(cumcmEnvironmentNode("missing")).toBeUndefined();
  });

  it("keeps chapter insertion isolated per chapter key", () => {
    const restatement = cumcmChapterNodes("restatement");
    expect(restatement[0]).toMatchObject({
      type: "heading",
      attrs: { level: 1 },
    });
    expect(JSON.stringify(restatement)).toContain("\\begin{problem}");
    expect(JSON.stringify(restatement)).toContain("\\label{pro:1}");
  });

  it("registers and clears feature slash items", () => {
    const items = cumcmEnvironmentSlashItems();
    expect(items.length).toBe(CUMCM_ENVIRONMENTS.length);
    setSlashFeatureItems("cumcm", [
      { label: items[0].label, keywords: "", action: () => {} },
    ]);
    setSlashFeatureItems("other", [
      { label: "其他", keywords: "", action: () => {} },
    ]);
    clearSlashFeatureItems("cumcm");
    clearSlashFeatureItems("other");
  });

  it("wraps slash selection around the bounds", () => {
    expect(nextSlashSelection(0, "up", 3)).toBe(2);
    expect(nextSlashSelection(2, "down", 3)).toBe(0);
    expect(nextSlashSelection(0, "down", 0)).toBe(0);
  });
});
