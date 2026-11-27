// The gallery's color and gradient covers are CSS, and stand for a 1500×600
// cover image: the download paints that image, and the page lays it as one,
// cropped to the cover's band and moved by "위치 변경" as an image is (Notion's
// gradients are images and move so, seen 2026-11-06). The gradient line runs
// through the center at the angle, long enough that the corners take the end
// colors, as CSS draws it over that image.
const GRADIENT_COVER = /^linear-gradient\((-?\d+(?:\.\d+)?)deg,(#[0-9a-f]{3,8}),(#[0-9a-f]{3,8})\)$/iu;
export const GRADIENT_WIDTH = 1500;
export const GRADIENT_HEIGHT = 600;

type Point = { x: number; y: number };
export type CoverGradient = { from: Point; to: Point; colors: [string, string] };

/** A gallery color or gradient cover's line and colors on its 1500×600 image. */
export function coverGradient(cover: string): CoverGradient | null {
  const match = GRADIENT_COVER.exec(cover.replace(/\s+/gu, ""));
  if (!match) return null;
  const angle = (Number(match[1]) * Math.PI) / 180;
  const dx = Math.sin(angle);
  const dy = -Math.cos(angle);
  const half = (Math.abs(GRADIENT_WIDTH * dx) + Math.abs(GRADIENT_HEIGHT * dy)) / 2;
  const cx = GRADIENT_WIDTH / 2;
  const cy = GRADIENT_HEIGHT / 2;
  return { from: { x: cx - dx * half, y: cy - dy * half }, to: { x: cx + dx * half, y: cy + dy * half }, colors: [match[2], match[3]] };
}

/** The cover as its image, for CSS to lay as any cover image; null when it isn't one of the gallery's. */
export function coverGradientImage(cover: string): string | null {
  const gradient = coverGradient(cover);
  if (!gradient) return null;
  const { from, to, colors } = gradient;
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${GRADIENT_WIDTH}" height="${GRADIENT_HEIGHT}">`
    + `<linearGradient id="g" gradientUnits="userSpaceOnUse" x1="${from.x}" y1="${from.y}" x2="${to.x}" y2="${to.y}">`
    + `<stop offset="0" stop-color="${colors[0]}"/><stop offset="1" stop-color="${colors[1]}"/></linearGradient>`
    + `<rect width="${GRADIENT_WIDTH}" height="${GRADIENT_HEIGHT}" fill="url(#g)"/></svg>`;
  return `url("data:image/svg+xml,${encodeURIComponent(svg)}")`;
}
