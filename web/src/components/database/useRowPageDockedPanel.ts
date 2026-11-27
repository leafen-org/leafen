import { useEffect, useLayoutEffect, useRef } from "react";
import { panelOpeningBeside, usePageSidePanel } from "../page/usePageSidePanel";

/** A panel a database page can dock beside it: whether it is open, and how it opens and closes. */
type RowPagePanel = { open: boolean; show?: () => void; close: () => void };

/**
 * What a database page opened whole docks beside it, as a page does
 * (PageView, usePageSidePanel) and as Notion's row page does (seen 2026-11-25
 * in Chrome at 1440, dpr 2, two tabs at different times: its "모든 토론" docked
 * as its aside, 1055·0·385, the page narrowed beside it, its bar's "»" closing
 * it, open again over a reload; its updates likewise): all its discussions or
 * its updates, the one opened last, the other closing as it opens. In a peek
 * nothing docks — a peek's "모두 보기" opens the page whole with them docked
 * (openWholeWith) — and on a phone they open over the page as before.
 *
 * With `keep`, the page keeps the panel open as it opens and closes there
 * (pageSidePanelKept), and opens again with the one kept or asked from a peek
 * once it is read (panelOpeningBeside) — asked once a row, when first read,
 * whatever it answers: asked again later, it took the panel the reader had
 * just docked for kept and opened it again as the reader's "»" or the bar's
 * comments closed it (2026-11-25).
 */
export function useRowPageDockedPanel({ rowId, whole, read, keep, all, updates }: {
  rowId: string;
  /** The page is open whole, not in a peek. */
  whole: boolean;
  /** The page has been read: a kept or asked panel opens with it then. */
  read: boolean;
  keep: boolean;
  /** Its "모든 토론". */
  all: RowPagePanel;
  updates: RowPagePanel;
}) {
  const kind = !whole ? null : all.open ? "discussions" : updates.open ? "updates" : null;
  const latest = useRef({ all, updates });
  latest.current = { all, updates };
  const docked = usePageSidePanel(kind, () => { latest.current.all.close(); latest.current.updates.close(); }, whole && keep ? rowId : undefined);
  // The one opened now takes the other's place before it is drawn.
  const was = useRef({ all: all.open, updates: updates.open });
  useLayoutEffect(() => {
    const before = was.current;
    was.current = { all: all.open, updates: updates.open };
    if (!whole || !all.open || !updates.open) return;
    if (!before.updates) latest.current.all.close();
    else if (!before.all) latest.current.updates.close();
  }, [all.open, updates.open, whole]);
  const askedFor = useRef<string | null>(null);
  useEffect(() => {
    if (askedFor.current === rowId || !whole || !keep || !read) return;
    askedFor.current = rowId;
    const beside = panelOpeningBeside(rowId);
    (beside === "updates" ? latest.current.updates : beside === "discussions" ? latest.current.all : null)?.show?.();
  }, [keep, read, rowId, whole]);
  const discussionsDocked = docked && kind === "discussions";
  return {
    /** A panel is docked beside the page, which makes room for it. */
    docked,
    /** Its "모든 토론" is the panel docked: the bar's comments close it (barCommentsPress). */
    discussionsDocked,
    /** The bar's "»", closing the panel docked. */
    onClosePanel: docked ? (discussionsDocked ? all.close : updates.close) : undefined,
  };
}
