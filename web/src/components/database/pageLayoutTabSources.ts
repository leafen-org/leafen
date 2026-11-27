import { useEffect, useState } from "react";
import type { ContentGraph } from "@/contentGraph";
import { collectContentGraphStructure } from "@/lib/authenticatedContentGraph";
import { databaseNamingSourceTitles, inlineDatabaseName } from "@/lib/inlineDatabaseFullPage";
import { databaseNamingSourceIds, databaseOwnNamingSourceIds, databaseSourceInTrash } from "@/lib/nativeDataSourceManagement";
import type { RelationDatabaseOption } from "@/lib/relationPropertyCreationModel";
import type { Session } from "@/types";
import { dataSourceDisplayTitle } from "../editor/databaseTitles";
import { existingSourcesShown } from "./linkableSources";

/**
 * The database's own sources, in its order, as the layout tab menu offers
 * them (pageLayoutTabSourceGroups): those its block holds and still shows,
 * none in the trash, each named and placed as a search of sources names and
 * places it (createRelationDatabaseSearch) — its database's sources' titles
 * while it holds several, an inline one's own name with them.
 */
export function databaseOwnTabSources(graph: ContentGraph, pageId: string, ownSourceId: string): RelationDatabaseOption[] {
  const block = graph.blocks.find((candidate) => candidate.type === "inline_database"
    && databaseOwnNamingSourceIds(candidate).includes(ownSourceId));
  if (!block) return [];
  const sourceOf = (id: string) => graph.dataSources.find((source) => source.id === id) ?? { id, title: "" };
  const sources = databaseOwnNamingSourceIds(block).map(sourceOf);
  // Named, and its untitled ones called, by every source naming it — those
  // it links among them, "C 및 X 보기" (databaseNamingSourceIds).
  const naming = databaseNamingSourceIds(block).map(sourceOf);
  const several = naming.length > 1;
  const containerId = graph.databaseContainers.find((container) => container.blockId === block.id)?.id;
  const databaseTitle = several ? inlineDatabaseName(block, naming).title : "";
  return sources.filter((source) => !databaseSourceInTrash(block, source)).map((source, position) => ({
    id: source.id, title: dataSourceDisplayTitle(source.title, naming.length), position, pageId, containerId,
    ...(several ? { sourceTitles: databaseNamingSourceTitles(block, naming) } : {}),
    ...(databaseTitle ? { databaseTitle } : {}),
    ...(source.title.trim() ? {} : { untitled: true }),
    ...(source.icon && source.iconType ? { mark: { icon: source.icon, iconType: source.iconType } } : {}),
  }));
}

/** The database's own sources (databaseOwnTabSources), read from its page as the tab menu opens; null until read. */
export function useDatabaseOwnTabSources(session: Session | undefined, pageId: string | undefined, ownSourceId: string | undefined) {
  const [own, setOwn] = useState<RelationDatabaseOption[] | null>(null);
  useEffect(() => {
    if (!session || !pageId || !ownSourceId) {
      setOwn([]);
      return undefined;
    }
    let live = true;
    collectContentGraphStructure(session, pageId, () => live).then(
      (read) => { if (live) setOwn(read ? databaseOwnTabSources(read.graph, pageId, ownSourceId) : []); },
      () => { if (live) setOwn([]); },
    );
    return () => { live = false; };
  }, [ownSourceId, pageId, session]);
  return own;
}

/**
 * The lists of the layout tab menu's "기존 데이터 소스 연결하기", as Notion's
 * (seen 2026-11-18 at 1440, from the pages of databases of one, two and
 * three sources, and again from a peek over the one of two): a database of several lists its own sources first under
 * "추천", in its order, the one its pages are of among them, then
 * "기존 데이터 소스" the others, four of them and "N개 더 보기" counting the
 * rest; a database of one lists its source first under "기존 데이터 소스",
 * then four others. Either way the hundred counted are its pages' source and
 * 99 others (95 more each time), and a search keeps under "추천" those of
 * its own whose names hold it, the rest of what it finds under "기존 데이터
 * 소스", its own sources among them (seen 2026-11-18 twice, from a page and
 * from a peek over its database: "w1810" kept all three under "추천",
 * "w1810 한원본" none, its three below; "w1810 나 소스" kept 나 alone, 가
 * below), those among the first twenty it found, then "N개 더 보기" (seen
 * 2026-11-18: 20 and "51개 더 보기", 40 and "31개 더 보기") — `limit`, the
 * reach every source window keeps (useSourceListLimit, existingSourcesShown).
 * "기존 데이터 소스" says "결과 없음" when it lists none, as the view
 * settings' list of sources does (DatabaseViewSourceScreen).
 */
export function pageLayoutTabSourceGroups({ own, found, ownSourceId, query, limit }: {
  own: readonly RelationDatabaseOption[];
  found: readonly RelationDatabaseOption[];
  ownSourceId?: string;
  query: string;
  /** How far the list reaches (useSourceListLimit). */
  limit: number;
}) {
  const needle = query.trim().toLocaleLowerCase();
  const fromSearch = (source: RelationDatabaseOption) => found.find(({ id }) => id === source.id) ?? source;
  const suggested = own.length > 1
    ? own.filter((source) => !needle || source.title.toLocaleLowerCase().includes(needle)).map(fromSearch) : [];
  const suggestedIds = new Set(suggested.map(({ id }) => id));
  const key = ({ id }: RelationDatabaseOption) => id;
  if (needle) {
    const { shown, hidden } = existingSourcesShown(found.map(key), found.filter(({ id }) => !suggestedIds.has(id)), { searched: true, limit, key });
    return { suggested, existing: shown, hidden };
  }
  const ownIds = new Set(own.map(({ id }) => id));
  const others = found.filter(({ id }) => !ownIds.has(id) && id !== ownSourceId);
  const current = own.find(({ id }) => id === ownSourceId) ?? found.find(({ id }) => id === ownSourceId);
  // The pages' own source counts first, among the five and the hundred, whether listed here or under "추천".
  const { shown, hidden } = existingSourcesShown([], others, { searched: false, limit, ahead: 1, key });
  return { suggested, existing: current && own.length <= 1 ? [fromSearch(current), ...shown] : shown, hidden };
}
