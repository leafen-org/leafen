import { useEffect, useState } from "react";
import { readDatabaseRowPage } from "@/databaseRowPageApi";
import type { Page, Session } from "@/types";
import type { PageHistoryProperty } from "./PageHistoryProperties";

/** A wiki page's bar: the properties side by side under its title, and those its page never shows. */
export type PageHistoryWikiBar = { bar: readonly string[]; hidden: readonly string[] };

export type PageHistoryPageProperties = { properties?: readonly PageHistoryProperty[]; wiki?: PageHistoryWikiBar };

/**
 * A page's properties as its page shows them, which its version preview
 * draws under its title whoever opened its history — its menu, its updates'
 * "버전 보기", its wiki's updates — as Notion's (seen 2026-11-18 at 1416: a
 * database page's listed, a wiki page's as its bar of Owner, Verification
 * and Tags with "⋯"). They are read once from its page, its first block
 * only (readDatabaseRowPage); what the opener knows stands until then. A
 * page that is no database's page has none.
 */
export function usePageHistoryProperties(
  session: Session, page: Page, given?: readonly PageHistoryProperty[],
): PageHistoryPageProperties {
  const databasePage = Boolean(given || page.databaseRow || (page.wikiDataSourceId && !page.isWiki));
  const [read, setRead] = useState<{ pageId: string; value: PageHistoryPageProperties } | null>(null);
  useEffect(() => {
    if (!databasePage) return;
    const controller = new AbortController();
    readDatabaseRowPage(session, page.id, { firstBlock: true, signal: controller.signal }).then((snapshot) => {
      setRead({ pageId: page.id, value: { properties: snapshot.properties,
        wiki: snapshot.wiki ? { bar: snapshot.wiki.barPropertyIds, hidden: snapshot.wiki.hiddenPropertyIds } : undefined } });
    }, () => undefined);
    return () => controller.abort();
  }, [databasePage, page.id, session]);
  return read?.pageId === page.id ? read.value : { properties: given };
}
