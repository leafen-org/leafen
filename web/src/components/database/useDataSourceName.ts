import { useContext } from "react";
import { dataSourceDisplayTitle } from "@/components/editor/databaseTitles";
import { DatabaseDataSourcesContext } from "./databaseDataSourcesContext";

/**
 * A data source's name where a view of its database names it by itself —
 * a chart's or a board group's preview "⋯ › ↗ ⛁ 〈source〉 열기": its own,
 * else what an untitled source is called by how many of its database's own
 * sources name the database (untitledDataSourceTitle, blockDataSourceName):
 * "새 데이터베이스" as its only one, "새 데이터 소스" as one of several, as
 * Notion's chart rows window names it (seen 2026-11-18). One another
 * database owns goes by one.
 */
export function useDataSourceName(source: { id: string; title: string }) {
  const surface = useContext(DatabaseDataSourcesContext);
  const naming = surface?.own.some((own) => own.id === source.id) ? surface.naming : 1;
  return dataSourceDisplayTitle(source.title, naming);
}
