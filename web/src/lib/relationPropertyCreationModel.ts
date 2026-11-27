import { foldNfcText } from "@/shared/database/natural-order.mjs";
import type { GraphDataSource } from "@/contentGraph";
import type { GraphDataSourceIconType } from "@/contentGraphWire.generated";
import type { Place } from "./pagePath";
import { propertyOwnSettings } from "./propertyOwnSettings";
import { dataSourceInTrash } from "./dataSourcePage";
import type { DatabaseSourceTitle } from "@/components/editor/databaseTitles";

export type RelationDatabaseOption = {
  id: string;
  position: number;
  title: string;
  /** The page holding the database: its own page when it is a full page. */
  pageId?: string;
  /** The database holding the source, as a page search names it. */
  containerId?: string;
  /** The page holding it, as a search names it, until its place is read. */
  where?: string;
  /** Where it sits, "Page / Page", as Notion's list shows it under its name (databaseSourcePlace). */
  place?: Place;
  /** Its database's sources' titles, while that holds more than one. */
  sourceTitles?: DatabaseSourceTitle[];
  /** Its source has no name of its own, `title` being what it is called (dataSourceDisplayTitle). */
  untitled?: boolean;
  /** An inline database's own name while it holds several (inlineDatabaseName). */
  databaseTitle?: string;
  /** The source's own icon, none where it has none (DataSourceMark); apart from a picker's own `icon`. */
  mark?: { icon: string; iconType: GraphDataSourceIconType };
};

/**
 * The sources a page shows, as a new relation's list offers them first, each with its icon; one in the
 * trash is offered by no list, as the workspace's search leaves it out (database_container_picker_order.go).
 */
export function relationDatabaseOptions(dataSources: readonly GraphDataSource[]): RelationDatabaseOption[] {
  return dataSources.filter((source) => !dataSourceInTrash(source)).map((source, position) => ({
    id: source.id, title: source.title, position, pageId: source.pageId,
    ...(source.icon && source.iconType ? { mark: { icon: source.icon, iconType: source.iconType } } : {}) }));
}

export type RelationPropertyCreationBounds = {
  maxDatabases: number;
  maxNameCharacters: number;
  maxSearchCharacters: number;
};

/** How many databases, and how long a name or search, a new relation's dialog takes. */
export const RELATION_CREATION_BOUNDS: RelationPropertyCreationBounds = {
  // However many databases the workspace holds.
  maxDatabases: Number.MAX_SAFE_INTEGER,
  maxNameCharacters: 200,
  maxSearchCharacters: 200,
};

export type ExistingRelationProperty = {
  id: string;
  config?: Record<string, unknown>;
};

export type RelationPropertyCommand = {
  contextIdentity: string;
  databaseId: string;
  existingPropertyId?: string;
  mutationId: string;
  name: string;
  propertyConfig: Record<string, unknown>;
  snapshotIdentity: string;
  targetDatabaseId: string;
};

export type CurrentRelationPropertyOperation = {
  complete: () => void;
  isCurrent: () => boolean;
  signal: AbortSignal;
};

export function createRelationPropertyOperationOwner() {
  let generation = 0;
  let active: { controller: AbortController; generation: number } | null = null;

  return Object.freeze({
    begin(): CurrentRelationPropertyOperation | null {
      if (active) return null;
      generation += 1;
      const owned = { controller: new AbortController(), generation };
      active = owned;
      const isCurrent = () => active === owned
        && generation === owned.generation
        && !owned.controller.signal.aborted;
      return Object.freeze({
        complete() {
          if (isCurrent()) active = null;
        },
        isCurrent,
        signal: owned.controller.signal,
      });
    },
    retire() {
      generation += 1;
      active?.controller.abort();
      active = null;
    },
  });
}

function positiveBound(value: number, name: string) {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new Error(`${name} must be a positive safe integer`);
  }
  return value;
}

function nonempty(value: string, name: string) {
  const normalized = value.trim();
  if (!normalized) throw new Error(`${name} must not be empty`);
  return normalized;
}

function characterCount(value: string) {
  return Array.from(value).length;
}

export function prepareRelationDatabases(
  databases: RelationDatabaseOption[],
  bounds: RelationPropertyCreationBounds,
) {
  const maxDatabases = positiveBound(bounds.maxDatabases, "maxDatabases");
  if (databases.length > maxDatabases) {
    throw new Error("relation databases exceed the configured bound");
  }
  const seen = new Set<string>();
  const prepared = databases.map((database) => {
    const id = nonempty(database.id, "database id");
    if (seen.has(id)) throw new Error(`duplicate relation database: ${id}`);
    seen.add(id);
    if (!Number.isFinite(database.position)) {
      throw new Error("database position must be finite");
    }
    return { ...database, id };
  });
  // In the order given: the database itself, the page's own, then the
  // ones found elsewhere, as Notion lists them (seen 2026-11-02).
  prepared.sort((left, right) => left.position - right.position);
  return prepared;
}

// A search lists what the workspace's search found for it, in its order
// (database_container_picker_order.go: those holding every word, then those
// holding some, as Notion's finds "새 폼" by "새 데이터", seen 2026-11-12), each
// of the same name with its place; untitled ones it finds by no name, as
// Notion's (seen 2026-11-14: three untitled databases under a page found
// neither by "새" nor by that page's name). Till its answer comes, or with
// no workspace search, the titles here are read by the same first rule.
export function filterRelationDatabases(
  prepared: RelationDatabaseOption[],
  search: string,
  bounds: RelationPropertyCreationBounds,
  /** The databases the workspace's search found for this search, in its order. */
  found?: readonly string[],
) {
  const maxSearchCharacters = positiveBound(bounds.maxSearchCharacters, "maxSearchCharacters");
  if (characterCount(search) > maxSearchCharacters) {
    throw new Error("relation search exceeds the configured bound");
  }
  const searchTerms = foldNfcText(search.trim()).split(/\s+/u).filter(Boolean);
  if (searchTerms.length === 0) return prepared;
  if (found) {
    const order = new Map(found.map((id, index) => [id, index]));
    return prepared.filter(({ id }) => order.has(id)).sort((left, right) => order.get(left.id)! - order.get(right.id)!);
  }
  const held = prepared.map((database) => {
    const title = foldNfcText(database.title);
    return { database, words: searchTerms.filter((term) => title.includes(term)).length };
  }).filter(({ words }) => words > 0);
  return [...held.filter(({ words }) => words === searchTerms.length), ...held.filter(({ words }) => words < searchTerms.length)]
    .map(({ database }) => database);
}

export function createRelationPropertyCommand({
  bounds,
  contextIdentity,
  databaseId,
  existingProperty,
  limitOne = false,
  mutationId,
  name,
  reciprocalName,
  reciprocalPropertyId,
  snapshotIdentity,
  targetDatabaseId,
  twoWay,
}: {
  bounds: RelationPropertyCreationBounds;
  contextIdentity: string;
  databaseId: string;
  existingProperty?: ExistingRelationProperty;
  /** Notion's "제한": the relation holds one page (limit 1) or any number. */
  limitOne?: boolean;
  mutationId: string;
  name: string;
  /** What the server names the related property; it names it itself without one. */
  reciprocalName?: string;
  reciprocalPropertyId?: string;
  snapshotIdentity: string;
  targetDatabaseId: string;
  twoWay: boolean;
}): RelationPropertyCommand {
  const normalizedName = nonempty(name, "property name");
  const maxNameCharacters = positiveBound(bounds.maxNameCharacters, "maxNameCharacters");
  if (characterCount(normalizedName) > maxNameCharacters) {
    throw new Error("relation property name exceeds the configured bound");
  }
  const sourceId = nonempty(databaseId, "source database id");
  const targetId = nonempty(targetDatabaseId, "target database id");
  const config = existingProperty?.config;
  // A property made a relation keeps what is its own whatever its type.
  const propertyConfig: Record<string, unknown> = {
    ...propertyOwnSettings(config),
    relationDatabaseId: targetId,
    ...(limitOne ? { limit: 1 } : {}),
  };
  // A two-way relation to its own database pairs with a second property on
  // it, as Notion adds one ("… 다시 관계됨"); without the pair it relates
  // both rows by itself, as Notion's made on its screen does (seen
  // 2026-11-12: relating A to B relates B to A).
  if (!twoWay && targetId === sourceId) propertyConfig.relationSymmetric = true;
  if (twoWay) {
    propertyConfig.relatedPropertyId = nonempty(
      reciprocalPropertyId ?? "",
      "reciprocal property id",
    );
    const pairName = reciprocalName?.trim();
    if (pairName) {
      if (characterCount(pairName) > maxNameCharacters) {
        throw new Error("related property name exceeds the configured bound");
      }
      propertyConfig.reciprocalName = pairName;
    }
  }
  return {
    contextIdentity: nonempty(contextIdentity, "context identity"),
    databaseId: sourceId,
    ...(existingProperty ? { existingPropertyId: nonempty(existingProperty.id, "property id") } : {}),
    mutationId: nonempty(mutationId, "mutation id"),
    name: normalizedName,
    propertyConfig,
    snapshotIdentity: nonempty(snapshotIdentity, "snapshot identity"),
    targetDatabaseId: targetId,
  };
}
