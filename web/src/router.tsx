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

function RootShell() {
  const { loading, user, needsSetup } = useAuth();
  const router = useRouter();
  const routerState = useRouterState();
  const pathname = routerState.location.pathname;

  useEffect(() => {
    if (loading) return;
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
      const isPublic = PUBLIC_PATHS.includes(pathname) || ANON_OK_PATHS.includes(pathname) || isReader;
      if (!isPublic) {
        router.navigate({
          to: "/login",
          search: { redirect: pathname },
          replace: true,
        });
      }
    }
  }, [loading, user, needsSetup, pathname, router]);

  if (loading) {
    return (
      <AppLayout>
        <p className="text-muted-foreground">载入中…</p>
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
    redirect: typeof search.redirect === "string" ? search.redirect : undefined,
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

const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/settings",
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
    throw redirect({ to: "/r/$id/$chapter", params: { id: params.id, chapter: "0" }, replace: true });
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
