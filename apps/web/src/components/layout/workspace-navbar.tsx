"use client";

import { Menu } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";

import { useCurrentProject } from "@/components/providers/project-provider";
import { Button } from "@/components/ui/button";
import { UserMenu } from "@/components/user-menu";
import { useWorkspaceStore } from "@/stores/workspace";
import { apiClient } from "@/lib/api-client";

export function WorkspaceNavbar() {
  const project = useCurrentProject();
  const setSidebarOpen = useWorkspaceStore((state) => state.setSidebarOpen);
  const unread = useQuery({
    queryKey: ["inbox-unread-count"],
    queryFn: () => apiClient.request<{ count: number }>("/inbox/unread-count"),
  });

  return (
    <header className="sticky top-0 z-20 flex h-16 items-center gap-3 border-b border-border bg-background/95 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/80 lg:px-6">
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
      <div className="ml-auto flex items-center gap-4">
        <Link
          className="relative text-sm text-muted-foreground hover:text-foreground"
          href="/inbox"
        >
          Inbox
          {unread.data?.count ? (
            <span className="ml-1 rounded-full bg-primary px-1.5 py-0.5 text-[10px] text-primary-foreground">
              {unread.data.count}
            </span>
          ) : null}
        </Link>
        <UserMenu showIdentity />
      </div>
    </header>
  );
}
