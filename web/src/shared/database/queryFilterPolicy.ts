import { foldNfcText } from "./natural-order.mjs";
import {
  filterReferenceDay,
  dateEndValue,
  dateKey,
  dateMinute,
  dateOrderMatches,
} from "./queryDatePolicy";
import {
  isNoValueRelativeDateFilterOperator,
  isRelativeDateFilterOperator,
  isRelativeFilterDay,
  relativeDateFilterMatches,
  relativeFilterDay,
} from "./queryRelativeDate.mjs";
import { statusFilterOptionIds } from "./queryStatusPolicy.mjs";
import { canonicalRollupFunction } from "@/components/database/rollupPolicy";
import {
  PAGE_NO_ROW_HOLDS,
  type QueryAdapters,
  type QueryFilter,
  type QueryPage,
  type QueryProperty,
} from "./queryTypes";

/** A filter value that stands for the page or the person reading the database. */
const LEAFEN_CURRENT_PAGE_FILTER_KIND = "leafen.current_page";
const LEAFEN_CURRENT_USER_FILTER_KIND = "leafen.current_user";
/**
 * A sprint relation's value that stands for its current, next or last
 * sprints, as Notion's "현재", "다음" and "마지막" (seen 2026-11-04; the
 * server's database_query_relative_sprints.go).
 */
const LEAFEN_SPRINT_FILTER_KIND = "leafen.sprint";
export const RELATIVE_SPRINTS = ["current", "next", "last"] as const;
export type RelativeSprint = typeof RELATIVE_SPRINTS[number];

export function sprintFilterValue(sprint: RelativeSprint) {
  return { kind: LEAFEN_SPRINT_FILTER_KIND, sprint };
}

/** Which sprints a filter value stands for, when it is a relative one. */
export function relativeSprintOf(value: unknown): RelativeSprint | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const { kind, sprint } = value as { kind?: unknown; sprint?: unknown };
  return kind === LEAFEN_SPRINT_FILTER_KIND && RELATIVE_SPRINTS.includes(sprint as RelativeSprint) ? sprint as RelativeSprint : null;
}

function isLeafenCurrentPageFilterKind(value: unknown) {
  return value === LEAFEN_CURRENT_PAGE_FILTER_KIND;
}

function isLeafenCurrentUserFilterKind(value: unknown) {
  return value === LEAFEN_CURRENT_USER_FILTER_KIND;
}

export function currentPageFilterValue() {
  return { kind: LEAFEN_CURRENT_PAGE_FILTER_KIND };
}

export function isCurrentPageFilterValue(value: unknown) {
  return (
    !!value &&
    typeof value === "object" &&
    !Array.isArray(value) &&
    isLeafenCurrentPageFilterKind((value as { kind?: unknown }).kind)
  );
}

export function currentUserFilterValue() {
  return { kind: LEAFEN_CURRENT_USER_FILTER_KIND };
}

export function isCurrentUserFilterValue(value: unknown) {
  return (
    !!value &&
    typeof value === "object" &&
    !Array.isArray(value) &&
    isLeafenCurrentUserFilterKind((value as { kind?: unknown }).kind)
  );
}

function filterValueContainsCurrentUser(value: unknown) {
  return isCurrentUserFilterValue(value)
    || (Array.isArray(value) && value.some((item) => isCurrentUserFilterValue(item)));
}

export function resolveFilterValue(
  value: unknown,
  currentPageId?: string,
  currentUserId?: string,
  sprints?: (which: RelativeSprint) => string[] | undefined,
): unknown {
  if (isCurrentPageFilterValue(value)) return currentPageId ?? "";
  if (isCurrentUserFilterValue(value)) return currentUserId ?? "";
  const sprint = relativeSprintOf(value);
  if (sprint) return sprints?.(sprint) ?? [];
  if (!Array.isArray(value)) return value;
  return value.flatMap((item) => {
    if (isCurrentPageFilterValue(item)) return [currentPageId ?? ""];
    if (isCurrentUserFilterValue(item)) return [currentUserId ?? ""];
    const itemSprint = relativeSprintOf(item);
    if (itemSprint) return sprints?.(itemSprint) ?? [];
    return [item];
  });
}

export function optionIds(value: unknown): string[] {
  if (Array.isArray(value)) return value.map(String).filter(Boolean);
  return value ? [String(value)] : [];
}

function selectOptions(prop: QueryProperty): Array<{ id: string; name: string }> {
  const options = prop.config?.options;
  return Array.isArray(options) ? (options as Array<{ id: string; name: string }>) : [];
}

export function optionIndex(prop: QueryProperty, id: string): number {
  // Match by id or name (unified to backend's superset; web values are ids so
  // the name branch is a no-op there).
  const idx = selectOptions(prop).findIndex((option) => option.id === id || option.name === id);
  return idx < 0 ? Number.MAX_SAFE_INTEGER : idx;
}

function optionTargets(prop: QueryProperty, value: unknown): Set<string> {
  if (prop.type === "status") return new Set(statusFilterOptionIds(prop.config, value));
  const values = Array.isArray(value) ? value : [value];
  const targets = new Set<string>();
  for (const item of values) {
    const raw = String(item ?? "").trim();
    if (!raw) continue;
    const lower = raw.toLowerCase();
    targets.add(raw);
    for (const option of selectOptions(prop)) {
      if (option.id.toLowerCase() === lower || option.name.toLowerCase() === lower) {
        targets.add(option.id);
      }
    }
  }
  return targets;
}

export function isDateLike(prop: QueryProperty): boolean {
  return prop.type === "date" || prop.type === "created_time" || prop.type === "last_edited_time";
}

// A computed value that is a date: a formula's date result, {start: ...}.
const DAY_ORDER_OPERATORS = new Set(["before", "after", "on_or_before", "on_or_after"]);

function isDateObject(value: unknown): boolean {
  return !!value && typeof value === "object" && !Array.isArray(value) && typeof (value as { start?: unknown }).start === "string";
}

// A rollup either SURFACES its related leaf values (show_original/show_unique)
// or AGGREGATES them into a scalar (sum/average/min/max/range/count_*/percent_*/
// checked/unchecked/earliest_date/latest_date/date_range/…). Only surfacing
// rollups filter by relation membership over the individual leaf ids; aggregate
// rollups fall through to the numeric/date/text comparison paths that operate on
// the scalar cell value, so equals/does_not_equal agree with greater_than/less_than.
// One counting its values per group filters them as one showing them does,
// as Notion's (its API reads it as the values, seen 2026-11-11).
function isRelationSurfacingRollup(prop: QueryProperty): boolean {
  const raw = prop.config?.rollupFunction;
  const fn = canonicalRollupFunction(raw);
  return typeof raw !== "string" || fn === "show_original" || fn === "show_unique" || fn === "count_per_group" || fn === "percent_per_group";
}

/**
 * The kind of value Notion names a related value by (the server's
 * databaseRollupLeafKind): a filter naming another kind matches it only by
 * being empty or not (seen 2026-11-11).
 */
function rollupLeafKind(target: QueryProperty, value: unknown): string {
  switch (target.type) {
    case "title": case "rich_text": case "url": case "email": case "phone":
      return "rich_text";
    case "number": case "unique_id":
      return "number";
    case "date": case "created_time": case "last_edited_time":
      return "date";
    case "person": case "created_by": case "last_edited_by":
      return "people";
    case "formula":
      if (typeof value === "number") return "number";
      if (typeof value === "string") return "rich_text";
      if (typeof value === "boolean") return "checkbox";
      return isDateObject(value) ? "date" : "";
  }
  return target.type;
}

/**
 * Whether a filter still waits for its value: it doesn't filter yet, as a
 * new filter in Notion shows every row until a value is chosen (seen
 * 2026-09-30). "Empty", "not empty" and the fixed relative dates ("this
 * week") need no value. The server leaves such filters out the same way.
 */
/**
 * A "범위 내" date filter's days, either of which may be missing.
 */
export function dateRangeFilterValue(value: unknown): { start?: string; end?: string } {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {};
  const { start, end } = value as { start?: unknown; end?: unknown };
  return {
    ...(typeof start === "string" && start.trim() ? { start: start.trim() } : {}),
    ...(typeof end === "string" && end.trim() ? { end: end.trim() } : {}),
  };
}

export function filterAwaitsValue(filter: QueryFilter): boolean {
  if (filter.operator === "is_empty" || filter.operator === "is_not_empty") return false;
  if (isNoValueRelativeDateFilterOperator(filter.operator)) return false;
  if (filter.operator === "is_between") {
    const range = dateRangeFilterValue(filter.value);
    return !range.start && !range.end;
  }
  const value = filter.value;
  if (value == null) return true;
  if (typeof value === "string") return value.trim() === "";
  if (Array.isArray(value)) return value.length === 0;
  return false;
}

/** A formula's or rollup's date as a date value: its own, or its text ("2026-11-02", "2026-11-02 → 2026-11-09"). */
function computedDate(value: unknown): unknown {
  if (typeof value !== "string") return value;
  const [start, end] = value.split(" → ").map((part) => part.trim());
  const day = /^\d{4}-\d{2}-\d{2}(T[\d:.]+(Z|[+-]\d{2}:?\d{2})?)?$/;
  if (!day.test(start)) return value;
  return end && day.test(end) ? { start, end } : { start };
}

/**
 * A date's filter: its start, or its end (its start when it has none) when
 * the filter reads "종료일", as Notion's (seen 2026-10-01), from today, in a
 * range, empty or before, after or on a day.
 */
function dateFilterMatches(v: unknown, f: QueryFilter, a: QueryAdapters): boolean {
  const dv = dateKey(f.datePart === "end" ? dateEndValue(v) : v, a.timeZone);
  if (isRelativeDateFilterOperator(f.operator)) {
    return relativeDateFilterMatches(dv, f.operator, filterReferenceDay(a), f.value);
  }
  if (f.operator === "is_between") {
    // "범위 내" takes both days; with one, it runs open on the other side.
    const range = dateRangeFilterValue(f.value);
    const start = range.start ? dateKey(range.start, a.timeZone) : "";
    const end = range.end ? dateKey(range.end, a.timeZone) : "";
    return dv !== "" && (!start || dv >= start) && (!end || dv <= end);
  }
  if (f.operator === "is_empty") return dv === "";
  if (f.operator === "is_not_empty") return dv !== "";
  return dateOrderMatches(f.operator, f.datePart === "end" ? dateEndValue(v) : v, f.value, a.timeZone);
}

/**
 * A formula that gives no result meets no "is not" or "does not contain", of
 * a number or a text, as Notion's (seen 2026-11-14 through its API:
 * prop("n") does_not_equal 5 leaves out the row with no n), while a property
 * with no value, a rollup with none and a formula's made "" still meet them.
 * The server keeps the same rule (databaseQueryFormulaNegationWithoutResult).
 */
function formulaNegationWithoutResult(prop: QueryProperty, value: unknown, operator: string) {
  return prop.type === "formula" && value == null && (operator === "does_not_equal" || operator === "does_not_contain");
}

function verificationFilterState(value: unknown) {
  const record = value && typeof value === "object" ? value as { state?: unknown; date?: { end?: unknown } | null } : null;
  if (record?.state === "expired") return "expired";
  if (record?.state !== "verified") return "empty";
  const end = typeof record.date?.end === "string" ? Date.parse(record.date.end) : Number.NaN;
  return end < Date.now() ? "expired" : "verified";
}

export function filterMatches(
  row: QueryPage,
  prop: QueryProperty,
  f: QueryFilter,
  a: QueryAdapters
): boolean {
  // A rollup shown as its related values filters them one by one, as values
  // of the property rolled up: at least one, none or every one of them, as
  // Notion's. A row relating to nothing has none (seen 2026-11-05).
  if (prop.type === "rollup" && isRelationSurfacingRollup(prop)) {
    const read = a.rollupLeaves?.(row, prop);
    if (read) {
      const inner = { ...f, rollupMatch: undefined, rollupKind: undefined };
      const emptiness = f.operator === "is_empty" || f.operator === "is_not_empty";
      const matches = (leaf: { value: unknown; text: string }) =>
        (!f.rollupKind || emptiness || rollupLeafKind(read.target, leaf.value) === f.rollupKind) &&
        filterMatches(row, read.target, inner, { ...a, cellValue: () => leaf.value, displayText: () => leaf.text });
      if (f.rollupMatch === "none") return !read.leaves.some(matches);
      if (f.rollupMatch === "every") return read.leaves.length > 0 && read.leaves.every(matches);
      return read.leaves.some(matches);
    }
  }
  const v = a.cellValue(row, prop);
  // A day named from today ("1주 전") is that day on the reader's clock, for
  // a date and a formula's or rollup's date alike (Notion's, seen 2026-11-11;
  // the server's databaseQueryRelativeFilterDay).
  if (isRelativeFilterDay(f.value) && (isDateLike(prop) || isDateObject(computedDate(v)) || DAY_ORDER_OPERATORS.has(f.operator))) {
    f = { ...f, value: relativeFilterDay(f.value, filterReferenceDay(a)) ?? f.value };
  }

  if (prop.type === "select" || prop.type === "multi_select" || prop.type === "status") {
    const ids = optionIds(v);
    const targets = optionTargets(prop, f.value);
    const hasTarget = ids.some((id) => targets.has(id));
    switch (f.operator) {
      case "equals":
      case "contains":
        return hasTarget;
      case "does_not_equal":
      case "does_not_contain":
        return !hasTarget;
      case "is_empty":
        return ids.length === 0;
      case "is_not_empty":
        return ids.length > 0;
      default:
        return true;
    }
  }

  // A verification as Notion's filter lists it: verified, expired or empty (seen 2026-11-05).
  if (prop.type === "verification") {
    const targets = optionIds(f.value);
    if (!targets.length) return true;
    const has = targets.includes(verificationFilterState(v));
    return f.operator === "does_not_equal" ? !has : has;
  }

  if (prop.type === "checkbox") {
    const checked = v === true || v === "true";
    const want = f.value === true || f.value === "true";
    switch (f.operator) {
      case "equals":
        return checked === want;
      case "does_not_equal":
        return checked !== want;
      default:
        return true;
    }
  }

  if (prop.type === "relation" || prop.type === "person" || prop.type === "created_by" || prop.type === "last_edited_by") {
    const ids = prop.type === "relation" ? optionIds(v) : a.personIds(v);
    if (prop.type !== "relation" && filterValueContainsCurrentUser(f.value) && !a.currentUserId) {
      return false;
    }
    let sprintsUnknown = false;
    const resolvedFilterValue = resolveFilterValue(f.value, a.currentPageId || PAGE_NO_ROW_HOLDS, a.currentUserId, (which) => {
      const ids = a.relativeSprintIds?.(prop, which);
      if (!ids) sprintsUnknown = true;
      return ids;
    });
    if (sprintsUnknown && f.operator !== "is_empty" && f.operator !== "is_not_empty") return true;
    const targetIds = optionIds(resolvedFilterValue);
    const hasTarget = targetIds.length > 0 && ids.some((id) => targetIds.includes(id));
    const t = foldNfcText(a.displayText(row, prop));
    const q = foldNfcText(a.asText(resolvedFilterValue).trim());
    switch (f.operator) {
      case "is_empty":
        return ids.length === 0;
      case "is_not_empty":
        return ids.length > 0;
      case "contains":
        if (targetIds.length > 0) return hasTarget;
        return q === "" || t.includes(q);
      case "does_not_contain":
        if (targetIds.length > 0) return !hasTarget;
        if (q === "") return true;
        return !t.includes(q);
      case "equals":
        if (targetIds.length > 0) return hasTarget;
        if (q === "") return true;
        return t === q;
      case "does_not_equal":
        if (targetIds.length > 0) return !hasTarget;
        if (q === "") return true;
        return t !== q;
      default:
        return true;
    }
  }

  if (prop.type === "number" || prop.type === "unique_id") {
    const empty = v == null || v === "";
    if (f.operator === "is_empty") return empty;
    if (f.operator === "is_not_empty") return !empty;
    // A cleared/unset cell has no numeric value: it must not coerce to 0 and
    // spuriously match `equals 0` / `> -1`. Value comparisons treat it as
    // "no match", except `does_not_equal` which an empty cell trivially satisfies
    // (mirrors the permissive negation in the relation/text branches).
    if (empty) return f.operator === "does_not_equal";
    const n = Number(v);
    const q = Number(f.value);
    switch (f.operator) {
      case "equals":
        return n === q;
      case "does_not_equal":
        return n !== q;
      case "greater_than":
        return n > q;
      case "greater_than_or_equal_to":
        return n >= q;
      case "less_than":
        return n < q;
      case "less_than_or_equal_to":
        return n <= q;
      default:
        return true;
    }
  }

  if (isDateLike(prop)) return dateFilterMatches(v, f, a);
  // A formula's or rollup's date filters as a date does, by its start or
  // end, from today or in a range (Notion's, seen 2026-11-11); its other
  // conditions read it below.
  const computedDay = (prop.type === "formula" || prop.type === "rollup") && !isRelationSurfacingRollup(prop);
  const computed = computedDay ? computedDate(v) : v;
  if (computedDay || isDateObject(v)) {
    const dated = isDateObject(computed);
    const onDay = dated && (f.operator === "equals" || DAY_ORDER_OPERATORS.has(f.operator))
      && Number.isFinite(dateMinute(String(f.value ?? ""), "UTC"));
    if (onDay || (dated && f.datePart === "end") || isRelativeDateFilterOperator(f.operator) || f.operator === "is_between") {
      return dateFilterMatches(computed, f, a);
    }
  }

  if (prop.type === "rollup" && isRelationSurfacingRollup(prop)) {
    let sprintsUnknown = false;
    const resolvedFilterValue = resolveFilterValue(f.value, a.currentPageId || PAGE_NO_ROW_HOLDS, a.currentUserId, (which) => {
      const ids = a.relativeSprintIds?.(prop, which);
      if (!ids) sprintsUnknown = true;
      return ids;
    });
    if (sprintsUnknown && f.operator !== "is_empty" && f.operator !== "is_not_empty") return true;
    const targetIds = optionIds(resolvedFilterValue);
    const rollupTargetIds = a.rollupTargetIds(row, prop);
    // A filter may name the related page rather than hold its id (a view that
    // came from Notion): no id matching leaves it to the names the cell shows.
    if (targetIds.length > 0 && rollupTargetIds.some((id) => targetIds.includes(id))) {
      switch (f.operator) {
        case "contains":
        case "equals":
          return true;
        case "does_not_contain":
        case "does_not_equal":
          return false;
        default:
          break;
      }
    }
  }

  // Text-like (title, rich_text, url, email, phone, files, formula).
  const t = foldNfcText(a.displayText(row, prop));
  const q = foldNfcText(a.asText(f.value));
  // An empty-value negation filter is an inert no-op (mirrors the relation/person
  // branch): a blank "does not contain"/"does not equal" must not hide every row.
  if (q === "" && (f.operator === "does_not_contain" || f.operator === "does_not_equal")) return true;
  if (formulaNegationWithoutResult(prop, v, f.operator)) return false;
  switch (f.operator) {
    case "equals":
      return t === q;
    case "does_not_equal":
      return t !== q;
    case "contains":
      return t.includes(q);
    case "does_not_contain":
      return !t.includes(q);
    case "starts_with":
      return t.startsWith(q);
    case "ends_with":
      return t.endsWith(q);
    case "is_empty":
      return t === "";
    case "is_not_empty":
      return t !== "";
    // An empty value isn't 0: no number matches it.
    case "greater_than":
      return t !== "" && Number(v) > Number(f.value);
    case "greater_than_or_equal_to":
      return t !== "" && Number(v) >= Number(f.value);
    case "less_than":
      return t !== "" && Number(v) < Number(f.value);
    case "less_than_or_equal_to":
      return t !== "" && Number(v) <= Number(f.value);
    // A formula's or rollup's date compares by day, as a date's does.
    case "before":
    case "after":
    case "on_or_after":
    case "on_or_before":
      return dateOrderMatches(f.operator, v, f.value, a.timeZone);
    default:
      return true;
  }
}
