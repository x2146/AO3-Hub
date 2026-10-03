import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  redirect,
  useRouter,
  useRouterState,
} from "@tanstack/react-router";
import { useEffect } from "react";
import { AppLayout } from "./components/AppLayout";
import { Alert, AlertDescription, AlertTitle } from "./components/ui/alert";
import { Button } from "./components/ui/button";
import { Skeleton } from "./components/ui/skeleton";
import { Library } from "./pages/Library";
import { ImportPage } from "./pages/Import";
import { Settings } from "./pages/Settings";
import { Reader } from "./pages/Reader";
import { Version } from "./pages/Version";
import { NotFound } from "./pages/NotFound";
import { LoginPage } from "./pages/Login";
import { SetupPage } from "./pages/Setup";
import { UsersPage } from "./pages/Users";
import { AuthProvider, useAuth } from "./lib/auth";

const PUBLIC_PATHS = ["/login", "/setup"];
const ANON_OK_PATHS = ["/", "/version"];
const ADMIN_PATHS = ["/settings", "/users"];

function safeRedirectPath(value: unknown): string | undefined {
  if (
    typeof value !== "string" ||
    !value.startsWith("/") ||
    value.startsWith("//") ||
    value.includes("\\") ||
    /[\u0000-\u001f\u007f]/.test(value) ||
    /^\/(?:login|setup)(?:[/?#]|$)/.test(value)
  ) {
    return undefined;
  }
  return value;
}

function RootShell() {
  const { loading, user, needsSetup, error: authError, refresh } = useAuth();
  const router = useRouter();
  const routerState = useRouterState();
  const pathname = routerState.location.pathname;

  useEffect(() => {
    if (loading || authError) return;
    if (needsSetup && pathname !== "/setup") {
      router.navigate({ to: "/setup", replace: true });
      return;
    }
    if (!needsSetup && pathname === "/setup") {
      router.navigate({ to: user ? "/" : "/login", replace: true });
      return;
    }
    if (!user) {
      const isReader = pathname.startsWith("/r/");
      const isPublic =
        PUBLIC_PATHS.includes(pathname) ||
        ANON_OK_PATHS.includes(pathname) ||
        isReader;
      if (!isPublic) {
        router.navigate({
          to: "/login",
          search: { redirect: pathname },
          replace: true,
        });
      }
      return;
    }
    if (ADMIN_PATHS.includes(pathname) && user.role !== "admin") {
      router.navigate({ to: "/", replace: true });
    }
  }, [loading, user, needsSetup, authError, pathname, router]);

  if (loading) {
    return (
      <AppLayout>
        <div className="flex flex-col gap-4">
          <Skeleton className="h-8 w-56" />
          <Skeleton className="h-5 w-80" />
          <Skeleton className="h-40 w-full" />
        </div>
      </AppLayout>
    );
  }
  if (authError) {
    return (
      <AppLayout>
        <Alert variant="destructive">
          <AlertTitle>无法确认登录状态</AlertTitle>
          <AlertDescription className="flex flex-col items-start gap-3">
            {authError}
            <Button variant="outline" size="sm" onClick={() => void refresh()}>
              重试
            </Button>
          </AlertDescription>
        </Alert>
      </AppLayout>
    );
  }
  const isReader = pathname.startsWith("/r/");
  const isPublic =
    PUBLIC_PATHS.includes(pathname) ||
    ANON_OK_PATHS.includes(pathname) ||
    isReader;
  const unauthorized =
    (!user && !isPublic) ||
    (ADMIN_PATHS.includes(pathname) && user?.role !== "admin");
  if (unauthorized || (needsSetup && pathname !== "/setup")) {
    return (
      <AppLayout>
        <div className="flex flex-col gap-4">
          <Skeleton className="h-8 w-48" />
          <Skeleton className="h-32 w-full" />
        </div>
      </AppLayout>
    );
  }

  return (
    <AppLayout>
      <Outlet />
    </AppLayout>
  );
}

const rootRoute = createRootRoute({
  component: () => (
    <AuthProvider>
      <RootShell />
    </AuthProvider>
  ),
  notFoundComponent: () => <NotFound />,
});

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: Library,
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  validateSearch: (search: Record<string, unknown>) => ({
    redirect: safeRedirectPath(search.redirect),
  }),
  component: LoginPage,
});

const setupRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/setup",
  component: SetupPage,
});

const usersRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/users",
  component: UsersPage,
});

const importRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/import",
  component: ImportPage,
});

const SETTINGS_TABS = ["server", "llm", "ao3", "reader", "update"] as const;
export type SettingsTab = (typeof SETTINGS_TABS)[number];

const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/settings",
  // The open tab lives in the URL so a reload or a shared link lands on it.
  validateSearch: (search: Record<string, unknown>): { tab?: SettingsTab } => ({
    tab: SETTINGS_TABS.includes(search.tab as SettingsTab)
      ? (search.tab as SettingsTab)
      : undefined,
  }),
  component: Settings,
});

const versionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/version",
  component: Version,
});

const readerRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/r/$id/$chapter",
  component: Reader,
});

const readerEntry = createRoute({
  getParentRoute: () => rootRoute,
  path: "/r/$id",
  beforeLoad: ({ params }) => {
    throw redirect({
      to: "/r/$id/$chapter",
      params: { id: params.id, chapter: "0" },
      replace: true,
    });
  },
  component: () => null,
});

const routeTree = rootRoute.addChildren([
  indexRoute,
  loginRoute,
  setupRoute,
  usersRoute,
  importRoute,
  settingsRoute,
  versionRoute,
  readerEntry,
  readerRoute,
]);

export const router = createRouter({
  routeTree,
  defaultPreload: "intent",
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
