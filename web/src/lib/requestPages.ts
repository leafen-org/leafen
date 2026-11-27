// The pages a request is about, as the server's routes name them: in the
// path, a page, its room or its row page (pages, a workspace's pages, page
// links, collaboration state); in the body, for the few that take them there: analytics (with the
// pages it observed), presence, jobs and forms; in the query, the paths read
// above pages (page_path_route.go). Asking for a page to be made (POST
// /pages) names none.
const PAGE_IN_PATH = /^\/(?:pages|workspaces\/[^/?#]+\/pages|page-links|collaboration-state|collaboration-status|database-rows)\/([^/?#]+)/u;
const PAGES_IN_BODY = /^\/(?:page-analytics|page-left|page-past-viewers|jobs|forms)(?:[?#]|$)/u;
// A page made here whose path was read before the server had it read as one
// with nothing above it, and kept that: a page just made with "/페이지" in a
// database's page showed only its own name in its bar, and ⌘⇧U went nowhere,
// until the window was reloaded (2026-11-18, seen once under load).
const PAGES_IN_QUERY = /^\/workspaces\/[^/?#]+\/page-paths\?(?:[^#]*&)?ids=([^&#]*)/u;

function decoded(id: string): string[] {
  try {
    return id ? [decodeURIComponent(id)] : [];
  } catch {
    return [];
  }
}

/**
 * The pages a request names, which a request about a page made here waits
 * for the server to have made (apiTransport.ts, lib/pagesNotYetMade.ts).
 */
export function pagesOfRequest(path: string, body: unknown): string[] {
  const named = PAGE_IN_PATH.exec(path)?.[1];
  if (named) return decoded(named);
  const listed = PAGES_IN_QUERY.exec(path)?.[1];
  if (listed) return listed.split(",").flatMap(decoded);
  if (typeof body !== "string" || !PAGES_IN_BODY.test(path)) return [];
  let value: unknown;
  try {
    value = JSON.parse(body);
  } catch {
    return [];
  }
  const pages = new Set<string>();
  const take = (item: unknown) => {
    const pageId: unknown = item && typeof item === "object" ? Reflect.get(item, "pageId") : undefined;
    if (typeof pageId === "string" && pageId) pages.add(pageId);
  };
  take(value);
  for (const field of value && typeof value === "object" ? Object.values(value) : []) {
    if (Array.isArray(field)) field.forEach(take);
  }
  return [...pages];
}
