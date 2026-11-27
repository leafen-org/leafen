// The shade a window over the page lays on it — a settings window, a
// question card — goes on over the window's strip of tabs, as Notion
// 7.44.1's app shades its tab bar and the ground of its window buttons with
// its page's (measured in its Windows app 2026-11-24: under its import
// window the tab bar's #191919 went #101010 and the buttons' #111111, their
// marks staying #ada9a3 — the page's rgba(15, 15, 15, 0.8)). The page tells
// the app the shade's color (desktopTabs.ts), which the strip and the
// Windows buttons draw over themselves (desktop/tab_strip.js,
// window_tabs_windows_shade.go).

/** The shade over the page's top now: its color, or "" with none. */
export function pageShade(): string {
  const x = Math.round(window.innerWidth / 2);
  for (const element of document.elementsFromPoint(x, 0)) {
    if (!(element instanceof HTMLElement)) continue;
    const style = getComputedStyle(element);
    if (style.position !== "fixed" && style.position !== "absolute") continue;
    const box = element.getBoundingClientRect();
    // A shade covers the window from its top, its whole width.
    if (box.top > 0 || box.left > 0 || box.right < window.innerWidth || box.height < window.innerHeight * 0.9) continue;
    const ground = style.backgroundColor;
    const alpha = /rgba?\([^)]*,\s*([\d.]+)\s*\)$/.exec(ground);
    if (alpha && Number(alpha[1]) > 0 && Number(alpha[1]) < 1) return ground;
  }
  return "";
}

/**
 * Calls `changed` with the shade as a window opens over the page or closes:
 * a window that keeps the page from the keys hides the rest of it from
 * assistive tools and the keyboard (modalFocus.ts), which is watched.
 */
export function watchPageShade(changed: (shade: string) => void): void {
  let told = "";
  let frame = 0;
  const look = () => {
    frame = 0;
    const shade = pageShade();
    if (shade === told) return;
    told = shade;
    changed(shade);
  };
  const later = () => {
    if (!frame) frame = window.requestAnimationFrame(look);
  };
  new MutationObserver(later).observe(document.body, { subtree: true, attributes: true, attributeFilter: ["aria-hidden", "inert", "aria-modal"] });
  // A window drawn into the page's own box (no hiding) comes as its children change.
  new MutationObserver(later).observe(document.body, { childList: true });
}
