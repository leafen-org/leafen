import { pageAddressId } from "@/lib/inlineDatabaseAddress";
import { serverUrl } from "@/lib/serverOrigin";

const SITE_NAVIGATION_TOKEN_PREFIX = "leafen-site-path:";

export function siteNavigationToken(basePath: string) {
  return `${SITE_NAVIGATION_TOKEN_PREFIX}${encodeURIComponent(basePath)}`;
}

function sitePathFromNavigationToken(token: string) {
  if (!token.startsWith(SITE_NAVIGATION_TOKEN_PREFIX)) return null;
  try {
    const path = decodeURIComponent(token.slice(SITE_NAVIGATION_TOKEN_PREFIX.length));
    return path === "/" || /^\/site\/[^/?#]+$/.test(path) ? path : null;
  } catch {
    return null;
  }
}

function normalizeSearch(search?: string | URLSearchParams | null) {
  if (!search) return new URLSearchParams();
  if (typeof search === "string") {
    return new URLSearchParams(search.startsWith("?") ? search.slice(1) : search);
  }
  return new URLSearchParams(search);
}

/**
 * A page's address: /p/ and its ID, a full-page database's its database's,
 * as Notion's one address, which its turning inline and back keeps
 * (inlineDatabaseAddress.ts). An empty query — none, "", "?" or no
 * parameters — adds nothing: no bare "?" ends the address.
 */
export function pageHref(pageId: string, search?: string | URLSearchParams | null) {
  const path = `/p/${encodeURIComponent(pageAddressId(pageId))}`;
  const query = search ? String(search).replace(/^\?/, "") : "";
  return query ? `${path}?${query}` : path;
}

/** A page's own address in full; one to copy carries the view the address names (pageLinkToCopy). */
export function absolutePageUrl(pageId: string) {
  const href = pageHref(pageId);
  if (typeof window === "undefined") return href;
  return serverUrl(href);
}

export function sharedPageHref(
  token: string,
  pageId?: string | null,
  search?: string | URLSearchParams | null
) {
  const path = sitePathFromNavigationToken(token) ?? `/share/${encodeURIComponent(token)}`;
  const params = normalizeSearch(search);
  if (pageId) params.set("page", pageId);
  else params.delete("page");
  const suffix = params.toString();
  return suffix ? `${path}?${suffix}` : path;
}
