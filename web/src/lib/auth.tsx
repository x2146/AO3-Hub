import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { PublicUser } from "@ao3hub/shared";
import {
  api,
  AUTH_INVALID_EVENT,
  isAuthStateEpochCurrent,
  markAuthStateFresh,
} from "./api";

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
  const queryClient = useQueryClient();
  const [state, setState] = useState<AuthState>({
    user: null,
    needsSetup: false,
    loading: true,
  });

  const clearCachedData = useCallback(async () => {
    await queryClient.cancelQueries();
    queryClient.clear();
  }, [queryClient]);

  const refresh = useCallback(async () => {
    try {
      const me = await api.me();
      await clearCachedData();
      markAuthStateFresh();
      setState({ user: me.user, needsSetup: me.needsSetup, loading: false });
    } catch {
      await clearCachedData();
      setState({ user: null, needsSetup: false, loading: false });
    }
  }, [clearCachedData]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  useEffect(() => {
    const onAuthInvalid = (event: Event) => {
      const requestEpoch = (event as CustomEvent<unknown>).detail;
      void clearCachedData().finally(() => {
        if (!isAuthStateEpochCurrent(requestEpoch)) return;
        setState((current) => ({ ...current, user: null, loading: false }));
      });
    };
    window.addEventListener(AUTH_INVALID_EVENT, onAuthInvalid);
    return () => window.removeEventListener(AUTH_INVALID_EVENT, onAuthInvalid);
  }, [clearCachedData]);

  const login = useCallback(
    async (username: string, password: string) => {
      const { user } = await api.login(username, password);
      await clearCachedData();
      markAuthStateFresh();
      setState({ user, needsSetup: false, loading: false });
    },
    [clearCachedData],
  );

  const logout = useCallback(async () => {
    await api.logout();
    await clearCachedData();
    markAuthStateFresh();
    setState((s) => ({ ...s, user: null }));
  }, [clearCachedData]);

  const setup = useCallback(
    async (username: string, password: string) => {
      const { user } = await api.setup(username, password);
      await clearCachedData();
      markAuthStateFresh();
      setState({ user, needsSetup: false, loading: false });
    },
    [clearCachedData],
  );

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
