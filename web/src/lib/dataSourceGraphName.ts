import type { ContentGraph, GraphBlock, GraphDataSource } from "@/contentGraph";
import { dataSourceDisplayTitle } from "@/components/editor/databaseTitles";
import { plainTitleOf } from "@/lib/closedMentions";
import { databaseBlockMembers, databaseNamingSourceIds } from "@/lib/nativeDataSourceManagement";

/**
 * A data source's name where a page's content names it by itself: its own,
 * else what an untitled source is called by how many sources its database
 * goes by (untitledDataSourceTitle) — "새 데이터베이스" as its database's
 * only one, inline or full page, "새 데이터 소스" as one of several — as
 * Notion's sidebar view section names it in its heading, its rename field and
 * its "출처" (seen 2026-11-18, twice). A source the page holds no database of
 * its own for goes by one.
 */
export function graphDataSourceName(graph: ContentGraph, source: Pick<GraphDataSource, "id" | "title" | "titleSpans">) {
  const block = graph.blocks.find((candidate) => candidate.type === "inline_database"
    && databaseBlockMembers(candidate).own.includes(source.id));
  return blockDataSourceName(block, source);
}

/**
 * The same name where a database block names the sources it shows — a
 * dashboard's widget sources: one of its own by how many sources name it
 * (databaseNamingSourceIds, those it links among them), one it links,
 * another database's, by one.
 */
export function blockDataSourceName(block: GraphBlock | undefined, source: Pick<GraphDataSource, "id" | "title" | "titleSpans">) {
  const naming = block && databaseBlockMembers(block).own.includes(source.id) ? databaseNamingSourceIds(block).length : 1;
  return dataSourceDisplayTitle(plainTitleOf(source), naming);
}
