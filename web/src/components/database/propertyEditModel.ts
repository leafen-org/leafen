import type { TFunction } from "i18next";
import type { GraphProperty, GraphValueType } from "@/contentGraph";
import { compareGraphOrder } from "@/contentGraph";
import { graphPositionBetween } from "@/lib/nativeBoardProductAdapter";
import { newId } from "@/lib/ids";
import { nextPropertyCopyName } from "@/lib/persistentGeneratedLabels";
import type { PropertyConfig, PropertyType } from "@/lib/types";
import { configForType } from "./propertyTypes";
import { clonePropertyConfig } from "./rowPropertiesModel";

/** A property turned into another type, starting that type's settings from what carries over. */
export function propertyAfterTypeChange(property: GraphProperty, type: PropertyType, dataSourceId: string): GraphProperty {
  return {
    ...property,
    type: type as GraphValueType,
    config: type === property.type ? property.config : { ...configForType(type, property.config as PropertyConfig, dataSourceId, property.type as PropertyType) },
  };
}

/**
 * A copy of a property placed right after it, named as Notion names copies,
 * with its description and settings and none of its values (seen 2026-11-12).
 * What is the source's own stays with it: the names it had (a formula naming
 * one reads the source) and the Notion property it was imported from.
 */
export function duplicatedProperty(property: GraphProperty, properties: readonly GraphProperty[], t: TFunction): GraphProperty {
  const ordered = [...properties].sort(compareGraphOrder);
  const following = ordered[ordered.findIndex((candidate) => candidate.id === property.id) + 1];
  // A locked rollup's copy is a person's own to change.
  const {
    rollupLocked: _locked, formerNames: _formerNames, notionPropertyId: _notionPropertyId, ...config
  } = (clonePropertyConfig(property.config as PropertyConfig) ?? {}) as PropertyConfig & { formerNames?: unknown };
  return {
    ...property,
    id: newId(),
    name: nextPropertyCopyName([...properties], property.name, t),
    position: graphPositionBetween(property.position, following?.position),
    config,
  };
}
