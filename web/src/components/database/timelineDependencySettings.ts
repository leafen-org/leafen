import type { GraphProperty, GraphView } from "@/contentGraph";

/** Native binding wins over a former view picker. Explicit imported/API
 * arrows_by still works when the source has no native dependency setting. */
export function timelineDependencySettings(view: GraphView, properties: readonly GraphProperty[]) {
  const native = view.timelineDependency;
  const propertyId = native
    ? native.enabled ? native.predecessorPropertyId : ""
    : typeof view.config.dependencyProperty === "string" ? view.config.dependencyProperty : "";
  const property = properties.find((candidate) => candidate.id === propertyId
    && candidate.dataSourceId === view.dataSourceId && candidate.type === "relation"
    && candidate.config.relationDatabaseId === view.dataSourceId);
  // A revision copied into a legacy view/property is not binding authority.
  // Without the server projection, keep the relation presentation fallback.
  const raw = native?.enabled ? native.revision : 0;
  const revision = Number(raw);
  return {
    propertyId: property?.id ?? "",
    revision: property && Number.isSafeInteger(revision) && revision > 0 ? revision : 0,
  };
}
