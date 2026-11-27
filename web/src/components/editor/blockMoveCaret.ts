import { type CaretLanding, caretMayGoOnceAnswered, caretOnceAnswered } from "./focus";
import { choosePageBlocks } from "./pageUndo";

/**
 * No caret once a move or its ⌘Z has its answer, as Notion's leaves none
 * (moved chosen to another page or into a database, or taken back from one)
 * — unless something was pressed or typed since, which keeps it
 * (caretMayGoOnceAnswered).
 */
export function leaveNoCaret(mayGo: () => boolean) {
  if (!mayGo()) return;
  if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
  getSelection()?.removeAllRanges();
}

/** The caret a move of blocks off their page, or a step of its ⌘Z and ⌘⇧Z, leaves, asked for at its key (askMovedOffCaret). */
export type MovedOffCaret = { readonly forward: boolean; readonly land: CaretLanding | null; readonly mayGo: () => boolean };

/**
 * Where blocks moved off their page — to another page (blockMoveHistory.ts
 * pageMove) or into a database (blockMoveIntoDatabase.ts) — leave the caret:
 * one rule for the move, its ⌘Z and its ⌘⇧Z, as Notion's. Gone, or done
 * again by ⌘⇧Z, they leave it at the start of the page's title when they
 * went with it (`withCaret`: a line that held it, lines made pages), and
 * none otherwise, nothing chosen, the next key going nowhere; taken back by
 * ⌘Z they leave none, chosen blocks chosen again where they stand (seen in
 * Chrome at 1440 under 한지테스트페이지, pages made by "/페이지", nothing in the
 * page before the keys and a letter typed after each: to another page
 * 2026-11-05, 2026-11-16 and 2026-11-24; into a database 2026-11-24 — a line
 * chosen by Escape, its ⌘Z choosing it back and its ⌘⇧Z leaving none, and a
 * line's caret then ⌘⇧P, the letter going before the title's words moved and
 * after ⌘⇧Z, nowhere after ⌘Z, in two tabs at two times).
 *
 * Asked for at the key (caretOnceAnswered: what is typed till the answer goes
 * there); one it leaves nowhere goes only if nothing was done since.
 */
export function askMovedOffCaret(forward: boolean, withCaret: boolean): MovedOffCaret {
  return { forward, land: forward && withCaret ? caretOnceAnswered() : null, mayGo: caretMayGoOnceAnswered() };
}

/**
 * Once the move or its step has its answer, the caret where askMovedOffCaret
 * said: `atTitle` puts it at the start of the page's title, and blocks taken
 * back that were chosen (`chosen`, on the page they stand on) are chosen again.
 */
export function landMovedOffCaret(
  asked: MovedOffCaret, atTitle: (land: CaretLanding) => void, chosen?: { pageId: string; blockIds: readonly string[] },
) {
  if (asked.land) {
    atTitle(asked.land);
    return;
  }
  leaveNoCaret(asked.mayGo);
  if (!asked.forward && chosen) choosePageBlocks(chosen.pageId, chosen.blockIds);
}
