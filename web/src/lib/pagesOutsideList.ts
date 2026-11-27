import { api } from "@/api";
import { LeafenApiError } from "@/apiTransport";
import type { Bootstrap, Page, Session } from "@/types";
import { upsertBootstrapPage } from "./bootstrapPageProjection";
import { openPageAsRow } from "./pageBecomingRow";
import { isPageBusyRefusal } from "./pageBusy";

// A page the reader may open that their list doesn't hold — an open
// teamspace's page they haven't joined — opens from search or by its
// address as any page does,
// read from the server when the list lacks it. It stays through the list's
// later reads, as it is shown, and stands in no section of the sidebar
// (pageTreeModel's inPageTree), as Notion's search opens a page its sidebar
// doesn't list (2026-11-14: its address said "이 페이지에 대한 사용 권한
// 없음", and opened from search it stood under "개인 페이지" until the list
// was read again). Once a list holds it, it is the list's. Open, it is read
// again as the list is and when its page says it changed, so what became of
// it meanwhile shows as on a page the list holds (recheckPageOutsideList).
// A database's page read so is no page of the list: it opens as its row's
// page (pageBecomingRow.ts).

const kept = new Set<string>();
// Counts the changes to which pages are kept so: a result worked out from a
// list stands for a list made from it only while this is the same (pageTreeModel.ts).
let keptChanges = 0;

type ListChange = (change: (current: Bootstrap) => Bootstrap) => void;

/**
 * The list with a page being opened: one the list holds keeps the list's own
 * record, since what asked to open it may hold a copy made for showing (a
 * favorite under a trashed page is marked trashed for the sidebar, without
 * its trash time, so opening it lost the trash banner, 2026-11-09); one it
 * lacks (a search result's, or one read by its address) comes in, kept.
 */
export function withOpenedPage(current: Bootstrap, page: Page): Bootstrap {
  if (current.pages.some(({ id }) => id === page.id)) return current;
  kept.add(page.id);
  keptChanges += 1;
  return upsertBootstrapPage(current, page);
}

// The open pages a list's changes took away, by the workspace they were
// listed in (notePagesTakenAway).
const takenFrom = new Map<string, string>();
// A page held a while — moved to another workspace, copied — is read again
// past its hold, a second apart, for up to two minutes.
const heldReadPauseMs = 1_000;
const heldReadTries = 120;

/**
 * Notes the open pages a list's changes took away (listFollowReads.ts), by
 * the workspace the list held them in, for the screen that reads them again.
 */
export function notePagesTakenAway(listed: Bootstrap, removedPageIds: readonly string[]) {
  for (const id of removedPageIds) {
    const page = listed.pages.find((item) => item.id === id);
    if (page) takenFrom.set(id, page.workspaceId);
  }
}

/**
 * Reads a page the list doesn't hold, for the screen of a page not in it
 * (AuthenticatedProductRoute's PageProblem): one the reader may open comes
 * in, kept; a database's page — the open page moved into a database in
 * another tab or by someone else — opens as its row's page. One an
 * operation holds (a move to another workspace, page_workspace_move.go) is
 * read again once it lets go, rather than said to be closed to the reader
 * (2026-11-23: a page moved by an agent while open here said "회사 A에서 이
 * 페이지에 대한 사용 권한 없음" until reloaded). One the list took away that
 * now reads in another workspace of the person's moved there: that
 * workspace's list, which the list's changes of the first didn't follow, is
 * read again whole, so the page stands in its sidebar there with its pages.
 */
export async function readPageOutsideList(session: Session, pageId: string, change: ListChange, readList?: () => Promise<unknown>) {
  const page = await readPastHold(session, pageId);
  if (page.databaseRow) return openPageAsRow(session, page.id);
  const from = takenFrom.get(pageId);
  takenFrom.delete(pageId);
  change((current) => withOpenedPage(current, page));
  if (from && from !== page.workspaceId) await readList?.();
}

async function readPastHold(session: Session, pageId: string) {
  for (let tries = 1; ; tries += 1) {
    try {
      return await api.readPage(session, pageId);
    } catch (error) {
      if (!isPageBusyRefusal(error) || tries >= heldReadTries) throw error;
      await new Promise((resolve) => setTimeout(resolve, heldReadPauseMs));
    }
  }
}

/**
 * Reads the open page kept here again: put in the trash, it shows its
 * banner; no longer the reader's or gone, it leaves, and its screen says so
 * (PageProblem); a database's page now, it opens as its row's page. Not
 * reached, it stays as shown (2026-11-14: kept as shown through every later
 * list read, it stayed open after its trash or its access taken away).
 */
export function recheckPageOutsideList(session: Session, pageId: string | null, change: ListChange) {
  if (!pageId || !kept.has(pageId)) return;
  const leave = () => {
    kept.delete(pageId);
    keptChanges += 1;
    change((current) => ({ ...current, pages: current.pages.filter(({ id }) => id !== pageId) }));
  };
  void api.readPage(session, pageId).then(async (page) => {
    if (!kept.has(pageId)) return;
    if (page.databaseRow) return openPageAsRow(session, pageId).then(leave);
    change((current) => current.pages.some(({ id }) => id === pageId) ? upsertBootstrapPage(current, page) : current);
  }, (error: unknown) => {
    if (kept.has(pageId) && error instanceof LeafenApiError && error.status === 404) leave();
  });
}

/** Whether the page is open here though the list doesn't hold it. */
export function pageOutsideList(pageId: string) {
  return kept.has(pageId);
}

/** How many times which pages are kept outside the list changed. */
export function pagesOutsideListChanges() {
  return keptChanges;
}

/** A list read afresh, with the pages kept here it lacks as `shown` holds them now, in its workspaces. */
export function withPagesOutsideList(list: Bootstrap, shown: readonly Page[]): Bootstrap {
  if (!kept.size) return list;
  const listed = new Set(list.pages.map(({ id }) => id));
  for (const id of [...kept]) {
    if (listed.has(id)) {
      kept.delete(id);
      keptChanges += 1;
    }
  }
  const workspaces = new Set(list.workspaces.map(({ id }) => id));
  const adding = shown.filter((page) => kept.has(page.id) && workspaces.has(page.workspaceId));
  return adding.length ? { ...list, pages: [...list.pages, ...adding] } : list;
}
