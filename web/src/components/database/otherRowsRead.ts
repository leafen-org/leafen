import type { DbProperty, Page } from "@/lib/types";
import type { PropertyCellSnapshot } from "./propertyCellTypes";
import { valueAsIds } from "./rollup";
import { formulaPropertyReferences, resolveFormulaPropertyReference } from "./formulaReferences";

/**
 * What a relation's cell and a rollup's of plain values read of other rows:
 * their own row's value and, by it, the related pages. Compared by those
 * alone, such a cell is drawn again for their changes, not for each snapshot
 * made anew as any row changes — a row's peek holding a relation of 100
 * pages drew its 1,400 parts again for each of the peek's drawings, eight
 * as it opened (seen 2026-11-13). A rollup of rollups, relations or
 * formulas reads further rows, and a formula anything: those go by the
 * snapshot whole.
 */

// A relation's picker, open, lists its database's pages: drawn again for any change (PropertyRelationCell).
export const openRelationPickers = new Set<string>();
export const relationPickerKey = (rowId: string, propertyId: string) => `${rowId}\u0000${propertyId}`;

// The lookups of other rows, compared by the related pages alone.
const LOOKUPS = new Set(["pagesById", "propertiesByDataSourceId", "hydratedRelationTargetIds", "loadedDataSourceIds", "relationPagesByDataSourceId"]);
// The values a rollup reads of the related pages alone, no further.
const PLAIN_TARGETS = new Set([
  "title", "rich_text", "number", "select", "multi_select", "status", "date", "checkbox", "url", "email", "phone", "person",
  "files", "created_time", "last_edited_time", "created_by", "last_edited_by", "unique_id", "place", "verification",
]);

type Read = { relation: DbProperty; target: string; also: readonly string[] };

// The relation whose pages the cell reads, the database they are in and the databases whose properties it reads.
function readOf(prop: DbProperty, snapshot: PropertyCellSnapshot): Read | null {
  if (prop.config?.appEditor) return null;
  if (prop.type === "relation") {
    return { relation: prop, target: prop.config?.relationDatabaseId ?? prop.databaseId, also: [prop.databaseId] };
  }
  if (prop.type !== "rollup") return null;
  const relation = snapshot.propertiesByDataSourceId[prop.databaseId]?.find((item) => item.id === prop.config?.rollupRelationPropertyId);
  if (!relation) return null;
  const target = relation.config?.relationDatabaseId ?? relation.databaseId;
  const rolled = snapshot.propertiesByDataSourceId[target]?.find((item) => item.id === prop.config?.rollupTargetPropertyId);
  return rolled && PLAIN_TARGETS.has(rolled.type) ? { relation, target, also: [prop.databaseId] } : null;
}

/** Whether a relation's or a plain rollup's cell reads the same of its row and of other rows in both. */
export function sameOtherRowsRead(prop: DbProperty, beforeRow: Page, afterRow: Page,
  before: PropertyCellSnapshot, after: PropertyCellSnapshot): boolean {
  if (beforeRow.id !== afterRow.id || beforeRow.subitemParentId !== afterRow.subitemParentId
    || openRelationPickers.has(relationPickerKey(afterRow.id, prop.id))) return false;
  const read = readOf(prop, after);
  if (!read || readOf(prop, before)?.relation !== read.relation) return false;
  const value = beforeRow.properties?.[read.relation.id];
  if (value !== afterRow.properties?.[read.relation.id] || beforeRow.properties?.[prop.id] !== afterRow.properties?.[prop.id]) return false;
  const keys = Object.keys(after) as (keyof PropertyCellSnapshot)[];
  if (Object.keys(before).length !== keys.length || keys.some((key) => !LOOKUPS.has(key) && before[key] !== after[key])) return false;
  const databases = [read.target, ...read.also];
  if (databases.some((id) => before.pagesById[id] !== after.pagesById[id]
    || before.propertiesByDataSourceId[id] !== after.propertiesByDataSourceId[id]
    || before.loadedDataSourceIds.has(id) !== after.loadedDataSourceIds.has(id)
    || before.relationPagesByDataSourceId[id] !== after.relationPagesByDataSourceId[id])) return false;
  return valueAsIds(value, after.formulaBounds.rollup).every((id) => before.pagesById[id] === after.pagesById[id]
    && before.hydratedRelationTargetIds.has(id) === after.hydratedRelationTargetIds.has(id));
}

// The values that read other rows than their own: a formula reads related
// pages only through these (formula.ts).
const OTHER_ROWS_VALUES = new Set(["relation", "rollup"]);
// Whether each formula reads other rows, by its database's properties as read.
const formulaReads = new WeakMap<readonly DbProperty[], Map<string, boolean>>();

/**
 * Whether a cell reads other rows than its own: a relation's and a rollup's
 * do, a formula's where it names one of those, itself or through the
 * formulas it names. A formula of its own row's values alone is drawn again
 * for its row and its database's properties, not for each snapshot made anew
 * as any row changes (2026-11-14: a row made or copied in a table with a
 * formula column drew all its rows again, 86–111 ms, CPU ×2).
 */
export function readsOtherRows(prop: DbProperty, properties: readonly DbProperty[] | undefined): boolean {
  if (OTHER_ROWS_VALUES.has(prop.type)) return true;
  if (prop.type !== "formula") return false;
  if (!properties || prop.config?.appEditor) return true;
  let kept = formulaReads.get(properties);
  if (!kept) formulaReads.set(properties, kept = new Map());
  const key = `${prop.id}\u0000${String(prop.config?.formula ?? "")}`;
  let reads = kept.get(key);
  if (reads === undefined) kept.set(key, reads = formulaNamesOtherRows(prop, properties, new Set()));
  return reads;
}

function formulaNamesOtherRows(prop: DbProperty, properties: readonly DbProperty[], seen: Set<string>): boolean {
  if (seen.has(prop.id)) return false;
  seen.add(prop.id);
  for (const name of formulaPropertyReferences(String(prop.config?.formula ?? ""))) {
    const named = resolveFormulaPropertyReference(properties, name);
    if (named && (OTHER_ROWS_VALUES.has(named.type) || (named.type === "formula" && formulaNamesOtherRows(named, properties, seen)))) return true;
  }
  return false;
}

// A database's properties as a formula reads them, kept per list.
const propertiesRead = new WeakMap<readonly DbProperty[], string>();

function propertiesAsRead(properties: readonly DbProperty[] | undefined) {
  if (!properties) return "";
  let read = propertiesRead.get(properties);
  if (read === undefined) propertiesRead.set(properties, read = JSON.stringify(properties.map(({ id, name, type, config }) => [id, name, type, config])));
  return read;
}

/**
 * Whether a cell reads the same of the snapshot's other rows and of its
 * database's properties in both: a relation's and a plain rollup's by the
 * pages they relate (sameOtherRowsRead), a formula of its own row alone by
 * the properties it may name, any other cell always.
 */
export function cellReadsSame(prop: DbProperty, beforeRow: Page, afterRow: Page, before: PropertyCellSnapshot, after: PropertyCellSnapshot): boolean {
  const properties = after.propertiesByDataSourceId[prop.databaseId];
  if (readsOtherRows(prop, properties)) return sameOtherRowsRead(prop, beforeRow, afterRow, before, after);
  if (prop.type !== "formula") return true;
  const was = before.propertiesByDataSourceId[prop.databaseId];
  return was === properties || propertiesAsRead(was) === propertiesAsRead(properties);
}
