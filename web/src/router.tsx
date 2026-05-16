import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
} from "@tanstack/react-router";
import { AppLayout } from "./components/AppLayout";
import { Library } from "./pages/Library";
import { ImportPage } from "./pages/Import";
import { Settings } from "./pages/Settings";
import { Reader } from "./pages/Reader";
import { Version } from "./pages/Version";
import { NotFound } from "./pages/NotFound";

const rootRoute = createRootRoute({
  component: () => (
    <AppLayout>
      <Outlet />
    </AppLayout>
  ),
  notFoundComponent: () => <NotFound />,
});

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: Library,
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
    window.location.replace(`/r/${params.id}/0`);
  },
  component: () => null,
});

const routeTree = rootRoute.addChildren([
  indexRoute,
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
