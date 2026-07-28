"use client";

import { CircleAlert } from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/cn";

export function ErrorState({
  className,
  description = "请求未能完成，请稍后重试。",
  onRetry,
  title = "出现了问题",
}: Readonly<{
  className?: string;
  description?: string;
  onRetry?: () => void;
  title?: string;
}>) {
  return (
    <div
      className={cn(
        "flex min-h-48 flex-col items-center justify-center rounded-xl border border-destructive/30 bg-destructive/5 p-8 text-center",
        className,
      )}
      role="alert"
    >
      <CircleAlert
        aria-hidden="true"
        className="mb-4 size-6 text-destructive"
      />
      <h2 className="text-sm font-semibold">{title}</h2>
      <p className="mt-1 max-w-md text-sm leading-6 text-muted-foreground">
        {description}
      </p>
      {onRetry ? (
        <Button className="mt-5" onClick={onRetry} variant="outline">
          重新加载
        </Button>
      ) : null}
    </div>
  );
}
