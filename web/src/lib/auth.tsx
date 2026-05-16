import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import type { PublicUser } from "@ao3hub/shared";
import { api } from "./api";

type AuthState = {
  user: PublicUser | null;
  needsSetup: boolean;
  loading: boolean;
};

type AuthContextValue = AuthState & {
  refresh: () => Promise<void>;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  setup: (username: string, password: string) => Promise<void>;
};

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({
    user: null,
    needsSetup: false,
    loading: true,
  });

  const refresh = useCallback(async () => {
    try {
      const me = await api.me();
      setState({ user: me.user, needsSetup: me.needsSetup, loading: false });
    } catch {
      setState({ user: null, needsSetup: false, loading: false });
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const login = useCallback(
    async (username: string, password: string) => {
      const { user } = await api.login(username, password);
      setState({ user, needsSetup: false, loading: false });
    },
    [],
  );

  const logout = useCallback(async () => {
    await api.logout();
    setState((s) => ({ ...s, user: null }));
  }, []);

  const setup = useCallback(async (username: string, password: string) => {
    const { user } = await api.setup(username, password);
    setState({ user, needsSetup: false, loading: false });
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({ ...state, refresh, login, logout, setup }),
    [state, refresh, login, logout, setup],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used inside AuthProvider");
  return ctx;
}
