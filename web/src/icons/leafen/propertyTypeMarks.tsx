import { IconSvg, type LeafenIconProps } from "./iconPrimitives";

// Every property type's mark drawn here after where Notion's own put their
// ink, each on its 20 box, its lines mostly Notion's 1.875 (read 2026-11-22
// at 1440 in dark, dpr 1, from a database's "+ 필터" list, its column heads
// and its "+" type list, whose marks are the same), not from Notion's paths:
// each shape's lines, arcs and corners set where Notion's ink lies, painting
// within 1.5% of its ink and covering 97% or more of it. Phosphor's glyphs
// fitted into Notion's place thinned their lines with the fit (a "#" drawn
// 16.4 across in 13.75 kept 84% of its line) or were other shapes (its link
// and paperclip covered half of Notion's ink); these paint what Notion's do.
// Each keeps the glyph name the mark it replaces had ("hash", "user-circle" …).

/** A number's "#": two bars 1.875 thick and two strokes leaning a quarter to the left, all 1.875 across; ink 3.125–16.875 by 2.5–17.5. */
export const NumberTypeMark = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="hash">
    <path fill="currentColor" stroke="none"
      d="M3.75 6.25h13.125v1.875H3.75ZM3.125 11.875H16.25v1.875H3.125ZM8.4375 2.5h1.875l-3.75 15H4.6875ZM13.4375 2.5h1.875l-3.75 15H9.6875Z" />
  </IconSvg>
);

/** A status: eight square-ended rays 1.875 wide, from 4.02 to 7.19 out of the middle. */
export const StatusTypeMark = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="spinner">
    <path strokeWidth="1.875" strokeLinecap="butt"
      d="M10 2.81v3.17M10 14.02v3.17M2.81 10h3.17M14.02 10h3.17M4.92 4.92l2.24 2.24M12.84 12.84l2.24 2.24M4.92 15.08l2.24-2.24M12.84 7.16l2.24-2.24" />
  </IconSvg>
);

/**
 * A phone: a solid handset, its two square ends turned to each other across
 * a band bowed out to the lower left — its outer edge an arc 2.65 out of
 * straight at its middle, its inner one 0.88 — its outer corners rounded;
 * ink 2.5–17.5 each way.
 */
export const PhoneTypeMark = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="phone">
    <path fill="currentColor" stroke="none"
      d="M17.5 15l-1.62 1.62A3 3 0 0 1 13.75 17.5 13.26 13.26 0 0 1 2.5 6.25 3 3 0 0 1 3.38 4.12L5 2.5l3.75 3.75-2.5 2.5A7.51 7.51 0 0 0 11.25 13.75l2.5-2.5Z" />
  </IconSvg>
);

/**
 * People: two heads 5.08 across at 6.25 and 13.75 over two domes 3.9 high,
 * the left one in front from 1.25 to 11.075 under its head, the right one
 * showing past it after a gap that narrows from its top to 1.25 at the foot
 * and reaching 18.75; each dome's outer quarter a little wider than its
 * inner one, so the ink spans Notion's 1.25–18.75 by 3.75–15 (2026-11-22).
 */
export const PeopleTypeMark = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="person-property">
    <g fill="currentColor" stroke="none">
      <circle cx="6.25" cy="6.25" r="2.54" />
      <circle cx="13.75" cy="6.25" r="2.54" />
      <path d="M1.25 15v-1.1a5 3.9 0 0 1 5-3.9 4.825 3.9 0 0 1 4.825 3.9V15Z" />
      <path d="M10.798 10.869a4.77 3.9 0 0 1 3.002-.869 4.95 3.9 0 0 1 4.95 3.9V15h-6.21q-.39-3.9-1.742-4.131Z" />
    </g>
  </IconSvg>
);

/** A person in a ring: the ring 1.875 thick, 15 across; a 5 head and the shoulders filling the ring's foot under a bow 1.9 below the head. */
export const PersonCircleTypeMark = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="user-circle">
    <circle cx="10" cy="10" r="6.5625" strokeWidth="1.875" />
    <g fill="currentColor" stroke="none">
      <circle cx="10" cy="8.125" r="2.5" />
      <path d="M5.86 13.8C6.71 12.57 8.14 11.875 10 11.875S13.29 12.57 14.14 13.8A5.625 5.625 0 0 1 5.86 13.8Z" />
    </g>
  </IconSvg>
);

/** A time: a solid disc 15 across with its hands cut out, 1.875 wide — up from the middle to 5, and right to 14.375. */
export const ClockTypeMark = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="clock">
    <path fill="currentColor" fillRule="evenodd" stroke="none"
      d="M2.5 10a7.5 7.5 0 1 0 15 0 7.5 7.5 0 1 0-15 0ZM9.0625 5h1.875v4.375h3.4375v1.875H9.0625Z" />
  </IconSvg>
);

/** A checkbox: a solid square 13.75 across with a tick 1.875 wide cut out of it. */
export const CheckboxTypeMark = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="check-square">
    <path fill="currentColor" fillRule="evenodd" stroke="none"
      d="M3.125 3.125h13.75v13.75H3.125ZM5.625 10.39l1.325-1.327 2.113 2.112 4.3-4.3 1.325 1.325-5.625 5.625Z" />
  </IconSvg>
);

/**
 * An email's "@": a ring 1.875 thick round the middle, open at its lower
 * right; from its right side a hook turns down and back up into a stem at
 * 12.5 that holds a small round bowl, all 1.875 thick.
 */
export const AtTypeMark = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="at">
    <path strokeWidth="1.875" strokeLinecap="butt" d="M13.26 15.7A6.5625 6.5625 0 1 1 16.5625 10c0 1.7-.81 2.5-2.03 2.5S12.5 11.6 12.5 10.4V6.88" />
    <circle cx="9.65" cy="10" r="2.47" strokeWidth="1.875" />
  </IconSvg>
);

/**
 * A title's "Aa": a capital A of two legs 1.98 across leaning a third to
 * each side under a flat top, its bar 11.175 to 13.05, and a round a whose
 * bowl is 6.59 by 7.28 round a hole 3.47 by 3.94 beside a stem 1.81 wide;
 * ink 1.1–18.9 by 4.45–15.55.
 */
export const TitleTypeMark = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="text-aa">
    <g fill="currentColor" stroke="none" fillRule="evenodd">
      <path d="M1.13 15.55 4.96 4.45H7.7l3.83 11.1H9.55l-.863-2.5H3.973l-.863 2.5ZM6.33 6.218l-1.71 4.957h3.42Z" />
      <path d="M11.66 11.92a3.295 3.64 0 1 0 6.59 0 3.295 3.64 0 1 0-6.59 0ZM13.54 11.91a1.735 1.97 0 1 0 3.47 0 1.735 1.97 0 1 0-3.47 0Z" />
      <path d="M17.09 8.6h1.81v6.95h-1.81Z" />
    </g>
  </IconSvg>
);

/** A text's three square-ended lines 1.875 thick, 5 apart from 5, 15 long from 2.5, the last 7.9. */
export const TextTypeMark = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="text-lines">
    <path strokeWidth="1.875" strokeLinecap="butt" d="M2.5 5h15M2.5 10h15M2.5 15h7.9" />
  </IconSvg>
);

/**
 * A URL's two links, each an open loop along the rising diagonal, 1.89
 * thick, its lines square-ended: from its end by the middle round a 2.8
 * arc to the outer side, along it, round the far 2.8 arc and back a short
 * way; the other link the same turned half round the middle.
 */
export const UrlTypeMark = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="url-property">
    <path strokeWidth="1.89" strokeLinecap="butt" strokeLinejoin="miter"
      d="M8.272 10.905A2.8 2.8 0 0 0 12.6 11.36l3.14-3.14a2.8 2.8 0 0 0-3.96-3.96l-1.103 1.103M11.728 9.095A2.8 2.8 0 0 0 7.4 8.64l-3.14 3.14a2.8 2.8 0 0 0 3.96 3.96l1.103-1.103" />
  </IconSvg>
);

/**
 * A file's paperclip: one wire 1.9 thick with round ends, rising at 45.5°
 * from its inner end up the outer side, round a 2.8 arc at the top, down
 * the other side, round a 2.35 arc at the foot, up the inside and round a
 * 1.885 hook back down to its end.
 */
export const FileTypeMark = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="paperclip">
    <path strokeWidth="1.9"
      d="M6.838 9.229 11.723 4.257A2.8 2.8 0 0 1 15.717 8.183L8.095 15.939A2.35 2.35 0 0 1 4.742 12.645L9.568 7.734A1.885 1.885 0 0 1 12.257 10.377L10.175 12.495" />
  </IconSvg>
);

/** A multi-select's list: three square-ended lines 1.875 thick from 6.85 to 17.5, 5 apart from 5, each after a 2.5 dot at 3.75. */
export const MultiSelectTypeMark = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="list-bullets">
    <path strokeWidth="1.875" strokeLinecap="butt" d="M6.85 5H17.5M6.85 10H17.5M6.85 15H17.5" />
    <g fill="currentColor" stroke="none">
      <circle cx="3.75" cy="5" r="1.25" />
      <circle cx="3.75" cy="10" r="1.25" />
      <circle cx="3.75" cy="15" r="1.25" />
    </g>
  </IconSvg>
);

/** A relation's arrow: a square corner of two lines 1.875 thick, 9.69 each from 4.69 and 5.63, and a square-ended stroke from it down to the left; ink 4.05–15.35 each way. */
export const RelationTypeMark = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="arrow-up-right">
    <path strokeWidth="1.875" strokeLinecap="butt" strokeLinejoin="miter" d="M4.688 5.625h9.687v9.688M14.15 5.85 4.717 15.283" />
  </IconSvg>
);

/** A rollup's magnifier: a ring 1.875 thick, 5.31 round 8.75, and a square-ended handle 2.5 thick out to 17.5. */
export const RollupTypeMark = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="rollup">
    <circle cx="8.75" cy="8.75" r="5.31" strokeWidth="1.875" />
    <path strokeWidth="2.5" strokeLinecap="butt" d="M12.498 12.498 16.617 16.617" />
  </IconSvg>
);

/** A formula's Σ: bars 1.875 thick from 5.375 to 14.625 at the top and foot, the strokes between them meeting at 13.15 in the middle, cut straight at 5.375. */
export const FormulaTypeMark = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="sigma">
    <path fill="currentColor" stroke="none" d="M5.375 3.313h9.25v1.875H8L13.148 10 8 14.813h6.625v1.875h-9.25V14.7L10.402 10 5.375 5.3Z" />
  </IconSvg>
);

/** An ID's "№": an N 1.9 thick from 0.85 to 10, its slant 2.2 across, by a ring 3.88 round with a 2 hole over a bar 1.875 thick; ink 0.85–19.15 by 4.45–15.5. */
export const NumeroTypeMark = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="numero">
    <path fill="currentColor" stroke="none"
      d="M.85 4.55h1.9v10.925H.85Zm7.25 0H10v10.925H8.1ZM.85 4.55h2.2L10 15.475H7.8Zm10.51 3.81a3.88 3.88 0 1 0 7.76 0 3.88 3.88 0 1 0-7.76 0Zm1.88 0a2 2 0 1 1 4 0 2 2 0 1 1-4 0Zm-1.89 5.24H19v1.875h-7.65Z" />
  </IconSvg>
);

/**
 * A button: a solid 16.25 by 7.5 card rounded 2.3, a pointer cut 1.25 clear
 * of it at its foot, the pointer's tip at 10 by 7.8 and its tail running
 * out under the card to 17.77.
 */
export const ButtonTypeMark = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="cursor-click">
    <g fill="currentColor" stroke="none">
      <path fillRule="evenodd"
        d="M4.175 3.75h11.65a2.3 2.3 0 0 1 2.3 2.3v2.9a2.3 2.3 0 0 1-2.3 2.3H4.175a2.3 2.3 0 0 1-2.3-2.3v-2.9a2.3 2.3 0 0 1 2.3-2.3ZM8.75 11.25V7.875l2.2-.925 1.55 1.55 2.75 2.75Z" />
      <path d="M10 7.8l5.425 5.4.275.82-2.26.01 1.335 3.24-1.495.5-1.26-3.1L10 16.425Z" />
    </g>
  </IconSvg>
);

/** A place's pin: a disc 10 across round 10 by 6.9 drawn down to a point 0.49 round at 17.5, a 6.12 hole in it. */
export const PlaceTypeMark = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="map-pin">
    <path fill="currentColor" stroke="none" fillRule="evenodd"
      d="M10.439 17.265a.49.49 0 0 1-.878 0L5.521 9.122a5 5 0 1 1 8.958 0ZM6.94 6.875a3.06 3.06 0 1 0 6.12 0 3.06 3.06 0 1 0-6.12 0Z" />
  </IconSvg>
);

/** A verification's seal: eight 2.57 lobes, their middles 5.52 out, round a tick cut 1.875 wide; ink 1.85–18.15 each way. */
export const VerificationTypeMark = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="seal-check">
    <path fill="currentColor" stroke="none"
      d="M7.488 3.936a2.57 2.57 0 0 1 5.024 0 2.57 2.57 0 0 1 3.552 3.552 2.57 2.57 0 0 1 0 5.024 2.57 2.57 0 0 1-3.552 3.552 2.57 2.57 0 0 1-5.024 0 2.57 2.57 0 0 1-3.552-3.552 2.57 2.57 0 0 1 0-5.024 2.57 2.57 0 0 1 3.552-3.552ZM5.637 10.388l3.438 3.438 5.738-5.738-1.326-1.326-4.412 4.412-2.112-2.112Z" />
  </IconSvg>
);
