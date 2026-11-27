import type { RouteInfo } from "@/lib/router";

export type AuthenticatedProductRouteResolution =
  | "app"
  | "apps"
  | "external"
  | "home"
  | "invalid"
  | "join"
  | "library"
  | "loading"
  | "not-found"
  | "page"
  | "people"
  | "shared"
  | "trash";

export function resolveAuthenticatedProductRoute(
  route: RouteInfo,
  selectedPageId: string | null,
  bootstrapLoaded: boolean,
): AuthenticatedProductRouteResolution {
  // The settings' former addresses open the settings window over home.
  if (route.kind === "home" || route.kind === "workspace" || route.kind === "settings" || route.kind === "account") return "home";
  if (route.kind === "trash") return "trash";
  if (route.kind === "library") return "library";
  if (route.kind === "people") return "people";
  if (route.kind === "app") return "app";
  if (route.kind === "apps") return "apps";
  if (route.kind === "join") return "join";
  if (route.kind === "page" || route.kind === "database") {
    const requestedPageId = route.kind === "page" ? route.pageId : route.databaseId;
    if (selectedPageId === requestedPageId) return "page";
    return bootstrapLoaded ? "not-found" : "loading";
  }
  if (route.kind === "invalid") {
    return route.routeKind === "share" ? "shared" : "invalid";
  }
  if (route.kind === "unknown") return "not-found";
  return "external";
}
