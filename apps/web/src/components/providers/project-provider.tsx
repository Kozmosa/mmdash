"use client";

import { useQuery } from "@tanstack/react-query";
import { createContext, type ReactNode, useContext, useMemo } from "react";

import { apiClient } from "@/lib/api-client";

export type CurrentProject = {
  id: string;
  name: string;
};

const ProjectContext = createContext<CurrentProject | null>(null);

export function ProjectProvider({
  children,
  project,
}: Readonly<{ children: ReactNode; project: CurrentProject }>) {
  const query = useQuery({
    initialData: project,
    queryFn: () =>
      apiClient.request<CurrentProject>(
        `/projects/${encodeURIComponent(project.id)}`,
      ),
    queryKey: ["project", project.id],
  });
  const value = useMemo(
    () => ({ id: query.data.id, name: query.data.name }),
    [query.data.id, query.data.name],
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
