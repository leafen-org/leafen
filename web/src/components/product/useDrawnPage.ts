import { useMemo, useState } from "react";
import { collaborationLost } from "@/lib/collaborationStatus";
import { inlineDatabaseOpenedAlone } from "@/lib/inlineDatabaseFullPage";
import { useOpenPageBody, type OpenPageBody } from "@/lib/openPageBody";
import { usePageBusy } from "@/lib/pageBusy";
import { pageHref } from "@/lib/navigation";
import { knownPathHref, pathParent } from "@/lib/pagePathPieces";
import { usePageNames } from "@/lib/pageNames";
import { peekedPageId, peekModeOf } from "@/lib/pagePeek";
import { useSearchParams } from "@/lib/router";
import { useRowDrawnWhole, type DrawnPage } from "@/lib/rowDrawnWhole";
import type { Page } from "@/types";

export type { DrawnPage };

/**
 * The page drawn at the address, one place for what the product says of the
 * open page: the page view's own (`visitedPage`, the open page the address
 * names), or a database's page drawn there whole, which tells it as it shows
 * — one opened whole (⤢) or from its link (`showRow`, StandaloneRowPage's
 * onShown), and one opened full over its database's page (`?p=<it>&pm=f`, as
 * search, the trash, backlinks and the library open one; lib/rowDrawnWhole.ts),
 * which stands over that page — a database's page is in no page list and no
 * open page. None on a screen that draws no page. What reads the open page
 * reads this: the page link keys and the Mac app's "현재 페이지 링크 복사"
 * (useAuthenticatedProductShortcuts.ts), the parent key (drawnPageParent),
 * search's "현재 페이지", the sync badge (useDrawnPageSync) and
 * the top bar's "오프라인". On a row's page they read nothing, where Notion's
 * row page is a page to them all (seen 2026-11-18 on a row opened at its
 * address and one opened whole from its peek: ⌘⌥L copies its link, ⌘K marks
 * it "현재 페이지" and leaves it out of the pages visited, ⌘⇧U goes up to its
 * database, its bar says "오프라인").
 */
export function useDrawnPage(selectedPage: Page | null, routePageId: string | null) {
  const pageNames = usePageNames();
  const [shownRow, showRow] = useState<DrawnPage | null>(null);
  const search = useSearchParams();
  const fullAt = peekModeOf(search.get("pm")) === "full" ? peekedPageId(search.get("p")) : null;
  const fullOver = useRowDrawnWhole(fullAt);
  const visitedPage = selectedPage && selectedPage.id === routePageId ? selectedPage : null;
  const row = fullOver ?? (!visitedPage && shownRow?.id === routePageId ? shownRow : null);
  const page = row ? null : visitedPage;
  const id = page?.id ?? row?.id ?? null;
  const title = page ? pageNames.title(page) : row?.title ?? "";
  const inTrash = page ? page.inTrash === true : row?.inTrash === true;
  const parentId = (page ? page.parentId : row?.parentId) || null;
  const drawnPage = useMemo<DrawnPage | null>(() => (id ? { id, title, inTrash, parentId } : null), [id, title, inTrash, parentId]);
  // The page view's page under a database's page opened full over it is not
  // visited: that page keeps its own visit (DatabaseRowPage), as Notion's
  // opens at its own address, its database's page unvisited.
  return { visitedPage, drawnPage, showRow, visitedPageCovered: Boolean(fullAt) };
}

/**
 * What the sync badge says of the page drawn: whether its live editing is
 * lost (lib/collaborationStatus.ts collaborationLost) and whether its reader
 * may only read it, by its own body — the page view's (`viewBody`, of
 * `viewPageId`), or the one a database's page drawn whole tells
 * (lib/openPageBody.ts), over its database's page too. One in the trash or
 * held by an operation has no live editing to lose: that is no lost
 * connection (2026-11-05: a page being deleted said so).
 */
export function useDrawnPageSync(drawn: DrawnPage | null, viewBody: OpenPageBody, viewPageId: string | null) {
  const body = useOpenPageBody(drawn && drawn.id === viewPageId ? viewBody : null, drawn?.id ?? null);
  const takesNoLiveEdits = Boolean(usePageBusy(drawn?.id ?? null)) || drawn?.inTrash === true;
  return {
    realtimeLost: Boolean(drawn) && !takesNoLiveEdits && collaborationLost(body.state),
    readOnly: Boolean(drawn) && !body.canEdit && !body.canSuggest,
  };
}

/**
 * Where the parent key (⌘⇧U) goes from the page drawn, as its path's piece
 * just above it opens (pagePathPieces.ts pathParent): a page's parent page; a
 * database's page its database — a full-page one's page, an inline one opened
 * alone, as Notion's goes up from a row's page (seen 2026-11-18, at its
 * address and opened whole); and from a database opened alone the page
 * holding it. One of the reader's pages opens as a page (`page`), another
 * piece at its address; none at the top, or above it in the trash.
 */
export function drawnPageParent(drawn: DrawnPage | null, pages: readonly Page[]): { page?: Page; href: string } | null {
  if (!drawn) return null;
  const local = (id: string) => pages.find((page) => page.id === id);
  const alone = inlineDatabaseOpenedAlone();
  const parent = alone && !local(alone) ? local(drawn.id) : pathParent({ id: drawn.id, parentId: drawn.parentId ?? "" }, local);
  if (!parent || parent.inTrash) return null;
  return local(parent.id) ? { page: parent, href: pageHref(parent.id) } : { href: knownPathHref(parent.id) ?? pageHref(parent.id) };
}
