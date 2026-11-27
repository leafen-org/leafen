import { ClockCounterClockwise as PhosphorClockCounterClockwise } from "@phosphor-icons/react";
import { PhosphorIconSvg, type LeafenIconProps } from "./iconPrimitives";

// Home's "⋯ › 기본 시작 페이지 변경 › 마지막 방문 페이지": a clock turning back,
// as Notion's (seen 2026-11-13).
export const LastVisitedIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorClockCounterClockwise} />
);
