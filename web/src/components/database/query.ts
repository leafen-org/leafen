import { rowsInViewOrder } from "./viewRowOrder";
import { withRowTimestampFilterProperties } from "./rowTimestampFilters";
import { relativeSprintIds } from "./databaseSprintContext";
import type { DbProperty, DbView, FilterGroup, Page } from "@/lib/types";
import { resolvedViewerTimeZone } from "@/lib/timeZone";
import { foldViewSearchText } from "@/lib/viewSearchText";
import { effectiveFilterGroup as coreEffectiveFilterGroup, matchesFilterGroup as coreMatchesFilterGroup } from "@/shared/database/queryFilterGroups";
import { compareKeys as coreCompareKeys, sortKey as coreSortKey, sortKeyRank } from "@/shared/database/querySortPolicy";
import type {
  QueryAdapters,
  QueryFilterGroup,
  QueryPage,
  QueryProperty,
} from "@/shared/database/queryTypes";
import { admitQueryRequest, boundedQueryConfig, type QueryBounds } from "./queryBudget";
import {
  asText,
  cellValue,
  dateKey,
  isDateLike,
  optionIds,
  optionLabel,
  rollupRelationTargetIds,
  type ApplyViewOptions,
  type QueryContext,
} from "./queryProjection";
import { normalizePersonIds } from "./people";
import { evaluateFormula, evaluateRollupCellValue, formatFormulaValue } from "./formula";
import { formatDateForProperty } from "./dateUtils";

export * from "./queryProjection";
export * from "./querySeeds";
export type { QueryBounds } from "./queryBudget";

function displayText(v: unknown, prop: DbProperty, ctx: QueryContext, row?: Page): string {
  if (prop.type === "select" || prop.type === "multi_select" || prop.type === "status") {
    const ids = prop.type === "multi_select" ? Array.from(new Set(optionIds(v))) : optionIds(v);
    return ids.map((id) => optionLabel(prop, id)).join(" ");
  }
  if (prop.type === "checkbox") return v ? "checked true yes" : "unchecked false no";
  if (prop.type === "unique_id") {
    if (v == null || v === "") return "";
    const prefix = prop.config?.idPrefix?.trim();
    return prefix ? `${prefix}-${v}` : String(v);
  }
  if (isDateLike(prop)) return dateKey(v);
  if (prop.type === "relation") {
    return optionIds(v)
      .map((id) => ctx.pagesById[id]?.title || "")
      .filter(Boolean)
      .join(" ");
  }
  if (prop.type === "formula" && row) {
    return formatFormulaValue(
      evaluateFormula({
        row,
        prop,
        props: ctx.props,
        pagesById: ctx.pagesById,
        propsByDb: ctx.propsByDb,
        bounds: ctx.bounds.formula,
      }),
      ctx.bounds.formula,
    );
  }
  return asText(v, prop, ctx.bounds.personValue);
}

// A date is searched by the words its cell shows, as Notion's (seen
// 2026-11-15 at 1440 and on a phone: "5" finds 2026년 11월 13일 → 2026년 11월
// 15일, "13일" finds it too); its ISO start alone ("2026-11-13") found neither.
function searchableText(row: Page, prop: DbProperty, ctx: QueryContext): string {
  const value = cellValue(row, prop);
  return prop.type === "date" ? formatDateForProperty(value, prop) : displayText(value, prop, ctx, row);
}

/** The text a row's property shows, as the database's own search reads it. */
/** A sort's direction; a view written through Notion's words may read "descending". */
export function sortIsDescending(direction: string) {
  return direction === "desc" || direction === "descending";
}

// Notion sorts a rollup only by a number it counts or works out: not by its
// values shown as they are nor by a date (seen 2026-11-05: "원래대로 표시"
// and "최근 날짜" rollups are missing from "정렬 기준"; 2026-11-12 on a phone,
// from "새 정렬" and "정렬 추가" alike).
const UNSORTED_ROLLUPS = new Set(["show_original", "show_unique", "earliest_date", "latest_date", "date_range"]);

/** Whether a view can sort by the property: every list a sort is picked from, a first sort's, "정렬 추가"'s and a sort's own property, on any screen. */
export function sortableProperty(property: { type: string; config?: unknown }) {
  if (property.type === "place" || property.type === "button") return false;
  if (property.type !== "rollup") return true;
  const fn = (property.config as { rollupFunction?: string } | undefined)?.rollupFunction ?? "show_original";
  return !UNSORTED_ROLLUPS.has(fn);
}

export function propertySearchText(row: Page, prop: DbProperty, ctx: QueryContext): string {
  return searchableText(row, prop, ctx);
}

// The viewer's IANA timezone, resolved once. Passed to the shared engine so an
// absolute-instant date (created_time/last_edited_time, stored as "…Z") is
// filtered/sorted on the same local calendar day the cell displays — without it
// a KST user's post-15:00-UTC row keys to the previous day and drops out of a
// "created today" filter.
const viewerTimeZone = resolvedViewerTimeZone();

// What a filter or a sort reads of a formula or a rollup: the value its cell
// works out. The rest are stored.
function computedCellValue(row: Page, prop: DbProperty, ctx: QueryContext): unknown {
  const stored = cellValue(row, prop);
  if (stored != null || (prop.type !== "formula" && prop.type !== "rollup")) return stored;
  try {
    if (prop.type === "formula") {
      const value = evaluateFormula({ row, prop, props: ctx.props, pagesById: ctx.pagesById, propsByDb: ctx.propsByDb, bounds: ctx.bounds.formula });
      return value == null || typeof value === "number" || typeof value === "boolean" || typeof value === "string"
        ? value : formatFormulaValue(value, ctx.bounds.formula);
    }
    const relation = ctx.props.find((candidate) => candidate.id === prop.config?.rollupRelationPropertyId);
    const targetId = relation?.config?.relationDatabaseId ?? prop.databaseId;
    return evaluateRollupCellValue({ row, prop, sourceProps: ctx.props, pagesById: ctx.pagesById, propsByDb: ctx.propsByDb,
      targetProps: ctx.propsByDb?.[targetId] ?? (targetId === prop.databaseId ? ctx.props : []), bounds: ctx.bounds.formula });
  } catch {
    // A value too large to work out reads as nothing, as its cell shows.
    return undefined;
  }
}

// Web adapters over the shared filter/sort engine (shared/database/query-core.ts):
// injects web value-reading (`cellValue` → computedCellValue, which works out
// formulas and rollups here), display text, date-key, person-id, and
// rollup-target resolution. The operator
// predicates, group evaluation, and sort keys live in the shared core.
function webAdapters(ctx: QueryContext): QueryAdapters {
  return {
    cellValue: (row, prop) => computedCellValue(row as Page, prop as DbProperty, ctx),
    displayText: (row, prop) =>
      displayText(computedCellValue(row as Page, prop as DbProperty, ctx), prop as DbProperty, ctx, row as Page),
    asText: (value, prop) => asText(
      value,
      prop as DbProperty | undefined,
      ctx.bounds.personValue,
    ),
    personIds: (value) => normalizePersonIds(value, ctx.bounds.personValue),
    rollupTargetIds: (row, prop) => rollupRelationTargetIds(row as Page, prop as DbProperty, ctx),
    rollupLeaves: (row, prop) => rollupLeaves(row as Page, prop as DbProperty, ctx) as unknown as ReturnType<NonNullable<QueryAdapters["rollupLeaves"]>>,
    relativeSprintIds: (prop, which) => relativeSprintIds(prop.config?.relationDatabaseId, which),
    currentPageId: ctx.currentPageId,
    currentUserId: ctx.currentUserId,
    timeZone: viewerTimeZone,
    referenceTime: new Date(Date.now()),
  };
}

/** A rollup's related values one by one, each as the property rolled up reads it. */
function rollupLeaves(row: Page, prop: DbProperty, ctx: QueryContext) {
  const relation = ctx.props.find(({ id }) => id === prop.config?.rollupRelationPropertyId);
  const databaseId = relation?.config?.relationDatabaseId ?? relation?.databaseId;
  const props = databaseId ? ctx.propsByDb?.[databaseId] : undefined;
  const target = props?.find(({ id }) => id === prop.config?.rollupTargetPropertyId);
  if (!relation || !props || !target || target.type === "rollup") return null;
  const leaves: { value: unknown; text: string }[] = [];
  const related = { ...ctx, props };
  for (const id of optionIds(row.properties?.[relation.id])) {
    const page = ctx.pagesById[id];
    if (!page) return null;
    if (page.inTrash) continue;
    const value = computedCellValue(page, target, related);
    leaves.push({ value, text: displayText(value, target, related, page) });
  }
  return { target, leaves };
}

/**
 * Recursively evaluate a nested AND/OR filter tree against a row. Leaf conditions
 * (`group.filters`) and nested sub-groups (`group.groups`) are combined with
 * `group.conjunction`: "and" requires every term, "or" requires some term. Leaves
 * whose property is missing from `byId` are skipped. An empty group matches every row.
 */
export function matchesFilterGroup(
  row: Page,
  group: FilterGroup,
  ctx: QueryContext,
  byId: Map<string, DbProperty>,
  bounds: QueryBounds,
): boolean {
  if (byId.size > bounds.maxProperties) {
    throw new RangeError("Database query properties exceed their caller-owned bound.");
  }
  group = boundedQueryConfig({ filterGroup: group }, bounds).filterGroup;
  admitQueryRequest(
    [row],
    Array.from(byId.values()),
    { config: { filterGroup: group } } as Pick<DbView, "config">,
    {},
    {},
    bounds,
  );
  return coreMatchesFilterGroup(
    row as unknown as QueryPage,
    group as unknown as QueryFilterGroup,
    webAdapters(ctx),
    byId as unknown as Map<string, QueryProperty>
  );
}

/** A row as a view orders it: its sort keys and its place in the view's own order. */
export type ViewRowEntry = { row: Page; index: number; keys: Array<number | string> };

/**
 * What a view keeps of its rows and how it orders two, as applyView reads
 * them, for one row at a time.
 */
export function viewRowQuery(
  props: DbProperty[],
  view: DbView,
  pagesById: Record<string, Page>,
  options: ApplyViewOptions,
  bounds: QueryBounds,
) {
  // A view saved with more filters than a query takes shows with its first.
  view = { ...view, config: boundedQueryConfig(view.config, bounds) };
  const ctx: QueryContext = {
    props,
    pagesById,
    propsByDb: options.propsByDb,
    currentPageId: options.currentPageId,
    currentUserId: options.currentUserId,
    bounds,
  };
  const adapters = webAdapters(ctx);
  const byId = new Map(props.map((property) => [property.id, property]));
  // Its filter may name a row's page timestamps too (rowTimestampFilters.ts).
  const coreById = withRowTimestampFilterProperties(byId) as unknown as Map<string, QueryProperty>;
  // Its search compares cells accents aside, as Notion's (foldViewSearchText).
  const search = foldViewSearchText((options.search ?? view.config?.search ?? "").trim());
  const filterGroup = coreEffectiveFilterGroup(view.config, coreById);
  const sortDescriptors = (view.config?.sorts ?? []).flatMap((sort) => {
    const property = byId.get(sort.propertyId);
    return property ? [{ sort, property: property as unknown as QueryProperty }] : [];
  });
  const keeps = (row: Page) => (!search || props.some((property) => (
    foldViewSearchText(searchableText(row, property, ctx)).includes(search)
  ))) && (!filterGroup || coreMatchesFilterGroup(row as unknown as QueryPage, filterGroup, adapters, coreById));
  const entry = (row: Page, index: number): ViewRowEntry => ({
    row,
    index,
    keys: sortDescriptors.map(({ property }) => coreSortKey(row as unknown as QueryPage, property, adapters)),
  });
  // Sorted rows that tie keep the view's own order (viewRowOrder.ts), as
  // Notion's (seen 2026-11-04); without sorts it is the order they show.
  const compare = (left: ViewRowEntry, right: ViewRowEntry) => {
    for (let index = 0; index < sortDescriptors.length; index += 1) {
      const leftRank = sortKeyRank(left.keys[index]), rightRank = sortKeyRank(right.keys[index]);
      if (leftRank !== rightRank) return leftRank - rightRank;
      const comparison = leftRank ? 0 : coreCompareKeys(left.keys[index], right.keys[index]);
      if (comparison === 0) continue;
      return sortIsDescending(sortDescriptors[index].sort.direction) ? -comparison : comparison;
    }
    return left.index - right.index;
  };
  return { view, keeps, entry, compare, sorted: sortDescriptors.length > 0 };
}

/** Apply a view's filters + sorts to the row set. */
export function applyView(
  rows: Page[],
  props: DbProperty[],
  view: DbView,
  pagesById: Record<string, Page> = {},
  options: ApplyViewOptions = {},
  bounds: QueryBounds,
): Page[] {
  // A view saved with more filters than a query takes shows with its first.
  view = { ...view, config: boundedQueryConfig(view.config, bounds) };
  admitQueryRequest(rows, props, view, pagesById, options, bounds);
  const query = viewRowQuery(props, view, pagesById, options, bounds);
  const ordered = rowsInViewOrder(rows, view.config as Record<string, unknown> | undefined);
  const collected: ViewRowEntry[] = [];
  for (let index = 0; index < ordered.length; index += 1) {
    if (query.keeps(ordered[index])) collected.push(query.entry(ordered[index], index));
  }
  if (query.sorted) collected.sort(query.compare);
  return collected.map(({ row }) => row);
}
