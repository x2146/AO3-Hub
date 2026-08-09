import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
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
  error: string | null;
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
  const operationRef = useRef(0);
  const mountedRef = useRef(true);
  const [state, setState] = useState<AuthState>({
    user: null,
    needsSetup: false,
    loading: true,
    error: null,
  });

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      operationRef.current += 1;
    };
  }, []);

  const clearCachedData = useCallback(async () => {
    await queryClient.cancelQueries();
    queryClient.clear();
  }, [queryClient]);

  const refresh = useCallback(async () => {
    const operation = ++operationRef.current;
    if (mountedRef.current) {
      setState((current) => ({ ...current, loading: true, error: null }));
    }
    try {
      const me = await api.me();
      if (!mountedRef.current || operation !== operationRef.current) return;
      await clearCachedData();
      if (!mountedRef.current || operation !== operationRef.current) return;
      markAuthStateFresh();
      setState({
        user: me.user,
        needsSetup: me.needsSetup,
        loading: false,
        error: null,
      });
    } catch (error) {
      if (!mountedRef.current || operation !== operationRef.current) return;
      await clearCachedData();
      if (!mountedRef.current || operation !== operationRef.current) return;
      setState({
        user: null,
        needsSetup: false,
        loading: false,
        error: error instanceof Error ? error.message : "无法读取登录状态",
      });
    }
  }, [clearCachedData]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  useEffect(() => {
    const onAuthInvalid = (event: Event) => {
      const requestEpoch = (event as CustomEvent<unknown>).detail;
      void clearCachedData().finally(() => {
        if (!mountedRef.current || !isAuthStateEpochCurrent(requestEpoch)) {
          return;
        }
        setState((current) => ({
          ...current,
          user: null,
          loading: false,
          error: null,
        }));
      });
    };
    window.addEventListener(AUTH_INVALID_EVENT, onAuthInvalid);
    return () => window.removeEventListener(AUTH_INVALID_EVENT, onAuthInvalid);
  }, [clearCachedData]);

  const login = useCallback(
    async (username: string, password: string) => {
      const operation = ++operationRef.current;
      const { user } = await api.login(username, password);
      if (!mountedRef.current || operation !== operationRef.current) return;
      await clearCachedData();
      if (!mountedRef.current || operation !== operationRef.current) return;
      markAuthStateFresh();
      setState({ user, needsSetup: false, loading: false, error: null });
    },
    [clearCachedData],
  );

  const logout = useCallback(async () => {
    const operation = ++operationRef.current;
    await api.logout();
    if (!mountedRef.current || operation !== operationRef.current) return;
    await clearCachedData();
    if (!mountedRef.current || operation !== operationRef.current) return;
    markAuthStateFresh();
    setState((s) => ({ ...s, user: null, error: null }));
  }, [clearCachedData]);

  const setup = useCallback(
    async (username: string, password: string) => {
      const operation = ++operationRef.current;
      const { user } = await api.setup(username, password);
      if (!mountedRef.current || operation !== operationRef.current) return;
      await clearCachedData();
      if (!mountedRef.current || operation !== operationRef.current) return;
      markAuthStateFresh();
      setState({ user, needsSetup: false, loading: false, error: null });
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
