import { LoadingState } from "@/components/states/loading-state";

export function WorkspaceShellSkeleton() {
  return (
    <div className="min-h-screen bg-background md:grid md:grid-cols-[16rem_1fr]">
      <div className="hidden border-r border-border bg-sidebar md:block" />
      <div>
        <div className="h-16 border-b border-border" />
        <main className="p-6 lg:p-8">
          <LoadingState label="正在进入项目工作区…" />
        </main>
      </div>
    </div>
  );
}
