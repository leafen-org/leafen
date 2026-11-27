import { i18next } from "@/i18n";
import type { Page } from "@/types";
import { pageMapOf } from "./pageIndex";
import { actorLabel, LEAFEN_SYSTEM_USER_ID } from "./peopleDirectory";
import { dayLabel } from "./relativeTime";

export { pageLookup } from "./pageIndex";

/** Search shows active pages unless asked for archived ones, both, or the verified ones. */
export type WorkspaceSearchContentState = "active" | "archived" | "all" | "verified";

/**
 * The archived page that archives `page`: the page itself or its nearest
 * archived ancestor. Subpages are archived with their archived parent.
 */
export function pageArchiveRoot<P extends { id: string; parentId?: string | null; archivedAt?: string | null }>(
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
    if (current.archivedAt) return current;
    current = current.parentId ? pageById(current.parentId) : undefined;
  }
  return null;
}

/** Whether a page is in a search's content state: a verified one is live and its verification current (isPageVerified). */
export function searchContentStateMatches(
  contentState: WorkspaceSearchContentState,
  archived: boolean,
  verified: boolean,
) {
  if (contentState === "all") return true;
  if (contentState === "verified") return !archived && verified;
  return contentState === "archived" ? archived : !archived;
}

const archivedOfList = new WeakMap<readonly Page[], ReadonlySet<string>>();
const NO_ARCHIVED: ReadonlySet<string> = new Set();

/**
 * IDs of the pages archived themselves or with an archived parent page,
 * worked out once for a list (a list is never changed in place): each page's
 * way up is walked once, the library's every opening took it again (2026-11-15).
 */
export function archivedPageIds(pages: readonly Page[]): ReadonlySet<string> {
  const kept = archivedOfList.get(pages);
  if (kept) return kept;
  // A list with no page archived itself, as most are, is answered by one look
  // at each page: walking every way up took the library's first opening of a
  // 44,000-page workspace 18 ms at CPU ×2 (2026-11-15).
  if (!pages.some((page) => page.archivedAt)) {
    archivedOfList.set(pages, NO_ARCHIVED);
    return NO_ARCHIVED;
  }
  const byId = pageMapOf(pages);
  // Each page's answer once known; null while its way up is being walked (a
  // chain that comes back on itself ends there).
  const known = new Map<string, boolean | null>();
  const path: string[] = [];
  const ids = new Set<string>();
  for (const page of pages) {
    let result = known.get(page.id);
    if (result === undefined || result === null) {
      path.length = 0;
      result = false;
      let current: Page | undefined = page;
      while (current) {
        const cached = known.get(current.id);
        if (cached !== undefined) {
          result = cached ?? false;
          break;
        }
        path.push(current.id);
        known.set(current.id, null);
        if (current.archivedAt) {
          result = true;
          break;
        }
        current = current.parentId ? byId.get(current.parentId) : undefined;
      }
      for (const id of path) known.set(id, result);
    }
    if (result) ids.add(page.id);
  }
  archivedOfList.set(pages, ids);
  return ids;
}

/**
 * What an archived page's band says: who archived the page and on what day,
 * the day without its time (its year when another year's), and
 * a page an automation archived — Leafen's own archive, which only an
 * automation makes — as "Automation", as Notion's band names its automations
 * (seen 2026-11-15; a person's band 2026-10-11).
 */
export function archiveBandText(archivedBy: string | null | undefined, archivedAt: string | null | undefined, currentUserId?: string) {
  const actor = archivedBy === LEAFEN_SYSTEM_USER_ID ? i18next.t("pageArchive:automationActor") : actorLabel(archivedBy, currentUserId);
  return i18next.t("pageArchive:archivedBy", { actor, date: dayLabel(archivedAt) });
}
