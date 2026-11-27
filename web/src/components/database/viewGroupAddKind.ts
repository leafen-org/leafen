import type { GraphProperty } from "@/contentGraph";
import { usePageLayoutTabMode } from "./pageLayoutTabEnvironment";
import { isTextGroupProperty, tableTextGrouping } from "./tableGrouping";

/**
 * Whether a view here offers "+ 신규 그룹", as Notion's (seen 2026-11-12):
 * on a page, at its address or in a side peek, a layout tab's board, table,
 * list and gallery have none; in the layout builder its board shows one
 * that does nothing ("inert" — pressed, it adds no group, as the preview
 * changes no rows), the others none. A board's sub-groups follow its groups,
 * none in the builder.
 */
export function useViewAddsGroups(layout: "board" | "table" | "list" | "gallery"): boolean | "inert" {
  const tab = usePageLayoutTabMode();
  if (tab === null) return true;
  return tab === "builder" && layout === "board" ? "inert" : false;
}

/**
 * What a view's "+ 신규 그룹" adds, by what it groups by, as Notion's table,
 * list and board alike (seen 2026-11-12): a select's or multi-select's
 * option, named; a title's, text's, URL's, email's or phone's group by its
 * whole text, named; a person's, picked from the workspace's people; a
 * related page's, picked from the related database. A status, box, number,
 * date, formula, rollup, who made or last edited a page, or a text grouped
 * by its first letter, offers none.
 */
export type ViewGroupAddKind = "option" | "text" | "person" | "page";

export function viewGroupAddKind(property: Pick<GraphProperty, "type"> | null | undefined, config: Record<string, unknown>): ViewGroupAddKind | undefined {
  if (!property) return undefined;
  if (property.type === "select" || property.type === "multi_select") return "option";
  if (property.type === "person") return "person";
  if (property.type === "relation") return "page";
  return isTextGroupProperty(property) && tableTextGrouping(config) === "exact" ? "text" : undefined;
}

/**
 * The key of the group a name or pick adds, as a table's, list's and
 * gallery's groups — and a board's by what its cards hold — are keyed; a
 * board's people are keyed by the person alone.
 */
export function viewGroupAddedKey(kind: Exclude<ViewGroupAddKind, "option">, value: string) {
  return kind === "person" ? `person:${value}` : kind === "page" ? `page:${value}` : `value:${value}`;
}

/**
 * The value a group added by hand stands for, read from its key, so that it
 * is named and shown while no row holds it; none for another group's key.
 */
export function keptGroupSeed(propertyType: string, key: string): unknown {
  if (propertyType === "person" && key.startsWith("person:")) return [key.slice("person:".length)];
  if (propertyType === "relation" && key.startsWith("page:")) return [key.slice("page:".length)];
  if (isTextGroupProperty({ type: propertyType as GraphProperty["type"] }) && key.startsWith("value:")) return key.slice("value:".length);
  return undefined;
}
