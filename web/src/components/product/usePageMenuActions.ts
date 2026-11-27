import type { ProductStatusSink } from "@/lib/productStatus";
import { i18next } from "@/i18n";
import { startTransition, useEffect, useRef, type Dispatch, type SetStateAction } from "react";
import type * as Y from "yjs";
import { announceWikiChange } from "@/lib/wikiChangeEvents";
import { authenticatedProductErrorMessage } from "@/lib/authenticatedProductErrors";
import {
  isWikiDatabasePage,
  optimisticPageMetadata,
  pageMetadataBaseline,
  type NativePageMetadataChange,
  type sendKeptPageMetadata,
} from "@/lib/nativePageMetadata";
import type { NativePageMutationBrowser } from "@/lib/nativePageMutationBrowser";
import { duplicateDatabaseRowPage, readDatabaseRowPage } from "@/databaseRowPageApi";
import { keptSentFor } from "@/lib/keptSentFor";
import { newId } from "@/lib/ids";
import { holdOwnWrite } from "@/lib/ownWritesInFlight";
import { noteOwnLockFlip } from "@/lib/ownLockFlips";
import { notePageChangeStep } from "@/components/editor/pageUndo";
import { presentationBefore, type PagePresentationChange } from "@/components/page/pagePresentation";
import { duplicateNativePage, shownPageCopy, type PageDuplicationOptions } from "@/lib/pageDuplicationService";
import { upsertBootstrapPage } from "@/lib/bootstrapPageProjection";
import { pageBodyStats } from "@/lib/pageBodyStats";
import type { ContentGraph } from "@/contentGraph";
import { notePageTitlesFromServer, type PageTitleBase } from "@/lib/pageTitleFollow";
import type { TextSpan } from "@/lib/types";
import type { Bootstrap, Page, Session } from "@/types";
import { keptChangeStatus } from "@/lib/serverAnswers";
import { pageMapOf } from "@/lib/pageIndex";
import { noteTrashMove } from "@/lib/pageTrashMoves";
import { dropTypedPageTitle, knownPageName, showTypedPageTitle, typedPageTitle } from "@/lib/pageNames";
import { notePageWriteOnItsWay, sendTypedPageTitles } from "@/lib/pageKindChange";

const COPY_OPENS_BY_MS = 1_600;
// A copy not in the list yet is looked for again after this, twice as long
// each time up to COPY_LOOKS_AGAIN_MOST_MS, while the server may be stopped.
const COPY_LOOKS_AGAIN_MS = 300;
const COPY_LOOKS_AGAIN_MOST_MS = 3_000;

export function usePageMenuActions({
  document,
  currentPageId,
  readGraph,
  keepPage,
  onStatus,
  pageMutationOutbox,
  pages,
  refreshBootstrap,
  session,
  setData,
}: {
  document: Y.Doc | null;
  currentPageId: string | null;
  /** The open page's graph as it is now, which its words are counted in. */
  readGraph(): ContentGraph;
  /** Shows and keeps on this device a page whose change waits for the server. */
  keepPage(page: Page): void;
  onStatus: ProductStatusSink;
  /** Keeps a page's settings change until the server takes it. */
  pageMutationOutbox: Pick<NativePageMutationBrowser<unknown>, "admitAndSendMetadata" | "keepMetadata">;
  /** The page list as shown, whose record of a page a change starts from. */
  pages: readonly Page[];
  refreshBootstrap(activeSession: Session, signal?: AbortSignal): Promise<Bootstrap | null>;
  session: Session;
  setData: Dispatch<SetStateAction<Bootstrap>>;
}) {
  const lifecycleGeneration = useRef(0);
  const metadataQueues = useRef(new Map<string, Promise<void>>());
  // Each page's settings changes made here not yet answered, oldest first.
  const unanswered = useRef(new Map<string, string[]>());
  const latestPages = useRef(new Map<string, Page>());
  // A caller may hold the record it was given when the page first showed;
  // a change starts from the newest record of the page instead.
  const shownPages = useRef(pages);
  shownPages.current = pages;
  const newestPage = (page: Page) => {
    const kept = latestPages.current.get(page.id);
    const shown = shownPages.current.find((item) => item.id === page.id) ?? page;
    return kept && kept.cursor >= shown.cursor ? kept : shown;
  };

  useEffect(() => {
    lifecycleGeneration.current += 1;
    return () => {
      lifecycleGeneration.current += 1;
      metadataQueues.current.clear();
      latestPages.current.clear();
    };
  }, [currentPageId, session.user.id]);

  function isCurrent(generation: number, signal?: AbortSignal) {
    return lifecycleGeneration.current === generation && !signal?.aborted;
  }

  // Whether the page open is this page or under it.
  function holdsOpenPage(pageId: string) {
    const byId = pageMapOf(shownPages.current);
    const seen = new Set<string>();
    for (let at = currentPageId ? byId.get(currentPageId) : undefined; at && !seen.has(at.id); at = byId.get(at.parentId)) {
      if (at.id === pageId) return true;
      seen.add(at.id);
    }
    return currentPageId === pageId;
  }

  function enqueueMetadata<T>(pageId: string, operation: () => Promise<T>) {
    const previous = metadataQueues.current.get(pageId) ?? Promise.resolve();
    const current = previous.catch(() => undefined).then(operation);
    const tail = current.then(() => undefined, () => undefined);
    metadataQueues.current.set(pageId, tail);
    void tail.finally(() => {
      if (metadataQueues.current.get(pageId) === tail) metadataQueues.current.delete(pageId);
    });
    return current;
  }

  function changePageMetadata(
    page: Page,
    change: NativePageMetadataChange,
    signal?: AbortSignal,
    titleBase?: PageTitleBase,
  ) {
    const generation = lifecycleGeneration.current;
    // A lock changed here reopens the page's room for this tab too, as a
    // move to or out of the trash does, which turns it read-only or editable
    // (lib/ownLockFlips.ts).
    if (change.isLocked !== undefined || change.inTrash !== undefined) noteOwnLockFlip(page.id);
    // A move — into a wiki or out of one changes what the page is — goes after the titles typed before it (lib/pageKindChange.ts).
    if (change.parentId !== undefined || change.teamspaceId !== undefined) sendTypedPageTitles();
    // A title typed in an open title field goes with the server's title it
    // was typed on, which a newer title is merged with (sendKeptPageMetadata):
    // the page as shown here holds the typing itself.
    const madeOn = (current: Page) => ({ ...pageMetadataBaseline(current), ...titleBase });
    // Kept on this device as it is made, though the page's earlier change
    // isn't answered yet: one waiting behind it was lost when the page went
    // meanwhile (as the content graph's writes, 2026-11-04). A change that
    // may be called off (a signal) waits its turn to be kept. A wiki's page's
    // goes so too, sent as its wiki database's page (sendKeptPageMetadata).
    const mutationId = newId();
    const queued = newestPage(page);
    // Made after the page's changes not yet answered here, it names them: the
    // server takes it on the page as they leave it, whose cursor this device
    // doesn't know yet (page_metadata_after_own.go; 2026-11-27: a title's ⌘Z
    // made before its letter's save was answered was refused for its cursor,
    // then merged away, the letter left).
    const before = unanswered.current.get(page.id) ?? [];
    unanswered.current.set(page.id, [...before, mutationId]);
    const after = before.length ? { after: [...before] } : {};
    if (!signal) {
      void pageMutationOutbox.keepMetadata({ baseline: madeOn(queued), change, mutationId, pageId: page.id, ...after }).catch(() => undefined);
    }
    const saving = enqueueMetadata<Page | false>(page.id, async () => {
      signal?.throwIfAborted();
      const baseline = newestPage(page);
      const nextTitle = change.title === undefined ? baseline.title : change.title;
      const optimistic = { ...optimisticPageMetadata(baseline, change, session.user.id), title: nextTitle };
      latestPages.current.set(page.id, optimistic);
      // The list shows the change before the server answers (user decision
      // 2026-11-12: a press answers on the next frame), a page going to the
      // trash too, which leaves the sidebar at once; a refusal reads the list
      // again below, which puts it back. The page open, or one it is under,
      // the menu that trashes it takes from the list as it moves on (RowMenu).
      const leavesOpenPage = change.inTrash === true && baseline.inTrash !== true && holdsOpenPage(page.id);
      // Shown before it goes, it holds the reads asked for meanwhile until
      // it has landed (lib/ownWritesInFlight.ts): the page it moved from or
      // to, open, reads its body once the server has it.
      const release = holdOwnWrite();
      // A title typed in its field shows already wherever the page is named
      // (lib/pageNames.ts): the list takes it, and the server's answer,
      // in a drawing a key pressed meanwhile goes before, not in one that held
      // the keys of a 44,000-page workspace 218–357 ms at CPU ×2 (2026-11-15).
      const shownAlready = change.title !== undefined && Object.keys(change).every((key) => key === "title" || key === "titleSpans")
        && typedPageTitle(page.id, baseline.title) === change.title;
      const putInList = (next: SetStateAction<Bootstrap>) => (shownAlready ? startTransition(() => setData(next)) : setData(next));
      if (!leavesOpenPage && isCurrent(generation, signal)) {
        putInList((current) => ({
          ...current,
          pages: current.pages.map((item) => item.id === page.id ? { ...optimistic, hasContent: optimistic.hasContent ?? item.hasContent } : item),
        }));
      }
      try {
        // The change is kept on this device and goes out in order with the
        // page's other kept changes; one the server can't be reached for
        // waits for it, across a reload (2026-10-26: a title typed then was
        // gone after a reload). A change made since the page was read here —
        // on another device, or the page becoming a database — is made again
        // on the page as it now is (sendKeptPageMetadata).
        const delivery = await pageMutationOutbox.admitAndSendMetadata({
          baseline: madeOn(baseline), change, mutationId, pageId: page.id, ...after,
        }, signal).finally(release);
        if (delivery.state !== "committed") {
          // Kept here for a server slow to answer (past the outbox's wait), or sent by another tab, it isn't
          // refused: it goes out in turn and lands, though the open page changed meanwhile — as trashing the
          // open page moves the screen on at once (2026-11-17: a server stalled 9 s said "couldn't move" and
          // put the page back, which went to the trash once the server came back).
          if (!isCurrent(generation, signal)) return signal?.aborted ? false : optimistic;
          keepPage(optimistic);
          if (delivery.state === "kept") {
            onStatus(i18next.t(keptChangeStatus()));
          }
          return optimistic;
        }
        const result = delivery.result as Awaited<ReturnType<typeof sendKeptPageMetadata>>;
        latestPages.current.set(page.id, result.page);
        notePageTitlesFromServer([result.page]);
        // The server made it: a change from a list holds though the open page
        // changed meanwhile, as the page the app falls back to does when a
        // list's chosen pages go to the trash (2026-11-05: the library's
        // fifteen chosen pages stopped after three with "couldn't move", their
        // fallback page among them). Only this screen's showing of it waits.
        if (!isCurrent(generation, signal)) return signal?.aborted ? false : result.page;
        putInList((current) => ({
          ...current,
          cursor: result.receipt.cursor,
          pages: current.pages.map((item) => item.id === page.id ? { ...result.page, hasContent: result.page.hasContent ?? item.hasContent } : item),
        }));
        // A page moved or restored into a wiki became its database's page
        // with the pages under it.
        if (result.page.wikiDataSourceId && !baseline.wikiDataSourceId) announceWikiChange(page.id);
        // A page moved to another place took the pages under it along; the
        // workspace is read again for where a wiki's page's change put it.
        if ((result.page.teamspaceId ?? "") !== (baseline.teamspaceId ?? "") || isWikiDatabasePage(baseline)) await refreshBootstrap(session, signal);
        // Moving to or from the trash says so in its own notice; nothing was restyled.
        if (change.inTrash === undefined) {
          onStatus(change.title === undefined ? i18next.t("syncStatusBadge:status.pageStyleSaved") : i18next.t("syncStatusBadge:status.titleSaved"));
        }
        return result.page;
      } catch (error) {
        release();
        if (signal?.aborted) throw error;
        // Refused, the change goes back at once wherever it showed — the
        // list (the sidebar's rows) and a title typed over it (the top bar's
        // path, the window's title), as the page's head does — before the
        // list is read again (2026-11-15: a title the DLP policy refused
        // stayed in the sidebar and the tab).
        if (latestPages.current.get(page.id) === optimistic) latestPages.current.set(page.id, baseline);
        if (change.title !== undefined) dropTypedPageTitle(page.id);
        if (isCurrent(generation, signal)) {
          putInList((current) => ({ ...current, pages: current.pages.map((item) => item.id === page.id ? { ...baseline, hasContent: item.hasContent ?? baseline.hasContent } : item) }));
          onStatus(authenticatedProductErrorMessage(error, "pageTitleUpdate"), "error");
          await refreshBootstrap(session, signal);
        }
        return false;
      }
    });
    // A write changing a page's kind waits for it (lib/pageKindChange.ts).
    notePageWriteOnItsWay(saving);
    const settled = () => {
      const rest = (unanswered.current.get(page.id) ?? []).filter((id) => id !== mutationId);
      if (rest.length) unanswered.current.set(page.id, rest);
      else unanswered.current.delete(page.id);
    };
    void saving.then(settled, settled);
    // A page opened while it moves to or out of the trash is read once it has (lib/pageTrashMoves.ts).
    return change.inTrash === undefined ? saving : noteTrashMove(page.id, saving);
  }

  async function duplicatePage(page: Page, signal: AbortSignal, options?: PageDuplicationOptions & { onOpened?: (copy: Page) => void }) {
    const generation = lifecycleGeneration.current;
    // The copy shows in the list at once, beside its page or last in the page
    // it goes in (shownPageCopy), takes the server's ID once the server has
    // it and its record once it is whole, and goes if it fails. One to private
    // pages or another workspace, or a wiki's page, shows once it is made.
    const shown = isWikiDatabasePage(page) || options?.toPrivatePages
      || (options?.workspaceId && options.workspaceId !== page.workspaceId) ? null : shownPageCopy(page, shownPages.current, options?.parentId);
    let shownId = shown?.id;
    const showAs = (copyId: string | undefined) => {
      if (!shown || shownId === copyId) return;
      const was = shownId;
      shownId = copyId;
      setData((current) => {
        const listed = current.pages.find((item) => item.id === was) ?? shown;
        const rest = { ...current, pages: current.pages.filter((item) => item.id !== was) };
        return copyId && !rest.pages.some((item) => item.id === copyId) ? upsertBootstrapPage(rest, { ...listed, id: copyId }) : rest;
      });
    };
    if (shown) setData((current) => upsertBootstrapPage(current, shown));
    try {
      return await duplicateShown(page, signal, generation, showAs, (copyId) => shown ? { ...shown, id: copyId } : null, options);
    } catch (error) {
      // Not made, it goes; made and then failed, the list is read again.
      if (shown && shownId === shown.id) showAs(undefined);
      else if (shown) void refreshBootstrap(session).catch(() => undefined);
      throw error;
    }
  }

  async function duplicateShown(page: Page, signal: AbortSignal, generation: number, showAs: (copyId: string) => void,
    shownCopy: (copyId: string) => Page | null, options?: PageDuplicationOptions & { onOpened?: (copy: Page) => void }) {
    // A wiki's page is duplicated as its database page, beside it in the wiki,
    // once what this device wrote of it went (keptSentFor); to private pages
    // it goes as a page of its own.
    const readRow = () => readDatabaseRowPage(session, page.id, { signal });
    const duplicateId = isWikiDatabasePage(page) && !options?.toPrivatePages
      ? (await duplicateDatabaseRowPage(session, page.id, {
        requestId: newId(),
        expectedAuthorityRevision: (await readRow().then(async (read) => await keptSentFor(session, read.row.containerPageId) ? readRow() : read)).row.authorityRevision,
      }, signal)).rowId
      : (await duplicateNativePage(session, page, signal, { ...options, onStarted: (targetId, finished) => {
        showAs(targetId);
        // A copy quick enough opens whole, the way it ends below. A longer
        // one opens by then, as Notion's (seen 2026-11-05: 1.6 s): the one
        // the list shows, under the ID of the top the server made as it took
        // the copy on, opens without reading the list again (2026-11-11: a
        // workspace of 7,600 pages opened it after 4.7 s of list reads, and
        // never while the server stopped meanwhile, the six reads failing),
        // filling as the copy goes. One the list doesn't show (a wiki's page,
        // one to private pages or another workspace) opens once a list read
        // has it, looked for until the copy ends, through a server stopped.
        void (async () => {
          let settled = false;
          const ended = finished.then(() => { settled = true; }, () => { settled = true; });
          await Promise.race([ended, new Promise((resolve) => window.setTimeout(resolve, COPY_OPENS_BY_MS))]);
          if (settled || !isCurrent(generation, signal)) return;
          const listed = shownCopy(targetId);
          if (listed) {
            options?.onOpened?.(listed);
            return;
          }
          for (let wait = COPY_LOOKS_AGAIN_MS; !settled && isCurrent(generation, signal); wait = Math.min(wait * 2, COPY_LOOKS_AGAIN_MOST_MS)) {
            const bootstrap = await refreshBootstrap(session, signal).catch(() => null);
            const copy = bootstrap?.pages.find((item) => item.id === targetId);
            if (copy) {
              if (!settled && isCurrent(generation, signal)) options?.onOpened?.(copy);
              return;
            }
            await Promise.race([ended, new Promise((resolve) => window.setTimeout(resolve, wait))]);
          }
        })().catch(() => undefined);
      } })).targetRootPageId;
    signal.throwIfAborted();
    const bootstrap = await refreshBootstrap(session, signal);
    signal.throwIfAborted();
    if (!isCurrent(generation, signal)) throw new DOMException("Aborted", "AbortError");
    const duplicate = bootstrap?.pages.find((item) => item.id === duplicateId);
    if (!duplicate) throw new Error("Duplicated page is missing from the current bootstrap snapshot.");
    return duplicate;
  }

  function readDocumentStats(page: Page) {
    latestPages.current.set(page.id, page);
    return pageBodyStats(readGraph(), document, page.content, (id) => shownPages.current.find((item) => item.id === id)?.title);
  }

  return {
    changePageMetadata,
    changePagePresentation: (page: Page, change: NativePageMetadataChange) => {
      const before = presentationBefore(newestPage(page), change);
      const saved = changePageMetadata(page, change);
      void saved.then((done) => {
        if (done !== false) notePageChangeStep(page.id, before, change as PagePresentationChange, (step) => changePageMetadata(page, step));
      }, () => undefined);
      return saved;
    },
    /** Saves a title, with its spans when written with them (none for plain words), on the title it was typed on. */
    changePageTitle: (page: Page, title: string, base?: PageTitleBase, titleSpans?: TextSpan[]) =>
      changePageMetadata(page, { title, ...(titleSpans ? { titleSpans } : {}) }, undefined, base),
    /**
     * A title being typed, in every place that names the page at once (sidebar, path, window title), which
     * each follow it alone (lib/pageNames.ts); the list takes it as changePageTitle saves it.
     */
    // Over the name every place knows now: a rename box writes what it has as typing pauses, its last syllable still composing.
    showTypedTitle: (page: Page, title: string) => showTypedPageTitle(page.id, title, knownPageName(page.id)?.title ?? page.title),
    duplicatePage,
    readDocumentStats,
    trashPage: (page: Page, inTrash: boolean, signal: AbortSignal) => (
      changePageMetadata(page, { inTrash }, signal)
    ),
  };
}
