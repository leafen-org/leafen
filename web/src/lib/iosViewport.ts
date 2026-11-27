/**
 * The viewport's keys that depend on the browser, added as the app starts to
 * the plain width and scale index.html gives every browser.
 *
 * A soft keyboard makes the window shorter where the browser can (Android's
 * Chrome, Firefox), as an app's window: interactive-widget=resizes-content.
 * The page under the top bar takes what is left and scrolls to its line
 * there. Laid over the page instead, the keyboard showed a line it covered by
 * sliding the whole window up, the top bar out of sight (seen 2026-11-11:
 * 168px). Only the engines that know the key are given it (knowsInteractiveWidget):
 * WebKit — every browser on an iPhone or iPad, the Mac's Safari, the Mac
 * app's window — said in the console each time a page opened that it doesn't
 * (2026-11-12 on an iPhone, 2026-11-22 on the Mac); on a phone there
 * phoneWindowHold.ts keeps the window still instead.
 *
 * iOS zooms the page in when a field whose text is under 16px takes focus —
 * the chat's box, a search, a cell — and leaves it zoomed after, the page cut
 * off on the right. Notion's phone pages hold the scale at one (their
 * viewport says maximum-scale=1, seen 2026-10-25); Leafen does so on iOS
 * alone, where the zoom happens: iPhone Safari still lets a person pinch the
 * page larger, and Android keeps its pinch as it was.
 */
export function settleViewportKeys() {
  const meta = document.querySelector<HTMLMetaElement>('meta[name="viewport"]');
  if (!meta) return;
  const keys = [...(appleTouchDevice() ? ["maximum-scale=1"] : []), ...(knowsInteractiveWidget() ? ["interactive-widget=resizes-content"] : [])];
  const missing = keys.filter((key) => !meta.content.includes(key.slice(0, key.indexOf("="))));
  if (missing.length) meta.content = [meta.content, ...missing].join(", ");
}

/** Whether the page runs on an iPhone or iPad, in Safari or an app's window. */
export function appleTouchDevice() {
  if (typeof navigator === "undefined") return false;
  const agent = navigator.userAgent;
  // An iPad's Safari says it is a Mac; its touch tells it apart.
  return /iPhone|iPad|iPod/u.test(agent) || (/Macintosh/u.test(agent) && navigator.maxTouchPoints > 1);
}

/**
 * Whether the viewport's interactive-widget key is one the engine knows:
 * Chromium's (Chrome, Edge, Android's and Windows' web views) and Firefox's,
 * not WebKit's. Chromium names itself after WebKit too, so WebKit is the one
 * whose agent says AppleWebKit without Chrome (an iPhone's Chrome says CriOS,
 * its Firefox FxiOS: both WebKit).
 */
export function knowsInteractiveWidget() {
  if (typeof navigator === "undefined") return false;
  const agent = navigator.userAgent;
  return !/AppleWebKit\//u.test(agent) || /(Chrome|Chromium)\//u.test(agent);
}
