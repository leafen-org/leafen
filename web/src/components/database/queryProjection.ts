import type {
  DbProperty,
  DbView,
  FilterGroup,
  Page,
} from "@/lib/types";
import { normalizeFileAttachments } from "./files";
import { normalizePersonIds, personLabel } from "./people";
import { dateKey as normalizeDateKey } from "./dateUtils";
import { subitemFilterScope, type SubitemFilterScope } from "./subitemFilterScope";
import { orderViewProperties as orderSharedViewProperties } from "./viewPropertyOrder";
import {
  currentPageFilterValue as coreCurrentPageFilterValue,
  currentUserFilterValue as coreCurrentUserFilterValue,
  isCurrentPageFilterValue as coreIsCurrentPageFilterValue,
  isCurrentUserFilterValue as coreIsCurrentUserFilterValue,
} from "@/shared/database/queryFilterPolicy";
import { effectiveFilterGroup as coreEffectiveFilterGroup } from "@/shared/database/queryFilterGroups";
import type {
  QueryProperty,
  QueryViewConfig,
} from "@/shared/database/queryTypes";
import {
  admitQueryProperties,
  admitQueryProjectionLifecycle,
  admitQueryViewConfig,
  boundedQueryConfig,
  type QueryBounds,
} from "./queryBudget";
import { valueAsRead } from "./valueAsRead";

export const TABLE_INITIAL_LOAD_OPTIONS = [10, 25, 50, 100] as const;
/** The layouts that keep "처음 불러오기" off their own page (a feed's on any page). */
export const INITIAL_LOAD_VIEW_TYPES: ReadonlySet<string> = new Set(["table", "board", "list", "gallery", "timeline", "chart", "map"]);

/**
 * "처음 불러오기" as chosen on the view, when it was: every layout then shows
 * that many and that many more at each "더 불러오기", a board's columns and a
 * grouped table's groups each that many (Notion's, seen 2026-11-05 at 10, 25,
 * 50 and 100 on a table, list, gallery, board and grouped table). Left unchosen,
 * each layout keeps its own first count and step.
 */
export function chosenInitialLoadLimit(view?: Pick<DbView, "config">): number | undefined {
  const raw = Number(view?.config?.initialLoadLimit);
  return TABLE_INITIAL_LOAD_OPTIONS.includes(raw as (typeof TABLE_INITIAL_LOAD_OPTIONS)[number]) ? raw : undefined;
}
const DEFAULT_TABLE_INITIAL_LOAD_LIMIT = 50;

export interface ViewFilterSeedValues {
  title?: string;
  properties: Record<string, unknown>;
}

/** Context needed to resolve relation/formula/person values during a query. */
export interface QueryContext {
  props: DbProperty[];
  pagesById: Record<string, Page>;
  /** The related databases' properties, which a rollup and a formula read. */
  propsByDb?: Record<string, DbProperty[]>;
  currentPageId?: string;
  currentUserId?: string;
  bounds: QueryBounds;
}

export interface ApplyViewOptions {
  search?: string;
  currentPageId?: string;
  currentUserId?: string;
  propsByDb?: Record<string, DbProperty[]>;
}

export type CardViewSubitemPresentationMode = "card_property" | "flattened" | "disabled";

export interface CardViewSubitemPresentation {
  rows: Page[];
  mode: CardViewSubitemPresentationMode;
  /** Which of the kept rows a flattened list shows ("포함"). */
  scope: SubitemFilterScope;
  childCountByParent: ReadonlyMap<string, number>;
  /** The sub-items property each card lists them by, as a card property. */
  childrenPropertyId?: string;
}

/**
 * A board's, gallery's or calendar's rows with sub-items on, as Notion's
 * (seen 2026-10-11): as a card property (the default) the kept top rows,
 * each card listing its sub-items; as a flattened list the kept top rows,
 * rows or sub-items its "포함" says — the locked "상위 항목: 비어 있음"
 * or "비어 있지 않음" filter Notion shows — a parent with how many it holds;
 * disabled, every kept row on its own.
 */
export function projectCardViewSubitems(
  database: Pick<Page, "databaseFeatures">,
  view: Pick<DbView, "config" | "type">,
  rows: Page[],
  bounds: QueryBounds,
): CardViewSubitemPresentation {
  admitQueryProjectionLifecycle(rows, [database, view], bounds);
  const isCardView =
    view.type === "board" || view.type === "calendar" || view.type === "gallery";
  const subitems = database.databaseFeatures?.subitems;
  const displayMode = view.config?.subtasks?.displayMode ?? "show";
  if (!isCardView || subitems?.enabled !== true || displayMode === "disabled") {
    return { rows, mode: "disabled", scope: "parents_and_subitems", childCountByParent: new Map() };
  }

  const childCountByParent = new Map<string, number>();
  const exactChildCountByParent = new Map<string, number>();
  for (const row of rows) {
    if (row.subitemParentId) {
      childCountByParent.set(
        row.subitemParentId,
        (childCountByParent.get(row.subitemParentId) ?? 0) + 1
      );
    }
    if (
      typeof row.subitemChildCount === "number"
      && Number.isSafeInteger(row.subitemChildCount)
      && row.subitemChildCount >= 0
    ) {
      exactChildCountByParent.set(row.id, row.subitemChildCount);
    }
  }
  // The server scalar is exact even when the bounded window does not contain
  // every child; loaded-row counts are only a standalone/local fallback.
  for (const [parentId, count] of exactChildCountByParent) {
    childCountByParent.set(parentId, count);
  }
  const childrenPropertyId = subitems.childrenPropertyId;

  if (displayMode === "flattened") {
    const scope = subitemFilterScope(view.config);
    return {
      rows: scope === "parents" ? rows.filter((row) => !row.subitemParentId)
        : scope === "subitems" ? rows.filter((row) => !!row.subitemParentId) : rows,
      mode: "flattened", scope, childCountByParent, childrenPropertyId,
    };
  }

  // A card property, and the import's "hidden", show the kept top rows.
  return {
    rows: rows.filter((row) => !row.subitemParentId),
    mode: "card_property", scope: "parents", childCountByParent, childrenPropertyId,
  };
}

/**
 * The count a view added in "새 보기" is made with, as a table or a list 50
 * and as a feed 10: it goes with the view through its layouts until a "처음
 * 불러오기" is chosen, as Notion's (seen 2026-11-27, each in two tabs at
 * different times: a list or a table added and made a feed shows 50 posts,
 * a feed added and made a table 10 rows). A gallery or a board is made with
 * none, nor a database's first view or a linked view's first table: each
 * shows its layout's own (made a feed, each shows 10 posts).
 */
export function madeInitialLoadLimit(type: string): number | undefined {
  return type === "feed" ? FEED_INITIAL_LOAD_LIMIT : type === "table" || type === "list" ? DEFAULT_TABLE_INITIAL_LOAD_LIMIT : undefined;
}

/**
 * How many rows, cards or posts a view first shows, and shows more by: its
 * chosen "처음 불러오기", else the count it was made with, else its layout's
 * own (`fallback`). Its settings show the chosen count or the layout's own
 * alone (tableInitialLoadLimit), as Notion's do — its feed made from a list
 * says 10 and shows 50 posts.
 */
export function firstLoadLimit(view?: Pick<DbView, "config">, fallback: number = DEFAULT_TABLE_INITIAL_LOAD_LIMIT) {
  const made = Number(view?.config?.madeLoadLimit);
  return chosenInitialLoadLimit(view) ?? (TABLE_INITIAL_LOAD_OPTIONS.includes(made as (typeof TABLE_INITIAL_LOAD_OPTIONS)[number]) ? made : fallback);
}

export function tableInitialLoadLimit(view?: Pick<DbView, "config">, fallback: number = DEFAULT_TABLE_INITIAL_LOAD_LIMIT) {
  const raw = Number(view?.config?.initialLoadLimit);
  return TABLE_INITIAL_LOAD_OPTIONS.includes(raw as (typeof TABLE_INITIAL_LOAD_OPTIONS)[number])
    ? raw
    : fallback;
}

/**
 * A board first shows 25 cards in each column and 20 more in every column
 * at each "더 불러오기", as Notion's (seen 2026-10-01: 25, 45, then all 60);
 * a list or gallery first shows 50 and as many more, as a table does.
 */
export const BOARD_INITIAL_LOAD_LIMIT = 25;
/**
 * Whether a board's columns end in "더 불러오기" after the cards its "처음
 * 불러오기" says: inline, or split by a sub-group on its database's own page,
 * whose layout then offers that choice; an own page's board without one draws
 * more as a column's end nears the window's foot, 25 at first (Notion's, seen
 * 2026-11-07, and 2026-11-15: a sub-grouped board at 10 ended a cell of 16 in
 * "↓ 더 불러오기 ⋯").
 */
export function boardLoadsByButton(view: Pick<DbView, "config"> | undefined, ownPage: boolean) {
  return !ownPage || Boolean(view?.config?.subGroupBy);
}
/** A feed first shows 10 posts, as Notion's (seen 2026-10-05). */
export const FEED_INITIAL_LOAD_LIMIT = 10;
export const BOARD_LOAD_MORE_CARDS = 20;
/** How near the window's foot a database page's board column ends before
 * its next cards draw, as Notion's (seen 2026-11-07). */
export const BOARD_NEAR_END_DISTANCE = 550;

/** How many more rows a grouped inline table's group "더 불러오기" shows, as Notion's (seen 2026-10-01). */
export const TABLE_LOAD_MORE_ROWS = 50;
/** A grouped inline table's first rows in each group, as Notion's (seen 2026-10-01). */
export const GROUPED_TABLE_INITIAL_ROWS = 25;
/**
 * On a phone a database's own page shows each group's first 10 rows and 10
 * more at each "↓ 더 불러오기", as Notion's phone (seen 2026-11-15 at 402pt: a
 * grouped table and list 10, 20, then all; a board's columns 10, then more
 * as their end nears); a table's on a tablet too (an iPad's, 2026-11-16).
 */
export const PHONE_GROUP_ROWS = 10;

/**
 * A board's first cards in each column (or each cell of its sub-groups),
 * how many more each time, and whether a button draws them
 * (boardLoadsByButton): its "처음 불러오기" (25 unless set) and 20 more, or as
 * many as set; a database's own page without sub-groups 25 and 20 more as a
 * column's end nears the window's foot (BOARD_NEAR_END_DISTANCE). On a
 * phone's own page 10 and 10 more (PHONE_GROUP_ROWS), the number set winning
 * where it is offered, a column's next once its end reaches the window's
 * foot, as Notion's phone (seen 2026-11-15 at 402pt: a sub-grouped board's
 * cells 10 and "↓ 더 불러오기"; one without sub-groups 10 a column, more at its
 * end: 10, 20, 21).
 */
export function boardCardLoads(view: Pick<DbView, "config">, ownPage: boolean, phone: boolean) {
  const byButton = boardLoadsByButton(view, ownPage), chosen = byButton ? chosenInitialLoadLimit(view) : undefined;
  const step = phone && ownPage ? PHONE_GROUP_ROWS : undefined;
  return { byButton, first: chosen ?? step ?? BOARD_INITIAL_LOAD_LIMIT, more: chosen ?? step ?? BOARD_LOAD_MORE_CARDS,
    nearEnd: step === undefined ? BOARD_NEAR_END_DISTANCE : 0 };
}

/**
 * The rows an inline table shows, as Notion's (seen 2026-10-01): its first
 * `count` top rows with their sub-items, and any row made here since it
 * opened; and whether more wait behind "더 불러오기".
 */
/** How many of the rows count toward tableRowWindow's first ones: those whose parent isn't among them. */
export function topRowCount(rows: readonly { id: string; subitemParentId?: string | null }[]) {
  const ids = new Set(rows.map(({ id }) => id));
  let count = 0;
  for (const row of rows) if (!row.subitemParentId || !ids.has(row.subitemParentId)) count += 1;
  return count;
}

export function tableRowWindow<T extends { id: string; subitemParentId?: string | null }>(
  rows: readonly T[],
  count: number,
  made: ReadonlySet<string> = new Set(),
) {
  const byId = new Map(rows.map((row) => [row.id, row]));
  const topOf = (row: T) => {
    let current = row;
    for (let steps = 0; current.subitemParentId && byId.has(current.subitemParentId) && steps < rows.length; steps += 1) {
      current = byId.get(current.subitemParentId) as T;
    }
    return current.id;
  };
  const tops = rows.filter((row) => topOf(row) === row.id);
  const shown = new Set(tops.filter((row, index) => index < count || made.has(row.id)).map((row) => row.id));
  return { rows: rows.filter((row) => shown.has(topOf(row))), hasMore: shown.size < tops.length };
}

export function currentPageFilterValue() {
  return coreCurrentPageFilterValue();
}

export function isCurrentPageFilterValue(value: unknown) {
  return coreIsCurrentPageFilterValue(value);
}

export function currentUserFilterValue() {
  return coreCurrentUserFilterValue();
}

export function isCurrentUserFilterValue(value: unknown) {
  return coreIsCurrentUserFilterValue(value);
}

/** Raw value for a (row, property) pair. */
export function cellValue(row: Page, prop: DbProperty): unknown {
  if (prop.type === "title") return row.title;
  if (prop.type === "created_time") return row.createdAt;
  if (prop.type === "last_edited_time") return row.updatedAt;
  if (prop.type === "created_by") return row.createdBy;
  if (prop.type === "last_edited_by") return row.lastEditedBy;
  return valueAsRead(row, prop);
}

/** Property types that can be meaningfully filtered/sorted in the query layer. */
export function isQueryableProperty(_prop: DbProperty): boolean {
  return true;
}

function orderViewProperties(
  props: DbProperty[],
  view: DbView,
  bounds: QueryBounds,
): DbProperty[] {
  admitQueryProperties(props, { config: boundedQueryConfig(view.config, bounds) }, bounds);
  return orderSharedViewProperties(props, view.config?.propertyOrder);
}

export function visibleViewProperties(
  props: DbProperty[],
  view: DbView,
  bounds: QueryBounds,
): DbProperty[] {
  const legacyImportedGalleryAllowlist =
    view.type === "gallery"
    && view.config?.visibleProperties === undefined
    && view.config?.notionPropertySettings !== undefined
      ? []
      : undefined;
  const visible = view.config?.visibleProperties ?? legacyImportedGalleryAllowlist;
  const hidden = new Set(view.config?.hiddenProperties ?? []);
  if (visible && isImportedNotionViewConfig(view)) {
    const visibleIds = new Set(visible);
    const byId = new Map(props.map((prop) => [prop.id, prop]));
    const orderedVisible: DbProperty[] = [];
    for (const id of visible) {
      const prop = byId.get(id);
      if (prop) orderedVisible.push(prop);
    }
    const seen = new Set(orderedVisible.map((prop) => prop.id));
    for (const prop of orderViewProperties(props, view, bounds)) {
      if ((prop.type === "title" || visibleIds.has(prop.id)) && !seen.has(prop.id)) {
        orderedVisible.push(prop);
        seen.add(prop.id);
      }
    }
    return withTitlePropertyFirst(orderedVisible);
  }
  // A timeline's bars show their title unless it was hidden (viewTitleFree).
  return orderViewProperties(props, view, bounds).filter((prop) => {
    if (prop.type === "title") return view.type !== "timeline" || !hidden.has(prop.id);
    return visible ? visible.includes(prop.id) : !hidden.has(prop.id);
  });
}

function isImportedNotionViewConfig(view: DbView) {
  return (
    typeof view.config?.notionViewId === "string" ||
    Array.isArray(view.config?.notionPropertySettings)
  );
}

function withTitlePropertyFirst(props: DbProperty[]): DbProperty[] {
  const title = props.find((prop) => prop.type === "title");
  if (!title || props[0]?.id === title.id) return props;
  return [title, ...props.filter((prop) => prop.id !== title.id)];
}

export function asText(
  v: unknown,
  prop: DbProperty | undefined,
  personBounds: QueryBounds["personValue"],
): string {
  if (v == null) return "";
  if (prop?.type === "files") {
    return normalizeFileAttachments(v)
      .map((file) => `${file.name} ${file.url}`)
      .join(" ");
  }
  if (prop?.type === "place" && typeof v === "object" && !Array.isArray(v)) {
    const place = v as { name?: unknown; address?: unknown };
    return [place.name, place.address].filter((part) => typeof part === "string" && part.trim()).join(" ");
  }
  if (prop?.type === "person" || prop?.type === "created_by" || prop?.type === "last_edited_by") {
    return Array.from(new Set(normalizePersonIds(v, personBounds)))
      .map((id) => `${personLabel(id)} ${id}`)
      .join(" ");
  }
  if (Array.isArray(v)) return v.join(" ");
  return String(v);
}

export function optionIds(value: unknown): string[] {
  if (Array.isArray(value)) return value.map(String).filter(Boolean);
  return value ? [String(value)] : [];
}

export function rollupRelationTargetIds(row: Page, prop: DbProperty, ctx: QueryContext): string[] {
  const ids = new Set<string>();
  const relationPropId = prop.config?.rollupRelationPropertyId;
  const targetPropId = prop.config?.rollupTargetPropertyId;
  if (!relationPropId || !targetPropId) return [];

  for (const relatedId of optionIds(row.properties?.[relationPropId])) {
    const related = ctx.pagesById[relatedId];
    if (!related || related.inTrash) continue;
    for (const targetId of optionIds(related.properties?.[targetPropId])) {
      ids.add(targetId);
    }
  }
  return Array.from(ids);
}

export function optionLabel(prop: DbProperty, id: string) {
  return prop.config?.options?.find((option) => option.id === id)?.name ?? id;
}

export function effectiveFilterGroup(
  config: DbView["config"],
  byId: Map<string, DbProperty> | undefined,
  bounds: QueryBounds,
): FilterGroup | undefined {
  config = boundedQueryConfig(config, bounds);
  admitQueryViewConfig(config, bounds);
  return coreEffectiveFilterGroup(
    config as unknown as QueryViewConfig | undefined,
    byId as unknown as Map<string, QueryProperty> | undefined
  ) as unknown as FilterGroup | undefined;
}

export function isDateLike(prop: DbProperty) {
  return prop.type === "date" || prop.type === "created_time" || prop.type === "last_edited_time";
}

/** Normalize any date-ish value (ISO string, YYYY-MM-DD, timestamp) to YYYY-MM-DD. */
export function dateKey(value: unknown): string {
  return normalizeDateKey(value);
}
