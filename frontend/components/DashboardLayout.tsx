"use client";

import { useEffect } from "react";
import { usePathname, useRouter } from "next/navigation";
import { AppSidebar } from "./app-sidebar";
import { AppNavbar } from "./app-navbar";
import { SidebarProvider } from "@/components/ui/sidebar";
import api from "@/lib/api";
import { useAuthStore } from "@/stores/auth";

export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const pathname = usePathname();
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const token = useAuthStore((s) => s.token);
  const setAuth = useAuthStore((s) => s.setAuth);
  const logout = useAuthStore((s) => s.logout);

  useEffect(() => {
    const storedToken = localStorage.getItem("token");
    if (!storedToken) {
      logout();
      router.replace(`/auth/login?next=${encodeURIComponent(pathname)}`);
      return;
    }
    if (user) return;
    api
      .get("/auth/me")
      .then((res) => setAuth(res.data, storedToken))
      .catch(() => {
        logout();
        router.replace(`/auth/login?next=${encodeURIComponent(pathname)}`);
      });
  }, [logout, pathname, router, setAuth, user]);

  if (!token && !user) {
    return null;
  }

  return (
    <SidebarProvider>
      <AppSidebar />
      <div className="relative flex w-full flex-1 flex-col bg-background min-h-svh">
        <AppNavbar />
        <main className="flex-1 p-6 overflow-auto">{children}</main>
      </div>
    </SidebarProvider>
  );
}
