import type { Placement } from "@floating-ui/dom";
import type { PopoverRect, PopoverRequest } from "./popoverPlacement";

/**
 * Where the dark tip stands by the window's edges, as Notion's (seen
 * 2026-11-17 in Chrome at 1440×723, light and dark alike): its box stands
 * flush with what it names and the drawn tip sits its gap further off, and
 * that box keeps 4px from the window's edges, the gap not counted. Under a
 * block's "+" ending at 674.9 the 43.6-high tip stayed under it, drawn down
 * to 724.5, past the window's foot; at 675.4 it went over it. The emoji
 * groups' bar ending at 680.9 keeps its 26.8-high tip under it at
 * 686.9–713.7, and a sidebar row's "⋯" three quarters down the window its
 * tip under it too. By the window's side it slides to 4px from it (the
 * page's "⋯" tip ending at 1436.6). The top edge is taken alike; tips beside
 * what they name are not seen and keep the popovers' margin.
 */
export const TIP_EDGE = 4;

/**
 * How a dark tip lines up with what it names along its side: centred on it,
 * as Notion's tips are (the emoji picker's "랜덤" at 664–692 put its 36.8-wide
 * tip at 659.6–696.4, seen 2026-11-17 in Chrome at 1416), except under the
 * emoji picker's groups bar, where each tip starts where its button starts
 * (seen the same day in two tabs: "최근 사용한 항목" 338/338, "사람"
 * 373.2/373.2, "사용자 지정" 654.8/654.8, "이모지 추가" 690/690, each tip 6
 * under its 32px button), its end left free (EmojiCatalogPanel).
 */
export const GROUP_BAR_TIP = "bottom-start" satisfies Placement;

/** What a dark tip `gap` from its anchor on `placement`'s side is placed by (usePopoverPlacement). */
export function tipRequest(anchor: PopoverRect, placement: Placement, gap: number): PopoverRequest {
  const beside = placement.startsWith("left") || placement.startsWith("right");
  return { anchor, placement, gap, options: beside ? {} : { margin: TIP_EDGE, marginY: TIP_EDGE - gap } };
}

/** Whether a tip `height` high stands under what it names at `rect` (iconTips). */
export function tipFitsBelow(rect: Pick<DOMRect, "bottom">, height: number) {
  return rect.bottom + height + TIP_EDGE <= window.innerHeight;
}

/** Whether a tip `height` high stands over what it names at `rect` (iconTips). */
export function tipFitsAbove(rect: Pick<DOMRect, "top">, height: number) {
  return rect.top - height >= TIP_EDGE;
}
