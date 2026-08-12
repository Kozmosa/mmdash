import katex from "katex";
import hljs from "highlight.js/lib/common";
import type { ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

import { cn } from "@/lib/cn";

export function MarkdownPreview({
  className,
  source,
}: Readonly<{ className?: string; source: string }>) {
  return (
    <article
      className={cn(
        "prose prose-neutral max-w-none rounded-xl border border-border bg-card p-6 text-sm leading-7",
        className,
      )}
    >
      <ReactMarkdown
        components={{
          code: ({ children, className, node, ...properties }) => {
            void node;
            const language = languageFromClassName(className);
            const source = String(children).replace(/\n$/, "");
            if (!language || !hljs.getLanguage(language)) {
              return (
                <code className={className} {...properties}>
                  {children}
                </code>
              );
            }
            const highlighted = hljs.highlight(source, { language }).value;
            return (
              <code
                className={cn("hljs", className)}
                dangerouslySetInnerHTML={{ __html: highlighted }}
                {...properties}
              />
            );
          },
          div: ({ children, node, ...properties }) => {
            void node;
            return (properties as Record<string, unknown>)[
              "data-mmdash-math"
            ] === "display" ? (
              <MathExpression display>{children}</MathExpression>
            ) : (
              <div {...properties}>{children}</div>
            );
          },
          span: ({ children, node, ...properties }) => {
            void node;
            return (properties as Record<string, unknown>)[
              "data-mmdash-math"
            ] === "inline" ? (
              <MathExpression>{children}</MathExpression>
            ) : (
              <span {...properties}>{children}</span>
            );
          },
        }}
        remarkPlugins={[remarkGfm, remarkMathLite]}
      >
        {normalizeMathDelimiters(source)}
      </ReactMarkdown>
    </article>
  );
}

function languageFromClassName(className?: string): string | null {
  const match = className?.match(/(?:^|\s)language-([^\s]+)/);
  if (!match?.[1]) return null;
  const aliases: Record<string, string> = {
    c: "c",
    "c++": "cpp",
    cs: "csharp",
    csharp: "csharp",
    js: "javascript",
    jsx: "javascript",
    md: "markdown",
    py: "python",
    rb: "ruby",
    sh: "bash",
    shell: "bash",
    ts: "typescript",
    tsx: "typescript",
    yml: "yaml",
  };
  return aliases[match[1].toLowerCase()] ?? match[1].toLowerCase();
}

function MathExpression({
  children,
  display = false,
}: Readonly<{ children: ReactNode; display?: boolean }>) {
  const expression = textContent(children).trim();
  if (!expression) {
    return null;
  }
  const html = katex.renderToString(expression, {
    displayMode: display,
    strict: false,
    throwOnError: false,
    trust: false,
  });
  const Element = display ? "div" : "span";
  return (
    <Element
      className={display ? "my-3 overflow-x-auto py-1" : "inline-math"}
      data-mmdash-equation={display ? "display" : "inline"}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}

function textContent(value: ReactNode): string {
  if (typeof value === "string" || typeof value === "number") {
    return String(value);
  }
  if (Array.isArray(value)) {
    return value.map(textContent).join("");
  }
  return "";
}

type MarkdownNode = {
  children?: MarkdownNode[];
  data?: {
    hName?: string;
    hProperties?: Record<string, string>;
  };
  type: string;
  value?: string;
};

// A deliberately small Markdown extension for the math syntax used in Agent
// answers. It leaves fenced/inline code untouched and produces ordinary HAST
// elements that ReactMarkdown can render without enabling raw HTML.
function remarkMathLite() {
  return (tree: MarkdownNode) => transformMath(tree);
}

function transformMath(node: MarkdownNode): void {
  if (!node.children || node.type === "code" || node.type === "inlineCode") {
    return;
  }
  if (
    node.type === "paragraph" &&
    node.children.length === 1 &&
    node.children[0]?.type === "text"
  ) {
    const display = displayMath(node.children[0].value ?? "");
    if (display !== null) {
      node.data = {
        hName: "div",
        hProperties: { "data-mmdash-math": "display" },
      };
      node.children = [{ type: "text", value: display }];
      return;
    }
  }
  const next: MarkdownNode[] = [];
  for (const child of node.children) {
    if (child.type === "text") {
      next.push(...inlineMath(child.value ?? ""));
    } else {
      transformMath(child);
      next.push(child);
    }
  }
  node.children = next;
}

function normalizeMathDelimiters(source: string): string {
  let fence: { character: string; length: number } | null = null;
  return source
    .split("\n")
    .map((line) => {
      const marker = line.match(/^\s*(`{3,}|~{3,})/);
      if (marker?.[1]) {
        const character = marker[1][0]!;
        if (!fence) {
          fence = { character, length: marker[1].length };
        } else if (
          fence.character === character &&
          marker[1].length >= fence.length
        ) {
          fence = null;
        }
        return line;
      }
      if (fence) return line;

      let output = "";
      let inlineTicks = 0;
      for (let index = 0; index < line.length; index += 1) {
        if (line[index] === "`") {
          let length = 1;
          while (line[index + length] === "`") length += 1;
          output += "`".repeat(length);
          inlineTicks = inlineTicks === length ? 0 : inlineTicks || length;
          index += length - 1;
          continue;
        }
        const delimiter = line.slice(index, index + 2);
        if (!inlineTicks && ["\\(", "\\)", "\\[", "\\]"].includes(delimiter)) {
          output += delimiter === "\\(" || delimiter === "\\)" ? "$" : "$$";
          index += 1;
          continue;
        }
        output += line[index];
      }
      return output;
    })
    .join("\n");
}

function displayMath(value: string): string | null {
  const dollars = value.match(/^\s*\$\$([\s\S]+)\$\$\s*$/);
  if (dollars?.[1]) return dollars[1];
  const brackets = value.match(/^\s*\\\[([\s\S]+)\\\]\s*$/);
  return brackets?.[1] ?? null;
}

function inlineMath(value: string): MarkdownNode[] {
  const output: MarkdownNode[] = [];
  const pattern = /\\\((.+?)\\\)|(^|[^\\$])\$([^$\n]+?)\$/g;
  let offset = 0;
  for (const match of value.matchAll(pattern)) {
    const index = match.index ?? 0;
    const prefix = match[2] ?? "";
    const start = index + prefix.length;
    if (start > offset)
      output.push({ type: "text", value: value.slice(offset, start) });
    const expression = match[1] ?? match[3] ?? "";
    output.push(mathNode(expression, "span", "inline"));
    offset = index + match[0].length;
  }
  if (offset < value.length)
    output.push({ type: "text", value: value.slice(offset) });
  return output.length ? output : [{ type: "text", value }];
}

function mathNode(
  expression: string,
  hName: "div" | "span",
  display: "display" | "inline",
): MarkdownNode {
  return {
    data: {
      hName,
      hProperties: { "data-mmdash-math": display },
    },
    type: "text",
    value: expression,
  };
}
