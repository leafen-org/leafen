import { parseStored, personValue, storePersonValue } from "@/lib/browserStorage";
import type { LibraryViewId } from "@/lib/router";
import { libraryViews } from "./libraryModel";

export const LIBRARY_VIEW_ORDER_KEY = "leafen:library-view-order:v1";
const LIBRARY_LAST_VIEW_KEY = "leafen:library-last-view:v1";

export function readLibraryViewOrder(userId: string, workspaceId: string, views: readonly LibraryViewId[]): LibraryViewId[] {
  const stored = parseStored(personValue(LIBRARY_VIEW_ORDER_KEY, { userId, part: workspaceId }));
  const kept = Array.isArray(stored) ? stored.filter((view): view is LibraryViewId => views.includes(view)) : [];
  // A view the stored order doesn't name keeps its place after the ones it does.
  return [...new Set([...kept, ...views])];
}

/** Keeps the library's tab last seen in a workspace, on this browser. */
export function rememberLibraryView(userId: string, workspaceId: string, view: LibraryViewId): void {
  storePersonValue(LIBRARY_LAST_VIEW_KEY, view, { userId, part: workspaceId });
}

/**
 * The tab "/library" opens on, naming none: the one last seen in the
 * workspace, as Notion's (seen 2026-11-12, a member and a guest alike);
 * before any, a guest's pages shared with them and otherwise the first tab.
 */
export function lastLibraryView(userId: string, workspace: { id: string; guest?: boolean }): LibraryViewId {
  const guest = workspace.guest === true;
  const views = libraryViews(guest);
  const last = personValue(LIBRARY_LAST_VIEW_KEY, { userId, part: workspace.id });
  if (last && (views as readonly string[]).includes(last)) return last as LibraryViewId;
  if (guest) return "shared";
  return readLibraryViewOrder(userId, workspace.id, views)[0] ?? "recents";
}

/**
 * The tab the sidebar's "라이브러리" opens on: a member's the last one seen,
 * as Notion's "더 보기 › 라이브러리"; a guest's always the pages shared with
 * them, as Notion's guest "라이브러리" button (both seen 2026-11-12).
 */
export function libraryOpeningView(userId: string, workspace: { id: string; guest?: boolean }): LibraryViewId {
  return workspace.guest === true ? "shared" : lastLibraryView(userId, workspace);
}
