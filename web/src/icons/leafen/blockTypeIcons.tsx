import { IconSvg, type LeafenIconProps } from "./iconPrimitives";

// The block types' marks in the "/" and "+" menus and "전환" — headings,
// toggle headings, the lists, to-do, quote and synced block — drawn after
// where Notion's own put their ink (read 2026-11-15 at 1440 in dark, each on
// its 20 box, at 4 and 10 times its size), not from Notion's paths: lines
// 1.25 with round ends, dots and marks filled.

type Digit = 1 | 2 | 3 | 4;

/** A heading's level after its H, 7.7 high, its right end 17.2–18.4 across. */
const DIGITS: Record<Digit, string> = {
  1: "M14.05 10 16.55 8.75V15.15",
  2: "M12.95 9.85C13.3 8.95 14.25 8.5 15.15 8.5 16.45 8.5 17.35 9.35 17.35 10.55 17.35 11.45 16.8 12.1 16.15 12.65L12.95 15.15H17.35",
  3: "M12.95 9.8C13.3 9.15 14.1 8.75 15 8.75 16.2 8.75 17.05 9.45 17.05 10.35 17.05 11.2 16.3 11.8 15.15 11.8H14.35M15.15 11.8C16.4 11.8 17.2 12.55 17.2 13.5 17.2 14.5 16.3 15.15 15 15.15 14 15.15 13.2 14.75 12.85 14.05",
  4: "M16.45 15.25V8.55L12.75 13.1H17.8",
};

/** Where a heading's H stands: its two strokes' middles across, closer in for a wider level. */
const HEADING_H: Record<Digit, readonly [number, number]> = {
  1: [3.45, 11.15],
  2: [3.05, 10.65],
  3: [2.85, 10.55],
  4: [2.85, 10.55],
};

/** A heading: a tall H, 4.2–15.8 down, and its level beside it, smaller. */
export function HeadingLevelIcon({ level = 1, ...p }: LeafenIconProps & { level?: Digit }) {
  const [left, right] = HEADING_H[level];
  return (
    <IconSvg {...p} viewBox="0 0 20 20" glyph={`heading-${level}`} strokeWidth="1.25">
      <path d={`M${left} 4.825V15.175M${right} 4.825V15.175M${left} 10H${right}`} />
      <path d={DIGITS[level]} />
    </IconSvg>
  );
}

export const HeadingOneIcon = (p: LeafenIconProps) => <HeadingLevelIcon {...p} level={1} />;
export const HeadingTwoIcon = (p: LeafenIconProps) => <HeadingLevelIcon {...p} level={2} />;
export const HeadingThreeIcon = (p: LeafenIconProps) => <HeadingLevelIcon {...p} level={3} />;
export const HeadingFourIcon = (p: LeafenIconProps) => <HeadingLevelIcon {...p} level={4} />;

/** A toggle heading's level, the heading's drawn smaller at the box's end (19 for 1, 19.5 for the rest). */
const TOGGLE_DIGIT_SHIFT: Record<Digit, number> = { 1: 4.5, 2: 4.33, 3: 4.46, 4: 3.95 };

/** A toggle heading: a small triangle, a shorter H 6.6–13.5 across, 5–15 down, and its level. */
export function ToggleHeadingIcon({ level = 1, ...p }: LeafenIconProps & { level?: Digit }) {
  return (
    <IconSvg {...p} viewBox="0 0 20 20" glyph={`toggle-heading-${level}`} strokeWidth="1.1">
      <path d="M1.45 8.85 3.55 10.1 1.45 11.35Z" fill="currentColor" strokeWidth="0.9" />
      <path d="M6.6 5.55V14.45M13.5 5.55V14.45M6.6 9.85H13.5" />
      <path d={DIGITS[level]} strokeWidth="1.25" transform={`matrix(0.844 0 0 0.844 ${TOGGLE_DIGIT_SHIFT[level]} 1.665)`} />
    </IconSvg>
  );
}

/** Two lines 6 and 14 down, 8.5–16 across, beside what marks each. */
const LIST_LINES = "M8.5 6H16M8.5 14H16";

/** A bulleted list block: two dots, each beside a line. */
export const BulletedListBlockIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="bulleted-list-block" strokeWidth="1.25">
    <circle cx="4.8" cy="6" r="1.3" fill="currentColor" stroke="none" />
    <circle cx="4.8" cy="14" r="1.3" fill="currentColor" stroke="none" />
    <path d={LIST_LINES} />
  </IconSvg>
);

/** A numbered list: a 1 and a 2, each beside a line. */
export const NumberedListIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="numbered-list" strokeWidth="1.25">
    <path d="M3.75 3.55H4.8V7.5M3.45 12.95C3.6 12.3 4.15 11.95 4.8 11.95 5.5 11.95 6 12.45 6 13.05 6 13.5 5.7 13.85 5.35 14.15L3.5 15.95H6.05" strokeWidth="1.1" />
    <path d={LIST_LINES} />
  </IconSvg>
);

/** A to-do list: a check over an empty box, each beside a line a little further out. */
export const ChecklistIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="checklist" strokeWidth="1.25">
    <path d="M2.6 6.15 4.3 8.25 7.3 3.75" />
    <rect x="2.925" y="11.425" width="3.75" height="3.75" rx="0.6" />
    <path d="M9.75 6.65H16.85M9.75 13.3H16.85" />
  </IconSvg>
);

/** A toggle list: two small triangles, each beside a line. */
export const ToggleListIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="toggle-list" strokeWidth="1.25">
    <path d="M3.2 4.5 5.85 6 3.2 7.5ZM3.2 12.5 5.85 14 3.2 15.5Z" fill="currentColor" strokeWidth="0.9" />
    <path d={LIST_LINES} />
  </IconSvg>
);

/** A quote: two opening marks, each a round bowl under a tail rising to the right. */
export const QuoteIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="quote" strokeWidth="1.3">
    {[0, 9].map((x) => (
      <g key={x} transform={`translate(${x} 0)`}>
        <ellipse cx="5.5" cy="11.85" rx="3.8" ry="3.25" fill="currentColor" stroke="none" />
        <path d="M2.35 10.9C2.35 7.6 4.6 5.65 7.4 5.65" />
      </g>
    ))}
  </IconSvg>
);

/** A synced block: a long line and a short one over a ring of two arrows. */
export const SyncedBlockIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="synced-block" strokeWidth="1.25">
    <path d="M2.6 4.4H14.4M2.6 8.4H6.9" />
    <path d="M16.6 10.85A4.1 4.1 0 0 0 9.25 11.9M9.65 15.2A4.1 4.1 0 0 0 16.95 14.3" />
    <path d="M7.75 12.15H10.4L9.05 13.95ZM15.85 13.85H18.55L17.2 12Z" fill="currentColor" strokeWidth="0.6" />
  </IconSvg>
);
