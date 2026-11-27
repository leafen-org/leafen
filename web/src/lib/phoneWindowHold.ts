import { PHONE_TOOLS_FOOT_GAP } from "@/lib/phoneTools";
import { bringLineOver, caretLineBottom, pageScrollOf } from "@/lib/phoneLineInSight";

/**
 * A phone's window stays at the top: the page scrolls under its top bar, the
 * window never does. Its document is the screen's height and doesn't scroll
 * by itself, but with a soft keyboard up the window can be scrolled by the
 * keyboard's height, and it is, to show what lies under the keyboard: iPhone
 * Safari scrolls it to a field or a line the keyboard covers as it opens, the
 * editor (ProseMirror) after Enter by what it reckons still lies under the
 * keyboard once it has scrolled the page. The top bar then slid under the
 * status bar (seen 2026-11-11 in iPhone Safari: 15–107px after Enter or a tap
 * on a low line, 259px as the keyboard opened on one).
 *
 * So whenever the window has scrolled while a field in the page has focus,
 * it is put back at once, and if the caret is then under the foot of what
 * shows, the page's scroll that holds the field brings it up over that foot
 * (8px, where the phone's tools stand at the lowest): it is shown in the
 * page, the top bar where it was. A field in a layer over the page (a sheet,
 * a window) is left as the browser shows it, as what holds it doesn't scroll
 * with the page. A page zoomed in by a pinch is panned as the person moves it.
 */
export function holdPhoneWindow(page: HTMLElement) {
  const viewport = window.visualViewport;
  const zoomed = () => Boolean(viewport && Math.abs(viewport.scale - 1) > 0.01);
  const fieldInPage = () => {
    const field = document.activeElement;
    return field instanceof HTMLElement && page.contains(field) ? field : null;
  };
  let fingers = 0, returning = 0;
  const putBack = () => {
    if (fingers || performance.now() < returning || window.scrollY === 0 || zoomed()) return;
    const field = fieldInPage();
    if (!field || !pageScrollOf(field, page)) return;
    window.scrollTo(window.scrollX, 0);
    bringLineOver(field, caretLineBottom(field), (viewport?.height ?? window.innerHeight) - PHONE_TOOLS_FOOT_GAP, { gap: 0, page });
  };
  // A finger dragging what doesn't scroll (the top bar, the tools) drags the
  // window while the keyboard is up: it is let follow, then brought back
  // once the finger lifts, as the page bounces at its ends.
  const touched = (event: TouchEvent) => { fingers = event.touches.length; returning = 0; };
  const lifted = (event: TouchEvent) => {
    fingers = event.touches.length;
    if (fingers || window.scrollY === 0 || zoomed() || !fieldInPage()) return;
    returning = performance.now() + 600;
    window.scrollTo({ left: window.scrollX, top: 0, behavior: "smooth" });
  };
  const listening = { capture: true, passive: true };
  window.addEventListener("touchstart", touched, listening);
  window.addEventListener("touchend", lifted, listening);
  window.addEventListener("touchcancel", lifted, listening);
  window.addEventListener("scroll", putBack, { passive: true });
  viewport?.addEventListener("scroll", putBack);
  return () => {
    window.removeEventListener("touchstart", touched, listening);
    window.removeEventListener("touchend", lifted, listening);
    window.removeEventListener("touchcancel", lifted, listening);
    window.removeEventListener("scroll", putBack);
    viewport?.removeEventListener("scroll", putBack);
  };
}
