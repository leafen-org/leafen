import { placeUnder, severalSourcesDatabaseName, type Place } from "@/lib/pagePath";
import type { Page } from "@/types";
import type { MoveDatabaseResult } from "./useMoveSearches";

/** A database a move search found, standing in its list among the pages (rankedAmongPages). */
export type MoveListDatabaseRow<D> = { kind: "database"; key: string; group: "pages"; database: D };

/**
 * A move search's list with the databases it found among its pages, the
 * window's and a phone's sheet alike: each where its name's rank
 * (pageSearchRank, the score useMoveSearches gives it) puts it, then by its
 * name, as the pages themselves are ordered (moveDestinations) — Notion's
 * list mixes them so (seen 2026-11-16: "옮길" put its one database second of
 * eight, among the pages). The places and teamspaces ahead of the pages stay
 * there (`pageOf` says null for them); a database past every page goes last.
 */
export function rankedAmongPages<R, D extends { id: string; title: string; score: number }>(
  rows: readonly R[],
  pageOf: (row: R) => { score: number; label: string } | null,
  databases: readonly D[],
): (R | MoveListDatabaseRow<D>)[] {
  const waiting = [...databases].sort((left, right) => left.score - right.score || left.title.localeCompare(right.title));
  const listed: (R | MoveListDatabaseRow<D>)[] = [];
  const take = () => {
    const database = waiting.shift()!;
    listed.push({ kind: "database", key: `database:${database.id}`, group: "pages", database });
  };
  for (const row of rows) {
    const page = pageOf(row);
    while (page && waiting.length
      && (waiting[0].score < page.score || waiting[0].score === page.score && waiting[0].title.localeCompare(page.label) < 0)) take();
    listed.push(row);
  }
  while (waiting.length) take();
  return listed;
}

/** Whether a row of a move search's list is a database it found. */
export function isMoveListDatabase<D>(row: object): row is MoveListDatabaseRow<D> {
  return (row as { kind?: unknown }).kind === "database";
}

/**
 * Where a database a move search found stands, said as a page's place is:
 * under the page holding it or beside its own page's, then its name while
 * it holds several sources ("… / A 및 B", Notion's, seen 2026-11-15).
 */
export function moveDatabaseLocation(database: Pick<MoveDatabaseResult, "pageId" | "sourceTitles" | "databaseTitle">,
  pagesById: Record<string, Page>): Place {
  const holder = database.pageId ? pagesById[database.pageId] : undefined;
  if (!holder) return [];
  const name = severalSourcesDatabaseName(database.sourceTitles, holder, database.databaseTitle);
  if (holder.kind !== "database") return placeUnder(holder.id, pagesById, ...name);
  return holder.parentId ? placeUnder(holder.parentId, pagesById, ...name) : [];
}
