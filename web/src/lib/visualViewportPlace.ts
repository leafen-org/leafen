// What shows of the page above a phone's soft keyboard: the visual
// viewport's top and height, kept as --visual-viewport-top and
// --visual-viewport-height on a full-screen sheet for it to fill exactly
// that. iPhone Safari keeps the window's height under the keyboard and shows
// only its top (seen 2026-11-11 at 402: 714 and 384), so a sheet over the
// whole window had its foot behind the keyboard; Android Chrome shortens the
// window itself with the keyboard (iosViewport.ts's resizes-content), the two the
// same.
//
// Kept on the sheet and only while it is open, a value only when it changed:
// written on the root, where every style in the page reads it, each change
// worked out all of them again, and once a sheet had opened it went on with
// every move of the visual viewport (2026-11-13 in iPhone Safari, a page of
// 38k elements: 415–432 ms each).

/** Keeps the visual viewport's place on `sheet` while it is open; returns what stops it. */
export function followVisualViewport(sheet: HTMLElement | null | undefined): () => void {
  const viewport = typeof window === "undefined" ? null : window.visualViewport;
  if (!sheet || !viewport) return () => {};
  let top = "", height = "";
  const place = () => {
    const nextTop = `${viewport.offsetTop}px`, nextHeight = `${viewport.height}px`;
    if (nextTop !== top) sheet.style.setProperty("--visual-viewport-top", top = nextTop);
    if (nextHeight !== height) sheet.style.setProperty("--visual-viewport-height", height = nextHeight);
  };
  viewport.addEventListener("resize", place);
  viewport.addEventListener("scroll", place);
  place();
  return () => {
    viewport.removeEventListener("resize", place);
    viewport.removeEventListener("scroll", place);
    sheet.style.removeProperty("--visual-viewport-top");
    sheet.style.removeProperty("--visual-viewport-height");
  };
}
