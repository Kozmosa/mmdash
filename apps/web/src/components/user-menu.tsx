"use client";

import { useQueryClient } from "@tanstack/react-query";
import { ChevronDown, LogOut, Moon, Server, Sun, Trash2, UserRound } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { useCurrentUser } from "@/components/providers/user-provider";
import { UserAvatar } from "@/components/user-avatar";
import { apiClient } from "@/lib/api-client";
import { cn } from "@/lib/cn";

export function UserMenu({
  showIdentity = false,
}: Readonly<{ showIdentity?: boolean }>) {
  const user = useCurrentUser();
  const router = useRouter();
  const queryClient = useQueryClient();
  const containerRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const [loggingOut, setLoggingOut] = useState(false);
  const [isDark, setIsDark] = useState(false);

  useEffect(() => {
    setIsDark(document.documentElement.classList.contains("dark"));
    function handleThemeChange() {
      setIsDark(document.documentElement.classList.contains("dark"));
    }
    window.addEventListener("theme-change", handleThemeChange);
    return () => {
      window.removeEventListener("theme-change", handleThemeChange);
    };
  }, []);

  useEffect(() => {
    if (!open) return;

    function closeOnOutsidePointer(event: PointerEvent) {
      if (!containerRef.current?.contains(event.target as Node)) {
        setOpen(false);
      }
    }

    function closeOnEscape(event: KeyboardEvent) {
      if (event.key === "Escape") {
        setOpen(false);
        triggerRef.current?.focus();
      }
    }

    document.addEventListener("pointerdown", closeOnOutsidePointer);
    document.addEventListener("keydown", closeOnEscape);
    return () => {
      document.removeEventListener("pointerdown", closeOnOutsidePointer);
      document.removeEventListener("keydown", closeOnEscape);
    };
  }, [open]);

  async function logout() {
    setLoggingOut(true);
    try {
      await apiClient.request("/auth/logout", { method: "POST" });
      queryClient.clear();
      toast.success("已退出登录");
      router.replace("/login");
      router.refresh();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "退出登录失败");
      setLoggingOut(false);
    }
  }

  return (
    <div className="relative" ref={containerRef}>
      <button
        aria-expanded={open}
        aria-haspopup="menu"
        aria-label={`当前用户：${user?.displayName ?? "加载中"}`}
        className={cn(
          "flex items-center rounded-full outline-none transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring",
          showIdentity ? "gap-2 rounded-md px-2 py-1.5" : null,
        )}
        onClick={() => setOpen((value) => !value)}
        ref={triggerRef}
        type="button"
      >
        <UserAvatar
          className={showIdentity ? undefined : "size-9 rounded-full"}
          displayName={user?.displayName}
          email={user?.email}
        />
        {showIdentity ? (
          <>
            <span className="hidden max-w-36 truncate text-sm sm:inline">
              {user?.displayName ?? "加载中"}
            </span>
            <ChevronDown
              aria-hidden="true"
              className="hidden size-3.5 text-muted-foreground sm:block"
            />
          </>
        ) : null}
      </button>

      {open ? (
        <div
          aria-label="用户菜单"
          className="absolute right-0 top-full z-50 mt-2 w-56 overflow-hidden rounded-xl border border-border bg-popover p-1.5 text-popover-foreground shadow-lg"
          role="menu"
        >
          <div className="border-b border-border px-3 py-2.5">
            <p className="truncate text-sm font-medium">
              {user?.displayName ?? "正在加载"}
            </p>
            <p className="mt-0.5 truncate text-xs text-muted-foreground">
              {user?.email}
            </p>
          </div>
          <div className="pt-1.5">
            <Link
              className="flex min-h-9 items-center gap-2 rounded-md px-2.5 text-sm outline-none hover:bg-accent focus-visible:bg-accent"
              href="/account"
              onClick={() => setOpen(false)}
              role="menuitem"
            >
              <UserRound aria-hidden="true" className="size-4" />
              个人中心
            </Link>
            <Link
              className="flex min-h-9 items-center gap-2 rounded-md px-2.5 text-sm outline-none hover:bg-accent focus-visible:bg-accent"
              href="/account/boxes"
              onClick={() => setOpen(false)}
              role="menuitem"
            >
              <Server aria-hidden="true" className="size-4" />
              Box 管理
            </Link>
            <Link
              className="flex min-h-9 items-center gap-2 rounded-md px-2.5 text-sm outline-none hover:bg-accent focus-visible:bg-accent"
              href="/projects/trash"
              onClick={() => setOpen(false)}
              role="menuitem"
            >
              <Trash2 aria-hidden="true" className="size-4" />
              项目回收站
            </Link>
            <button
              className="flex min-h-9 w-full items-center gap-2 rounded-md px-2.5 text-sm outline-none hover:bg-accent focus-visible:bg-accent"
              onClick={() => {
                const nextDark = !document.documentElement.classList.contains("dark");
                if (nextDark) {
                  document.documentElement.classList.add("dark");
                  localStorage.setItem("theme", "dark");
                } else {
                  document.documentElement.classList.remove("dark");
                  localStorage.setItem("theme", "light");
                }
                window.dispatchEvent(new Event("theme-change"));
              }}
              role="menuitem"
              type="button"
            >
              {isDark ? (
                <Sun className="size-4 shrink-0 text-amber-500" />
              ) : (
                <Moon className="size-4 shrink-0 text-slate-500" />
              )}
              <span>{isDark ? "日间模式" : "黑夜模式"}</span>
            </button>
            <button
              className="flex min-h-9 w-full items-center gap-2 rounded-md px-2.5 text-sm text-destructive outline-none hover:bg-destructive/10 focus-visible:bg-destructive/10 disabled:opacity-50"
              disabled={loggingOut}
              onClick={() => void logout()}
              role="menuitem"
              type="button"
            >
              <LogOut aria-hidden="true" className="size-4" />
              {loggingOut ? "正在退出…" : "退出登录"}
            </button>
          </div>
        </div>
      ) : null}
    </div>
  );
}
