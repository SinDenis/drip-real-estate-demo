import { createRootRoute, createRoute, createRouter, Outlet } from "@tanstack/react-router";

import { App } from "./App";

const rootRoute = createRootRoute({ component: Outlet });
const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", component: App });
const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  component: App,
});
const sessionsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/sessions",
  component: App,
});
const newSessionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/sessions/new",
  component: App,
});
const sessionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/sessions/$sessionId",
  component: App,
});
const employeesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/employees",
  component: App,
});

const routeTree = rootRoute.addChildren([
  indexRoute,
  loginRoute,
  sessionsRoute,
  newSessionRoute,
  sessionRoute,
  employeesRoute,
]);

export const createAppRouter = () => createRouter({ routeTree, defaultPreload: "intent" });

export const router = createAppRouter();

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
