import { useContext } from "react";
import { DatabaseDataSourcesContext } from "./databaseDataSourcesContext";
import { useDatabaseFirstViewLink, useDatabaseFullPageLink, useDatabaseOwnPage } from "./databaseFullPageLink";
import { useLinkedSourceHref } from "./InlineDatabaseLinkedTitle";

/**
 * Where a source's database opens on its own page, as a board group
 * preview's "⋯ › 〈DB〉 열기" goes (Notion's, seen 2026-11-11): a linked
 * view's to its source's database, at the view that opens on; an inline
 * one's to it alone, at its first view whichever it shows; and one opened
 * alone already to its own page again, its view staying. None where it
 * opens nowhere.
 */
export function useDatabaseOpenHref(dataSourceId: string): string | null {
  const surface = useContext(DatabaseDataSourcesContext);
  const fullPageHref = useDatabaseFullPageLink(), firstViewHref = useDatabaseFirstViewLink(), ownPage = useDatabaseOwnPage();
  const sourceHref = useLinkedSourceHref();
  const linked = surface?.linked.find((source) => source.dataSourceId === dataSourceId);
  if (linked?.href) return linked.href;
  // Only where it opens alone: a row page's layout tab inside it opens nowhere (PageLayoutTabView).
  if (fullPageHref && firstViewHref) return firstViewHref;
  if (!ownPage || !surface) return null;
  // Its own page: the database page itself, or the page holding it opened at it.
  return sourceHref({ id: dataSourceId, title: "", pageId: surface.pageId }, surface.databaseId) ?? null;
}
