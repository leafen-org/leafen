import { layersOpened } from "./overlayLayer";

/**
 * The focus given back as a window, menu or question closes, by the fifteen
 * closings that give it themselves rather than through their layer
 * (overlayLayer's restoreFocus): the inbox's phone sheet, a comment's card,
 * the move picker, a reply's "delete?" question, a database's filter pill and
 * advanced filter window, a property's type-change card, the "+" line menu,
 * a phone's drawer, the sign-out question, a files cell's add window, the
 * reaction picker, ⌘N's menu, a picture's alt text window and a table cell's
 * editor left by Tab (to the cell beside).
 *
 * Each site keeps where the focus goes and when it may — what Notion did
 * there, measured site by site (the comment beside each call) — and passes
 * how it gives it as it always has: once the closing is painted (the next
 * frame), the page kept where it is or not, the keyboard's ring as said or
 * as the browser judges, or a caret put back by the place's own focus (an
 * editor's selection, a field's end). Only a place still in the page takes it.
 *
 * Where Notion was seen to keep the focus in a window or menu opened right as
 * the closing one went — Escape and ⌘⇧O at once, the share window keeping it
 * and a letter typed next going into its field, measured 2026-11-26 in Chrome
 * in two tabs at different times, judged after the keys with nothing run in
 * the page before them (the move picker, the reaction picker, a reply's
 * "delete?" question, a filter pill; the same day, a picture's alt text
 * window, the "+" line menu and a files cell's add window, Enter and ⌘⇧O
 * there) — the site says so (`unlessOpened`), and
 * a window or menu opened before the frame comes keeps it (overlayLayer's
 * count of layers opened, as focusOpenerAfterPaint; 2026-11-25, a row's menu).
 */
type Place = HTMLElement | null | undefined;

export type FocusGiveBack = {
  /** Where: the element, or one read once the frame comes. */
  to?: Place | (() => Place);
  /** How, where the place gives its own focus: an editor's (its caret where it was), a field's at its end. */
  give?: () => void;
  /** Whether it is still given once the frame comes (the focus left on nothing, the editor still there…). */
  when?: () => boolean;
  /** The page stays where it is (focus's preventScroll). */
  keepScroll?: boolean;
  /** The keyboard's ring on it, read once the frame comes (focus's focusVisible); unsaid, the browser judges. */
  ring?: () => boolean;
  /** Not given if a window or menu opened meanwhile: that one has the focus now. */
  unlessOpened?: boolean;
};

/** Gives the focus back a frame on, as `back` says; returns what cancels it before then. */
export function giveFocusBack(back: FocusGiveBack): () => void {
  const opened = layersOpened();
  const frame = window.requestAnimationFrame(() => {
    if (back.unlessOpened && layersOpened() !== opened) return;
    if (back.when && !back.when()) return;
    if (back.give) { back.give(); return; }
    const place = typeof back.to === "function" ? back.to() : back.to;
    if (!place?.isConnected) return;
    const options: FocusOptions & { focusVisible?: boolean } = {};
    if (back.keepScroll) options.preventScroll = true;
    if (back.ring) options.focusVisible = back.ring();
    place.focus(options);
  });
  return () => window.cancelAnimationFrame(frame);
}
