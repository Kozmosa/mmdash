import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { MarkdownPreview } from "@/components/ui/markdown-preview";

afterEach(cleanup);

describe("MarkdownPreview", () => {
  it("renders Markdown together with inline and display LaTeX", () => {
    const { container } = render(
      <MarkdownPreview
        source={[
          "# 分析结果",
          "",
          "这是 **重点**，内联公式为 $x^2 + y^2$。",
          "",
          "$$\\int_0^1 x\\,dx = \\frac{1}{2}$$",
          "",
          "代码中的公式标记保持原样：`$not_math$`。",
        ].join("\n")}
      />,
    );

    expect(
      screen.getByRole("heading", { name: "分析结果" }),
    ).toBeInTheDocument();
    expect(screen.getByText("重点")).toBeInTheDocument();
    expect(
      container.querySelector('[data-mmdash-equation="inline"] .katex'),
    ).toBeTruthy();
    expect(
      container.querySelector(
        '[data-mmdash-equation="display"] .katex-display',
      ),
    ).toBeTruthy();
    expect(screen.getByText("$not_math$")).toBeInTheDocument();
  });

  it("supports parenthesized and bracketed LaTeX delimiters", () => {
    const { container } = render(
      <MarkdownPreview source={"\\(a+b\\)\n\n\\[c=d\\]"} />,
    );

    expect(
      container.querySelectorAll('[data-mmdash-equation="inline"]'),
    ).toHaveLength(1);
    expect(
      container.querySelectorAll('[data-mmdash-equation="display"]'),
    ).toHaveLength(1);
  });

  it("renders GFM deletion, tables, fenced code, and nested lists", () => {
    const { container } = render(
      <MarkdownPreview
        source={[
          "**粗体**，*斜体*，~~删除线~~，`行内代码`",
          "",
          "- 无序项一",
          "- 无序项二",
          "  - 嵌套项",
          "",
          "1. 有序项一",
          "2. 有序项二",
          "",
          "| 列 A | 列 B |",
          "| --- | --- |",
          "| 1 | 2 |",
          "| 3 | 4 |",
          "",
          "```python",
          "def hello():",
          "    return 'world'",
          "```",
        ].join("\n")}
      />,
    );

    expect(container.querySelector("del")).toHaveTextContent("删除线");
    expect(container.querySelector("table")).toBeTruthy();
    expect(container.querySelectorAll("tbody tr")).toHaveLength(2);
    expect(container.querySelector("pre")).toHaveTextContent("def hello");
    expect(container.querySelector("pre code")).toHaveClass("language-python");
    expect(container.querySelector("pre code")).toHaveClass("hljs");
    expect(container.querySelector("pre code .hljs-keyword")).toHaveTextContent(
      "def",
    );
    expect(container.querySelectorAll("ul ul li")).toHaveLength(1);
    expect(container.querySelectorAll("ol > li")).toHaveLength(2);
  });
});
