import ReactMarkdown from "react-markdown";

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
      <ReactMarkdown>{source}</ReactMarkdown>
    </article>
  );
}
