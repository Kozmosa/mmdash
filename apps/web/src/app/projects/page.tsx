import { FolderKanban } from "lucide-react";

import { EmptyState } from "@/components/states/empty-state";
import { Button } from "@/components/ui/button";

export default function ProjectsPage() {
  return (
    <main className="mx-auto w-full max-w-6xl p-6 lg:p-10">
      <header className="mb-8 flex items-start justify-between gap-4">
        <div>
          <div className="mb-3 flex size-10 items-center justify-center rounded-lg border border-border bg-card shadow-xs">
            <FolderKanban aria-hidden="true" className="size-5" />
          </div>
          <h1 className="text-2xl font-semibold tracking-tight">项目</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            选择数学建模或研究项目进入工作区
          </p>
        </div>
        <Button disabled>创建项目</Button>
      </header>
      <EmptyState
        description="Project 领域能力将在 3.8 接入。当前页面已预留项目列表、创建与归档入口。"
        title="还没有可用项目"
      />
    </main>
  );
}
