import { useId, type SVGProps } from "react";
import { IconSvg, type LeafenIconProps } from "./iconPrimitives";

const BUBBLE = "M5.07 4.53h9.86a1.07 1.07 0 0 1 1.07 1.07v7.2a1.07 1.07 0 0 1-1.07 1.07H9.2L6.67 16.4v-2.53h-1.6A1.07 1.07 0 0 1 4 12.8V5.6a1.07 1.07 0 0 1 1.07-1.07z";
const LINES = "M6.93 7.87h6.67M6.93 10.4h5.07";

/**
 * A square speech bubble, its tail down at the lower start, two lines inside,
 * the upper longer: Notion's comment (a phone's caret tools, seen 2026-11-13
 * at 3x). Filled (`weight="fill"`) its lines are cut out of it, as a page's
 * "댓글 추가" draws it.
 */
export const CommentIcon = ({ weight, ...p }: LeafenIconProps) => {
  const mask = useId();
  if (weight !== "fill") {
    return (
      <IconSvg {...p} glyph="comment">
        <path d={BUBBLE} />
        <path d={LINES} />
      </IconSvg>
    );
  }
  return (
    <IconSvg {...p} glyph="comment">
      <mask id={mask}>
        <rect x="2" y="2" width="16" height="16" fill="#fff" stroke="none" />
        <path d={LINES} stroke="#000" />
      </mask>
      <path d={BUBBLE} fill="currentColor" mask={`url(#${mask})`} />
    </IconSvg>
  );
};

/**
 * A square bubble on a 16 box, its tail down at the lower start, as Notion's
 * small comment marks: a picture's new comment (seen 2026-11-05) and, with
 * two lines of text in it, a block's or a picture's discussions
 * (commentFilledSmall, seen 2026-11-09, and 2026-11-25 at 1000 and 1440,
 * dpr 2: its ink 2.1–13.9 across and 2.6–14.3 down, the lines 4.6–11.4
 * across) — smaller in its box than the comment tools' bubble (CommentIcon).
 */
export const CommentSmallIcon = ({ lines = false, ...p }: LeafenIconProps & { lines?: boolean }) => (
  <IconSvg {...p} viewBox="0 0 16 16" glyph={lines ? "comment-small-lines" : "comment-small"} line={1.25}>
    <path d="M4 3.2h8a1.25 1.25 0 0 1 1.25 1.25v5.5A1.25 1.25 0 0 1 12 11.2H8.1l-3 2.4v-2.4H4a1.25 1.25 0 0 1-1.25-1.25v-5.5A1.25 1.25 0 0 1 4 3.2Z" />
    {lines ? <path d="M5.2 6h5.6M5.2 8.4h4" strokeWidth={1.15} /> : null}
  </IconSvg>
);

/**
 * Two square speech bubbles, the back one at the upper start, its tail down
 * at its lower start, and the front one over its lower end, its tail down
 * at its own lower end: a block's mark when it holds both suggestions and
 * comments, as Notion's (chatSmall, seen 2026-11-25 at 1000 and 1440, dpr 2:
 * 1.25 lines of 16, its ink 1–15 across and 2.3–14.5 down). The back
 * bubble's lines stop where the front one stands.
 */
export const ChatBubblesIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 16 16" glyph="chat-bubbles" line={1.25}>
    <path d="M12 6.5V4.35A1.4 1.4 0 0 0 10.6 2.95H3A1.4 1.4 0 0 0 1.6 4.35V8.4A1.4 1.4 0 0 0 3 9.8h.65v2.85L6.9 10.3" />
    <path d="M12.5 12.35h.55a1.4 1.4 0 0 0 1.4-1.4V7.9a1.4 1.4 0 0 0-1.4-1.4H8.3a1.4 1.4 0 0 0-1.4 1.4v3.05a1.4 1.4 0 0 0 1.4 1.4h1.65L12.5 13.9Z" />
  </IconSvg>
);

/**
 * A page's comment settings' mark, as Notion's phone "페이지 사용자 지정"
 * draws its rows (commentFilled, seen 2026-11-16 on the A90): a wide
 * rounded bubble, its tail down at the lower start, drawn as a ring with two
 * lines inside, the upper longer, in the words' colour.
 */
export const CommentBubbleIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="comment-bubble">
    <path fill="currentColor" stroke="none" d="M5.875 7.505c0-.345.28-.625.625-.625h7a.625.625 0 1 1 0 1.25h-7a.625.625 0 0 1-.625-.625m0 3c0-.345.28-.625.625-.625h5a.625.625 0 1 1 0 1.25h-5a.625.625 0 0 1-.625-.625" />
    <path fill="currentColor" stroke="none" d="M17.625 5.255A2.125 2.125 0 0 0 15.5 3.13h-11a2.125 2.125 0 0 0-2.125 2.125v7.5c0 1.173.951 2.125 2.125 2.125h1.188v2.482a.625.625 0 0 0 1.006.496l3.87-2.978H15.5a2.125 2.125 0 0 0 2.125-2.125zM15.5 4.38c.483 0 .875.392.875.875v7.5a.875.875 0 0 1-.875.875h-5.148a.63.63 0 0 0-.38.13l-3.034 2.333v-1.838a.625.625 0 0 0-.625-.625H4.5a.875.875 0 0 1-.875-.875v-7.5c0-.483.392-.875.875-.875z" />
  </IconSvg>
);

/**
 * A comment's send: a round mark with an arrow up cut out of it, filling the
 * button it is drawn in, as Notion's (arrowInCircleUpFill, seen 2026-11-15:
 * a 24px mark, its round 15.25 of its 20 units, the arrow's line 1.25 from
 * 6.5 to 12.86 with arms 2.6 each way).
 */
export const SendCircleIcon = (p: SVGProps<SVGSVGElement>) => {
  const mask = useId();
  return (
    <svg viewBox="0 0 20 20" width="100%" height="100%" aria-hidden="true" {...p}
      data-leafen-icon="true" data-leafen-icon-source="leafen" data-leafen-icon-glyph="send-circle">
      <mask id={mask}>
        <rect width="20" height="20" fill="#fff" />
        <path d="M10 12.86V6.6M7.4 9.11 10 6.51l2.6 2.6" fill="none" stroke="#000" strokeWidth="1.25"
          strokeLinecap="round" strokeLinejoin="round" />
      </mask>
      <circle cx="10" cy="10" r="7.625" fill="currentColor" mask={`url(#${mask})`} />
    </svg>
  );
};

/**
 * A suggestion's reject beside its accept's tick, Notion's thin cross
 * (xMarkSmall, seen 2026-11-22 at 1440 in a suggestion card's buttons, the
 * margin's and "모든 토론"'s alike): two 1.25 lines of 16 with round ends
 * crossing in its middle, from 3.8 to 12.2 both ways.
 */
export const XMarkSmallIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 16 16" glyph="x-mark-small" line={1.25}>
    <path d="M3.8 3.8 12.2 12.2M12.2 3.8 3.8 12.2" />
  </IconSvg>
);
