"use client";

import type { ReactNode } from "react";

import { WorkspaceNavbar } from "@/components/layout/workspace-navbar";
import { WorkspaceSidebar } from "@/components/layout/workspace-sidebar";

export function WorkspaceShell({
  children,
  projectId,
}: Readonly<{ children: ReactNode; projectId: string }>) {
  return (
    <div className="min-h-screen bg-background md:grid md:grid-cols-[auto_1fr]">
      <WorkspaceSidebar projectId={projectId} />
      <div className="min-w-0">
        <WorkspaceNavbar />
        <main className="mx-auto w-full max-w-[1440px] p-5 md:p-6 lg:p-8">
          {children}
        </main>
      </div>
    </div>
  );
}
