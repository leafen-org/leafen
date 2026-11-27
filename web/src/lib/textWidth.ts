import { useEffect, useState } from "react";

let context: CanvasRenderingContext2D | null | undefined;

/**
 * How wide `text` draws in `element`'s type (the page's by default), or in
 * the type family given, at `size`, a CSS font's weight and size such as
 * "500 14px"; null where nothing can draw. Until the product's fonts load it
 * measures the fallback type, so a screen laid out by it reckons again with
 * useTextWidthsVersion. An element's type is read from its styles, which
 * works them out anew if the page has changed since they last were: a screen
 * drawn as the page changes passes the family it read when it last measured.
 */
export function textWidth(text: string, size: string, element?: Element | string): number | null {
  if (context === undefined) context = typeof document === "undefined" ? null : document.createElement("canvas").getContext("2d");
  if (!context) return null;
  const family = typeof element === "string" ? element : element ? getComputedStyle(element).fontFamily : pageFamily();
  context.font = `${size} ${family}`;
  return context.measureText(text).width;
}

// The page's type, read once and again as fonts load (a timeline reckons
// each bar's title by it as it draws).
let pageType: string | null = null;
function pageFamily(): string {
  if (pageType === null) {
    pageType = getComputedStyle(document.body).fontFamily;
    document.fonts?.addEventListener("loadingdone", () => { pageType = null; }, { once: true });
  }
  return pageType;
}

/**
 * Changes once the page's fonts have loaded, and again whenever more load,
 * so what is laid out by textWidth draws again in the type it shows (2026-11-07:
 * a database's view tabs reckoned by the fallback type before the bundled
 * Noto Sans KR loaded kept their tabs where the row should fold to its picker).
 */
export function useTextWidthsVersion(): number {
  const [version, setVersion] = useState(0);
  useEffect(() => {
    const fonts = typeof document === "undefined" ? undefined : document.fonts;
    if (!fonts) return undefined;
    let live = true;
    const again = () => { if (live) setVersion((current) => current + 1); };
    if (fonts.status !== "loaded") void fonts.ready.then(again);
    fonts.addEventListener("loadingdone", again);
    return () => {
      live = false;
      fonts.removeEventListener("loadingdone", again);
    };
  }, []);
  return version;
}
