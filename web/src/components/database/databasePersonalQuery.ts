import type { GraphView } from "@/contentGraph";

// A view's filters and sorts that a viewer changes apply for them alone
// until an editor saves them for everyone, as Notion's do; the view's other
// settings save for everyone as they change.

const QUERY_KEYS = ["filterGroup", "filters", "filterConjunction", "sorts", "quickFilters"] as const;

/** The filters and sorts one viewer applies to a view. */
export type PersonalQuery = Partial<Record<(typeof QUERY_KEYS)[number], unknown>>;

type ViewConfig = GraphView["config"];

/** A view's filters and sorts, as its settings hold them. */
export function viewQuery(config: ViewConfig): PersonalQuery {
  const query: PersonalQuery = {};
  for (const key of QUERY_KEYS) {
    if (config[key] !== undefined) query[key] = config[key];
  }
  return query;
}

function withoutQuery(config: ViewConfig): ViewConfig {
  const rest: Record<string, unknown> = { ...config };
  for (const key of QUERY_KEYS) delete rest[key];
  delete rest.search;
  return rest as ViewConfig;
}

/** A view's settings with the given filters and sorts. */
export function withViewQuery(config: ViewConfig, query: PersonalQuery): ViewConfig {
  return { ...withoutQuery(config), ...query } as ViewConfig;
}

/**
 * A view a screen writes from the view it shows, as it is saved: without the
 * toolbar's search, which is the screen's alone and no setting of the view,
 * and, given the view as saved (`saved`), with its own filters and sorts in
 * place of a viewer's. A search spread into a view's other changes was saved
 * with them, and narrowed the view for everyone (2026-11-15: a row made while
 * searching saved the search with its place).
 */
export function viewAsSaved(next: GraphView, saved?: GraphView): GraphView {
  return { ...next, config: withViewQuery(next.config, viewQuery((saved ?? next).config)) };
}

function emptyGroup(value: unknown): boolean {
  if (!value || typeof value !== "object") return true;
  const group = value as { filters?: unknown[]; groups?: unknown[] };
  return !group.filters?.length && (group.groups ?? []).every(emptyGroup);
}

function stable(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(stable).join(",")}]`;
  if (value && typeof value === "object") {
    return `{${Object.keys(value).filter((key) => (value as Record<string, unknown>)[key] !== undefined).sort()
      .map((key) => `${JSON.stringify(key)}:${stable((value as Record<string, unknown>)[key])}`).join(",")}}`;
  }
  return JSON.stringify(value) ?? "null";
}

function queryKey(query: PersonalQuery): string {
  const kept: Record<string, unknown> = {};
  for (const key of QUERY_KEYS) {
    const value = query[key];
    if (value === undefined || value === null || Array.isArray(value) && !value.length) continue;
    if (key === "filterGroup" && emptyGroup(value)) continue;
    kept[key] = value;
  }
  if (kept.filters === undefined && kept.filterGroup === undefined) delete kept.filterConjunction;
  return stable(kept);
}

/** Whether two sets of filters and sorts show the same rows in the same order. */
export function sameViewQuery(left: PersonalQuery, right: PersonalQuery): boolean {
  return queryKey(left) === queryKey(right);
}

/**
 * Splits a change to a view a viewer sees with their own filters and sorts:
 * the filters and sorts they now apply (null when they match the view's
 * again, undefined when unchanged), and the view to save for everyone when
 * its other settings changed, keeping the view's own filters and sorts.
 */
export function splitViewChange(saved: GraphView, seen: GraphView, next: GraphView): {
  own: PersonalQuery | null | undefined;
  save: GraphView | null;
} {
  const nextQuery = viewQuery(next.config);
  const own = sameViewQuery(nextQuery, viewQuery(seen.config)) ? undefined
    : sameViewQuery(nextQuery, viewQuery(saved.config)) ? null : nextQuery;
  const rest = (view: GraphView) => stable({ ...view, config: withoutQuery(view.config) });
  return { own, save: rest(next) === rest(seen) ? null : viewAsSaved(next, saved) };
}
