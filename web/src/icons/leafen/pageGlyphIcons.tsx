import { ICON_SIZES, type LeafenIconProps } from "./iconPrimitives";

/**
 * A page's glyph, Leafen's own drawing made to the measure of Notion's
 * (seen 2026-11-16, Chrome 1440 dark: a sidebar row's 18px box, a page
 * link's 19.8, a filter chip's 14.39 — the same in each): a sheet with its
 * top corner folded down, as tall as its box and 0.77 of it across, centred.
 * Its outline is 0.082 of the box and its corners round 0.14, the fold and a
 * page's lines 0.072, so its lines grow and shrink with the box as Notion's
 * do. A page with anything in it has two lines in its lower half, 0.61 and
 * 0.78 down and 0.43 across; an empty one has none. Its view is the glyph's
 * own bounds, 15.4 by 20 on a 20 grid, as Notion's is its glyph's: in a
 * square box it fills the height, centred; a place that sets the box
 * otherwise (a home card's, HomeView.module.css) gets Notion's size there.
 * Trimmed (`trim`, a filter pill's) its box is `glyphWidth` of a 16px box
 * across and the size's height, the sheet centred in it at that width, as
 * Notion's pill draws its page 11.8 by 20 around 15.2 of ink (seen
 * 2026-11-17 at 1440, the trash's and the search's "검색 범위").
 */
function PageSheet({ lines, glyph, size = "small", pixels: ownPixels, weight: _weight, frame: _frame, trim = false,
  glyphWidth = 16, artboard = false, ...rest }: LeafenIconProps & { lines: boolean; glyph: string; artboard?: boolean }) {
  const pixels = ownPixels ?? ICON_SIZES[size];
  return (
    <svg
      width={trim ? (pixels * glyphWidth) / 16 : pixels}
      height={pixels}
      viewBox={artboard ? ARTBOARD_VIEW : "2.3 0 15.4 20"}
      fill="none"
      stroke="currentColor"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      {...rest}
      data-leafen-icon="true"
      data-leafen-icon-source="leafen"
      data-leafen-icon-size={size}
      data-leafen-icon-glyph={glyph}
    >
      <path strokeWidth={1.64} d="M5.1 0.82H10.96L16.88 6.74V17.2A1.98 1.98 0 0 1 14.9 19.18H5.1A1.98 1.98 0 0 1 3.12 17.2V2.8A1.98 1.98 0 0 1 5.1 0.82Z" />
      <path strokeWidth={1.44} d="M10.64 0.82V5.4A1.6 1.6 0 0 0 12.24 7H16.88" />
      {lines ? <path strokeWidth={1.44} d="M6.4 12.29H13.6M6.4 15.57H13.6" /> : null}
    </svg>
  );
}

// A menu row's page (`artboard`: the "/" menu's "페이지", seen 2026-11-27)
// is the same sheet on Notion's 20-unit menu box, 0.7625 of it and centred
// (11.75 by 15.25 from 4.13, 2.38 at 20px, lines 1.25 and 1.1), as Notion's
// "page" glyph stands there rather than filling the row's box.
const ARTBOARD_VIEW = "-3.115 -3.115 26.23 26.23";

/** A page with something in it: the sheet with its lines (PageSheet). */
export const FileText = (p: LeafenIconProps & { artboard?: boolean }) => <PageSheet {...p} lines glyph="page" />;

/** A page with nothing in it yet, as Notion's empty document: the sheet alone (PageSheet). */
export const FileBlank = (p: LeafenIconProps) => <PageSheet {...p} lines={false} glyph="page-empty" />;
