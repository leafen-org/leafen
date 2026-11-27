import { untitledDatabaseSourcesName, type DatabaseSourceTitle } from "./databaseTitles";

/** A database a page holds in its body, as the page list carries it (InlineDatabaseProjection). */
type HeldDatabase = {
  containerId: string;
  pageId: string;
  title: string;
  icon?: string;
  iconType?: string;
  /** A view block shows another's database, none of its own. */
  viewBlock?: boolean;
  /** Its sources' names while it holds several, which name it while it has no name of its own. */
  sourceTitles?: readonly DatabaseSourceTitle[];
};

/**
 * The databases the listed pages hold in their bodies, each as a database
 * page under its page by its block's ID, in their pages' order: Notion's
 * page lists offer them among the pages — the "@" menu (seen 2026-11-10) and
 * a button's or automation's "열기" list (seen 2026-11-24 in two tabs, a
 * table's icon and "한지테스트페이지 / 〈its page〉" under each). A view
 * block, which shows another's database, isn't one, nor one whose page
 * isn't listed or that is a page of its own (a full-page database, listed
 * as its page). One of several sources unnamed goes by its sources' names
 * ("A 및 B").
 */
export function inlineDatabasePagesOf<P extends { id: string; workspaceId: string }>(
  databases: readonly HeldDatabase[] | undefined,
  pagesById: Readonly<Record<string, P>>,
): Map<string, P> {
  const pages = new Map<string, P>();
  for (const database of databases ?? []) {
    const holder = pagesById[database.pageId];
    if (database.viewBlock || !holder || pagesById[database.containerId]) continue;
    // Edited when its page was, a block of it, which orders a search's answers (searchAnswerOrder).
    const edited = (holder as { updatedAt?: string }).updatedAt;
    pages.set(database.containerId, {
      id: database.containerId, workspaceId: holder.workspaceId, parentId: holder.id, parentType: "page", kind: "database",
      title: database.title || (database.sourceTitles && database.sourceTitles.length > 1 ? untitledDatabaseSourcesName(database.sourceTitles) : ""),
      icon: database.icon ?? "", iconType: database.iconType || "none", position: 0, ...(edited ? { updatedAt: edited } : {}),
    } as unknown as P);
  }
  return pages;
}

/** The pages, each followed by the databases its body holds (inlineDatabasePagesOf), as a list offering both names them. */
export function pagesWithHeldDatabases<P extends { id: string; parentId?: string | null }>(pages: readonly P[], held: ReadonlyMap<string, P>): P[] {
  if (!held.size) return [...pages];
  const byHolder = new Map<string, P[]>();
  for (const database of held.values()) {
    const holderId = database.parentId ?? "";
    byHolder.set(holderId, [...(byHolder.get(holderId) ?? []), database]);
  }
  return pages.flatMap((page) => [page, ...(byHolder.get(page.id) ?? [])]);
}
