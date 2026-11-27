import type { GraphView } from "@/contentGraph";
import { i18next } from "@/i18n";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { keysLedFocus } from "@/lib/lastPress";

/**
 * Rows dropped in a sorted view, as Notion's (seen 2026-11-24 in two tabs at
 * 1685 wide, dpr 1: a database's table — its sort the viewer's own and saved
 * for everyone — list, gallery and timeline's table; "내 작업"'s list too):
 * the rows stay where the sort puts them, the view's own order takes the
 * place they were dropped at (viewRowOrderPlacing), and the shared card in
 * the window's middle asks "정렬을 제거하시겠습니까?" over a red-outlined
 * "제거", which holds the focus, and "제거하지 않음" (324×145, round 12).
 * "제거" takes the view's sorts off, the rows then standing as dropped;
 * "제거하지 않음", Escape and the shade keep the sort, the dropped order kept
 * for when it goes. A board, whose cards move in their groups, and a
 * calendar, whose cards don't move within a day, don't ask.
 *
 * A row made by a row's "+" (⌥ over it) asks the same, as Notion's (seen
 * 2026-11-24 in two tabs at 1685 wide, dpr 1: a table's, also grouped, a
 * list's and a timeline's table's; asked also by the last row, where the
 * sort puts it anyway): it shows where the sort puts it, "제거" takes the
 * sorts off with the row standing beside the row whose "+" made it, the
 * shown order written down, and "제거하지 않음" leaves it where the sort puts
 * it. ⌘Z after "제거" brings the sort back, then takes the row away; after
 * "제거하지 않음" it takes the row away (a timeline's took one more ⌘Z that
 * changed nothing shown, in both tabs; not followed). Its title is not open for typing
 * once answered: a letter typed then went nowhere. "+ 새 페이지" under the
 * rows, a group's "+" and a gallery's "+ 새 페이지" make a row that doesn't
 * ask (useDatabasePersonalQueries rowMadeBeside).
 */
export function viewShowsSorted(view: GraphView | undefined): view is GraphView {
  return Array.isArray(view?.config.sorts) && view.config.sorts.length > 0;
}

/**
 * Asks whether the sort goes, after rows were dropped in a sorted view: true
 * for "제거". Answered, it leaves the focus on the page, as Notion's (seen
 * 2026-11-24 in two tabs, judged after the answer: no handle ringed, and the
 * body held it after Escape and after "제거하지 않음", a letter typed going
 * nowhere) — back on a row's handle only when the keys moved it (⌥↑↓,
 * useDragReorder), the handle then holding the focus as it asks. Asked for a
 * row just made (`made`), it leaves the focus on the page whatever held it.
 */
export function askRemoveSortAfterDrop(made = false): Promise<boolean> {
  const active = typeof document === "undefined" || made ? null : document.activeElement;
  return confirmAction({
    message: i18next.t("databaseView:sortedDropQuestion"),
    confirmLabel: i18next.t("databaseView:sortedDropRemove"),
    cancelLabel: i18next.t("databaseView:sortedDropKeep"),
    danger: true,
    returnFocus: active instanceof HTMLElement && keysLedFocus(active),
  });
}
