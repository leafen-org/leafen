/** Loose property shape — both web's strict `DbProperty` and backend's loose one satisfy it. */
export interface QueryProperty {
  id: string;
  type: string;
  config?: Record<string, unknown> | null;
}

/** Loose page/row shape. Values are read through `QueryAdapters.cellValue`. */
export interface QueryPage {
  id: string;
  title?: string;
  properties?: Record<string, unknown> | null;
  inTrash?: boolean;
}

export interface QueryFilter {
  propertyId: string;
  operator: string;
  value?: unknown;
  /** Which day of a date property a date filter reads: its end, or else its start. */
  datePart?: string;
  /** Which of a rollup's related values the condition is about: "none" or "every", else at least one. */
  rollupMatch?: string;
  /** The kind of value a filter over a rollup's related values names, kept where it isn't theirs (Notion's API's any, every or none). */
  rollupKind?: string;
}

export interface QueryFilterGroup {
  conjunction: string;
  filters: QueryFilter[];
  groups?: QueryFilterGroup[];
}

/** The view-config subset the effective-filter-group derivation reads. */
export interface QueryViewConfig {
  filterGroup?: QueryFilterGroup;
  filters?: QueryFilter[];
  filterConjunction?: string;
  quickFilters?: Array<QueryFilter | QueryFilterGroup>;
}

export interface QueryAdapters {
  /** Raw value for a (row, property) pair (incl. computed formula/rollup). */
  cellValue: (row: QueryPage, prop: QueryProperty) => unknown;
  /** Human-readable text for the row's value (search/text-filter/string-sort). */
  displayText: (row: QueryPage, prop: QueryProperty) => string;
  /** Text for an arbitrary value (filter query values, files/person rendering). */
  asText: (value: unknown, prop?: QueryProperty) => string;
  /** Extract person ids from a person/created_by/last_edited_by value. */
  personIds: (value: unknown) => string[];
  /** Leaf ids a rollup relation reaches, for rollup relation filters. */
  rollupTargetIds: (row: QueryPage, prop: QueryProperty) => string[];
  /** A rollup's related values one by one with the property they're of; null when they can't be read here. */
  rollupLeaves?: (row: QueryPage, prop: QueryProperty) => { target: QueryProperty; leaves: { value: unknown; text: string }[] } | null;
  /**
   * The sprints a sprint relation's "현재", "다음" or "마지막" stands for;
   * undefined while they aren't known here, the rows staying as the server
   * filtered them.
   */
  relativeSprintIds?: (prop: QueryProperty, which: string) => string[] | undefined;
  /** The page the view sits in, its filters' "this page"; with none (a template's body) they name PAGE_NO_ROW_HOLDS. */
  currentPageId?: string;
  /** Authenticated principal resolved once for this query lifecycle. */
  currentUserId?: string;
  /**
   * IANA timezone used to resolve the *calendar day* of absolute-instant date
   * values (created_time/last_edited_time and zoned datetimes). The web app
   * passes the browser zone so that date filtering/sorting/grouping agree with
   * the local time the cell actually displays; when omitted the UTC day is used
   * (the correct default for headless/backend callers).
   */
  timeZone?: string;
  /** One in-memory clock snapshot shared by every row in this evaluation. */
  referenceTime?: Date;
  /** Durable calendar-day snapshot restored from a paged continuation. */
  referenceDay?: string;
}

/**
 * The page a view's "this page" names where the view sits in none — a
 * template's body (FilterThisPage `template`, NativeDatabaseViewMount): an id
 * no row holds, so a relation filter "contains this page" keeps no row and
 * "does not contain" keeps every one, as Notion's template views show them
 * (seen 2026-11-20, desktop and phone: a row with the relation empty not
 * shown). A row made there takes nothing for it (querySeeds.ts reads none).
 */
export const PAGE_NO_ROW_HOLDS = "leafen.page_no_row_holds";
