import { useSyncExternalStore } from "react";
import { inlineDatabaseViews } from "@/components/editor/InlineDatabaseBlock";
import { collectContentGraphStructure } from "@/lib/authenticatedContentGraph";
import { readDatabaseViewChoice } from "@/lib/databaseViewChoice";
import { pageAddressId } from "@/lib/inlineDatabaseAddress";
import { pageHref } from "@/lib/navigation";
import { serverUrl } from "@/lib/serverOrigin";
import type { Session } from "@/types";

/**
 * A page's link to copy, as Notion's "링크 복사" makes it from the page's bar,
 * its "⋯", a peek's bar and a page or database block's menu (seen twice
 * 2026-11-25 in Chrome, two tabs): the page's own address, carrying the view
 * the address names (`v`) whatever page it is for — an ordinary page's under
 * a view a closed peek left, a child page's or another database's, a row's in
 * a peek — and nothing else the address holds; with none there, a full-page
 * database's names the view it opens at (seen 2026-11-14: /p/<its ID>?v= the
 * view this person last saw there — the board after the board, the table
 * after the table): this person's last choice there, else its saved view,
 * else its first, as its own page names it in its address
 * (useDatabaseViewController) and an inline one's link does
 * (useBlockDatabaseMenu), so turning it inline or back keeps its link.
 *
 * Its views are read with its page's structure, without rows, when a menu
 * that copies its link opens (readDatabasePageViews), and kept for the tab.
 *
 * Its links in the app name that view too (databasePageHref).
 */
type LinkPage = { id: string; kind?: string; isWiki?: boolean };

// A wiki keeps its own link (WikiDatabaseView).
const fullPageDatabase = (page: LinkPage) => page.kind === "database" && !page.isWiki;

// A full-page database's block, its views in order and its saved view, by its page.
const known = new Map<string, { blockId: string; views: string[]; saved: unknown }>();
// The reads under way, by page.
const reading = new Map<string, Promise<void>>();
// Told whenever a database's views are read, for its links to name its view.
const listeners = new Set<() => void>();
let version = 0;

/** Reads a full-page database's views again, for its link; nothing for another page. */
export function readDatabasePageViews(session: Session | null | undefined, page: LinkPage): Promise<void> {
  if (!session || !fullPageDatabase(page)) return Promise.resolve();
  const running = reading.get(page.id);
  if (running) return running;
  const read = (async () => {
    const graph = (await collectContentGraphStructure(session, page.id))?.graph;
    const blockId = pageAddressId(page.id);
    const block = graph?.blocks.find((candidate) => candidate.type === "inline_database" && candidate.id === blockId)
      ?? graph?.blocks.find((candidate) => candidate.type === "inline_database" && !candidate.parentId);
    if (!graph || !block) return;
    known.set(page.id, { blockId: block.id, views: inlineDatabaseViews(graph, block).views.map((view) => view.id), saved: block.content.viewId });
    version += 1;
    for (const listener of listeners) listener();
  })().catch(() => undefined).finally(() => reading.delete(page.id));
  reading.set(page.id, read);
  return read;
}

/**
 * The view a full-page database opens at for this person, as now known: their
 * last choice there — one the views read before it was made don't have yet
 * included — else, once its views are read, its saved view, else its first;
 * none while neither is known (its views unread, a server out of reach).
 */
function openingView(page: LinkPage, userId: string | undefined) {
  const chosen = readDatabaseViewChoice(userId, pageAddressId(page.id));
  if (chosen) return chosen;
  const views = known.get(page.id);
  if (!views) return null;
  return views.views.find((id) => id === views.saved) ?? views.views[0] ?? null;
}

/** The view the address names now, which a copied page link carries (pageLinkToCopy). */
function addressView() {
  return typeof window === "undefined" ? null : new URLSearchParams(window.location.search).get("v");
}

/**
 * An address to copy for a page or a database by its ID: the view the address
 * names, else the one it names of its own — a database block's menu gives the
 * view it shows (useBlockDatabaseMenu), as Notion's carries the address's
 * there too (seen twice 2026-11-25).
 */
export function addressToCopy(id: string, ownView: string | null) {
  const viewId = addressView() ?? ownView;
  return serverUrl(pageHref(id, viewId ? new URLSearchParams({ v: viewId }) : null));
}

/**
 * A page's link to copy as now known: its address with the view the address
 * names, else a full-page database's with its view once its views are read.
 */
export function pageLinkToCopy(page: LinkPage, userId: string | undefined) {
  return addressToCopy(page.id, fullPageDatabase(page) ? openingView(page, userId) : null);
}

/**
 * A page's address for a link to it in the app: a full-page database's naming
 * the view it opens at (openingView), as Notion's links to a database do — a
 * sub-page line, a link block, a path's piece, a block menu's "새 탭에서 열기"
 * (seen twice 2026-11-25 in Chrome, two tabs: /p/<database>?v=<the view last
 * chosen there, else its first one>&pvs=…, ⌘-click opening a new tab at that
 * view, its title naming it); a mention's names none (seen twice that day).
 */
export function databasePageHref(page: LinkPage, userId: string | undefined) {
  const viewId = fullPageDatabase(page) ? openingView(page, userId) : null;
  return pageHref(page.id, viewId ? new URLSearchParams({ v: viewId }) : null);
}

/**
 * Opens a page in a new tab at its own address — a full-page database's
 * naming the view it opens at (databasePageHref) — and nothing else the
 * address holds now, as Notion's "새 탭에서 열기" does wherever it is pressed
 * (seen twice 2026-11-25 in Chrome, two tabs, the address holding another
 * database's view and a peek's: a page block's opened /p/<page>, a row's
 * /p/<row>, a full-page database block's /p/<database>?v=<its first view>,
 * none carrying the address's view), unlike a link copied, which carries the
 * address's view (addressToCopy). An inline database's opens at the view it
 * shows (useBlockDatabaseMenu), as Notion's did.
 */
export function openPageInNewTab(page: LinkPage, userId: string | undefined) {
  window.open(databasePageHref(page, userId), "_blank", "noopener,noreferrer");
}

/** Whether a full-page database's view to name is still unknown here: none chosen, its views unread. */
export function databasePageViewUnknown(page: LinkPage, userId: string | undefined) {
  return fullPageDatabase(page) && !openingView(page, userId) && !reading.has(page.id);
}

/** Draws again whenever a database's views are read (databasePageHref); the value is only a change counter. */
export function useDatabasePageViewsRead(follow = true) {
  return useSyncExternalStore((listener) => {
    if (!follow) return () => {};
    listeners.add(listener);
    return () => listeners.delete(listener);
  }, () => follow ? version : 0, () => 0);
}

/** A page's link to copy, a full-page database's once its views are read, if they aren't yet and the address names none. */
export async function readPageLinkToCopy(session: Session | null | undefined, page: LinkPage) {
  if (!addressView() && fullPageDatabase(page) && !known.has(page.id)) await (reading.get(page.id) ?? readDatabasePageViews(session, page));
  return pageLinkToCopy(page, session?.user.id);
}
