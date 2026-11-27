import { forgetAct } from "./actedSince";

/**
 * Drops a press on a page still opening that nothing took
 * (takePageFramePress, pageTitleFocus.ts) when it is a finger's or a pen's
 * tap: it puts no caret down and raises no soft keyboard, and the opening goes
 * on as if it had not come — a titled page's head scrolled away on a phone, an
 * untitled one's title taking the caret (emptyPageTitleCaret.ts, which leaves
 * an opening alone once the person acted) — as Notion's phone (seen 2026-11-24
 * in Chrome on a Galaxy A90, 411 wide: pages opened from their links on a
 * slowed network, tapped while they still drew their skeleton where their
 * text, their title and the room beside and below their lines were to stand,
 * six taps in two tabs with nothing in the page before them: no caret, no
 * keyboard, each page shown with its head scrolled away). Held for the body,
 * the tap gave the field holding what is typed the focus at once, raising the
 * keyboard before the page showed, and where the body then put no caret it
 * dropped up to five seconds on. A mouse's press still waits for the body, its
 * caret going where it was pressed (Notion's first press on a page just
 * opened, 2026-11-10). Says whether it dropped it.
 */
export function dropTapWhileOpening(press: PointerEvent) {
  if (press.pointerType === "mouse") return false;
  forgetAct(press);
  return true;
}
