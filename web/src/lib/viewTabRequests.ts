/**
 * A view's deletion is asked about by its tabs, which own it: a form's
 * "폼 설정" › "양식 삭제" (Notion's, seen 2026-11-04) asks them here.
 */
type DeletionListener = (viewId: string) => boolean;
const deletionListeners = new Set<DeletionListener>();

/** Asks the tabs showing this view to delete it, after their question; false when none can. */
export function requestViewDeletion(viewId: string) {
  for (const listener of deletionListeners) if (listener(viewId)) return true;
  return false;
}

export function listenViewDeletion(listener: DeletionListener) {
  deletionListeners.add(listener);
  return () => { deletionListeners.delete(listener); };
}

/**
 * "기존 데이터 소스 연결하기" in a database's "데이터 소스 관리" asks its tabs
 * for a new table view whose settings open at the sources to choose, as
 * Notion's (seen 2026-11-15: the press makes a "표" view and its "기존 데이터
 * 소스 연결하기" picker opens, × and no way back). The view made is marked so
 * its new-view settings start there whatever the database holds.
 */
type SourceFirstListener = (databaseId: string) => boolean;
const sourceFirstListeners = new Set<SourceFirstListener>();
const sourceFirstViews = new Set<string>();

/** Asks the database's tabs for that view; false when none can make one. */
export function requestSourceFirstView(databaseId: string) {
  for (const listener of sourceFirstListeners) if (listener(databaseId)) return true;
  return false;
}

export function listenSourceFirstView(listener: SourceFirstListener) {
  sourceFirstListeners.add(listener);
  return () => { sourceFirstListeners.delete(listener); };
}

export function markSourceFirstView(viewId: string) {
  sourceFirstViews.add(viewId);
}

/** Whether a view was made to choose its source first (requestSourceFirstView). */
export function sourceFirstView(viewId: string) {
  return sourceFirstViews.has(viewId);
}

// A linked view whose "새 보기" went on to "+ 새 표 보기" has its source
// chosen already: its new-view settings open at the settings themselves, not
// at the sources a linked database's new view asks for first
// (DatabaseLinkedStarter; Notion's, seen 2026-11-18).
const sourceChosenViews = new Set<string>();

export function markSourceChosenView(viewId: string) {
  sourceChosenViews.add(viewId);
}

/** Whether a new view's source was chosen before its settings opened. */
export function sourceChosenView(viewId: string) {
  return sourceChosenViews.has(viewId);
}
