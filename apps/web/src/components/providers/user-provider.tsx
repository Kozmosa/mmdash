"use client";

import { createContext, type ReactNode, useContext, useMemo } from "react";

export type CurrentUser = {
  displayName: string;
  email: string;
  id: string;
};

const UserContext = createContext<CurrentUser | null>(null);

export function UserProvider({
  children,
  user,
}: Readonly<{ children: ReactNode; user: CurrentUser }>) {
  const value = useMemo(
    () => ({
      displayName: user.displayName,
      email: user.email,
      id: user.id,
    }),
    [user.displayName, user.email, user.id],
  );

  return <UserContext.Provider value={value}>{children}</UserContext.Provider>;
}

export function useCurrentUser(): CurrentUser {
  const user = useContext(UserContext);
  if (!user) {
    throw new Error("useCurrentUser must be used inside UserProvider");
  }
  return user;
}
