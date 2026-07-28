"use client";

import { useQuery } from "@tanstack/react-query";
import { createContext, type ReactNode, useContext } from "react";

import { apiClient } from "@/lib/api-client";

export type CurrentUser = {
  displayName: string;
  email: string;
  id: string;
};

const UserContext = createContext<CurrentUser | null>(null);

export function UserProvider({ children }: Readonly<{ children: ReactNode }>) {
  const identity = useQuery({
    queryFn: async () => {
      const result = await apiClient.request<{
        user: {
          display_name: string;
          email: string;
          id: string;
        };
      }>("/auth/me");
      return {
        displayName: result.user.display_name,
        email: result.user.email,
        id: result.user.id,
      };
    },
    queryKey: ["current-user"],
    retry: false,
  });

  return (
    <UserContext.Provider value={identity.data ?? null}>
      {children}
    </UserContext.Provider>
  );
}

export function useCurrentUser(): CurrentUser | null {
  return useContext(UserContext);
}
