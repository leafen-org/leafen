import { createContext, useContext } from "react";
import type { PopoverRect, PopoverRequest, PopoverViewport } from "@/lib/popoverPlacement";
import { readsRightToLeft } from "@/lib/readingDirection";

/**
 * In the layout builder, the chosen tab's pill, which its view's settings
 * stand under, as Notion's (seen 2026-11-12 at 1440×723): centred under the
 * pill with the 4px after it, from the foot of the tab row, at most 70% of
 * the window high and moved up into the window to 4 from its foot — 290×506
 * at 213..719 for a pill at 165..280 in a row ending at 288. A database
 * opened within the tab (PageLayoutTabProviders) has none.
 */
export const PageLayoutTabPillContext = createContext<(() => HTMLElement | null) | null>(null);

export function usePageLayoutTabPill() {
  return useContext(PageLayoutTabPillContext);
}

// The room after each pill, which the settings are centred with.
const AFTER_PILL = 4;

export function placeUnderTabPill(pill: HTMLElement, viewport: PopoverViewport): PopoverRequest {
  const row = pill.closest<HTMLElement>("[data-layout-tabs]") ?? pill;
  const rtl = readsRightToLeft(pill);
  const anchor: PopoverRect = {
    get top() { return row.getBoundingClientRect().top; },
    get bottom() { return row.getBoundingClientRect().bottom; },
    get left() { return pill.getBoundingClientRect().left - (rtl ? AFTER_PILL : 0); },
    get right() { return pill.getBoundingClientRect().right + (rtl ? 0 : AFTER_PILL); },
    contextElement: pill,
  };
  return { anchor, placement: "bottom", gap: 0, options: { rise: true, marginY: 4, cap: viewport.height * 0.7 } };
}
