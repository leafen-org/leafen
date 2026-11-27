import { IconSvg, type LeafenIconProps } from "./iconPrimitives";

// The media and link blocks' marks, as Notion draws them (seen 2026-11-20 in
// Chrome at 1440, dpr 1): each on its whole 20-unit box, its ink within
// 2.375–17.625 and its lines 1.25 there. Notion draws the same mark at 20px in
// the "/" menu and a quarter larger, its lines with it, in an empty block's
// 25px row (EMPTY_ROW_MARKS), so a mark here is drawn on the 20-unit box and
// grows with the size it is drawn at. Leafen draws them itself, at the places
// Notion's lines stand.

const LINE = 1.25;

/** A picture: a rounded frame wider than high, a filled sun at its upper left, a small peak and a taller one (Notion's photo). */
export const ImageBlockIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" line={LINE} glyph="image-block">
    <rect x="3" y="4.75" width="14" height="10.5" rx="1.5" />
    <circle cx="8.5" cy="7.81" r="1.5" fill="currentColor" stroke="none" />
    <path d="M3 13.25 6.06 10.19l2.42 2.42 4.58-4.58L17 11.97" />
  </IconSvg>
);

/** A video: the picture's frame with a filled, rounded play triangle in it (Notion's playButton). */
export const VideoBlockIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" line={LINE} glyph="video-block">
    <rect x="3" y="4.75" width="14" height="10.5" rx="1.5" />
    <path d="M7.81 7.6a.4.4 0 0 1 .6-.35l4.15 2.4a.4.4 0 0 1 0 .7l-4.15 2.4a.4.4 0 0 1-.6-.35Z" fill="currentColor" stroke="none" />
  </IconSvg>
);

/** A bookmark: a tall rounded tab whose foot is cut into a notch (Notion's bookmark). */
export const BookmarkBlockIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" line={LINE} glyph="bookmark-block">
    <path d="M4.75 16.5V4a1.5 1.5 0 0 1 1.5-1.5h7.5a1.5 1.5 0 0 1 1.5 1.5v12.5a.5.5 0 0 1-.8.4L10 13.58l-4.45 3.32a.5.5 0 0 1-.8-.4Z" />
  </IconSvg>
);

/** A sound: six rounded bars 2.7 apart, the third the tallest (Notion's waveform, an empty audio block's row). */
export const WaveformBlockIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" line={LINE} glyph="waveform-block">
    <path d="M3.25 8.65v2.7M5.95 5.45v9.1M8.65 3.15v13.7M11.35 7.25v5.5M14.05 5.95v8.1M16.75 8.65v2.7" />
  </IconSvg>
);

/**
 * A page with its corner folded and an arrow up on it (Notion's arrowPageUp,
 * an empty file block's row), its fold in a 1px line as Notion's.
 */
export const FileUploadBlockIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" line={LINE} glyph="file-upload-block">
    <path d="M15.25 8.12v7.38a1.5 1.5 0 0 1-1.5 1.5h-7.5a1.5 1.5 0 0 1-1.5-1.5v-11a1.5 1.5 0 0 1 1.5-1.5h3.88a1.5 1.5 0 0 1 1.06.44l3.62 3.62a1.5 1.5 0 0 1 .44 1.06Z" />
    <path d="M10.5 3.4v2.85a1.5 1.5 0 0 0 1.5 1.5h2.6" strokeWidth={1} />
    <path d="M7.51 12 10 9.51l2.49 2.49M10 9.51v4.98" />
  </IconSvg>
);

/** The world: a circle, its equator and one meridian, a lens with near-upright sides from pole to pole (Notion's globe, an empty embed's row). */
export const GlobeBlockIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" line={LINE} glyph="globe-block">
    <circle cx="10" cy="10" r="7" />
    <path d="M3 10h14" />
    <path d="M10 3.25C9.35 3.35 8.7 3.95 8.25 5 7.8 6.1 7.55 7.8 7.51 10s.29 3.9.74 5c.45 1.05 1.1 1.65 1.75 1.75.65-.1 1.3-.7 1.75-1.75.45-1.1.78-2.8.74-5s-.29-3.9-.74-5C11.3 3.95 10.65 3.35 10 3.25Z" />
  </IconSvg>
);
