import type { PageHistoryRowLayout } from "@/lib/pageHistoryRowLayout";
import type { PageHistoryPreviewRow } from "@/lib/pageHistoryRows";
import type { PageHistoryBlock } from "@/pageHistoryWire.generated";
import { blockColorStyle } from "./editor/blockHandleModel";

// A version's block colors as Notion's preview draws them, as its editor does
// (measured 2026-11-12): a text color is the block's words and marker and
// those of the blocks under it, unless one has its own; a background is a box
// around the block and the blocks under it. A quote's line, in its words'
// color, runs down the blocks under it too.

/** Where a row stands in a background's box: its first row, one under it, its last, or the whole box. */
export type PageHistoryBandPlace = "open" | "inside" | "close" | "whole";

export type PageHistoryBand = {
  /** The background, a theme color. */
  background: string;
  /** How far before the row, in px, the box starts: at the block whose background it is. */
  back: number;
  /** How many blocks that block is under, each ending the box 6px short of the column's end. */
  depth: number;
  place: PageHistoryBandPlace;
  /** On its last row, how far past the row's own bottom the box ends (pageHistoryRowLayout ends). */
  end: number;
};

/** A quote's line beside a row under it: on down past the row, or ending on it. */
export type PageHistoryRail = { back: number; place: "inside" | "close"; end: number; color?: string };

export type PageHistoryRowColors = {
  /** The words' color, a theme color, from the row's block or the nearest one around it. */
  text?: string;
  /** The backgrounds the row stands in, the outermost first. */
  bands: PageHistoryBand[];
  /** The lines of the quotes the row is under. */
  rails: PageHistoryRail[];
};

/**
 * Each row's colors by its block. A callout draws its own background
 * (PageHistoryPreview calloutRow), so the rows in it stand in no box or
 * quote's line of its or of the blocks around it.
 */
export function pageHistoryPreviewColors(rows: readonly PageHistoryPreviewRow[], layouts: ReadonlyMap<string, PageHistoryRowLayout>,
  blocks: readonly PageHistoryBlock[]) {
  const byId = new Map<string, PageHistoryBlock>();
  for (const block of blocks) byId.set(block.id, block);
  for (const row of rows) byId.set(row.block.id, row.block);
  const rowAt = new Map(rows.map((row, index) => [row.block.id, index]));
  // The last row under each block, by the rows' depth.
  const lastUnder = (index: number) => {
    let end = index;
    while (end + 1 < rows.length && rows[end + 1].depth > rows[index].depth) end += 1;
    return end;
  };
  const textOf = (block: PageHistoryBlock) => {
    const seen = new Set<string>();
    for (let around: PageHistoryBlock | undefined = block; around && !seen.has(around.id); around = byId.get(around.parentId)) {
      seen.add(around.id);
      const color = blockColorStyle(around.content.color)?.color;
      if (typeof color === "string") return color;
      if (!around.parentId) break;
    }
    return undefined;
  };
  const colors = new Map<string, PageHistoryRowColors>();
  rows.forEach((row, index) => {
    const text = textOf(row.block);
    const bands: PageHistoryBand[] = [];
    const rails: PageHistoryRail[] = [];
    const layout = layouts.get(row.block.id);
    const indent = layout?.indent ?? 0;
    let inCallout = row.block.type === "callout";
    const seen = new Set<string>();
    for (let block: PageHistoryBlock | undefined = row.block; block && !seen.has(block.id); block = byId.get(block.parentId)) {
      seen.add(block.id);
      const at = rowAt.get(block.id);
      if (!inCallout && at !== undefined) {
        const own = at === index;
        const end = lastUnder(at);
        const back = indent - (layouts.get(block.id)?.indent ?? 0);
        const ending = layout?.ends.get(block.id) ?? 0;
        const background = blockColorStyle(block.content.color)?.background;
        if (typeof background === "string") {
          const place: PageHistoryBandPlace = own ? (end > at ? "open" : "whole") : index === end ? "close" : "inside";
          bands.unshift({ background, back, depth: rows[at].depth, place, end: ending });
        }
        // The line ends 8px over the quote's foot, its box's 1px further down.
        if (!own && block.type === "quote") rails.push({ back, place: index === end ? "close" : "inside", end: ending + 1, color: textOf(block) });
      }
      if (block.type === "callout") inCallout = true;
      if (!block.parentId) break;
    }
    if (text || bands.length || rails.length) colors.set(row.block.id, { text, bands, rails });
  });
  return colors;
}

/**
 * A row's colors as its style and marks: its words' color, and the outermost
 * and innermost backgrounds it stands in (PageHistoryPreview.module.css).
 */
export function pageHistoryRowColorProps(colors: PageHistoryRowColors | undefined) {
  const outer = colors?.bands[0];
  const inner = colors && colors.bands.length > 1 ? colors.bands.at(-1) : undefined;
  const style: Record<string, string | number> = {};
  if (colors?.text) style.color = colors.text;
  if (outer) {
    Object.assign(style, {
      "--history-band": outer.background, "--history-band-back": `${outer.back}px`, "--history-band-depth": outer.depth,
      "--history-band-end": `${outer.end}px`,
    });
  }
  if (inner) {
    Object.assign(style, {
      "--history-band-inner": inner.background, "--history-band-inner-back": `${inner.back}px`,
      "--history-band-inner-depth": inner.depth, "--history-band-inner-end": `${inner.end}px`,
    });
  }
  return { style, "data-history-band": outer?.place, "data-history-band-inner": inner?.place };
}
