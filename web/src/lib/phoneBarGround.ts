import { appleTouchDevice } from "./iosViewport";

/**
 * Lays a phone's layer over the whole screen (the inbox, search, settings, a
 * window over its shade) on the page rather than fixing it to the screen, so
 * the status bar and the toolbar at the screen's foot keep the soft ground,
 * as Notion's do in every screen (seen 2026-11-10, iPhone Safari).
 *
 * Safari colors each bar from a short box fixed at that edge of the screen,
 * then from a box fixed over the screen there, then from the body's ground
 * (a phone's is the soft ground, globals.css); it reads them again whenever
 * a fixed box comes or goes, a menu's or a toast's too, and a box can't stay
 * hidden for it: it reads only what shows (seen 2026-11-10: a strip clipped,
 * transparent or 1px in sight is passed over). A layer that is not fixed is
 * never read, so the body's ground holds the bars under it, and a phone's
 * page never scrolls (its body doesn't), so laid there it stands as still.
 * The fixed boxes of the layer and of what holds it that cover the screen
 * are laid absolute where that leaves them in place. A phone's sheets are
 * laid so in their styles (PhoneSheet.module.css), the drawer's shade too
 * (AppShell.module.css); index.html's strips hold the bars as the page loads.
 *
 * Only an iPhone's: Android's Chrome colours its bars from the page's theme
 * colour, and a layer laid on the page there made the page taller than the
 * window, which Chrome then sized as with its address bar gone, the layer's
 * head under the bar (2026-11-15, Galaxy A90: the inbox 21px up); fixed to
 * the screen it stands in the window, as Notion's there.
 */
export function layScreenLayerOnPhonePage(layer: HTMLElement) {
  if (!appleTouchDevice() || !document.querySelector('[data-phone="true"]')) return;
  const screenWidth = window.innerWidth, screenHeight = window.innerHeight;
  for (let box: HTMLElement | null = layer; box && box !== document.body; box = box.parentElement) {
    if (getComputedStyle(box).position !== "fixed") continue;
    // Where it is laid, before any slide in (a phone's inbox rises from below).
    if (box.offsetTop > 0.5 || box.offsetHeight < screenHeight * 0.9 || box.offsetWidth < screenWidth * 0.9) continue;
    const fixed = box.getBoundingClientRect();
    box.style.position = "absolute";
    const laid = box.getBoundingClientRect();
    if (Math.abs(laid.top - fixed.top) > 0.5 || Math.abs(laid.left - fixed.left) > 0.5
      || Math.abs(laid.width - fixed.width) > 0.5 || Math.abs(laid.height - fixed.height) > 0.5) box.style.position = "";
  }
}
