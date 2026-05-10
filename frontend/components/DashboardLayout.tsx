"use client";

import { useEffect } from "react";
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
  const user = useAuthStore((s) => s.user);
  const setAuth = useAuthStore((s) => s.setAuth);
  const logout = useAuthStore((s) => s.logout);

  useEffect(() => {
    const token = localStorage.getItem("token");
    if (!token || user) return;
    api
      .get("/auth/me")
      .then((res) => setAuth(res.data, token))
      .catch(() => logout());
  }, [user, setAuth, logout]);

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
