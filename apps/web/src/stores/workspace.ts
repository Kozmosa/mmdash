"use client";

import { create } from "zustand";

const workspaceSidebarStorageKey = "mmdash.workspace.sidebar-open";

function persistSidebarOpen(sidebarOpen: boolean) {
  if (typeof window !== "undefined") {
    window.localStorage.setItem(workspaceSidebarStorageKey, String(sidebarOpen));
  }
}

type WorkspaceState = {
  sidebarOpen: boolean;
  setSidebarOpen: (open: boolean) => void;
  toggleSidebar: () => void;
};

export const useWorkspaceStore = create<WorkspaceState>((set) => ({
  sidebarOpen: true,
  setSidebarOpen: (sidebarOpen) => {
    persistSidebarOpen(sidebarOpen);
    set({ sidebarOpen });
  },
  toggleSidebar: () =>
    set((state) => {
      const sidebarOpen = !state.sidebarOpen;
      persistSidebarOpen(sidebarOpen);
      return { sidebarOpen };
    }),
}));

export function restoreWorkspaceSidebar() {
  if (typeof window === "undefined") return;
  const saved = window.localStorage.getItem(workspaceSidebarStorageKey);
  if (saved !== null) {
    useWorkspaceStore.setState({ sidebarOpen: saved === "true" });
  }
}
