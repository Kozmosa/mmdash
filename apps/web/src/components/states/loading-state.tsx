import { LoaderCircle } from "lucide-react";

import { cn } from "@/lib/cn";

export function LoadingState({
  className,
  label = "正在加载…",
}: Readonly<{ className?: string; label?: string }>) {
  return (
    <div
      aria-busy="true"
      aria-live="polite"
      className={cn(
        "flex min-h-48 flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-border bg-muted/30 p-8 text-sm text-muted-foreground",
        className,
      )}
    >
      <LoaderCircle aria-hidden="true" className="size-5 animate-spin" />
      <span>{label}</span>
    </div>
  );
}
