import { cn } from "@/lib/cn";

export type TimelineItem = {
  description?: string;
  id: string;
  status?: "current" | "done" | "pending";
  timestamp: string;
  title: string;
};

export function Timeline({
  items,
  label = "时间线",
}: Readonly<{ items: readonly TimelineItem[]; label?: string }>) {
  return (
    <ol aria-label={label} className="space-y-0">
      {items.map((item, index) => (
        <li
          className="relative grid grid-cols-[1.5rem_1fr] gap-3"
          key={item.id}
        >
          {index < items.length - 1 ? (
            <span
              aria-hidden="true"
              className="absolute bottom-0 left-[0.45rem] top-4 w-px bg-border"
            />
          ) : null}
          <span
            aria-hidden="true"
            className={cn(
              "relative z-10 mt-1.5 size-2.5 rounded-full border-2 bg-background",
              item.status === "done"
                ? "border-emerald-600 bg-emerald-600"
                : item.status === "current"
                  ? "border-primary"
                  : "border-border",
            )}
          />
          <div className="pb-7">
            <div className="flex flex-wrap items-baseline justify-between gap-2">
              <h3 className="text-sm font-medium">{item.title}</h3>
              <time className="text-xs text-muted-foreground">
                {item.timestamp}
              </time>
            </div>
            {item.description ? (
              <p className="mt-1 text-sm leading-6 text-muted-foreground">
                {item.description}
              </p>
            ) : null}
          </div>
        </li>
      ))}
    </ol>
  );
}
