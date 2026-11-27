import { useEffect, useSyncExternalStore } from "react";
import { request } from "@/apiTransport";
import { useProductPagesSelect } from "@/components/product/productPagesStore";
import type { Page, Session } from "@/types";
import { keepRowPageReads, readKeptRowPage } from "./keptRowPage";
import { forgetPagePath, knownPathAboveId, pagePathsVersion, pathLeavesOwnPages, rememberPagePaths, subscribePagePaths, type PagePathPieceWire } from "./pagePathPieces";
import { isServerUnreachable, readUntilServerAnswers } from "./serverReach";

// As many pages as the server reads paths for at once (page_path_route.go).
const PATHS_PER_READ = 50;
// A path read this recently isn't read again for a page opened anew.
const FRESH_FOR_MS = 2_000;

const asking = new Set<string>();
const readAt = new Map<string, number>();
/** Where each page was, as its reader said, when its path was last asked for. */
const askedAt = new Map<string, string>();
/** The path last kept with each page's copy here, as the server read it. */
const keptPaths = new Map<string, string>();
/** The pages whose paths are read again once the server may answer (readAgainOnceAnswered). */
const awaiting = new Set<string>();

/**
 * Keeps the path of a page opened here with the page's copy
 * (keptRowPage.ts), each time the server reads it anew, so the page opened
 * without the server draws the path it drew with it (2026-11-11: offline,
 * a row page opened by its address lost its database, "A / 행").
 */
function keepPagePath(session: Session, pageId: string, path: readonly PagePathPieceWire[]) {
  const kept = JSON.stringify(path);
  if (keptPaths.get(pageId) === kept) return;
  keptPaths.set(pageId, kept);
  keepRowPageReads(session, pageId, { path: [...path] });
}

/** The paths the given pages' copies here keep, for those that keep one. */
async function readKeptPagePaths(session: Session, pageIds: readonly string[]) {
  const kept = await Promise.all(pageIds.map(async (id) => [id, (await readKeptRowPage(session, id))?.path] as const));
  return Object.fromEntries(kept.filter((entry): entry is readonly [string, PagePathPieceWire[]] => Array.isArray(entry[1])));
}

/** Asks the server for the paths above the pages. */
function requestPagePaths(session: Session, workspaceId: string, pageIds: readonly string[]) {
  return request<{ paths: Record<string, PagePathPieceWire[]> }>(
    `/workspaces/${encodeURIComponent(workspaceId)}/page-paths?ids=${pageIds.map(encodeURIComponent).join(",")}`, {}, session,
  );
}

/**
 * Takes the paths the server read for the pages: a page the reader can't
 * read has nothing above it to draw; `keep` keeps each with its page's copy.
 */
function pagePathsRead(session: Session, pageIds: readonly string[], paths: Record<string, PagePathPieceWire[]> | undefined, keep: boolean) {
  const read = Object.fromEntries(pageIds.map((id) => [id, paths?.[id] ?? []]));
  if (keep) for (const id of pageIds) keepPagePath(session, id, read[id] ?? []);
  return read;
}

/**
 * Reads the paths the server didn't answer for again once it may
 * (readUntilServerAnswers), so a bar drawn meanwhile from what this device
 * kept names what is there now: it kept the names it was opened with after
 * the server was back (2026-11-25). Each page waits once at a time.
 */
function readAgainOnceAnswered(session: Session, workspaceId: string, pageIds: readonly string[], keep: boolean) {
  const waiting = pageIds.filter((id) => !awaiting.has(id));
  for (const id of waiting) awaiting.add(id);
  if (!waiting.length) return;
  readUntilServerAnswers(() => requestPagePaths(session, workspaceId, waiting).catch((error: unknown) => {
    if (isServerUnreachable(error)) throw error;
    return null;
  }), (answer) => {
    for (const id of waiting) awaiting.delete(id);
    if (!answer) return;
    for (const id of waiting) readAt.set(id, Date.now());
    rememberPagePaths(workspaceId, pagePathsRead(session, waiting, answer.paths, keep));
  });
}

/**
 * Reads the paths above the pages from the server, or without it from what
 * their copies here keep, and again once it may answer; `keep` keeps each
 * path read with its page's copy, as the bar of a page opened here does.
 */
async function readPagePaths(session: Session, workspaceId: string, pageIds: readonly string[], keep: boolean) {
  for (let start = 0; start < pageIds.length; start += PATHS_PER_READ) {
    const batch = pageIds.slice(start, start + PATHS_PER_READ);
    for (const id of batch) asking.add(id);
    let read: Record<string, readonly PagePathPieceWire[]> = {};
    try {
      const { paths } = await requestPagePaths(session, workspaceId, batch);
      read = pagePathsRead(session, batch, paths, keep);
    } catch (error) {
      // Without the server a page draws the path its copy keeps, till the
      // server answers; else the path the reader's own pages draw stays.
      if (isServerUnreachable(error)) {
        read = await readKeptPagePaths(session, batch);
        readAgainOnceAnswered(session, workspaceId, batch, keep);
      }
    } finally {
      for (const id of batch) {
        asking.delete(id);
        readAt.set(id, Date.now());
      }
      // A path waiting on this read draws what it gave, else what the reader's own pages give.
      rememberPagePaths(workspaceId, read);
    }
  }
}

/**
 * Whether the server has yet to answer for the path above a page this window
 * never read it for: no read has named it, nor has one failed.
 */
export function pagePathUnread(pageId: string) {
  return knownPathAboveId(pageId) === undefined && !readAt.has(pageId);
}

/**
 * Reads ahead the path above a page this window hasn't read, for what a
 * press will draw under it: a sidebar section's row the pointer comes to,
 * whose "+" makes a page under it (useSidebarViewRowActions.tsx).
 */
export function readPagePathAhead(session: Session, workspaceId: string, pageId: string) {
  if (!workspaceId || !pageId || asking.has(pageId) || knownPathAboveId(pageId) !== undefined) return;
  void readPagePaths(session, workspaceId, [pageId], false);
}

/**
 * Reads from the server the paths above the given pages that this window
 * hasn't read — again for each page with `fresh`, as the bar of a page just
 * opened reads its path, and keeps it with the page's copy here — and draws
 * again once they come, returning the read paths' version for a memo to
 * follow. Without the server a page's path is the one its copy keeps. A
 * page given with where it is (`place`, as its reader knows it) that has
 * gone elsewhere since its path was read — restored, moved — forgets that
 * path and reads it again at once.
 */
export function usePagePathsAbove(
  pages: readonly (Pick<Page, "id" | "workspaceId"> & { place?: string })[], options: { fresh?: boolean } = {},
) {
  // The session alone of the product pages: a page list read again draws nothing here (productPagesStore.tsx).
  const session = useProductPagesSelect((productPages) => productPages?.session);
  const version = useSyncExternalStore(subscribePagePaths, pagePathsVersion, pagePathsVersion);
  const fresh = Boolean(options.fresh);
  const key = JSON.stringify(pages.map(({ id, workspaceId, place }) => [workspaceId, id, place ?? null]));
  useEffect(() => {
    const entries = JSON.parse(key) as [string, string, string | null][];
    if (!session || !entries.length) return;
    const wanted = new Map<string, string[]>();
    for (const [workspaceId, id, place] of entries) {
      if (!workspaceId || !id || asking.has(id)) continue;
      const moved = place !== null && askedAt.has(id) && askedAt.get(id) !== place;
      if (place !== null) askedAt.set(id, place);
      if (moved) forgetPagePath(id);
      const read = readAt.get(id);
      if (!moved && knownPathAboveId(id) !== undefined && (!fresh || (read !== undefined && Date.now() - read < FRESH_FOR_MS))) continue;
      wanted.set(workspaceId, [...(wanted.get(workspaceId) ?? []), id]);
    }
    for (const [workspaceId, ids] of wanted) void readPagePaths(session, workspaceId, ids, fresh);
  }, [fresh, key, session]);
  return version;
}

/**
 * Reads from the server the paths above the shown pages that leave the
 * reader's page list — a database's page, a page under a row — so a location
 * line names the database and the row, as Notion's does.
 */
type PlacedPage = { id: string; parentId?: string | null; workspaceId: string };
export function usePathsAboveShown(shown: readonly PlacedPage[], pagesById: Readonly<Record<string, PlacedPage | undefined>>) {
  return usePagePathsAbove(shown.filter((page) => pathLeavesOwnPages({ id: page.id, parentId: page.parentId ?? "" }, (id) => pagesById[id] as Page | undefined)));
}
