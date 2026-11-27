import {
  ArrowElbowDownRight as PhosphorArrowElbowDownRight,
  BracketsSquare as PhosphorBracketsSquare,
  Equals as PhosphorEquals,
  ArrowElbowUpRight as PhosphorArrowElbowUpRight,
  Question as PhosphorQuestion,
  QuestionMark as PhosphorQuestionMark,
  Warning as PhosphorWarning,
  WebhooksLogo as PhosphorWebhooksLogo,
} from "@phosphor-icons/react";
import { IconSvg, PhosphorIconSvg, type LeafenIconProps } from "./iconPrimitives";

// An automation's marks, as Notion draws them (seen 2026-10-10): "확인
// 표시" a question mark in a circle, an unfinished choice a filled warning
// triangle.

export const QuestionCircleIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorQuestion} />
);

// A variable whose formula gives a list, "[ ]", or a type that can't be told,
// a bare "?" (Notion's "bracket" and "questionMark", seen 2026-11-21).
export const ListResultIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorBracketsSquare} />
);

export const QuestionMarkIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorQuestionMark} />
);

export const WarningFillIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg weight="fill" {...p} icon={PhosphorWarning} glyph="warning-fill" />
);

// The outlined triangle over Notion's warning before a page is copied to
// another workspace (seen 2026-10-31).
export const WarningIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorWarning} glyph="warning" />
);

// "변수 정의" in the list of actions, as Notion's: an equals sign in a
// rounded square (seen 2026-11-03).
export const VariablesIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="0 0 20 20" glyph="variables">
    <rect x="3" y="3" width="14" height="14" rx="3" strokeWidth="1.5" />
    <PhosphorIconSvg icon={PhosphorEquals} size="tiny" x="3" y="3" stroke="none" />
  </IconSvg>
);

// An action's "위에 작업 추가" and "아래에 작업 추가", as Notion's arrows turning
// up and down into the right (seen 2026-10-24).
export const AddActionAboveIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowElbowUpRight} />
);

export const AddActionBelowIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowElbowDownRight} />
);

// An action card's fold, as Notion's (seen 2026-11-15, desktop and an A90):
// open, two arrows pressing into a bar ("접기"); folded, two arrows leaving a
// box open at the top and foot ("펼치기"). Drawn on the 20px grid, 1.25 lines.
export const FoldCardIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="fold-card" viewBox="0 0 20 20" line={1.25}>
    <path d="M10 1.75v4.9M7.5 4.15 10 6.65l2.5-2.5" />
    <rect x="3" y="8.75" width="14" height="2.5" rx="1.25" />
    <path d="M10 18.25v-4.9M7.5 15.85 10 13.35l2.5 2.5" />
  </IconSvg>
);

export const UnfoldCardIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="unfold-card" viewBox="0 0 20 20" line={1.25}>
    <path d="M10 6.7V1.9M7.5 4.4 10 1.9l2.5 2.5" />
    <path d="M10 13.3v4.8M7.5 15.6l2.5 2.5 2.5-2.5" />
    <path d="M8 6.75H4.5A1.5 1.5 0 0 0 3 8.25v3.5a1.5 1.5 0 0 0 1.5 1.5H8M12 6.75h3.5a1.5 1.5 0 0 1 1.5 1.5v3.5a1.5 1.5 0 0 1-1.5 1.5H12" />
  </IconSvg>
);

// A connection's webhooks, as Notion's "웹훅" tab marks them: three joined
// points (seen 2026-10-31).
export const WebhookIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorWebhooksLogo} />
);
