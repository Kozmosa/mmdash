import type { ReactNode } from "react";
import { Suspense } from "react";

import { WorkspaceShell } from "@/components/layout/workspace-shell";
import { WorkspaceShellSkeleton } from "@/components/layout/workspace-shell-skeleton";
import { ProjectProvider } from "@/components/providers/project-provider";

type WorkspaceLayoutProps = {
  children: ReactNode;
  params: Promise<{ projectId: string }>;
};

export default async function WorkspaceLayout({
  children,
  params,
}: Readonly<WorkspaceLayoutProps>) {
  const { projectId } = await params;
  const project = {
    id: projectId,
    name: `项目 ${projectId}`,
  };

  return (
    <ProjectProvider project={project}>
      <Suspense fallback={<WorkspaceShellSkeleton />}>
        <WorkspaceShell projectId={projectId}>{children}</WorkspaceShell>
      </Suspense>
    </ProjectProvider>
  );
}
