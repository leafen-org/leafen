import { viewLinkBounds } from "@/viewLinkWire.generated";

export type ViewLinkViewport = "desktop" | "tablet" | "mobile";

/** The screen a view link was made for, which a wider window shows it in a frame of. */
export type ViewLinkScreen = { viewport: Exclude<ViewLinkViewport, "desktop">; width: number; height: number };

const SIZES = {
  desktop: [viewLinkBounds.desktopWidth, viewLinkBounds.desktopHeight],
  tablet: [viewLinkBounds.tabletWidth, viewLinkBounds.tabletHeight],
  mobile: [viewLinkBounds.mobileWidth, viewLinkBounds.mobileHeight],
} as const;

/** The size of the screen a view link was made for. */
export function viewLinkSize(viewport: ViewLinkViewport) {
  return SIZES[viewport];
}

/** Whether this page is the frame a wider browser shows the link's screen in. */
export function insideViewFrame() {
  return typeof window !== "undefined" && new URLSearchParams(window.location.search).has("frame");
}

/**
 * The frame a view link made for this screen is shown in, in this window: a
 * tablet's or a phone's, when the window is wider than it and isn't that
 * frame itself; else none, the page laid on the window.
 */
export function viewLinkFrameFor(viewport: ViewLinkViewport | undefined): ViewLinkScreen | null {
  if (!viewport || viewport === "desktop" || typeof window === "undefined" || insideViewFrame()) return null;
  const [width, height] = SIZES[viewport];
  return window.innerWidth > width ? { viewport, width, height } : null;
}

/**
 * The frame the view link this window was opened at says it is shown in —
 * its screen rides after "#" beside its token (public_opening_shape.go) — for
 * the screen shown while the link is read, so the page doesn't move from the
 * window into its frame when it comes.
 */
export function viewLinkFrameScreen(): ViewLinkScreen | null {
  if (typeof window === "undefined") return null;
  const said = new URLSearchParams(window.location.hash.slice(1)).get("viewport");
  return said === "tablet" || said === "mobile" ? viewLinkFrameFor(said) : null;
}
