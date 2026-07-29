"use client";

import { ChevronDown, Menu } from "lucide-react";
import Link from "next/link";

import { useCurrentProject } from "@/components/providers/project-provider";
import { useCurrentUser } from "@/components/providers/user-provider";
import { Button } from "@/components/ui/button";
import { useWorkspaceStore } from "@/stores/workspace";
import { UserAvatar } from "@/components/user-avatar";

export function WorkspaceNavbar() {
  const project = useCurrentProject();
  const user = useCurrentUser();
  const setSidebarOpen = useWorkspaceStore((state) => state.setSidebarOpen);

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
      <div className="ml-auto flex items-center gap-2">
        <Button
          aria-label={`当前用户：${user?.displayName ?? "加载中"}`}
          className="gap-2"
          variant="ghost"
          onClick={() => {
            window.location.href = "/account";
          }}
        >
          <UserAvatar displayName={user?.displayName} email={user?.email} />
          <span className="hidden text-sm sm:inline">
            {user?.displayName ?? "加载中"}
          </span>
          <ChevronDown
            aria-hidden="true"
            className="hidden size-3.5 text-muted-foreground sm:block"
          />
        </Button>
      </div>
    </header>
  );
}
