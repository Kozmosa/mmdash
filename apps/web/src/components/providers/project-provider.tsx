"use client";

import { useQuery } from "@tanstack/react-query";
import { createContext, type ReactNode, useContext, useMemo } from "react";

import { apiClient } from "@/lib/api-client";

export type CurrentProject = {
  createdBy?: string;
  id: string;
  name: string;
  role?: "agent" | "box" | "editor" | "maintainer" | "owner" | "viewer";
};

const ProjectContext = createContext<CurrentProject | null>(null);

export function ProjectProvider({
  children,
  project,
}: Readonly<{ children: ReactNode; project: CurrentProject }>) {
  const query = useQuery({
    initialData: project,
    initialDataUpdatedAt: 0,
    queryFn: async () => {
      const result = await apiClient.request<{
        created_by: string;
        id: string;
        name: string;
        role: CurrentProject["role"];
      }>(`/projects/${encodeURIComponent(project.id)}`);
      return {
        createdBy: result.created_by,
        id: result.id,
        name: result.name,
        role: result.role,
      };
    },
    queryKey: ["project", project.id],
    refetchOnMount: "always",
  });
  const value = useMemo(
    () => ({
      createdBy: query.data.createdBy,
      id: query.data.id,
      name: query.data.name,
      role: query.data.role,
    }),
    [query.data.createdBy, query.data.id, query.data.name, query.data.role],
  );

  return (
    <ProjectContext.Provider value={value}>{children}</ProjectContext.Provider>
  );
}

export function useCurrentProject(): CurrentProject {
  const project = useContext(ProjectContext);
  if (!project) {
    throw new Error("useCurrentProject must be used inside ProjectProvider");
  }
  return project;
}
