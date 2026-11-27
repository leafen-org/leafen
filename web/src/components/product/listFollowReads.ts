import { startTransition, type Dispatch, type MutableRefObject, type SetStateAction } from "react";
import { api } from "@/api";
import { newestListedMoment, sameChangedPages, withChangedPages, withNewPages } from "@/lib/bootstrapNewPages";
import { noteFullPageDatabases } from "@/lib/inlineDatabaseAddress";
import type { NativeMetadataMutation } from "@/lib/nativePageMutationOutbox";
import { optimisticPageMetadata } from "@/lib/nativePageMetadata";
import { pageMapOf } from "@/lib/pageIndex";
import { notePageTitlesFromServer } from "@/lib/pageTitleFollow";
import { withPagesShownAhead } from "@/lib/pagesShownAhead";
import { notePagesTakenAway, recheckPageOutsideList, withPagesOutsideList } from "@/lib/pagesOutsideList";
import { requestedPageIdFromLocation } from "@/lib/authenticatedProductRoute";
import { requestWorkspaceChangeCheck } from "@/lib/workspaceChangeCheck";
import { followListToken } from "@/lib/workspaceListToken";
import { withDatabaseNamesKept } from "@/lib/databaseNamesKept";
import type { Bootstrap, Page, Session } from "@/types";

// The page changes kept for a whole read under way to take, the last ones
// (its change check reads again what changed since it began anyway).
const maxChangesReadAhead = 64;

/** What the list's reads of others' changes work on (useAuthenticatedBootstrap.ts). */
export type ListFollowState = {
  /** The list as last changed. */
  listNow: MutableRefObject<Bootstrap>;
  /** The list as read or cached. */
  readSnapshot: MutableRefObject<Bootstrap>;
  /** Whose read is shown. */
  shownPrincipalId: MutableRefObject<string | null>;
  /** The pages shown now. */
  shownPages: MutableRefObject<Page[]>;
  /** The page settings changes this device keeps for the server. */
  keptPageChanges: MutableRefObject<(() => Promise<Map<string, NativeMetadataMutation[]>>) | undefined>;
  setData: Dispatch<SetStateAction<Bootstrap>>;
};

/**
 * Draws another screen's change to the list as a transition: the list a
 * 47,000-page workspace draws again for a page made, renamed or put in the
 * trash elsewhere held the main thread 130–260 ms at CPU ×2 (2026-11-15), and
 * a key or click the person pressed meanwhile waited for it; drawn so, it is
 * drawn in pieces a press goes between, and the press is answered first. The
 * list itself (listNow) changes at once, so a change made here meanwhile is
 * made over it.
 */
function drawFollowed(setData: Dispatch<SetStateAction<Bootstrap>>, list: Bootstrap) {
  startTransition(() => setData(list));
}

/**
 * The reads that follow other screens' page changes ahead of, or in place
 * of, the whole list read again: the pages made since the newest one listed
 * (readNewPages), and the pages changed since the list, which the change
 * check brings (takePageChanges). A whole read begun before them that lacks
 * them keeps them (withPagesReadAhead); one begun after has them.
 */
export function createListFollowReads(state: ListFollowState) {
  const { listNow, readSnapshot, shownPrincipalId, shownPages, setData } = state;
  let readAhead: { read: Bootstrap; changed?: string }[] = [];

  /** A whole read's list with what was read ahead of it; its change check then reads again what changed since it began. */
  function withPagesReadAhead(list: Bootstrap): Bootstrap {
    readAhead = readAhead.filter(({ read }) => read.snapshotRevision > list.snapshotRevision);
    if (readAhead.some(({ changed }) => changed)) queueMicrotask(requestWorkspaceChangeCheck);
    return readAhead.reduce((shown, { read, changed }) => (changed
      ? withChangedPages(shown, read, changed, "", true) : withNewPages(shown, read, true)), list);
  }

  // A page made on another screen shows at once when the list has to be
  // read whole: the pages of the workspace made since the newest one listed
  // are read ahead of it (lib/bootstrapNewPages.ts).
  async function readNewPages(activeSession: Session, workspaceId: string): Promise<void> {
    if (shownPrincipalId.current !== activeSession.user.id) return;
    const since = newestListedMoment(readSnapshot.current, workspaceId);
    if (!since) return;
    const read = await api.bootstrapNewPages(activeSession, workspaceId, since);
    const listed = readSnapshot.current;
    if (read.hasMore || !read.pages.length || shownPrincipalId.current !== activeSession.user.id
      || read.snapshotRevision <= listed.snapshotRevision
      || (listed.authorityRevision !== undefined && read.authorityRevision !== listed.authorityRevision)) return;
    // The newest page listed comes again (made at the moment asked about): only a page not shown yet is drawn.
    const shown = pageMapOf(listNow.current.pages);
    if (read.pages.every(({ id }) => shown.has(id))) return;
    readAhead = [...readAhead, { read }];
    notePageTitlesFromServer(read.pages);
    const next = withDatabaseNamesKept(withNewPages(listNow.current, read));
    noteFullPageDatabases(withPagesShownAhead(next).pages);
    readSnapshot.current = next;
    drawFollowed(setData, next);
  }

  // Another person's rename, move, trash or deletion, or a favorite, shows
  // from the pages changed since the list, which the change check brings
  // (bootstrap_page_changes.go), in place of the whole list read again
  // (2026-11-15, NAS condition: 17–25 s in a workspace of 47,000 pages).
  // False when the list has to be read whole: what the person may see
  // changed, or the list doesn't say which changes it holds.
  async function takePageChanges(activeSession: Session, workspaceId: string, changes: Bootstrap, token: string): Promise<boolean> {
    const keep = state.keptPageChanges.current;
    const kept = keep && changes.pages.length ? await keep().catch(() => null) : null;
    const listed = listNow.current;
    if (shownPrincipalId.current !== activeSession.user.id || listed.pageChangeSeq === undefined || changes.pageChangeSeq === undefined
      || (listed.authorityRevision !== undefined && changes.authorityRevision !== listed.authorityRevision)) return false;
    // A whole read since holds more.
    if (changes.pageChangeSeq < listed.pageChangeSeq) return true;
    // A title kept for the server, or a database's name typed here (databaseNamesKept.ts), shows over the one read.
    const read = withDatabaseNamesKept(kept?.size ? withKeptPageChanges(changes, kept, null, activeSession.user.id) : changes);
    if (read.removedPageIds?.length) notePagesTakenAway(listed, read.removedPageIds);
    const next = withChangedPages(listed, read, workspaceId, token);
    followListToken(workspaceId, token);
    // Nothing the list shows changed (another page's body did): it takes
    // the new number and token without being drawn again.
    if (!read.removedPageIds?.length && sameChangedPages(listed, read)) {
      listNow.current = next;
      if (readSnapshot.current === listed) readSnapshot.current = next;
      return true;
    }
    readAhead = [...readAhead, { read: changes, changed: workspaceId }].slice(-maxChangesReadAhead);
    notePageTitlesFromServer(read.pages);
    const shown = withPagesOutsideList(withPagesShownAhead(next), shownPages.current);
    noteFullPageDatabases(shown.pages);
    readSnapshot.current = shown;
    drawFollowed(setData, shown);
    // An open page it took away is asked about, as a whole read's (pagesOutsideList.ts).
    if (read.removedPageIds?.length) recheckPageOutsideList(activeSession, requestedPageIdFromLocation(), setData);
    return true;
  }

  // Another screen's rename or new icon of a workspace the person is in shows
  // in the sidebar head and workspace menu: the workspaces are read again
  // (the list's first part, its workspaces whole and a page) and each listed
  // one takes what was read of it, unless what it shows is newer — this
  // screen's own save. The pages and their tokens stay the list's.
  async function readWorkspaces(activeSession: Session): Promise<void> {
    if (shownPrincipalId.current !== activeSession.user.id) return;
    const read = await api.bootstrap(activeSession, null, undefined, { pageLimit: 1 });
    if (shownPrincipalId.current !== activeSession.user.id) return;
    const readById = new Map(read.workspaces.map((workspace) => [workspace.id, workspace]));
    const withRead = (list: Bootstrap): Bootstrap => {
      let changed = false;
      const workspaces = list.workspaces.map((workspace) => {
        const now = readById.get(workspace.id);
        if (!now || (now.settingsRevision ?? 0) < (workspace.settingsRevision ?? 0)) return workspace;
        const next = { ...workspace, ...now };
        if ((Object.keys(next) as (keyof typeof next)[]).every((key) => next[key] === workspace[key])) return workspace;
        changed = true;
        return next;
      });
      return changed ? { ...list, workspaces } : list;
    };
    if (withRead(listNow.current) === listNow.current) return;
    readSnapshot.current = withRead(readSnapshot.current);
    startTransition(() => setData(withRead));
  }

  return { readNewPages, takePageChanges, withPagesReadAhead, readWorkspaces };
}

/**
 * A page list with the pages made on this device it lacks, in a workspace it
 * lists, and the settings changes kept on this device made over it, oldest first.
 */
export function withKeptPageChanges(list: Bootstrap, kept: Map<string, NativeMetadataMutation[]> | null, made: Page[] | null, actorId: string): Bootstrap {
  const listed = new Set(list.pages.map(({ id }) => id));
  const madeHere = (made ?? []).filter((page) => !listed.has(page.id) && list.workspaces.some(({ id }) => id === page.workspaceId));
  return {
    ...list,
    pages: [...list.pages, ...madeHere].map((page) => (kept?.get(page.id) ?? [])
      .reduce((shown, mutation) => optimisticPageMetadata(shown, mutation.change, actorId), page)),
  };
}
