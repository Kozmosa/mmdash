import { cn } from "@/lib/cn";

export type DiffLine = {
  content: string;
  newLine?: number;
  oldLine?: number;
  type: "added" | "context" | "removed";
};

const diffStyles: Record<DiffLine["type"], string> = {
  added: "bg-emerald-500/10 text-emerald-950 dark:text-emerald-100",
  context: "bg-card",
  removed: "bg-red-500/10 text-red-950 dark:text-red-100",
};

export function DiffViewer({
  label = "差异内容",
  lines,
}: Readonly<{ label?: string; lines: readonly DiffLine[] }>) {
  return (
    <div
      aria-label={label}
      className="overflow-auto rounded-xl border border-border bg-card font-mono text-xs"
      role="region"
    >
      {lines.length > 0 ? (
        lines.map((line, index) => (
          <div
            className={cn(
              "grid min-w-max grid-cols-[3rem_3rem_1.5rem_1fr] border-b border-border/50 last:border-0",
              diffStyles[line.type],
            )}
            key={`${line.oldLine ?? "x"}-${line.newLine ?? "x"}-${index}`}
          >
            <span className="select-none border-r border-border/50 px-2 py-1 text-right text-muted-foreground">
              {line.oldLine}
            </span>
            <span className="select-none border-r border-border/50 px-2 py-1 text-right text-muted-foreground">
              {line.newLine}
            </span>
            <span className="select-none px-2 py-1">
              {line.type === "added"
                ? "+"
                : line.type === "removed"
                  ? "−"
                  : " "}
            </span>
            <span className="whitespace-pre px-2 py-1">{line.content}</span>
          </div>
        ))
      ) : (
        <p className="p-8 text-center font-sans text-sm text-muted-foreground">
          没有差异
        </p>
      )}
    </div>
  );
}
