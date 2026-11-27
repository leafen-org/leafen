import { escapeClosing } from "@/lib/overlayLayer";
import { SIDE_PEEK } from "@/lib/peekBesideEscape";
import { holdsPlaced } from "@/lib/popoverLayer";
import { keepsViewChoice } from "./viewChoiceSteps";

/**
 * Whether an Escape pressed now is the window's or menu's open over a view
 * rather than the view's chosen items' (a table's held cell, its ticked rows,
 * a card view's chosen cards, inside `view`): the one Escape closes
 * (escapeClosing), wherever the focus is — on the control that opened it, in
 * it or on the page —, a window whose press keeps the view's choice
 * (keepsViewChoice: a menu, dialog or list, the view's own controls'
 * windows) that neither holds the view nor stands beside the page as a side
 * peek. Such a window takes the Escape before the items and alone: it
 * closes, the items staying chosen, as Notion's (seen 2026-11-26 in Chrome
 * at 1440 in two tabs at different times, a synthetic table, judged after the
 * key by the screen and the next key: a cell chosen, each of the toolbar's
 * "설정", "필터", "정렬", "⚡" and "새로 만들기 ⌄", the chosen view tab's menu
 * and "1개 더 보기" pressed and its window closed with Escape, the ring back
 * on the control — none after "⚡" —, the cell still chosen and ↓ choosing
 * the one under it, a letter after "설정" and after "1개 더 보기" going into
 * it; a row ticked, "설정" and the "+" by the tabs the same, the row still
 * ticked and ↓ ticking the next). A side peek beside the page, or a peek
 * holding the view, closes with the items let go (peekBesideEscape.ts), and
 * so does a panel beside the page as before.
 */
export function escapeForWindowOver(view: Element | null | undefined, event: { target?: EventTarget | null }) {
  const top = escapeClosing(event);
  // A layer's box may only hold its window: the chosen tab's menu draws its dialog inside a plain box.
  const shown = top?.querySelector(':scope > :is([role="menu"], [role="dialog"], [role="listbox"])') ?? top;
  return Boolean(top && shown && view && keepsViewChoice(shown) && !holdsPlaced(top, view) && !top.matches(SIDE_PEEK));
}
