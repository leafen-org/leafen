import { createNativePage, pageCreationHere } from "@/lib/pageCreationService";
import { showPageMadeHere, type PageMadeHereOutbox } from "@/lib/pageMadeHere";
import { pageHref } from "@/lib/navigation";
import { pushRoute } from "@/lib/router";
import type { Page } from "@/types";
import type { EditorMoveContext } from "./editorRuntimeTypes";

/** The field a sub-page made from its "@" hangs on: a page's title, or a database page's cell of a property. */
export const titleAnchor = "title";
export function cellAnchor(propertyId: string) { return `cell:${propertyId}`; }

/**
 * A sub-page made from "@" in a line, a table row's cell, a caption, a
 * page's title or a database page's cell hangs on that, with no block of its
 * own, as Notion's (user decision 2026-11-11; seen 2026-11-13: its parent is
 * the line, row or block, or the page whose title or cell it is): its
 * mention goes in at once (`write`, which says whether it could), the page
 * is made on this device, in the sidebar at once, and the server makes it
 * after, hanging on `anchor` (mention_anchored_pages.go). False when this
 * device can't make it so, for the caller to make it as before. One on a
 * table row's cell or a caption (`field`) stays out of the sidebar from the
 * first, as Notion's (pageTreeModel).
 */
export function makeAnchoredSubPage({ move, outbox, parentId, workspaceId, title, anchor, field = false, write }: {
  move: EditorMoveContext;
  outbox: PageMadeHereOutbox | undefined;
  parentId: string;
  workspaceId: string;
  title: string;
  anchor: string;
  field?: boolean;
  write(page: Page): boolean;
}) {
  if (!outbox) return false;
  const creation = pageCreationHere(move.session, { siblings: move.pages, parentId, title, workspaceId });
  creation.input.anchorBlockId = anchor;
  if (field) creation.page.fieldAnchored = true;
  // One on the title is its parent's first sub-page, as the title stands above the body (childPageBodyOrder).
  const siblings = move.pages.filter(page => page.parentId === parentId && page.workspaceId === workspaceId);
  if (anchor === titleAnchor && siblings.length) creation.page.position = Math.min(...siblings.map(page => page.position)) - 1;
  if (!write(creation.page)) return false;
  move.onPageChange(creation.page);
  // It shows in the press's own frame and is kept here behind it
  // (showPageMadeHere; 2026-11-13: an untitled one opened only once kept).
  void showPageMadeHere(outbox, move.session, creation, {
    announce: (message, tone) => move.onStatus?.(message, tone),
    // Before the server has the field's block (a table just made) it can't tell; this device can.
    made: (page) => { move.onPageChange(field ? { ...page, fieldAnchored: true } : page); void move.onPagesChanged?.(); },
    refused: () => void move.onPagesChanged?.(),
  });
  // An untitled page opens to be named, as Notion's (seen 2026-10-10).
  if (!title.trim()) pushRoute(pageHref(creation.pageId));
  return true;
}

/**
 * A page made under the page picked from "@" "페이지 추가", in a line or a
 * field (a title, a cell, a caption), shows in the press's own frame, as
 * the sidebar's sub-page (user decision 2026-11-12: a press answers on the
 * next frame — it waited for the page to be kept here, in a field for the
 * server): listed at once, made on this device and kept behind the press
 * (showPageMadeHere), `made` told once the server made it. Under a database
 * or a wiki's page, or without an outbox, the server makes it first. Either
 * way it is the page to mention.
 */
export function makePageUnder({ move, outbox, parentId, workspaceId, title, made }: {
  move: EditorMoveContext;
  outbox: PageMadeHereOutbox | undefined;
  parentId: string;
  workspaceId: string;
  title: string;
  made?: (page: Page) => void;
}): Page | Promise<Page> {
  const parent = move.pages.find(page => page.id === parentId);
  if (!outbox || !parent || parent.kind === "database" || parent.isWiki || parent.wikiRootId) {
    return createNativePage(move.session, { parentId, title, workspaceId }).then((page) => {
      move.onPageChange(page);
      void move.onPagesChanged?.();
      return page;
    });
  }
  const creation = pageCreationHere(move.session, { siblings: move.pages, parentId, teamspaceId: parent.teamspaceId, title, workspaceId });
  void showPageMadeHere(outbox, move.session, creation, {
    announce: (message, tone) => move.onStatus?.(message, tone),
    made: (page) => { move.onPageChange(page); void move.onPagesChanged?.(); made?.(page); },
    refused: () => void move.onPagesChanged?.(),
  });
  move.onPageChange(creation.page);
  return creation.page;
}
