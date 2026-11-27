import { compareGraphOrder, type GraphProperty } from "@/contentGraph";
import { newId } from "@/lib/ids";
import { graphPositionBetween } from "@/lib/nativeBoardProductAdapter";
import type { PropertyType } from "@/lib/types";
import { configForType, propertyTypeLabel } from "./propertyTypes";
import { newTablePropertyName } from "./tableAddPropertyModel";

/** A new property a database page adds after the database's last, named as typed or for its type and with the icon picked, as the table's "+" makes one. */
export function newRowPageProperty(type: PropertyType, dataSourceId: string, properties: readonly GraphProperty[], typed = "", icon?: string): GraphProperty {
  // After the last; the page lists it by name among the others, or after them
  // where the database has a page order (rowPagePropertyOrder), as Notion's
  // does (seen 2026-11-06: "되돌림" after "날짜"; 2026-11-08: "Aaa" last).
  const last = [...properties].sort(compareGraphOrder).at(-1);
  return {
    id: newId(),
    dataSourceId,
    type,
    name: newTablePropertyName(typed, propertyTypeLabel(type), properties.filter((property) => property.dataSourceId === dataSourceId).map((property) => property.name)),
    position: graphPositionBetween(last?.position, undefined),
    config: { ...(configForType(type, undefined, dataSourceId) ?? {}), ...(icon ? { icon } : {}) },
  };
}
