// CUMCM (全国大学生数学建模竞赛) template-specific authoring support.
//
// The chapter skeleton mirrors the user's texfile/ scaffold (问题重述 … 附录)
// and the environments come from cumcmthesis.cls. Everything here is inert
// for projects without the CUMCM built-in template: the workbench only mounts
// the CUMCM panel when such a template is registered, and insertion happens
// through window events consumed by the article editor.

import type { SlashItem } from "./slash-command";

export const articleCumcmInsertEvent = "mmdash:article-cumcm-insert";

export type ArticleCumcmInsertDetail = {
  kind: "chapter" | "env" | "all";
  key: string;
};

export function dispatchArticleCumcmInsert(detail: ArticleCumcmInsertDetail) {
  window.dispatchEvent(new CustomEvent(articleCumcmInsertEvent, { detail }));
}

type TiptapNode = Record<string, unknown>;

const text = (value: string): TiptapNode => ({ type: "text", text: value });

const paragraph = (value = ""): TiptapNode => ({
  type: "paragraph",
  content: value ? [text(value)] : undefined,
});

const heading = (level: 1 | 2 | 3, value: string): TiptapNode => ({
  type: "heading",
  attrs: { level },
  content: [text(value)],
});

const bulletItem = (value: string): TiptapNode => ({
  type: "listItem",
  content: [paragraph(value)],
});

const bulletList = (values: string[]): TiptapNode => ({
  type: "bulletList",
  content: values.map(bulletItem),
});

// Raw TeX passthrough block. The Core Markdown projection emits the text
// content unescaped and the Worker converts the manuscript with Pandoc's
// raw_tex reader, so the environment reaches cumcmthesis.cls verbatim.
const latexBlock = (value: string): TiptapNode => ({
  type: "latexBlock",
  content: [text(value)],
});

export type CumcmEnvironment = {
  key: string;
  label: string;
  keywords: string;
  env: string;
  labelPrefix: string;
  example: string;
};

// The theorem-like environments declared by cumcmthesis.cls via \newtheorem.
export const CUMCM_ENVIRONMENTS: CumcmEnvironment[] = [
  {
    key: "assumption",
    label: "模型假设",
    keywords: "assumption 假设",
    env: "assumption",
    labelPrefix: "asu",
    example: "本文假设……",
  },
  {
    key: "problem",
    label: "问题",
    keywords: "problem 问题",
    env: "problem",
    labelPrefix: "pro",
    example: "问题重述……",
  },
  {
    key: "theorem",
    label: "定理",
    keywords: "theorem 定理",
    env: "theorem",
    labelPrefix: "thm",
    example: "……",
  },
  {
    key: "lemma",
    label: "引理",
    keywords: "lemma 引理",
    env: "lemma",
    labelPrefix: "lem",
    example: "……",
  },
  {
    key: "corollary",
    label: "推论",
    keywords: "corollary 推论",
    env: "corollary",
    labelPrefix: "cor",
    example: "……",
  },
  {
    key: "definition",
    label: "定义",
    keywords: "definition 定义",
    env: "definition",
    labelPrefix: "def",
    example: "……",
  },
  {
    key: "solution",
    label: "解",
    keywords: "solution 解",
    env: "solution",
    labelPrefix: "sol",
    example: "……",
  },
  {
    key: "proof",
    label: "证明",
    keywords: "proof 证明",
    env: "proof",
    labelPrefix: "prf",
    example: "……",
  },
  {
    key: "example",
    label: "例",
    keywords: "example 例子",
    env: "example",
    labelPrefix: "exa",
    example: "……",
  },
  {
    key: "conjecture",
    label: "猜想",
    keywords: "conjecture 猜想",
    env: "conjecture",
    labelPrefix: "con",
    example: "……",
  },
  {
    key: "axiom",
    label: "公理",
    keywords: "axiom 公理",
    env: "axiom",
    labelPrefix: "axi",
    example: "……",
  },
  {
    key: "principle",
    label: "定律",
    keywords: "principle 定律",
    env: "principle",
    labelPrefix: "pri",
    example: "……",
  },
];

export function cumcmEnvironmentNode(key: string): TiptapNode | undefined {
  const item = CUMCM_ENVIRONMENTS.find((candidate) => candidate.key === key);
  if (!item) return undefined;
  return latexBlock(
    [
      `\\begin{${item.env}}`,
      item.example,
      `\\label{${item.labelPrefix}:1}`,
      `\\end{${item.env}}`,
      "",
    ].join("\n"),
  );
}

export function cumcmEnvironmentSlashItems() {
  return CUMCM_ENVIRONMENTS.map((item) => ({
    feature: "cumcm" as const,
    label: `CUMCM ${item.label}`,
    keywords: `${item.keywords} ${item.env} latex`,
    detail: item.key,
  }));
}

// Ready-to-register "/" entries. Available project-wide for CUMCM projects;
// the workbench registers them whenever the CUMCM template exists, so the
// menu works without opening the 国赛 sidebar tab first.
export function cumcmSlashFeatureItems(): SlashItem[] {
  return cumcmEnvironmentSlashItems().map((item) => ({
    label: item.label,
    keywords: item.keywords,
    action: (editor) => {
      const node = cumcmEnvironmentNode(item.detail);
      if (!node) return;
      editor
        .chain()
        .focus()
        .insertContent([node, { type: "paragraph" }])
        .run();
    },
  }));
}

export type CumcmChapter = {
  key: string;
  label: string;
  headings: string[];
  nodes: TiptapNode[];
};

const problemEnvironments = (count: number): TiptapNode[] => {
  const nodes: TiptapNode[] = [];
  for (let index = 1; index <= count; index += 1) {
    nodes.push(
      latexBlock(
        [
          "\\begin{problem}",
          `问题${index}的数学表达……`,
          `\\label{pro:${index}}`,
          "\\end{problem}",
          "",
        ].join("\n"),
      ),
      paragraph(),
    );
  }
  return nodes;
};

export const CUMCM_CHAPTERS: CumcmChapter[] = [
  {
    key: "restatement",
    label: "问题重述",
    headings: ["问题重述"],
    nodes: [
      heading(1, "问题重述"),
      heading(2, "问题背景"),
      paragraph(),
      heading(2, "问题条件"),
      bulletList(["已知信息", "已知信息", "已知信息"]),
      heading(2, "问题提出"),
      ...problemEnvironments(4),
    ],
  },
  {
    key: "analysis",
    label: "问题分析",
    headings: ["问题分析"],
    nodes: [
      heading(1, "问题分析"),
      heading(2, "问题一分析"),
      paragraph(),
      heading(2, "问题二分析"),
      paragraph(),
      heading(2, "问题三分析"),
      paragraph(),
      heading(2, "问题四分析"),
      paragraph(),
    ],
  },
  {
    key: "assumptions",
    label: "模型假设与符号说明",
    headings: ["模型假设", "符号说明"],
    nodes: [
      heading(1, "模型假设"),
      latexBlock(
        [
          "\\begin{assumption}",
          "本文假设……",
          "\\label{asu:1}",
          "\\end{assumption}",
          "",
        ].join("\n"),
      ),
      paragraph(),
      heading(1, "符号说明"),
      latexBlock(
        [
          "\\begin{table}[H]",
          "    \\centering",
          "    \\begin{tabular}{llc}",
          "    \\toprule",
          "    \\textbf{符号} & \\textbf{意义} & \\textbf{单位} \\\\",
          "    \\midrule",
          "    $t$ & 当前时间 & s \\\\",
          "    \\bottomrule",
          "    \\end{tabular}",
          "\\end{table}",
          "",
        ].join("\n"),
      ),
    ],
  },
  {
    key: "modeling",
    label: "模型建立与求解",
    headings: ["模型建立与求解"],
    nodes: [
      heading(1, "模型建立与求解"),
      heading(2, "问题一模型建立"),
      heading(3, "建模思路"),
      paragraph(),
      heading(3, "模型建立"),
      paragraph(),
      heading(2, "问题一模型求解"),
      paragraph(),
      heading(2, "问题二模型建立"),
      paragraph(),
      heading(2, "问题二模型求解"),
      paragraph(),
      heading(2, "问题三模型建立"),
      paragraph(),
      heading(2, "问题三模型求解"),
      paragraph(),
      heading(2, "问题四模型建立"),
      paragraph(),
      heading(2, "问题四模型求解"),
      paragraph(),
      heading(2, "模型汇总"),
      paragraph(),
    ],
  },
  {
    key: "evaluation",
    label: "模型评价与推广",
    headings: ["结果分析与检验", "模型的评价与推广"],
    nodes: [
      heading(1, "结果分析与检验"),
      heading(2, "问题一结果分析"),
      paragraph(),
      heading(2, "问题一结果检验"),
      paragraph(),
      heading(2, "问题二结果分析"),
      paragraph(),
      heading(2, "问题二结果检验"),
      paragraph(),
      heading(2, "问题三结果分析"),
      paragraph(),
      heading(2, "问题三结果检验"),
      paragraph(),
      heading(2, "问题四结果分析"),
      paragraph(),
      heading(2, "问题四结果检验"),
      paragraph(),
      heading(1, "模型的评价与推广"),
      heading(2, "模型优点"),
      bulletList(["……"]),
      heading(2, "模型局限"),
      bulletList(["……"]),
      heading(2, "改进方向"),
      paragraph(),
      heading(2, "模型推广"),
      paragraph(),
    ],
  },
  {
    key: "ai_statement",
    label: "AI 使用说明",
    headings: ["AI 使用说明"],
    nodes: [
      heading(1, "AI 使用说明"),
      paragraph("本论文使用 AI 工具的具体情况如下……"),
    ],
  },
  {
    key: "appendix",
    label: "附录",
    headings: ["附录"],
    nodes: [
      heading(1, "附录"),
      paragraph("求解代码通过“/代码块”插入;支撑材料清单如下……"),
    ],
  },
];

// One full skeleton in texfile order. 参考文献 is intentionally absent:
// the reference list is generated from Zotero citations at build time.
export function cumcmSkeletonNodes(): TiptapNode[] {
  return CUMCM_CHAPTERS.flatMap((chapter) => chapter.nodes);
}

export function cumcmChapterNodes(key: string): TiptapNode[] {
  return CUMCM_CHAPTERS.find((chapter) => chapter.key === key)?.nodes ?? [];
}
