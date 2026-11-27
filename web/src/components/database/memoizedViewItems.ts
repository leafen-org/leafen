import { useRef, useState } from "react";
import type { DbProperty } from "@/lib/types";
import type { PropertyValueReadContext } from "./PropValue";
import { readsOtherRows } from "./otherRowsRead";

/**
 * What a view's memoized items (a gallery's cards, a timeline's bars) share:
 * one item is drawn again when what it shows changed, not for each drawing of
 * its view. A gallery's state or a row made drew every card again, some
 * 1,900 parts for 34 cards, three times inside one ⌘D (2026-11-15, NAS +
 * CPU ×2); a timeline's every bar for one row's value.
 */

/**
 * What every item of a view draws alike, the same object while what it holds
 * reads the same: its lists by their items, its plain objects by their parts
 * (made anew as the view's settings change).
 */
export function useSteadyFrame<Frame extends object>(next: Frame): Frame {
  const kept = useRef(next);
  const was = kept.current;
  const same = Object.keys(next).length === Object.keys(was).length && (Object.keys(next) as (keyof Frame)[]).every((key) => {
    const left: unknown = was[key], right: unknown = next[key];
    if (left === right) return true;
    if (Array.isArray(left) && Array.isArray(right)) return left.length === right.length && left.every((item, index) => item === right[index]);
    return left !== null && right !== null && typeof left === "object" && typeof right === "object"
      && Object.getPrototypeOf(left) === Object.prototype && Object.getPrototypeOf(right) === Object.prototype
      && Object.keys(right).length === Object.keys(left).length
      && Object.entries(right).every(([part, value]) => (left as Record<string, unknown>)[part] === value);
  });
  if (!same) kept.current = next;
  return kept.current;
}

/**
 * What an item's presses do, by its view as last drawn: the same object for as
 * long as the view is drawn, so an item not drawn again presses with the
 * view's latest state and operations.
 */
export function useLiveWays<Ways extends object>(ways: Ways): Ways {
  const now = useRef(ways);
  now.current = ways;
  const [live] = useState(() => new Proxy({} as Ways, {
    get: (_, key) => (...args: unknown[]) => (now.current[key as keyof Ways] as (...args: unknown[]) => unknown)(...args),
  }));
  return live;
}

// The lookups of other rows in a view's value context, made anew as any row comes or goes.
const OTHER_ROWS_LOOKUPS = new Set(["pagesById", "hydratedRelationTargetIds", "snapshotIdentity", "propsByDatabaseId"]);

/**
 * Whether the values an item shows read the same of two value contexts: the
 * same context, or one whose lookups of other rows alone changed while none of
 * the values shown reads other rows (a relation, a rollup, a formula naming
 * one — otherRowsRead.ts) and their database's properties are the same.
 */
export function sameOwnRowValues(before: PropertyValueReadContext, after: PropertyValueReadContext, shown: readonly DbProperty[]) {
  if (before === after) return true;
  for (const key of Object.keys(after) as (keyof PropertyValueReadContext)[]) {
    if (!OTHER_ROWS_LOOKUPS.has(key) && before[key] !== after[key]) return false;
  }
  return shown.every((property) => {
    const properties = after.propsByDatabaseId[property.databaseId];
    return before.propsByDatabaseId[property.databaseId] === properties && !readsOtherRows(property, properties);
  });
}
