import type { LucideIcon } from "lucide-react";

import { Badge } from "@/components/ui/badge";

export function FeaturePlaceholder({
  description,
  icon: Icon,
  title,
}: Readonly<{
  description: string;
  icon: LucideIcon;
  title: string;
}>) {
  return (
    <section className="space-y-6" aria-labelledby="page-title">
      <header className="flex items-start justify-between gap-4">
        <div>
          <div className="mb-3 flex size-10 items-center justify-center rounded-lg border border-border bg-card shadow-xs">
            <Icon aria-hidden="true" className="size-5" />
          </div>
          <h1 id="page-title" className="text-2xl font-semibold tracking-tight">
            {title}
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">{description}</p>
        </div>
        <Badge>功能壳</Badge>
      </header>
      <div className="flex min-h-64 items-center justify-center rounded-xl border border-dashed border-border bg-muted/20 p-8 text-center">
        <div className="max-w-md">
          <p className="text-sm font-medium">该模块尚未接入业务数据</p>
          <p className="mt-2 text-sm leading-6 text-muted-foreground">
            当前阶段仅建立页面、路由和扩展边界。后续所属功能模块会在这里注册真实内容。
          </p>
        </div>
      </div>
    </section>
  );
}
