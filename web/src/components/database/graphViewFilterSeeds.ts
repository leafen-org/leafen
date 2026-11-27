import type { GraphProperty, GraphView } from "@/contentGraph";
import type { DbView, ViewConfig } from "@/lib/types";
import { galleryPresentationProperty } from "./galleryViewModel";
import type { QueryBounds } from "./queryBudget";
import { viewFilterSeedValues } from "./querySeeds";

/**
 * The values a row made in a view starts with so the view keeps showing it:
 * what its filters ask for, the title among them, keyed by property. Notion's
 * table gives a new page these from its "+ New page" and its "New" alike
 * (seen 2026-10-15: a checkbox, a select, a multi-select, a number and
 * "me"), as Leafen's other views did already.
 */
export function graphViewFilterSeeds(
  properties: readonly GraphProperty[],
  view: GraphView,
  options: { currentUserId?: string; currentPageId?: string; omitPropertyIds?: Iterable<string> },
  bounds: QueryBounds,
): Record<string, unknown> {
  const dbView: DbView = {
    id: view.id, databaseId: view.dataSourceId, name: view.name, type: view.type as DbView["type"],
    config: view.config as ViewConfig, position: 0,
  };
  const seeds = viewFilterSeedValues(properties.map(galleryPresentationProperty), dbView, options.omitPropertyIds ?? [],
    { currentUserId: options.currentUserId, currentPageId: options.currentPageId }, bounds);
  const title = properties.find((property) => property.type === "title");
  return title && seeds.title != null ? { ...seeds.properties, [title.id]: seeds.title } : { ...seeds.properties };
}
