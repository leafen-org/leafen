import { CircleHalf as PhosphorCircleHalf, Target as PhosphorTarget } from "@phosphor-icons/react";
import { PhosphorIconSvg, type LeafenIconProps } from "./iconPrimitives";

// The marks on the tables of a dashboard's "다중 출처 필터링" card, as
// Notion's (seen 2026-11-17): a blue tick, a red target and a yellow half
// circle, each in a ring of the card's ground (the tick is CheckCircleFillIcon).

export const TargetFillIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorTarget} weight="fill" />
);

export const CircleHalfFillIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorCircleHalf} weight="fill" />
);
