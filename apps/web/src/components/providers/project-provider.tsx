"use client";

import { createContext, type ReactNode, useContext, useMemo } from "react";

export type CurrentProject = {
  id: string;
  name: string;
};

const ProjectContext = createContext<CurrentProject | null>(null);

export function ProjectProvider({
  children,
  project,
}: Readonly<{ children: ReactNode; project: CurrentProject }>) {
  const value = useMemo(
    () => ({ id: project.id, name: project.name }),
    [project.id, project.name],
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
