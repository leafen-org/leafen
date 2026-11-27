import { placeInScrollBox } from "@/lib/scrollBoxPlace";
import { CHOSEN_BLOCK_ROOM, chosenBlockDrawn, showChosen } from "./chosenBlockShown";

/** The frames a paste's first block is looked for in before it is given up: its line is drawn by its node view, a frame or so after the change. */
const FRAMES_TO_DRAW = 4;

// The box that scrolls an element, as scrollIntoViewNearest finds it.
function scrollerOf(element: HTMLElement) {
  let box = element.parentElement;
  while (box && !(box.scrollHeight > box.clientHeight && /(auto|scroll)/u.test(getComputedStyle(box).overflowY))) box = box.parentElement;
  return box;
}

/**
 * Copied blocks pasted and chosen show their first block: in the frame they
 * are drawn in, at once, the page follows it as it follows a block the arrows
 * chose (showChosen: its wash 66 from the scroller's foot or top), as
 * Notion's does (seen 2026-11-27 in Chrome at 1441, two tabs at different
 * times, real ⌘V, judged after the paste: one block or three, at the page's
 * end with nothing chosen, after a chosen block, after the caret's line —
 * below the window the first one's wash stopped 66 above its foot, the
 * others left below it; already in that room, the page stayed). Pasted from
 * the caret (`fromCaret`) above the window, they show 66 below its top; after
 * chosen blocks above it, the page stays where it was, as Notion's (twice).
 * What is pasted chosen from words — a line of text or several, a table
 * cell's, another app's HTML — leaves the page where it was, as Notion's
 * does; words pasted at the caret show by the caret's own scroll
 * (nativeTiptapCaretClear).
 */
export function showPastedBlocks(root: HTMLElement, ids: readonly string[], fromCaret: boolean) {
  const first = ids[0];
  if (!first) return;
  let frames = FRAMES_TO_DRAW;
  const look = () => {
    if (!root.isConnected) return;
    const drawn = chosenBlockDrawn(root, first);
    if (!drawn) {
      if (--frames > 0) requestAnimationFrame(look);
      return;
    }
    const box = fromCaret ? null : scrollerOf(drawn);
    if (box && placeInScrollBox(drawn, box).top < CHOSEN_BLOCK_ROOM) return;
    showChosen(root, first);
  };
  requestAnimationFrame(look);
}
