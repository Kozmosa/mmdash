"use client";

import type { ReactNode } from "react";
import { usePathname } from "next/navigation";

import { WorkspaceNavbar } from "@/components/layout/workspace-navbar";
import { WorkspaceSidebar } from "@/components/layout/workspace-sidebar";
import { cn } from "@/lib/cn";

export function WorkspaceShell({
  children,
  projectId,
}: Readonly<{ children: ReactNode; projectId: string }>) {
  const pathname = usePathname();
  const agentWorkspace = /\/projects\/[^/]+\/agent\/?$/.test(pathname);
  return (
    <div className="min-h-screen bg-background md:grid md:grid-cols-[auto_1fr]">
      <WorkspaceSidebar projectId={projectId} />
      <div className="min-w-0">
        <WorkspaceNavbar />
        <main
          className={cn(
            agentWorkspace
              ? "h-[calc(100dvh-4rem)] w-full overflow-hidden p-0"
              : "mx-auto w-full max-w-[1440px] p-5 md:p-6 lg:p-8",
          )}
        >
          {children}
        </main>
      </div>
    </div>
  );
}
