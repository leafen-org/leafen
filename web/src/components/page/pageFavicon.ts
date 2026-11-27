import { useEffect, useState, useSyncExternalStore } from "react";
import { pageFaviconHref } from "@/lib/documentChrome";
import { builtInIcon, storedIconPresentation, type NativeIcon } from "./nativeIcons";
import { nativeIconMarkup } from "./nativeIconDrawing";
import { usePageIconImageUrl, type PageIconSource } from "./PageIcon";

// A page's or a workspace's icon as the window's icon and its tab's in the
// Mac and Windows apps (documentChrome.ts), as Notion's favicon is its icon
// (seen 2026-11-13): an emoji; a built-in icon in its color; a picture —
// uploaded, linked or a workspace emoji — drawn small once read, so the
// strip's own page, which can't read the signed-in file, and a tab brought
// back after a restart show it too. Else the document's or database's mark,
// which the tab leaves out.

const NO_ICON: PageIconSource = { icon: "", iconType: "none" };

/** The window's and tab's icon for a page or a database row. */
export function usePageFaviconHref(page: PageIconSource | null | undefined): string {
  const source = page ?? NO_ICON;
  const picture = useFaviconPicture(usePageIconImageUrl(source));
  const builtIn = builtInIcon(source);
  const theme = useDocumentTheme();
  if (builtIn) return builtInFaviconHref(builtIn, theme);
  return picture || pageFaviconHref(page);
}

/** The window's and tab's icon for an icon kept as a bare string: a workspace's (home, library). */
export function useStoredIconFaviconHref(icon: string | undefined, workspaceId?: string): string {
  const clean = icon?.trim() ?? "";
  return usePageFaviconHref(clean ? { ...storedIconPresentation(clean), workspaceId } : null);
}

// ---- Built-in icons ----

function subscribeTheme(change: () => void) {
  const watching = new MutationObserver(change);
  watching.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
  return () => watching.disconnect();
}

function documentTheme() {
  return document.documentElement.dataset.theme ?? "";
}

// The icon's color is the theme's (iconPalette.css), read again as it changes.
function useDocumentTheme() {
  return useSyncExternalStore(subscribeTheme, documentTheme, () => "");
}

function svgDataUrl(svg: string) {
  return `data:image/svg+xml,${encodeURIComponent(svg)}`;
}

/** A built-in icon as BuiltInIconGlyph (PageIcon.tsx) draws it, in its color for the theme shown. */
function builtInFaviconHref(icon: NativeIcon, theme: string) {
  const read = theme ? getComputedStyle(document.documentElement).getPropertyValue(`--icon-c-${icon.color}`).trim() : "";
  const paint = read || "#91918e";
  return svgDataUrl(nativeIconMarkup(icon.name, paint));
}

// ---- Pictures ----

// A picture's longer side as drawn for a tab: 16 points on a display of
// twice the density, and some to spare.
const PICTURE_SIDE = 64;
const KEPT_PICTURES = 32;
const drawn = new Map<string, string>();

function keepDrawn(url: string, href: string) {
  drawn.delete(url);
  drawn.set(url, href);
  for (const old of drawn.keys()) {
    if (drawn.size <= KEPT_PICTURES) break;
    drawn.delete(old);
  }
}

// The picture drawn small as a data address: the server's files let any
// page read them (the Mac and Windows apps' pages are of another origin).
// One from a site that doesn't stays its own address, which a favicon and
// the strip load as they are; a page's own (blob:) can't leave the page.
function drawPicture(url: string, shared = /^https?:/iu.test(url)): Promise<string> {
  const asIs = /^https?:/iu.test(url) ? url : "";
  return new Promise((resolve) => {
    const image = new Image();
    image.decoding = "async";
    if (shared) image.crossOrigin = "anonymous";
    image.onload = () => {
      try {
        const scale = Math.min(1, PICTURE_SIDE / Math.max(image.naturalWidth, image.naturalHeight, 1));
        const canvas = document.createElement("canvas");
        canvas.width = Math.max(1, Math.round(image.naturalWidth * scale));
        canvas.height = Math.max(1, Math.round(image.naturalHeight * scale));
        const context = canvas.getContext("2d");
        if (!context) return resolve(asIs);
        context.drawImage(image, 0, 0, canvas.width, canvas.height);
        resolve(canvas.toDataURL("image/png"));
      } catch {
        resolve(asIs);
      }
    };
    image.onerror = () => resolve(shared ? drawPicture(url, false) : "");
    image.src = url;
  });
}

function useFaviconPicture(url: string): string {
  const [shown, setShown] = useState({ url: "", href: "" });
  useEffect(() => {
    if (!url || drawn.has(url)) return;
    let live = true;
    void drawPicture(url).then((href) => {
      keepDrawn(url, href);
      if (live) setShown({ url, href });
    });
    return () => { live = false; };
  }, [url]);
  if (!url) return "";
  return drawn.get(url) ?? (shown.url === url ? shown.href : "");
}
