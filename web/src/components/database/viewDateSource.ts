import { useEffect, useMemo } from "react";
import { collectPropertyValueSchemaDatabaseIds } from "@/lib/propertyValueReadModel";
import type { DbProperty, Page } from "@/lib/types";
import type { PropertyValueReadContext } from "./PropValue";
import { rollupHopDatabaseIds } from "./rollup";
import { effectiveSummaryValue, type SummaryValueBounds } from "./summaryValue";
import { inPublicOrder } from "./propertyPublicOrder";

// What a calendar or a timeline may place its pages by, as Notion's
// "표시 기준" lists them (seen 2026-11-06): the date properties, and the
// rollups whose calculation gives one date of the related ones, as its
// "롤업 날짜" of their latest date. A rollup's dates are its related pages':
// its cards and bars stay where those put them, not moved or stretched here.

const DATE_ROLLUP_FUNCTIONS = new Set(["earliest_date", "latest_date", "date_range"]);

type DateSourceProperty = { type: string; config?: unknown };

/** Whether a calendar or timeline may place its pages by this property. */
function isViewDateSource(property: DateSourceProperty | undefined) {
  return property?.type === "date" || isDateRollup(property);
}

/** A rollup of related dates, whose dates are its related pages' to change. */
export function isDateRollup(property: DateSourceProperty | undefined) {
  const config = property?.config as { rollupFunction?: unknown } | null | undefined;
  return property?.type === "rollup" && DATE_ROLLUP_FUNCTIONS.has(String(config?.rollupFunction ?? ""));
}

/** The properties a calendar or timeline may place its pages by, in their order. */
export function viewDateSources<Property extends DateSourceProperty>(properties: readonly Property[]) {
  return properties.filter(isViewDateSource);
}

/**
 * What a calendar or timeline that names none places its pages by, a new
 * one's or one whose date went: the first in the order the API lists them
 * (inPublicOrder), as Notion's (seen 2026-11-12: dates made 날짜1, 날짜2,
 * 날짜3 with IDs "pNel", "JB~f", "nr:`" placed a new calendar and timeline by
 * 날짜2 and, 날짜2 deleted, by 날짜3).
 */
export function firstViewDateSource<Property extends DateSourceProperty>(sources: readonly Property[]): Property | undefined {
  return inPublicOrder(sources.filter(isViewDateSource))[0];
}

export type ViewDateContext = {
  properties: DbProperty[];
  pagesById: Record<string, Page>;
  propertiesByDatabaseId?: Record<string, DbProperty[]>;
  bounds: SummaryValueBounds;
};

/**
 * A row's date on a date source, as a date property holds it: a rollup's
 * day, or its range "start → end", as a start and an end.
 */
export function viewDateValue(row: Page, property: DbProperty, context: ViewDateContext): unknown {
  if (property.type !== "rollup") return row.properties?.[property.id];
  const value = effectiveSummaryValue(row, property, context.properties, context.pagesById, context.bounds, context.propertiesByDatabaseId);
  if (typeof value !== "string") return value && typeof value === "object" ? value : undefined;
  if (!value) return undefined;
  const [start, end] = value.split(" → ");
  return end ? { start, end } : { start };
}

/**
 * Reads the databases a date rollup's values come from while a view places
 * its pages by it, as its cell does when it shows: a view placing them by a
 * rollup it hides would otherwise find no related dates.
 */
export function useDateRollupReads(property: DbProperty | undefined, context: PropertyValueReadContext) {
  const ids = useMemo(() => {
    if (!property || !isDateRollup(property)) return "";
    const propsByDatabaseId = context.propsByDatabaseId as Record<string, DbProperty[]>;
    const relation = (propsByDatabaseId[property.databaseId] ?? []).find((item) => item.id === property.config?.rollupRelationPropertyId);
    const target = relation?.config?.relationDatabaseId ?? relation?.databaseId;
    const targetProps = target ? propsByDatabaseId[target] ?? [] : [];
    return collectPropertyValueSchemaDatabaseIds([target, ...rollupHopDatabaseIds(property, targetProps, propsByDatabaseId, context.bounds.rollup)]).join("\u0000");
  }, [context.bounds.rollup, context.propsByDatabaseId, property]);
  useEffect(() => {
    if (ids) context.schemaReadOwner.request(ids.split("\u0000"));
  }, [context.contextIdentity, context.schemaReadOwner, context.snapshotIdentity, ids]);
}
