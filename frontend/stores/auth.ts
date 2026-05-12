import { create } from "zustand";
import { clearAuthCookie, setAuthCookie } from "@/lib/auth-cookie";

interface User {
  id: string;
  email: string;
  username: string | null;
  display_name: string | null;
  created_at: string;
}

interface AuthState {
  user: User | null;
  token: string | null;
  setAuth: (user: User, token: string) => void;
  logout: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  token: null,
  setAuth: (user, token) => {
    localStorage.setItem("token", token);
    setAuthCookie(token);
    set({ user, token });
  },
  logout: () => {
    localStorage.removeItem("token");
    clearAuthCookie();
    set({ user: null, token: null });
  },
}));
