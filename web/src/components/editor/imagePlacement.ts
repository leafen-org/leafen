import type { GraphBlock } from "@/contentGraph";

/** Where a picture offers its alignment: the tools over it, and "맞춤" in its block menu. */
export type ImageAlignPlaces = { tools: boolean; menu: boolean };

/**
 * The widest picture uploaded in the page that is drawn 32px narrower than it
 * is: the default page's text column (Notion's, seen 2026-11-27 at 1440 dpr 1
 * in a full width page: 705, 707 and 708 drawn 673..676 in the middle, 709,
 * 712, 716, 720, 736, 737 and 1000 across its 1217px column).
 */
const UPLOAD_DRAWN_NARROWER_MOST = 708;

/**
 * Whether a picture uploaded in the page and never sized spans its column as
 * one sized across it: wider than UPLOAD_DRAWN_NARROWER_MOST, however wide
 * the page (a full width page's 1217, past the picture's own 1000 too), with
 * no alignment to choose, as Notion's (seen 2026-11-27, a 1000px picture
 * uploaded twice, into a page at default and at full width). `width` is the
 * picture's own, its drawn file's or the one its block keeps. One at its own
 * size — linked, uploaded through the block API, imported — never does.
 */
export function uploadSpansColumn(block: GraphBlock, managed: boolean, width: number) {
  const own = width || (typeof block.content.pixelWidth === "number" ? block.content.pixelWidth : 0);
  return managed && block.content.naturalSize !== true && own > UPLOAD_DRAWN_NARROWER_MOST;
}

/**
 * Where a picture offers its alignment, as Notion's (seen 2026-11-27 at 1440,
 * default and full width, twice in two tabs):
 * - sized (`sized`, its share of the column) narrower than its column: both;
 *   one uploaded in the page across it (uploadSpansColumn) neither;
 * - drawn at its own size and never sized (`ownSize`: a link, an upload the
 *   block API made, an import): the tools alone, however wide — it stays at
 *   the column's start whatever is chosen, until sized (seen 2026-11-12);
 * - uploaded in the page and never sized, drawn 32px narrower (`shown` px in a
 *   `column` px wide): both while narrower than its column, neither once as
 *   wide.
 * An uploaded picture keeps "맞춤" in its "추가 옵션", a linked one in the menu
 * itself (blockActionMenuImageRows).
 */
export function imageAlignPlaces(picture: { sized?: number; ownSize: boolean; shown: number; column: number }): ImageAlignPlaces {
  if (picture.sized) return { tools: picture.sized < 100, menu: picture.sized < 100 };
  if (picture.ownSize) return { tools: true, menu: false };
  const narrower = picture.shown > 0 && picture.shown < picture.column;
  return { tools: narrower, menu: narrower };
}

/**
 * Whether the picture drawn in `block` (a block's element) offers "맞춤" in its
 * menu: its figure is marked data-image-fit where it is drawn (ImageBlock), by
 * imageAlignPlaces.
 */
export function imageMenuFits(block: Element | null | undefined) {
  return Boolean(block?.querySelector("figure[data-image-fit]"));
}
