import { cn } from "@/lib/cn";

export type LogEntry = {
  id: string;
  level: "debug" | "error" | "info" | "warn";
  message: string;
  timestamp: string;
};

const levelStyles: Record<LogEntry["level"], string> = {
  debug: "text-slate-400",
  error: "text-red-400",
  info: "text-sky-300",
  warn: "text-amber-300",
};

export function LogViewer({
  entries,
  label = "日志输出",
}: Readonly<{ entries: readonly LogEntry[]; label?: string }>) {
  return (
    <div
      aria-label={label}
      className="max-h-96 overflow-auto rounded-xl bg-zinc-950 p-4 font-mono text-xs leading-6 text-zinc-200"
      role="log"
    >
      {entries.length > 0 ? (
        entries.map((entry) => (
          <div className="grid grid-cols-[auto_auto_1fr] gap-3" key={entry.id}>
            <time className="text-zinc-500">{entry.timestamp}</time>
            <span
              className={cn(
                "w-12 font-semibold uppercase",
                levelStyles[entry.level],
              )}
            >
              {entry.level}
            </span>
            <span className="whitespace-pre-wrap break-words">
              {entry.message}
            </span>
          </div>
        ))
      ) : (
        <span className="text-zinc-500">暂无日志</span>
      )}
    </div>
  );
}
