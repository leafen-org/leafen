// The mark Notion puts after a mention of, or a link to, a page in the trash
// (seen 2026-11-04 and 2026-11-06, the same for both): its trash glyph,
// 11.5px across, beside "휴지통에 있음" in gray on a pale chip. One drawing
// and one pair of colors for both places.
export const TRASH_MARK = `url("data:image/svg+xml,${encodeURIComponent("<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16' width='11.5' height='11.5' fill='none' stroke='%238e8b86' stroke-width='1.3' stroke-linecap='round' stroke-linejoin='round'><path d='M2.5 4h11M6.5 4V2.6h3V4M4 4l.7 9.4h6.6L12 4M6.8 6.5v4.5M9.2 6.5v4.5'/></svg>").replace(/%2523/g, "%23")}")`;

/** The chip's words and ground, as Notion's (142,139,134 on a pale mark). */
export const TRASH_MARK_COLOR = "var(--text-gray-accent)";
export const TRASH_MARK_BACKGROUND = "var(--action-mark-gray-bg)";
