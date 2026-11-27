import { placeAround, type PopoverRequest, type PopoverSize, type PopoverViewport } from "@/lib/popoverPlacement";
import { inlineStartX } from "@/lib/readingDirection";

// A comment box's row of marks: its start 48px before the chosen words' start
// and its foot 13px over their top (the 36px row's top 49 over them), as
// Notion's (measured 2026-11-16 in Chrome at 1440×723: a new page comment's
// box with its first or its third word chosen, a reply's box and an edit's box,
// each 48 and 49; and 2026-11-14 a row page's box, 48 and 49).
const ROW_LEAD = 48, ROW_GAP = 13;

/** Where a comment box's row of marks stands over the chosen words' box. */
export function placeCommentRow(rect: DOMRect, size: PopoverSize, viewport: PopoverViewport): PopoverRequest {
  const x = inlineStartX(rect, -ROW_LEAD);
  // It comes on as the selected text's panel does (popoverAppear.ts).
  return placeAround({ top: rect.top, bottom: rect.bottom, left: x, right: x }, size, viewport, { gap: ROW_GAP, prefer: "above", appear: "panel" });
}
