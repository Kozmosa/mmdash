import { create } from "zustand";

interface Team {
  id: string;
  name: string;
  invite_code: string;
}

interface Project {
  id: string;
  name: string;
  description: string | null;
  git_remote_url: string | null;
  model_data_page_id: string | null;
}

interface DataCacheState {
  teams: Team[] | null;
  teamsFetchedAt: number | null;
  projects: Record<string, Project[]>;
  projectsFetchedAt: Record<string, number>;

  setTeams: (teams: Team[]) => void;
  getTeams: () => Team[] | null;
  isTeamsStale: (maxAgeMs?: number) => boolean;

  setProjects: (teamId: string, projects: Project[]) => void;
  getProjects: (teamId: string) => Project[] | null;
  isProjectsStale: (teamId: string, maxAgeMs?: number) => boolean;

  invalidateTeams: () => void;
  invalidateProjects: (teamId: string) => void;
  clearCache: () => void;
}

const STALE_MS = 5 * 60 * 1000; // 5 minutes

export const useDataCache = create<DataCacheState>((set, get) => ({
  teams: null,
  teamsFetchedAt: null,
  projects: {},
  projectsFetchedAt: {},

  setTeams: (teams) =>
    set({ teams, teamsFetchedAt: Date.now() }),

  getTeams: () => get().teams,

  isTeamsStale: (maxAgeMs = STALE_MS) => {
    const fetchedAt = get().teamsFetchedAt;
    if (!fetchedAt) return true;
    return Date.now() - fetchedAt > maxAgeMs;
  },

  setProjects: (teamId, projects) =>
    set((state) => ({
      projects: { ...state.projects, [teamId]: projects },
      projectsFetchedAt: {
        ...state.projectsFetchedAt,
        [teamId]: Date.now(),
      },
    })),

  getProjects: (teamId) => get().projects[teamId] ?? null,

  isProjectsStale: (teamId, maxAgeMs = STALE_MS) => {
    const fetchedAt = get().projectsFetchedAt[teamId];
    if (!fetchedAt) return true;
    return Date.now() - fetchedAt > maxAgeMs;
  },

  clearCache: () =>
    set({
      teams: null,
      teamsFetchedAt: null,
      projects: {},
      projectsFetchedAt: {},
    }),

  invalidateTeams: () =>
    set({ teamsFetchedAt: null }),

  invalidateProjects: (teamId) =>
    set((state) => ({
      projectsFetchedAt: {
        ...state.projectsFetchedAt,
        [teamId]: 0,
      },
    })),
}));
