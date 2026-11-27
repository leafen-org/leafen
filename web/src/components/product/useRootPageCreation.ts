import { useCallback, useEffect, useRef, type Dispatch, type SetStateAction } from "react";
import { api } from "@/api";
import { i18next } from "@/i18n";
import { requestWorkspaceCreate } from "@/lib/openWorkspace";
import type { Bootstrap, Page, Session, Workspace } from "@/types";
import type { ProductStatusSink } from "@/lib/productStatus";
import { type PageMadeHereOutbox, showPageMadeHere, withServerMadePage } from "@/lib/pageMadeHere";
import { createNativePage, pageCreationHere } from "@/lib/pageCreationService";
import { startCreatedDatabasePage, startRootDatabaseHere, recoverFailedDatabaseCreation, type DatabaseStarterCreationChoice } from "@/lib/databaseStarterCreation";
import { startChosenDatabaseHere } from "@/lib/databaseStartKept";
import { closeAddPageWindow } from "@/components/sidebar/addPageWindowStore";
import { dropHeldTyping, holdTypingForNewPage, requestPageTitleFocus } from "@/lib/pageTitleFocus";
import { authenticatedProductErrorMessage } from "@/lib/authenticatedProductErrors";
import { inDesktopTab, openNewPageInNewDesktopTab } from "@/lib/desktopTabs";
import { answerWaitMs } from "@/lib/serverAnswers";

// How long a press for a new page waits for the server, or a few of its round
// trips where those are longer (lib/serverAnswers.ts answerWaitMs).
const SILENT_SERVER_WAIT_MS = 12_000;

/** Root creation owns its original workspace until committed; only its live context opens the result. */
export function useRootPageCreation({ session, selectedWorkspace, setCreatingPage, setData, refreshPages, announce, openPage, pageMutationOutbox, pages }: {
  session: Session | null; selectedWorkspace: Workspace | null;
  setCreatingPage: Dispatch<SetStateAction<boolean>>;
  setData: Dispatch<SetStateAction<Bootstrap>>; refreshPages(): Promise<unknown>; announce: ProductStatusSink;
  openPage(page: Page, replace: boolean): void;
  /** Where a page opened at once is kept on this device until the server makes it. */
  pageMutationOutbox?: PageMadeHereOutbox;
  /** The pages listed, which a page made here goes after. */
  pages?: readonly Page[];
}) {
  const creating = useRef<string | null>(null);
  const createRootContext = useRef("");
  // A page being made, and the add-page window, last as long as the person and the workspace: their sign-in
  // handed over from another tab or renewed keeps them (another person's starts the app over).
  createRootContext.current = session?.user.id + ":" + selectedWorkspace?.id;
  const listed = useRef(pages ?? []);
  listed.current = pages ?? [];

  /**
   * Makes a page on this device first, listed in the press's own frame and,
   * unless whoever asked opens it, opened (showPageMadeHere): here at once,
   * or in a new tab once its copy is kept for that tab to read. The server
   * makes it in turn. Null without an outbox, and the server makes it first.
   */
  function openMadeHere(activeSession: Session, workspaceId: string, teamspaceId: string | undefined, title: string, isCurrent: () => boolean,
    open: "here" | "tab" | "asker") {
    if (!pageMutationOutbox) return null;
    const creation = pageCreationHere(activeSession, { siblings: listed.current, teamspaceId, title, workspaceId });
    const ready = showPageMadeHere(pageMutationOutbox, activeSession, creation, {
      announce, isCurrent,
      made: (made) => setData((current) => ({ ...current, pages: withServerMadePage(current.pages, made) })),
      // Refused: the list shows what the server holds.
      refused: () => void refreshPages().catch(() => undefined),
    });
    setData((current) => ({ ...current, pages: [...current.pages.filter(({ id }) => id !== creation.pageId), creation.page] }));
    if (open === "tab") void ready.then((made) => { if (made && isCurrent()) openMade(creation.page, true); });
    else if (open === "here") openMade(creation.page, false);
    return { page: creation.page, ready };
  }

  // A page made opens with the caret in its title: here, or in a new tab
  // (openNewPageInNewDesktopTab), this tab staying as it was.
  function openMade(page: Page, inNewTab: boolean) {
    if (inNewTab) {
      openNewPageInNewDesktopTab(page.id);
      return;
    }
    requestPageTitleFocus(page.id);
    openPage(page, false);
  }

  async function createRootPage(teamspaceId?: string, options: {
    /** Whether it opens here; "asker" when whoever asked opens it here at
     *  once (the add window), false when they open it elsewhere once it can
     *  be (search's new tab). */
    open?: boolean | "asker"; database?: boolean; title?: string; databaseChoice?: DatabaseStarterCreationChoice;
    /** In the Mac and Windows apps, a page made from "새로" or ⌘N opens in a
     *  new tab at the end, in front, the tab it was asked from staying as it
     *  was — Notion's app's create menus (their source, read 2026-11-13). */
    inNewTab?: boolean;
  } = {}) {
    if (!session) return null;
    // Someone with no workspace is asked for one rather than given one unasked.
    if (!selectedWorkspace) {
      requestWorkspaceCreate();
      return null;
    }
    // A guest adds nothing to the workspace outside the pages shared with them.
    if (selectedWorkspace.guest) return null;
    const context = createRootContext.current;
    const isCurrent = () => createRootContext.current === context;
    if (creating.current === context) return null;
    creating.current = context;
    setCreatingPage(true);
    const asked = options.open === false || options.open === "asker";
    const newTab = Boolean(options.inNewTab) && !asked && !options.database && inDesktopTab();
    // What is typed meanwhile goes to the title of a page opening here; one
    // opening in a new tab takes what is typed there.
    const opening = !asked && !options.database && !newTab;
    if (opening) holdTypingForNewPage();
    let opened = false;
    // A server that doesn't answer doesn't keep the person waiting on it: they
    // pressed for a page and are typing its title (2026-11-05: nothing said
    // why, for a minute, and what was typed went with it).
    const silent = new AbortController();
    let giveUp = 0;
    try {
      // A page is made here first and shows in the press's own frame, opened
      // here or by the add window that asked; in a new tab, or by search for
      // one, once it can be read there (2026-11-13: the add window's asked
      // the server first, and the window stayed up until it answered).
      const madeHere = options.database ? null
        : openMadeHere(session, selectedWorkspace.id, teamspaceId, options.title ?? "", isCurrent, opening ? "here" : newTab ? "tab" : "asker");
      if (madeHere) {
        opened = true;
        if (options.open !== false) return madeHere.page;
        return await madeHere.ready && isCurrent() ? madeHere.page : null;
      }
      // A blank database is made here first (startRootDatabaseHere); one
      // from a template or a source shows here at once, the server making it
      // from what the outbox keeps (startChosenDatabaseHere).
      const place = {
        siblings: listed.current, teamspaceId, title: options.title ?? "", workspaceId: selectedWorkspace.id, announce, isCurrent,
        refresh: refreshPages, listed: (change: (pages: Page[]) => Page[]) => setData((current) => ({ ...current, pages: change(current.pages) })),
      };
      const database = !options.database ? null : options.databaseChoice ? startChosenDatabaseHere(session, pageMutationOutbox, options.databaseChoice, place)
        : startRootDatabaseHere(session, pageMutationOutbox, place);
      if (database) {
        if (!asked && isCurrent()) openMade(database, false);
        return database;
      }
      giveUp = window.setTimeout(() => silent.abort(), answerWaitMs(SILENT_SERVER_WAIT_MS));
      const page = await createNativePage(session, {
        parentId: "",
        signal: silent.signal,
        teamspaceId,
        title: options.title ?? "",
        workspaceId: selectedWorkspace.id,
      });
      window.clearTimeout(giveUp);
      // Asked again after a try that went unanswered, the server gives the
      // page that try made, which the list may have by now.
      if (isCurrent()) setData((current) => ({ ...current, pages: [...current.pages.filter(({ id }) => id !== page.id), page] }));
      let created = page;
      if (options.database) {
        let started: Page | null = null;
        try {
          // Where this device can't keep it first.
          const receipt = await startCreatedDatabasePage(session, page, options.databaseChoice);
          started = { ...page, kind: "database", ...(receipt.page ? { title: receipt.page.title, cursor: receipt.page.cursor, revision: receipt.page.revision } : {}) };
        } catch { started = await recoverFailedDatabaseCreation(session, page).catch(() => null); }
        if (!isCurrent()) return null;
        if (started) {
          created = started;
          await refreshPages();
        } else {
          await refreshPages();
          announce(i18next.t("syncStatusBadge:status.databaseStartFailed"), "error");
          return null;
        }
      }
      if (!isCurrent()) return null;
      if (!asked) {
        openMade(created, newTab);
        opened = opening;
      }
      return created;
    } catch (error) {
      if (!opening && !newTab) throw error;
      if (isCurrent()) announce(silent.signal.aborted ? i18next.t("settingsErrors:unreachableCreatePage")
        : authenticatedProductErrorMessage(error, "pageCreate"), "error");
      return null;
    } finally {
      window.clearTimeout(giveUp);
      // A page not opened lets go of what was held for it.
      if (opening && !opened) dropHeldTyping();
      if (creating.current === context) creating.current = null;
      if (isCurrent()) setCreatingPage(false);
    }
  }

  const loadAddDatabaseSources = useCallback((query: string, cursor: string | undefined, signal: AbortSignal) => session && selectedWorkspace
    ? api.databaseContainerSources(session, selectedWorkspace.id, query, { cursor, limit: cursor || query ? 25 : 4, signal })
    : Promise.resolve({ items: [] }), [session, selectedWorkspace]);
  useEffect(() => { closeAddPageWindow(); setCreatingPage(false); }, [session?.user.id, selectedWorkspace?.id]);

  return {createRootPage,loadAddDatabaseSources};
}
