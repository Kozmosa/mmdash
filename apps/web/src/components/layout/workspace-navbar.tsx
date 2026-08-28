"use client";

import { Menu } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";

import { useCurrentProject } from "@/components/providers/project-provider";
import { Button } from "@/components/ui/button";
import { UserMenu } from "@/components/user-menu";
import { useWorkspaceStore } from "@/stores/workspace";
import { cn } from "@/lib/cn";

import { InboxNavLink } from "./inbox-nav-link";

export function WorkspaceNavbar() {
  const project = useCurrentProject();
  const setSidebarOpen = useWorkspaceStore((state) => state.setSidebarOpen);
  const pathname = usePathname();

  const agentWorkspace = /\/projects\/[^/]+\/agent\/?$/.test(pathname);
  const articleWorkspace = /\/projects\/[^/]+\/article\/?$/.test(pathname);

  return (
    <header className="sticky top-0 z-40 flex h-16 items-center border-b border-border bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/80">
      <div
        className={cn(
          "flex w-full items-center gap-3",
          agentWorkspace
            ? "px-4 lg:px-6"
            : articleWorkspace
              ? "mx-auto max-w-[1440px] px-4 md:px-5 lg:px-6"
              : "mx-auto max-w-[1440px] px-5 md:px-6 lg:px-8",
        )}
      >
        <Button
          aria-label="打开导航"
          className="md:hidden"
          onClick={() => setSidebarOpen(true)}
          size="icon"
          variant="ghost"
        >
          <Menu aria-hidden="true" className="size-5" />
        </Button>
        <nav aria-label="面包屑" className="min-w-0">
          <ol className="flex min-w-0 items-center gap-2 text-sm">
            <li className="hidden text-muted-foreground sm:block">
              <Link className="hover:text-foreground" href="/projects">
                项目
              </Link>
            </li>
            <li
              aria-hidden="true"
              className="hidden text-muted-foreground sm:block"
            >
              /
            </li>
            <li className="truncate font-medium">{project.name}</li>
          </ol>
        </nav>
        <div className="ml-auto flex items-center gap-3">
          <InboxNavLink />
          <UserMenu showIdentity />
        </div>
      </div>
    </header>
  );
}
