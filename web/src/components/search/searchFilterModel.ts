import type { Page } from "../../types";
import type { WorkspaceSearchResult } from "../../workspaceSearchWire.generated";
import type { SearchHit } from "./searchModel";

// Search narrows as Notion's search does: to titles only, to pages people
// created, to teamspaces, to pages inside chosen pages and their sub-pages,
// and to pages created, or last edited, today, in the last 7 or 30 days or
// between chosen days; and sorts by best match or by when pages were last
// edited or created. Days are the viewer's own.

export type SearchSort = "best" | "edited_desc" | "edited_asc" | "created_desc" | "created_asc";
export type SearchDatePreset = "any" | "today" | "week" | "month" | "custom";

export type SearchPerson = { userId: string; label: string };

/** Which time a date filter bounds: when a page was created or last edited, as Notion's "생성일 · 최종 편집". */
export type SearchDateField = "created" | "edited";

export type SearchFilters = {
  titleOnly: boolean;
  createdBy: SearchPerson[];
  teamspaces: string[];
  inPages: string[];
  date: { preset: SearchDatePreset; from?: string; to?: string; field?: SearchDateField };
  sort: SearchSort;
};

export const SEARCH_SORTS: SearchSort[] = ["best", "edited_desc", "edited_asc", "created_desc", "created_asc"];
export const SEARCH_DATE_PRESETS: SearchDatePreset[] = ["today", "week", "month"];

export const EMPTY_SEARCH_FILTERS: SearchFilters = {
  titleOnly: false, createdBy: [], teamspaces: [], inPages: [], date: { preset: "any" }, sort: "best",
};

function startOfDay(day: Date) {
  return new Date(day.getFullYear(), day.getMonth(), day.getDate());
}

function localDay(value: string) {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/u.exec(value);
  return match ? new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3])) : null;
}

/** The instants a date filter covers, from inclusive and before exclusive. */
export function searchDateRange(date: SearchFilters["date"], now: Date): { after?: Date; before?: Date } {
  const today = startOfDay(now);
  const daysAgo = (days: number) => new Date(today.getFullYear(), today.getMonth(), today.getDate() - days);
  switch (date.preset) {
    case "today":
      return { after: today };
    case "week":
      return { after: daysAgo(6) };
    case "month":
      return { after: daysAgo(29) };
    case "custom": {
      const from = date.from ? localDay(date.from) : null;
      const to = date.to ? localDay(date.to) : null;
      return {
        ...(from ? { after: from } : {}),
        ...(to ? { before: new Date(to.getFullYear(), to.getMonth(), to.getDate() + 1) } : {}),
      };
    }
    default:
      return {};
  }
}

/** Whether any filter or sort changes plain search. */
export function searchFiltersActive(filters: SearchFilters) {
  return filters.titleOnly || filters.createdBy.length > 0 || filters.teamspaces.length > 0 ||
    filters.inPages.length > 0 || filters.date.preset !== "any" || filters.sort !== "best";
}

/** The search request's filter parameters. */
export function searchFilterParams(filters: SearchFilters, now: Date): Record<string, string> {
  const params: Record<string, string> = {};
  if (filters.titleOnly) params.titleOnly = "true";
  if (filters.createdBy.length) params.createdBy = filters.createdBy.map((person) => person.userId).join(",");
  if (filters.teamspaces.length) params.teamspace = filters.teamspaces.join(",");
  if (filters.inPages.length) params.in = filters.inPages.join(",");
  const range = searchDateRange(filters.date, now);
  if (range.after) params.after = range.after.toISOString();
  if (range.before) params.before = range.before.toISOString();
  if (range.after || range.before) params.dateField = filters.date.field ?? "edited";
  if (filters.sort !== "best") params.sort = filters.sort;
  return params;
}

function pageTime(value: string | undefined) {
  return value ? Date.parse(value) || 0 : 0;
}

/** Whether a page the viewer has is one the filters keep. */
export function searchFiltersMatchPage(
  page: Page,
  filters: SearchFilters,
  pagesById: Readonly<Record<string, Page>>,
  now: Date,
) {
  if (filters.createdBy.length && !filters.createdBy.some((person) => person.userId === page.createdBy)) return false;
  if (filters.teamspaces.length || filters.inPages.length) {
    // Every page above, to the top however deep; one met twice ends it.
    const chain: Page[] = [];
    const seen = new Set<string>();
    for (let current: Page | undefined = page; current && !seen.has(current.id); current = current.parentId ? pagesById[current.parentId] : undefined) {
      seen.add(current.id);
      chain.push(current);
    }
    if (filters.inPages.length && !chain.some((item) => filters.inPages.includes(item.id))) return false;
    if (filters.teamspaces.length && !chain.some((item) => item.teamspaceId && filters.teamspaces.includes(item.teamspaceId))) {
      return false;
    }
  }
  const range = searchDateRange(filters.date, now);
  if (range.after || range.before) {
    const within = (at: number) => at > 0 && (!range.after || at >= range.after.getTime()) &&
      (!range.before || at < range.before.getTime());
    if (!within(pageTime((filters.date.field ?? "edited") === "created" ? page.createdAt : page.updatedAt))) return false;
  }
  return true;
}

/** Orders hits by the chosen time, keeping best matches as they are. */
export function sortSearchHits(hits: SearchHit[], sort: SearchSort) {
  if (sort === "best") return hits;
  const key = (hit: SearchHit) => pageTime(sort.startsWith("edited") ? hit.page.updatedAt : hit.page.createdAt);
  const direction = sort.endsWith("desc") ? -1 : 1;
  return [...hits].sort((left, right) => direction * (key(left) - key(right)) || left.page.id.localeCompare(right.page.id));
}

/**
 * Browsing without keywords lists the pages the viewer has with what the
 * server found, newest edits first unless another time sort is chosen.
 */
export function browseSearchHits(local: SearchHit[], remote: WorkspaceSearchResult[], sort: SearchSort) {
  const byPage = new Map<string, SearchHit>();
  for (const hit of local) byPage.set(hit.page.id, { ...hit, match: "title", preview: "" });
  for (const [index, hit] of remote.entries()) {
    if (!byPage.has(hit.page.id)) byPage.set(hit.page.id, { ...hit, match: "title", preview: "", score: 2 + index / 100 });
  }
  return sortSearchHits([...byPage.values()], sort === "best" ? "edited_desc" : sort);
}
