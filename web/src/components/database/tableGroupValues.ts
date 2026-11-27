import type { GraphProperty, GraphRow } from "@/contentGraph";
import type { DbProperty, Page } from "@/lib/types";
import { pageNamesNow } from "@/lib/pageNames";
import { nativeBoardQueryBounds } from "./boardViewModel";
import { evaluateFormula, rollupFormulaValues } from "./formula";
import { evaluateRollupGroupingValue } from "./rollup";
import { galleryPresentationPage, galleryPresentationProperty } from "./galleryViewModel";
import { valueAsRead } from "./valueAsRead";

/**
 * What each row groups by: its value as it reads (a status's default while
 * it sets none, valueAsRead), its maker or last editor, or
 * for a formula or rollup the value its cell works out, over the rows' pages
 * and any pages the view holds besides (the related pages they read).
 */
export function tableGroupValueReader(
  property: GraphProperty,
  properties: readonly GraphProperty[],
  rows: readonly GraphRow[],
  pagesById?: Readonly<Record<string, Page>>,
  /** The properties of the databases a rollup reads, as the view's cells hold them. */
  propsByDb?: Readonly<Record<string, readonly DbProperty[]>>,
): (row: GraphRow) => unknown {
  // Who made or last edited a row groups by that person, as Notion's (seen 2026-10-11).
  if (property.type === "created_by") return (row) => row.createdBy || undefined;
  if (property.type === "last_edited_by") return (row) => row.lastEditedBy || undefined;
  if (property.type !== "formula" && property.type !== "rollup") return (row) => valueAsRead(row, property, row.values[property.id]?.value);
  const titleProperty = properties.find((candidate) => candidate.type === "title");
  const pages: Record<string, Page> = { ...(pagesById ?? {}) };
  rows.forEach((row, position) => {
    pages[row.id] ??= galleryPresentationPage({ row, titleProperty, workspaceId: "", position });
  });
  const props = properties.map(galleryPresentationProperty);
  const prop = props.find((candidate) => candidate.id === property.id)!;
  const dbProps = { [property.dataSourceId]: props };
  if (property.type === "rollup") {
    // A rollup groups by the values it works out, each related value it shows
    // as it is (tableGroupsOf), as Notion's (seen 2026-10-11, 2026-11-12).
    const relation = props.find((candidate) => candidate.id === prop.config?.rollupRelationPropertyId);
    const targetId = relation?.config?.relationDatabaseId ?? prop.databaseId;
    const others = Object.fromEntries(Object.entries(propsByDb ?? {}).map(([id, list]) => [id, [...list]]));
    const targetProps = others[targetId] ?? (targetId === prop.databaseId ? props : []);
    return (row) => {
      const page = pages[row.id];
      if (!page) return undefined;
      try {
        const everyProps = { ...others, [property.dataSourceId]: props };
        const value = evaluateRollupGroupingValue({ row: page, prop, sourceProps: props, targetProps, pagesById: pages, propsByDb: everyProps,
          resolveComputedValue: rollupFormulaValues({ pagesById: pages, propsByDb: everyProps, bounds: nativeBoardQueryBounds.formula }),
          bounds: nativeBoardQueryBounds.formula.rollup });
        return value === "" || value === null ? undefined : value;
      } catch {
        return undefined;
      }
    };
  }
  return (row) => {
    const page = pages[row.id];
    if (!page) return undefined;
    try {
      return evaluateFormula({ row: page, prop, props, pagesById: pages, propsByDb: dbProps, bounds: nativeBoardQueryBounds.formula });
    } catch {
      // A formula that can't be worked out groups with the empty ones.
      return undefined;
    }
  };
}

/**
 * What a related page's group is called: its title as its relation cell
 * shows it, from the view's own rows or the pages it holds besides.
 */
export function tableGroupPages(
  properties: readonly GraphProperty[],
  rows: readonly GraphRow[],
  pagesById?: Readonly<Record<string, Page>>,
) {
  const titleProperty = properties.find((candidate) => candidate.type === "title");
  // Made when a group asks for a page's title (a relation's), not for every grouping.
  let own: Map<string, GraphRow> | null = null;
  const pageOf = (pageId: string) => {
    own ??= new Map(rows.map((row) => [row.id, row]));
    const row = own.get(pageId);
    return row ? galleryPresentationPage({ row, titleProperty, workspaceId: "", position: 0 }) : pagesById?.[pageId];
  };
  return {
    pageTitle: (pageId: string) => pageNamesNow.title(pageOf(pageId)),
    pageIcon: (pageId: string) => {
      const page = pageOf(pageId);
      return page ? { icon: page.icon ?? "", iconType: page.iconType, workspaceId: page.workspaceId } : undefined;
    },
  };
}
