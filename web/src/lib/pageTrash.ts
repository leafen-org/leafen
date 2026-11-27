import { pageHref } from "@/lib/navigation";
import { openedPagesNewestFirst, routeInfoFromPath } from "@/lib/router";
import type { Page } from "@/types";

/**
 * The trashed page that holds a page in the trash: the page itself or its
 * nearest ancestor in the trash. A sub-page goes to the trash with the page
 * that holds it, as the server reads it.
 */
export function pageTrashRoot<P extends { id: string; parentId?: string | null; inTrash?: boolean }>(
  page: P,
  pageById: (id: string) => P | undefined,
): P | null {
  // Up to the top however deep; each page once, so a chain that comes back
  // on itself ends.
  let current: P | undefined = page;
  const seen = new Set<string>();
  while (current) {
    if (seen.has(current.id)) return null;
    seen.add(current.id);
    if (current.inTrash) return current;
    current = current.parentId ? pageById(current.parentId) : undefined;
  }
  return null;
}

/** Whether the page is the given one or under it. */
function pageWithin(page: Page, ancestorId: string, pageById: (id: string) => Page | undefined): boolean {
  let current: Page | undefined = page;
  const seen = new Set<string>();
  while (current && !seen.has(current.id)) {
    if (current.id === ancestorId) return true;
    seen.add(current.id);
    current = current.parentId ? pageById(current.parentId) : undefined;
  }
  return false;
}

/**
 * Where the reader goes when a page goes to the trash, as Notion's: from the
 * page itself or a page under it, to the page that holds it (seen
 * 2026-10-06), or, when none does, to the page they viewed before, outside
 * the trash (seen 2026-10-28) — home where there is none; from anywhere else,
 * nowhere — they stay.
 */
export function routeAfterTrashing(
  trashed: Page,
  openPageId: string | null,
  pageById: (id: string) => Page | undefined,
  opened: readonly string[] = openedPagesNewestFirst(),
): string | null {
  const open = openPageId ? pageById(openPageId) : undefined;
  if (!open || !pageWithin(open, trashed.id, pageById)) return null;
  if (trashed.parentId) {
    const parent = pageById(trashed.parentId);
    return parent && !pageTrashRoot(parent, pageById) ? pageHref(parent.id) : "/";
  }
  for (const id of opened) {
    const before = pageById(id);
    if (before && !pageWithin(before, trashed.id, pageById) && !pageTrashRoot(before, pageById)) return pageHref(before.id);
  }
  return "/";
}

/**
 * Where the reader goes as a page goes to the trash from where they are now
 * (routeAfterTrashing), and where they were, which a restore brings them
 * back to: both none when they stay.
 */
export function routeLeavingTrashed(trashed: Page, pageById: (id: string) => Page | undefined) {
  const route = routeInfoFromPath(window.location.pathname);
  const openPageId = route.kind === "page" ? route.pageId : route.kind === "database" ? route.databaseId : null;
  const next = routeAfterTrashing(trashed, openPageId, pageById);
  return { next, left: next ? `${window.location.pathname}${window.location.search}` : null };
}
