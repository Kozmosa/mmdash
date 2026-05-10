"use client";

import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import rehypeKatex from "rehype-katex";
import "katex/dist/katex.min.css";

interface MarkdownRendererProps {
  markdown: string;
}

export default function MarkdownRenderer({ markdown }: MarkdownRendererProps) {
  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm, remarkMath]}
      rehypePlugins={[rehypeKatex]}
      components={{
        h1: ({ children, ...props }) => (
          <h1 className="mb-5 mt-2 text-3xl font-bold leading-tight" {...props}>
            {children}
          </h1>
        ),
        h2: ({ children, ...props }) => (
          <h2 className="mb-4 mt-7 text-2xl font-semibold leading-tight" {...props}>
            {children}
          </h2>
        ),
        h3: ({ children, ...props }) => (
          <h3 className="mb-3 mt-6 text-xl font-semibold leading-tight" {...props}>
            {children}
          </h3>
        ),
        h4: ({ children, ...props }) => (
          <h4 className="mb-2 mt-5 text-lg font-semibold leading-tight" {...props}>
            {children}
          </h4>
        ),
        p: ({ children, ...props }) => (
          <p className="my-3 leading-7" {...props}>
            {children}
          </p>
        ),
        ul: ({ children, ...props }) => (
          <ul className="my-3 list-disc space-y-1 pl-6" {...props}>
            {children}
          </ul>
        ),
        ol: ({ children, ...props }) => (
          <ol className="my-3 list-decimal space-y-1 pl-6" {...props}>
            {children}
          </ol>
        ),
        blockquote: ({ children, ...props }) => (
          <blockquote className="my-4 border-l-4 border-border pl-4 text-muted-foreground" {...props}>
            {children}
          </blockquote>
        ),
        hr: (props) => <hr className="my-6 border-border" {...props} />,
        table: ({ children, ...props }) => (
          <div className="my-4 overflow-x-auto">
            <table className="w-full border-collapse text-sm" {...props}>
              {children}
            </table>
          </div>
        ),
        th: ({ children, ...props }) => (
          <th className="border bg-muted/60 px-3 py-2 text-left font-semibold" {...props}>
            {children}
          </th>
        ),
        td: ({ children, ...props }) => (
          <td className="border px-3 py-2 align-top" {...props}>
            {children}
          </td>
        ),
        code: ({ children, className, ...props }) => {
          const isBlock = className?.startsWith("language-");
          if (isBlock) {
            return (
              <code className={className} {...props}>
                {children}
              </code>
            );
          }
          return (
            <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-sm" {...props}>
              {children}
            </code>
          );
        },
        pre: ({ children, ...props }) => (
          <pre className="my-4 overflow-x-auto rounded-lg bg-muted p-4 font-mono text-sm leading-6" {...props}>
            {children}
          </pre>
        ),
        a: ({ children, ...props }) => (
          <a className="font-medium underline underline-offset-4" {...props}>
            {children}
          </a>
        ),
      }}
    >
      {markdown}
    </ReactMarkdown>
  );
}
