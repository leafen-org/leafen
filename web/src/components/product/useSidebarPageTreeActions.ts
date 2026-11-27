import { useCallback, useEffect, useMemo, useRef, type Dispatch, type SetStateAction } from "react";
import { createBlockMoveService, type BlockMoveRequest } from "@/lib/blockMoveService";
import { upsertBootstrapPage } from "@/lib/bootstrapPageProjection";
import { addFullPageDatabaseRow } from "@/lib/fullPageDatabase";
import { whenRowSaved } from "@/lib/rowMadeHere";
import { requestPageTitleFocus } from "@/lib/pageTitleFocus";
import { pageHref } from "@/lib/navigation";
import { createNativePage, pageCreationHere } from "@/lib/pageCreationService";
import { type PageMadeHereOutbox, showPageMadeHere, withServerMadePage } from "@/lib/pageMadeHere";
import type { ProductStatusSink } from "@/lib/productStatus";
import { pushRoute } from "@/lib/router";
import { newPagePeekHref } from "@/lib/pagePeek";
import { markFreshPage } from "@/components/page/freshPages";
import { afterOwnWrites } from "@/lib/ownWritesInFlight";
import type { Bootstrap, Page, Session } from "@/types";

export type SidebarBlockTransfer = BlockMoveRequest & { copy: boolean };

export function useSidebarPageTreeActions({
  announce,
  pageMutationOutbox,
  pages,
  refreshPages,
  selectedPageId,
  session,
  setData,
  rereadPage,
}: {
  announce: ProductStatusSink;
  /** Where a sub-page opened at once is kept on this device until the server makes it. */
  pageMutationOutbox?: PageMadeHereOutbox;
  pages: readonly Page[];
  refreshPages: () => Promise<unknown>;
  selectedPageId: string | null;
  session: Session;
  setData: Dispatch<SetStateAction<Bootstrap>>;
  /**
   * Reads what changed of the page shown once its own writes are saved, and
   * again when one is made meanwhile (useContentGraphWrites follow: its
   * blocks alone when a sub-page's block is all that changed, else the
   * whole page): read at once, a view hidden as the read went out came back
   * on screen until the next read (2026-11-12, a sub-page added under a
   * NAS's round trip); read whole, a sub-page added under a page of 5,000
   * rows read its 51 windows again (2026-11-25).
   */
  rereadPage: (pageId: string) => Promise<boolean>;
}) {
  const selectedPageIdRef = useRef(selectedPageId);
  selectedPageIdRef.current = selectedPageId;
  const blockMoveService = useMemo(() => createBlockMoveService(session), [session]);
  useEffect(() => () => blockMoveService.cancel(), [blockMoveService]);

  // A sub-page is a block in its parent's body: the page shown reads its
  // body again when a sub-page comes, goes or moves in it, once the changes
  // on their way have landed. A move shows in the list before the server has
  // it, and a read sent then came back without the page moved in, which
  // showed only when the room told of it, if it did (2026-11-12: 586ms after
  // the pick). Asks while one waits are that read, which waits for their
  // changes too.
  const rereadWait = useRef<{ pageId: string; until: Promise<unknown> } | null>(null);
  const rereadWhenBodyHolds = useCallback((...parentIds: (string | null | undefined)[]) => {
    const shown = selectedPageIdRef.current;
    if (!shown || !parentIds.includes(shown)) return;
    const landed = afterOwnWrites();
    const queued = rereadWait.current;
    if (queued?.pageId === shown) {
      queued.until = Promise.all([queued.until, landed]);
      return;
    }
    const wait = { pageId: shown, until: landed as Promise<unknown> };
    rereadWait.current = wait;
    void (async () => {
      for (let until = wait.until; ; until = wait.until) {
        await until;
        if (until === wait.until) break;
      }
      if (rereadWait.current === wait) rereadWait.current = null;
      if (selectedPageIdRef.current === shown) void rereadPage(shown);
    })();
  }, [rereadPage]);

  // The sub-pages the list shows under the page shown, and where: moved in,
  // out or along by the move window, a drag, the trash or another tab, the
  // list says so first — before the server answers, or once it has.
  const shownSubpages = useMemo(() => subpagesOf(pages, selectedPageId), [pages, selectedPageId]);
  const subpagesSeen = useRef({ pageId: selectedPageId, subpages: shownSubpages });
  useEffect(() => {
    const seen = subpagesSeen.current;
    subpagesSeen.current = { pageId: selectedPageId, subpages: shownSubpages };
    if (seen.pageId === selectedPageId && seen.subpages !== shownSubpages) rereadWhenBodyHolds(selectedPageId);
  }, [rereadWhenBodyHolds, selectedPageId, shownSubpages]);

  const changePage = useCallback((page: Page) => {
    setData((current) => upsertBootstrapPage(current, page));
  }, [setData]);

  // A sub-page shows in the press's own frame, made on this device first and
  // kept here behind it, as a top-level page does (showPageMadeHere); its
  // line comes to its parent's body when the server makes it, which the
  // parent shown then reads. Under a wiki's page the server makes it the
  // wiki's own page, so the server makes it first.
  const subpageMadeHere = useCallback((parent: Pick<Page, "id" | "isWiki" | "teamspaceId" | "wikiRootId" | "workspaceId">) => {
    if (!pageMutationOutbox || parent.isWiki || parent.wikiRootId) return null;
    const creation = pageCreationHere(session, {
      siblings: pages, parentId: parent.id, teamspaceId: parent.teamspaceId, title: "", workspaceId: parent.workspaceId,
    });
    void showPageMadeHere(pageMutationOutbox, session, creation, {
      announce,
      made: (made) => {
        // One the server put elsewhere (a wiki's page) shows as the server holds it.
        if (made.wikiRootId || made.parentId !== parent.id) void refreshPages().catch(() => undefined);
        else setData((current) => ({ ...current, pages: withServerMadePage(current.pages, made) }));
        rereadWhenBodyHolds(parent.id);
      },
      // Refused: the list shows what the server holds.
      refused: () => void refreshPages().catch(() => undefined),
    });
    return creation.page;
  }, [announce, pageMutationOutbox, pages, refreshPages, rereadWhenBodyHolds, session, setData]);

  const createSubpage = useCallback(async (parentId: string, workspaceId?: string): Promise<Page | null> => {
    const listed = pages.find((page) => page.id === parentId);
    const parent = listed ?? (workspaceId ? { id: parentId, kind: "page" as const, teamspaceId: "", workspaceId } : null);
    if (!parent) throw new Error("Parent page is not in the current bootstrap snapshot.");
    // As Notion's sidebar adds a page to a database, a full-page database
    // takes a new row, which opens over the page open with where it goes in
    // its bar (seen 2026-11-05), and closed untouched it goes (RowPeekAddTo).
    if (listed?.kind === "database") {
      const rowId = await addFullPageDatabaseRow(session, listed);
      // Made on this device first, it opens now; the database open reads it once the server has it.
      if (selectedPageIdRef.current === parent.id) {
        void whenRowSaved(rowId).then(() => selectedPageIdRef.current === parent.id && rereadPage(parent.id)).catch(() => undefined);
      }
      requestPageTitleFocus(rowId);
      if (document.querySelector('[data-phone="true"]')) {
        pushRoute(pageHref(parent.id, new URLSearchParams({ p: rowId, pm: "c" })));
        return null;
      }
      markFreshPage(rowId);
      pushRoute(newPagePeekHref(rowId));
      return null;
    }
    const madeHere = subpageMadeHere(parent);
    if (madeHere) {
      changePage(madeHere);
      return madeHere;
    }
    const page = await createNativePage(session, { parentId, title: "", workspaceId: parent.workspaceId });
    changePage(page);
    return page;
  }, [changePage, pages, rereadPage, session, subpageMadeHere]);

  const transferBlock = useCallback(async (input: SidebarBlockTransfer) => {
    const { copy, ...request } = input;
    await (copy ? blockMoveService.copy(request) : blockMoveService.move(request));
    if (!copy && selectedPageIdRef.current === request.sourcePageId) await rereadPage(request.sourcePageId);
  }, [blockMoveService, rereadPage]);

  return useMemo(() => ({ changePage, createSubpage, transferBlock }), [
    changePage,
    createSubpage,
    transferBlock,
  ]);
}

/** The sub-pages listed under a page and where they stand, as one value that changes when they do. */
function subpagesOf(pages: readonly Page[], pageId: string | null) {
  if (!pageId) return "";
  return pages.filter((page) => page.parentId === pageId)
    .map((page) => `${page.id}:${page.position}:${page.inTrash ? 1 : 0}`).sort().join(",");
}
