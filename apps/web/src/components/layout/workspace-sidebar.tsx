"use client";

import { FlaskConical, PanelLeftClose, PanelLeftOpen } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect } from "react";

import { Button } from "@/components/ui/button";
import { workspaceHref, workspaceRoutes } from "@/lib/navigation";
import { cn } from "@/lib/cn";
import {
  restoreWorkspaceSidebar,
  useWorkspaceStore,
} from "@/stores/workspace";

export function WorkspaceSidebar({
  projectId,
}: Readonly<{ projectId: string }>) {
  const pathname = usePathname();
  const sidebarOpen = useWorkspaceStore((state) => state.sidebarOpen);
  const setSidebarOpen = useWorkspaceStore((state) => state.setSidebarOpen);
  const toggleSidebar = useWorkspaceStore((state) => state.toggleSidebar);

  useEffect(() => restoreWorkspaceSidebar(), []);

  return (
    <>
      {sidebarOpen ? (
        <button
          aria-label="关闭导航"
          className="fixed inset-0 z-30 bg-black/20 md:hidden"
          onClick={() => setSidebarOpen(false)}
          type="button"
        />
      ) : null}
      <aside
        className={cn(
          "fixed inset-y-0 left-0 z-40 flex border-r border-sidebar-border bg-sidebar text-sidebar-foreground transition-[width,transform] duration-200 md:sticky md:top-0 md:h-screen md:translate-x-0",
          sidebarOpen
            ? "w-72 translate-x-0 md:w-64"
            : "w-64 -translate-x-full md:w-16",
        )}
      >
        <div className="flex min-w-0 flex-1 flex-col">
          <div
            className={cn(
              "flex h-16 items-center border-b border-sidebar-border px-3",
              sidebarOpen ? "gap-3" : "justify-center",
            )}
          >
            <div
              className={cn(
                "relative min-w-0",
                sidebarOpen ? "flex-1" : "group size-9",
              )}
            >
              <Link
                aria-label="返回项目列表"
                className={cn(
                  "flex min-w-0 items-center gap-3",
                  sidebarOpen ? null : "size-9 justify-center",
                )}
                href="/projects"
              >
                <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
                  <FlaskConical aria-hidden="true" className="size-4" />
                </span>
                {sidebarOpen ? (
                  <span className="min-w-0">
                    <span className="block truncate text-sm font-semibold">
                      mmdash
                    </span>
                    <span className="block truncate text-xs text-muted-foreground">
                      数学建模协作平台
                    </span>
                  </span>
                ) : null}
              </Link>
              {!sidebarOpen ? (
                <Button
                  aria-label="展开导航"
                  className="pointer-events-none absolute inset-0 opacity-0 transition-opacity group-hover:pointer-events-auto group-hover:opacity-100 group-focus-within:pointer-events-auto group-focus-within:opacity-100"
                  onClick={toggleSidebar}
                  size="icon"
                  variant="ghost"
                >
                  <PanelLeftOpen aria-hidden="true" className="size-4" />
                </Button>
              ) : null}
            </div>
            {sidebarOpen ? (
              <Button
                aria-label="收起导航"
                className="hidden md:inline-flex"
                onClick={toggleSidebar}
                size="icon"
                variant="ghost"
              >
                <PanelLeftClose aria-hidden="true" className="size-4" />
              </Button>
            ) : null}
          </div>

          <nav aria-label="项目一级导航" className="flex-1 overflow-y-auto p-2">
            {sidebarOpen ? (
              <p className="px-2 pb-2 pt-3 text-xs font-medium text-muted-foreground">
                导航
              </p>
            ) : null}
            <ul className="space-y-1">
              {workspaceRoutes.map((route) => {
                const href = workspaceHref(projectId, route.segment);
                const active = route.segment
                  ? pathname === href || pathname.startsWith(`${href}/`)
                  : pathname === href;
                return (
                  <li key={route.id}>
                    <Link
                      aria-current={active ? "page" : undefined}
                      className={cn(
                        "flex min-h-10 items-center gap-3 rounded-md px-3 text-sm transition-colors",
                        active
                          ? "bg-sidebar-accent font-medium text-sidebar-accent-foreground"
                          : "text-muted-foreground hover:bg-sidebar-accent/70 hover:text-sidebar-accent-foreground",
                        sidebarOpen ? null : "justify-center px-0",
                      )}
                      href={href}
                      onClick={() => {
                        if (window.matchMedia("(max-width: 767px)").matches) {
                          setSidebarOpen(false);
                        }
                      }}
                      title={sidebarOpen ? undefined : route.label}
                    >
                      <route.icon
                        aria-hidden="true"
                        className="size-4 shrink-0"
                      />
                      {sidebarOpen ? <span>{route.label}</span> : null}
                    </Link>
                  </li>
                );
              })}
            </ul>
          </nav>
        </div>
      </aside>
    </>
  );
}
