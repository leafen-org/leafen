import { compareNaturalText } from "./natural-order.mjs";
import { dateSortKey } from "./queryDatePolicy";
import { isDateLike, optionIds, optionIndex } from "./queryFilterPolicy";
import type { QueryAdapters, QueryPage, QueryProperty } from "./queryTypes";

/** Comparable key for sorting: number for numeric/ordinal types, else lowercased text. */
export function sortKey(row: QueryPage, prop: QueryProperty, a: QueryAdapters): number | string {
  const v = a.cellValue(row, prop);
  if (prop.type === "select" || prop.type === "status" || prop.type === "multi_select") {
    const ids = optionIds(v);
    return ids.length ? optionIndex(prop, ids[0]) : Number.MAX_SAFE_INTEGER;
  }
  if (prop.type === "number" || prop.type === "unique_id") {
    // Cleared (null) and never-set (undefined) cells both sort last; without the
    // explicit empty check `Number(null)` would coerce to 0 and interleave
    // cleared cells among real zeros.
    if (v == null || v === "") return "\uffff";
    const n = Number(v);
    return Number.isFinite(n) ? n : "\uffff";
  }
  // A formula or a rollup giving a number sorts as one, not as its text, an
  // infinite one too, past every other (Notion's, seen 2026-11-14: -Infinity,
  // 0, Infinity, then the empty one); one that is no number has no order
  // (UNORDERED_SORT_KEY).
  if ((prop.type === "formula" || prop.type === "rollup") && typeof v === "number") {
    return Number.isNaN(v) ? UNORDERED_SORT_KEY : v;
  }
  if (prop.type === "checkbox") return v ? 1 : 0;
  // A verification sorts by when it runs out, one verified indefinitely after
  // every dated one, an unverified page last, as Notion's (seen 2026-11-11;
  // the server's databaseQueryVerificationSortKey).
  if (prop.type === "verification") {
    const record = v && typeof v === "object" ? v as { state?: unknown; date?: { end?: unknown } | null } : null;
    if (record?.state !== "verified" && record?.state !== "expired") return "\uffff";
    const end = record.date?.end;
    return end != null && end !== "" ? dateSortKey(end) || "\uffff" : "9999-12-31T23:59:59.999Z";
  }
  if (isDateLike(prop)) {
    // Time-precise key so same-day rows order by time.
    const k = dateSortKey(v);
    return k === "" ? "￿" : k;
  }
  return a.displayText(row, prop).toLowerCase();
}

/**
 * The key of a formula's number that is no number, which orders with no
 * other: it comes after every number in either direction and before the rows
 * with no value, as the server's sort places it (databaseQueryUnorderedSortKey;
 * Notion's places it as its sort meets it, seen 2026-11-14).
 */
export const UNORDERED_SORT_KEY = "\ufffe";

/** Where a key stands whatever the direction: 0 ordered, 1 unordered, 2 no value. */
export function sortKeyRank(key: number | string): 0 | 1 | 2 {
  return isEmptySortKey(key) ? 2 : key === UNORDERED_SORT_KEY ? 1 : 0;
}

/** Whether a sort key says the row has no value: those rows come last in either direction, as Notion's (seen 2026-11-04). */
export function isEmptySortKey(key: number | string): boolean {
  return key === "" || key === "\uffff" || key === Number.MAX_SAFE_INTEGER;
}

export function compareKeys(a: number | string, b: number | string): number {
  if (typeof a === "number" && typeof b === "number") return a < b ? -1 : a > b ? 1 : 0;
  return compareNaturalText(a, b);
}
