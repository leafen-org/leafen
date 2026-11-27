import { popoverAnchorHeld, popoverRectOf } from "./popoverAnchor";
import { layerTakesEscape } from "./overlayLayer";
import type { PopoverRect } from "./popoverPlacement";

/**
 * The keys a card shown while the pointer rests on a mention (a page's, a
 * data source's, a link's, a date's, a person's name) takes: none, as
 * Notion's (seen 2026-11-26 in Chrome at 1440, dpr 2, three tabs, nothing run
 * in the page before the key and a letter typed after it: a page's card, a
 * date's tip and a person's card shown, Escape at the caret chose the line,
 * a second let it go, a third and a fourth with nothing left to answer them
 * did nothing, a letter went into the line and Backspace took it out, the
 * card staying each time until the pointer left its mention). What is under
 * the card answers the key while the card stays. An Escape nothing on the
 * page answered goes to a window or menu open under the card, which the
 * layers hand Escape to from the top (overlayLayer.ts), so the card ends
 * before that hand-off when one is open (layerTakesEscape); so does a key
 * whose work took the card's mention away (a Backspace over it, a panel
 * closed). Its layer is passed over by a surface's Escape (`escapeGoesOn`),
 * so the text under it does its own Escape work.
 */
export function listenHoverCardKeys(shown: () => Element | null, end: () => void): () => void {
  // After every listener on the page has had the key and before the window's, where the layers route Escape: added
  // again as each key comes in, so it is the document's last.
  const after = (event: KeyboardEvent) => {
    const mention = shown();
    if (mention && (!mention.isConnected || event.key === "Escape" && !event.defaultPrevented && layerTakesEscape())) end();
  };
  const before = () => {
    document.removeEventListener("keydown", after);
    document.addEventListener("keydown", after);
  };
  document.addEventListener("keydown", before, true);
  return () => {
    document.removeEventListener("keydown", before, true);
    document.removeEventListener("keydown", after);
  };
}

/** A mention card's layer: Escape goes on to what holds the focus under it (listenHoverCardKeys). */
export const HOVER_CARD_LAYER = { escapeGoesOn: true } as const;

/**
 * Where a mention's card stands: where its mention was as it came, moved only
 * as what holds it scrolls (popoverAnchorHeld), as Notion's (seen 2026-11-26
 * in Chrome at 1440, two tabs: a page's card stayed where it came while
 * letters typed before its mention moved the mention on under it).
 */
export function hoverCardAnchor(mention: Element): PopoverRect | null {
  const at = popoverRectOf(mention);
  return at && popoverAnchorHeld(at);
}
