import { routeInfoFromPath } from "./router";

export function requestedPageIdFromLocation(location = window.location) {
  const route = routeInfoFromPath(location.pathname);
  if (route.kind === "page") return route.pageId;
  if (route.kind === "database") return route.databaseId;
  return new URL(location.href).searchParams.get("page");
}

/**
 * Whether the screen at this address draws a page: a page's or a database's
 * own. Every other screen — home, the library, my tasks, apps and an app,
 * people, the developer tools, the trash's and the settings' addresses —
 * holds no page open (useAuthenticatedBootstrap.ts).
 */
export function screenDrawsPage(pathname = window.location.pathname) {
  const { kind } = routeInfoFromPath(pathname);
  return kind === "page" || kind === "database";
}
