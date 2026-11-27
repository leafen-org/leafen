import { edgeAutoScroll } from "@/lib/edgeAutoScroll";
import { watchEdgeFadeAsSized } from "@/lib/edgeFade";

/** How fast the sidebar list's fade grows with what is left to scroll at an edge, as Notion's: 1.7px a pixel. */
const SIDEBAR_FADE_RATE = 1.7;

/**
 * The sidebar list's edges, as Notion's: a drag held near its top or bottom
 * scrolls it (lib/edgeAutoScroll.ts), and where `fades` it fades at each edge
 * it scrolls on past (lib/edgeFade.ts, `[data-edge-fade]`) — 1.7px a pixel
 * left to scroll there, up to 34px, none at an edge it ends at or when it
 * fits (Notion's sidebar, seen 2026-11-22 in Chrome at 1440, dark, in two
 * workspaces and two tabs: 3.4px at 2 scrolled, 8.5 at 5, 25.5 at 15, 34
 * from 20; 27.2 with 16 left, 10.2 with 6, 1.7 with 1). The customize mode's
 * list is the same scroller there (seen only fitting, so unfaded); a phone's
 * drawer is not compared and does not fade. Returns its cleanup.
 */
export function sidebarListEdges(list: HTMLElement, fades: boolean): () => void {
  const stopScroll = edgeAutoScroll(list);
  if (!fades) return stopScroll;
  list.setAttribute("data-edge-fade", "");
  const stopFade = watchEdgeFadeAsSized(list, SIDEBAR_FADE_RATE);
  return () => {
    stopFade();
    stopScroll();
    list.removeAttribute("data-edge-fade");
  };
}
