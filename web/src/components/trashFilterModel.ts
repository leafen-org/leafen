import type { Page } from "@/types";

export type TrashPerson = { userId: string; label: string };
export type TrashFilterState = { editors: TrashPerson[]; inPages: string[]; teamspaces: string[] };

/** The trash's filters start with the reader's own pages, as Notion's do. */
export function initialTrashFilters(reader: TrashPerson): TrashFilterState {
  return { editors: [reader], inPages: [], teamspaces: [] };
}

/** Whether a trashed page passes the trash's filters. */
export function trashFilterMatches(page: Page, filters: TrashFilterState, pagesById: Record<string, Page>) {
  const editor = page.lastEditedBy || page.createdBy;
  if (filters.editors.length && !filters.editors.some((person) => person.userId === editor)) return false;
  if (filters.teamspaces.length && !filters.teamspaces.includes(page.teamspaceId ?? "")) return false;
  if (filters.inPages.length) {
    const seen = new Set<string>();
    let parentId = page.parentId;
    while (parentId && !seen.has(parentId) && !filters.inPages.includes(parentId)) {
      seen.add(parentId);
      parentId = pagesById[parentId]?.parentId ?? null;
    }
    if (!parentId || !filters.inPages.includes(parentId)) return false;
  }
  return true;
}

/**
 * The trash's search, as Notion's (seen 2026-11-07): the words typed are
 * looked for apart, and a line shows with any of them — `trashSearchHits`
 * counts how many it holds, 0 leaving it out (1 for every line while nothing
 * is typed).
 */
export function trashSearchHits(query: string, text: string) {
  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  if (!words.length) return 1;
  const haystack = text.toLowerCase();
  return words.filter((word) => haystack.includes(word)).length;
}

/** The lines a search keeps, more of its words first — all of them on top — the trash's own order among equals. */
export function searchTrash<T>(items: readonly T[], query: string, text: (item: T) => string): T[] {
  return items
    .map((item, index) => ({ item, index, hits: trashSearchHits(query, text(item)) }))
    .filter((entry) => entry.hits > 0)
    .sort((a, b) => b.hits - a.hits || a.index - b.index)
    .map((entry) => entry.item);
}

/**
 * Where a line stands in the trash, in the milliseconds the server counts:
 * a time to the nearest millisecond, as the server rounds a page's (the
 * time its database stands at, trashedPageTreeCTE) — read to the millisecond
 * below, a line came after its page's half the time (2026-11-12) — written
 * with a "T" or, as a page's last edit is, a space.
 */
export function trashLineMoment(at: string | number | null | undefined): number {
  if (typeof at === "number") return at;
  const text = (at ?? "").replace(/^(\d{4}-\d\d-\d\d) /, "$1T");
  const parts = /^(.*\d\d:\d\d:\d\d)\.(\d+)(.*)$/.exec(text);
  if (!parts) return Date.parse(text) || 0;
  const whole = Date.parse(parts[1] + parts[3]);
  return Number.isNaN(whole) ? 0 : whole + Math.round(Number(`0.${parts[2]}`) * 1000);
}
