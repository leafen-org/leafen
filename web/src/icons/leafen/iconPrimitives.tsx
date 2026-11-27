import type { Icon as PhosphorIcon, IconProps as PhosphorIconProps } from "@phosphor-icons/react";
import { Children, cloneElement, isValidElement, type ReactNode, type SVGProps } from "react";

// What every Leafen icon shares: its props, the sizes it comes in, the
// Phosphor-backed glyph and the hand-authored one, each marking its source,
// weight and glyph name.

/**
 * The sizes an icon comes in, by what it is for, as Notion's measured
 * (baseline "화면 규격 — 아이콘", 2026-10-06/07): most tools 20px, a page's
 * own icon 18px, a row's "+" and "…" 16px, a button by a page's title or a
 * section's chevron 14px, a row's disclosure 12px. A screen names one of
 * these; no screen sets its own number.
 */
export const ICON_SIZES = {
  /** A mark on a face's corner, as Notion's suggestion's pencil (10px, seen 2026-11-15). */
  badge: 10,
  /** A row's disclosure, a toggle's triangle. */
  micro: 12,
  /** A button by a page's title, a section's chevron, a mark inside a small chip. */
  tiny: 14,
  /** A row's "+" and "…", a tab row's search, an icon inside a small field. */
  small: 16,
  /** A page's own icon beside its name. */
  page: 18,
  /** Most tools: the sidebar's and top bar's, a menu row's, a toolbar's. */
  regular: 20,
  /** A wide tool, such as the sidebar's customize. */
  wide: 22,
  /** An empty state's or a panel's mark. */
  large: 24,
  /** A dialog's own mark above its title. */
  mark: 26,
  /** An account card's mark above its title, as Notion's (32px, seen 2026-10-30). */
  card: 32,
  /** An empty page's picture over its words, as Notion's "내 작업" (48px, seen 2026-11-08), its lines grown with it (PICTURE_LINE). */
  empty: 48,
} as const;

export type LeafenIconSize = keyof typeof ICON_SIZES;

export function iconPixels(size: LeafenIconSize = "small"): number {
  return ICON_SIZES[size];
}

export type LeafenIconProps = Omit<SVGProps<SVGSVGElement>, "size"> & {
  size?: LeafenIconSize;
  /**
   * Only for a glyph standing in for a page's, workspace's or template's own
   * icon, which the page icon's size sets (PageIconGlyph): that size in px.
   */
  pixels?: number;
  /**
   * Phosphor-backed icons draw their lines as Notion's (opticalLine):
   * "regular" from 18px, and below it "regular" outlined to Notion's small
   * marks' lines. "fill" and the rest may be asked for, and are drawn as
   * asked. Hand-authored `IconSvg` glyphs keep their own stroke.
   */
  weight?: PhosphorIconProps["weight"];
  /**
   * A hand-authored glyph's line in px at 16 (IconSvg's `line`), growing and
   * shrinking with the size it is drawn at; a Phosphor glyph takes `weight`.
   */
  line?: number;
  /**
   * Where the glyph is drawn in its box instead (glyphFrameViewBox): a mark
   * drawn to Notion's measure for the same mark, as a property's type is.
   */
  frame?: GlyphFrame;
  /**
   * Its box as wide as its glyph's own (`glyphWidth`, a 16px box's), its
   * height the size's, as Notion's small marks are drawn beside a button's
   * words (filterSmall 12.61×16, plusSmall 10.52×16, seen 2026-11-12); with
   * a `frame`, as wide as the frame's Notion ink.
   */
  trim?: boolean;
  /**
   * The glyph's own width in a 16px box that `trim` keeps, where a screen
   * draws a mark as Notion draws its own there (the search filters' marks,
   * seen 2026-11-14); an icon that knows its Notion width keeps its own.
   */
  glyphWidth?: number;
};

/** Where a trimmed icon's view starts across and how wide it is, on a view of `span` units from `origin`. */
function trimmedView(origin: number, span: number, glyphWidth: number) {
  const width = (span * glyphWidth) / 16;
  return { x: Math.round((origin + (span - width) / 2) * 1000) / 1000, width: Math.round(width * 1000) / 1000 };
}

/**
 * A glyph's painted extent as drawn and where Notion paints the same mark,
 * each [x, y, width, height] on a 20 box: the glyph is moved and scaled,
 * across and down apart, to fill the second, its lines with it.
 */
export type GlyphFrame = {
  drawn: readonly [number, number, number, number];
  notion: readonly [number, number, number, number];
};

/**
 * The view that draws a glyph's frame, on a view of `span` units from
 * (`origin`, `origin`). Trimmed to an icon `trimTo` px high, its box is as
 * wide as Notion's ink, which fills it across, as Notion trims a mark to its
 * glyph (the settings search's magnifyingGlassSmall, 12.45×16, seen
 * 2026-11-21).
 */
function glyphFrameViewBox(frame: GlyphFrame, origin: number, span: number, trimTo?: number) {
  const [dx, dy, dw, dh] = frame.drawn, [nx, ny, nw, nh] = frame.notion;
  const sx = nw / dw, sy = nh / dh, unit = span / 20;
  const left = trimTo === undefined ? nx : 0, across = trimTo === undefined ? 20 : nw;
  return {
    viewBox: [origin + (dx - left / sx) * unit, origin + (dy - ny / sy) * unit, (across / sx) * unit, (20 / sy) * unit]
      .map((value) => Math.round(value * 1000) / 1000).join(" "),
    ...(trimTo === undefined ? {} : { width: Math.round(((trimTo * nw) / 20) * 1000) / 1000 }),
    ...(Math.abs(sx - sy) > 0.001 ? { preserveAspectRatio: "none" } : {}),
  };
}

/**
 * A Phosphor glyph's frame for a size below 18px, where its regular is drawn
 * with an outline (opticalLine) that grows what it paints by half the
 * outline on every side: the drawn extent grown by the outline the frame's
 * own view gives it, so the ink painted, lines and all, lands on Notion's.
 * Both are square here, as a frame drawn across and down alike.
 */
export function outlinedFrame(frame: GlyphFrame): GlyphFrame {
  const [dx, dy, dw, dh] = frame.drawn, nw = frame.notion[2];
  // The view is 256·D/nw units high for a painted width D (20 units), its
  // outline SMALL_LINE·256·D/nw − 16 units, of which 20/256 adds to D.
  const painted = (dw - 1.25) / (1 - (SMALL_LINE * 20) / nw);
  const grow = (painted - dw) / 2;
  return { drawn: [dx - grow, dy - grow, dw + 2 * grow, dh + 2 * grow], notion: frame.notion };
}

/**
 * An icon its caller made at one size, drawn at another: a row that shows
 * its icons larger than they were asked for (a phone sheet's 22px) redraws
 * them at that size, so their lines keep that size's weight, as Notion's
 * phone rows (seen 2026-11-11), rather than thickening as a smaller icon
 * blown up by its box would. A mark wrapped around an icon (an unsaved dot
 * over it) has the icon inside redrawn; what is not a sized icon is left as
 * it is.
 */
export function iconAtSize(icon: ReactNode, size: LeafenIconSize): ReactNode {
  if (!isValidElement<{ size?: unknown; children?: ReactNode }>(icon)) return icon;
  if (typeof icon.props.size === "string") return cloneElement(icon, { size });
  if (typeof icon.type !== "string" || icon.props.children === undefined) return icon;
  return cloneElement(icon, undefined, Children.map(icon.props.children, (child) => iconAtSize(child, size)));
}

/**
 * Notion's small marks (its 14 and 16px glyphs, drawn on a 16px grid) have
 * lines 1.3px at 16px that scale with their size, 1.12px at 14 (seen
 * 2026-11-14: checkmarkSmall, plusSmall, filterSmall, slidersSmall 1.3,
 * xMarkSmall and arrowChevronSingleDownSmall at 14px 1.12); its 20px glyphs'
 * are about 1.25px. Phosphor draws its regular lines 16 of its 256 units,
 * Notion's at 20px, and its bold 24, half again too heavy at 16px. So below
 * 18px its regular is drawn with an outline of its own colour that takes the
 * lines to Notion's 1.3px of 16 (20.8 of 256 units), scaling with the size as
 * Notion's do, however much larger or smaller its view draws the glyph (an
 * ✕ drawn larger keeps Notion's xMarkSmall line).
 */
const SMALL_LINE = 1.3 / 16;

/** Whether Notion draws a mark this many px across from its 16px set (below 18px) rather than its 20px one. */
export function smallMark(pixels: number): boolean {
  return pixels < 18;
}

/** Phosphor's weight, and the outline added to it, whose lines come nearest Notion's at a size, on a view `viewSpan` units high. */
function opticalLine(pixels: number, viewSpan: number): { weight: PhosphorIconProps["weight"]; outline: number } {
  const outline = Math.round((SMALL_LINE * viewSpan - 16) * 100) / 100;
  return { weight: "regular", outline: !smallMark(pixels) || outline <= 0 ? 0 : outline };
}

function phosphorGlyphName(Icon: PhosphorIcon) {
  const displayName = Icon.displayName?.replace(/Icon$/, "");
  if (!displayName) {
    throw new Error("Phosphor icon is missing a stable display name");
  }
  return displayName
    .replace(/([a-z0-9])([A-Z])/g, "$1-$2")
    .replace(/([A-Z])([A-Z][a-z])/g, "$1-$2")
    .toLowerCase();
}

export function PhosphorIconSvg({
  icon: Icon,
  glyph = phosphorGlyphName(Icon),
  size = "small",
  pixels: ownPixels,
  weight,
  crop = 1,
  frame,
  trim = false,
  glyphWidth = 16,
  line: _line,
  ...rest
}: LeafenIconProps & {
  icon: PhosphorIcon;
  glyph?: string;
  /** The glyph's own width in a 16px box, Notion's for the same mark, which `trim` keeps. */
  glyphWidth?: number;
  /**
   * How much larger than Phosphor draws it a glyph is drawn in its box, to
   * Notion's measure for the same glyph: above 1 for the ones Phosphor
   * leaves wide margins around (X), below 1 for its ✕ in a circle.
   * The lines scale with it.
   */
  crop?: number;
}) {
  const {
    "aria-hidden": ariaHidden,
    ...iconProps
  } = rest as Omit<PhosphorIconProps, "size" | "weight">;
  const pixels = ownPixels ?? ICON_SIZES[size];
  const span = 256 / crop;
  const inset = (256 - span) / 2;
  const trimmed = trim && !frame ? trimmedView(inset, span, glyphWidth) : null;
  const view = frame ? glyphFrameViewBox(frame, 0, 256, trim ? pixels : undefined) : trimmed
    ? { width: (pixels * glyphWidth) / 16, viewBox: `${trimmed.x} ${inset} ${trimmed.width} ${span}`, overflow: "visible" as const }
    : crop === 1 ? {} : { viewBox: `${inset} ${inset} ${span} ${span}` };
  const optical = opticalLine(pixels, Number(view.viewBox?.split(" ")[3] ?? 256));
  const drawnWeight = weight ?? optical.weight;
  // A weight asked for is drawn as it is; the outline is the size's own.
  const outline = weight ? 0 : optical.outline;

  return (
    <Icon
      {...iconProps}
      size={pixels}
      weight={drawnWeight}
      {...(outline ? { stroke: iconProps.color ?? "currentColor", strokeWidth: outline, strokeLinejoin: "round", strokeLinecap: "round" } as const : {})}
      {...view}
      aria-hidden={ariaHidden ?? "true"}
      data-leafen-icon="true"
      data-leafen-icon-source="phosphor"
      data-leafen-icon-weight={drawnWeight}
      data-leafen-icon-size={size}
      data-leafen-icon-glyph={glyph}
    />
  );
}

/**
 * An empty state's picture is Notion's 20px glyph drawn larger, its lines
 * with it: about 1.25px at 20, so 3px at 48 (its home "홈 보기" table at 48,
 * and its events card's calendar at 34, seen 2026-11-16 at 1440: lines 3.0
 * of 48 and 3.0 of 34 for a heavier glyph). In units of a glyph's 16-unit view.
 */
const PICTURE_LINE = (1.25 * 16) / 20;

/**
 * A hand-authored glyph on a 20-unit grid, drawn within 2.8–17.3 of it. The
 * view takes 2–18, so the drawing fills its box as Notion's do, and its lines
 * stay 1.3px at every size, as Notion's — but for an empty state's picture,
 * whose lines grow with it (PICTURE_LINE). A glyph drawn after one of
 * Notion's 16px marks keeps that mark's own line instead (`line`, in px at
 * 16), which grows and shrinks with the size it is drawn at, as Notion draws
 * that mark larger or smaller (its ✓ at 18px in a phone sheet, seen 2026-11-15).
 */
export function IconSvg({
  glyph,
  size = "small",
  pixels: ownPixels,
  weight: _weight,
  frame,
  trim = false,
  glyphWidth = 16,
  line,
  children,
  ...rest
}: LeafenIconProps & { children: ReactNode; glyph: string; glyphWidth?: number }) {
  const pixels = ownPixels ?? ICON_SIZES[size];
  const trimmed = trim && !frame ? trimmedView(2, 16, glyphWidth) : null;
  const picture = line === undefined && ownPixels === undefined && size === "empty";
  const viewLine = line ?? (picture ? PICTURE_LINE : (1.3 * 16) / pixels);
  return (
    <svg
      width={trimmed ? (pixels * glyphWidth) / 16 : pixels}
      height={pixels}
      {...(frame ? glyphFrameViewBox(frame, 2, 16, trim ? pixels : undefined) : trimmed
        ? { viewBox: `${trimmed.x} 2 ${trimmed.width} 16`, overflow: "visible" } : { viewBox: "2 2 16 16" })}
      fill="none"
      stroke="currentColor"
      strokeWidth={viewLine}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      {...rest}
      data-leafen-icon="true"
      data-leafen-icon-source="leafen"
      data-leafen-icon-weight={line === undefined && !picture ? "1.3" : String(Math.round(((viewLine * pixels) / 16) * 100) / 100)}
      data-leafen-icon-size={size}
      data-leafen-icon-glyph={glyph}
    >
      {children}
    </svg>
  );
}
