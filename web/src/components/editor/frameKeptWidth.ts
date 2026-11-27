import { type RefObject, useEffect, useLayoutEffect, useRef, useState } from "react";
import type { GraphBlock } from "@/contentGraph";
import type { BlockBoxSize, ResizedBlockKind } from "./BlockCornerHandles";

/** How much wider a kind's figure is drawn than its frame: a picture's and a PDF's 2px each side, the others none. */
export const FIGURE_INSET: Record<ResizedBlockKind, number> = { image: 4, video: 0, audio: 0, pdf: 4, embed: 0 };
/** A Notion block stands 8px out from its frame either side. */
export const BLOCK_INSET = 16;

/**
 * The width a framed block's side handles left it, as Notion keeps it: its
 * block's in px, the frame and 8px either side ("sizedWidth", Notion's
 * block_width). A page turned full width, a narrower window or the column it
 * stands in keep it as it was dragged, as Notion's (seen 2026-11-27 at 1440
 * and 768, dpr 1: a picture dragged to 368 in a 708 column stayed 368 at full
 * width and in a 561 column; one dragged to 272 in a 337 column stayed 272 in
 * a 594 one and drew 241.5 in a 258 one; a video dragged to 464 and an embed's
 * 400 box the same).
 */
export function keptBlockWidth(content: GraphBlock["content"]): number | undefined {
  const px = content.sizedWidth;
  return typeof px === "number" && Number.isFinite(px) && px > BLOCK_INSET ? px : undefined;
}

/** Whether side handles sized the block: in px, or as a share of its column as kept before (width). */
export function keptSized(content: GraphBlock["content"]) {
  return keptBlockWidth(content) !== undefined || typeof content.width === "number" && Number.isFinite(content.width);
}

/**
 * The share of its column a framed block's figure is drawn at: a kept px
 * width at the column it stands in now — measured before it paints, again as
 * the column changes, and once more whenever the figure comes into another
 * place (a video's waiting place has none) — or a share kept before as it
 * is, `most` at most.
 */
export function useKeptFrameShare(figureRef: RefObject<HTMLElement | null>, content: GraphBlock["content"], kind: ResizedBlockKind,
  most = 1000): number | undefined {
  const px = keptBlockWidth(content);
  const [column, setColumn] = useState(0);
  const watched = useRef<{ parent: Element | null; observer?: ResizeObserver }>({ parent: null });
  // Checked after every render, but measured only when the figure's place changes: no layout read otherwise.
  useLayoutEffect(() => {
    const parent = px === undefined ? null : figureRef.current?.parentElement ?? null;
    if (parent === watched.current.parent) return;
    watched.current.observer?.disconnect();
    watched.current = { parent };
    // The box its share is of, as a drag reads it (frameDragShare): one drawing no box of its own passes it up.
    let holder: Element | null = parent;
    while (holder && holder.getBoundingClientRect().width === 0) holder = holder.parentElement;
    if (!holder) return;
    const box = holder;
    const observer = new ResizeObserver(() => setColumn(box.getBoundingClientRect().width));
    observer.observe(box);
    watched.current.observer = observer;
    setColumn(box.getBoundingClientRect().width);
  });
  useEffect(() => () => watched.current.observer?.disconnect(), []);
  if (px !== undefined) {
    if (!column) return undefined;
    return Math.max(1, Math.min(most, Math.round(((px - BLOCK_INSET + FIGURE_INSET[kind]) / column) * 1e8) / 1e6));
  }
  const share = content.width;
  return typeof share === "number" && Number.isFinite(share) ? Math.max(1, Math.min(most, share)) : undefined;
}

/**
 * What a drag's end keeps on its block: its block's px, or no width at all
 * where it stepped to the column's own block — a picture back at its size as
 * it came, a box back to its own, as Notion's (seen 2026-11-27 at 1440, two
 * tabs: a picture dragged to 368 then out to the column drew its own 600 at
 * the column's start again, in a full width page too) — the box an embed's
 * block API or link gave it with it (blockWidth). Null when nothing changes.
 */
export function keptWidthPatch(size: BlockBoxSize | null, content: GraphBlock["content"]): Record<string, undefined | number> | null {
  if (!size?.block) return null;
  if (size.whole) return keptSized(content) || content.blockWidth !== undefined ? { sizedWidth: undefined, width: undefined, blockWidth: undefined } : null;
  return size.block !== keptBlockWidth(content) || content.width !== undefined ? { sizedWidth: size.block, width: undefined } : null;
}
