// The page editor's caret kept clear of its scroller's edges, and the ask
// whether a person acted meanwhile (nativeTiptapController.ts).

/** The caret the browser shows in `root`, scrolled to keep clear of its scroller's edges (editorProps' scroll distance). */
export function keepCaretClear(root: HTMLElement) {
  const shown = root.ownerDocument.getSelection();
  if (!shown?.rangeCount || !shown.isCollapsed || !shown.anchorNode || !root.contains(shown.anchorNode)) return;
  let rect = shown.getRangeAt(0).getBoundingClientRect();
  if (!rect.height) {
    const line = shown.anchorNode instanceof Element ? shown.anchorNode : shown.anchorNode.parentElement;
    if (!line) return;
    rect = line.getBoundingClientRect();
  }
  let scroller = root.parentElement;
  while (scroller && !(/(auto|scroll)/.test(getComputedStyle(scroller).overflowY) && scroller.scrollHeight > scroller.clientHeight)) scroller = scroller.parentElement;
  if (!scroller) return;
  const box = scroller.getBoundingClientRect();
  if (rect.bottom > box.bottom - CARET_SCROLL_THRESHOLD) scroller.scrollTop += rect.bottom - (box.bottom - CARET_SCROLL_MARGIN);
  else if (rect.top < box.top + CARET_SCROLL_THRESHOLD) scroller.scrollTop -= box.top + CARET_SCROLL_MARGIN - rect.top;
}

/** Asked once: whether a key went down or a press began on `page` from the call until the ask. */
export function actedSince(page: Document) {
  const acted = new AbortController();
  for (const type of ["keydown", "pointerdown"]) page.addEventListener(type, () => acted.abort(), { capture: true, signal: acted.signal });
  return () => {
    const answer = acted.signal.aborted;
    acted.abort();
    return answer;
  };
}

// How near the caret may come to its scroller's top or bottom, and how far it
// is then scrolled from it (Notion's, seen 2026-11-06; nativeTiptapController's editorProps take them too).
export const CARET_SCROLL_THRESHOLD = 35;
export const CARET_SCROLL_MARGIN = 74;
