import { useSyncExternalStore, type ReactNode } from "react";
import type { ChartPictureSize } from "./chartExport";
import type { ChartGroup } from "./chartViewSeries";

/**
 * What a chart on the screen tells its view's settings: the groups its X and
 * Y axes have, which their sort menus list to show, hide and order, and the
 * chart drawn again at its picture's own size, which "다른 이름으로 차트
 * 저장" saves (chartExport.ts).
 */
export type ChartPublication = {
  groups: readonly ChartGroup[];
  stackGroups: readonly ChartGroup[];
  /** Where a new reference line starts: the middle of its value axis. */
  lineValue: number;
  /** The chart as its picture draws it, without its presses; null where it has none (a number). */
  drawPicture(size: ChartPictureSize): ReactNode;
};

const publications = new Map<string, ChartPublication>();
const listeners = new Set<() => void>();
let version = 0;

export function publishChart(viewId: string, publication: ChartPublication | null) {
  if (publication) publications.set(viewId, publication);
  else publications.delete(viewId);
  version += 1;
  for (const listener of [...listeners]) listener();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** The chart a view draws now, if one is on the screen. */
export function useChartPublication(viewId: string) {
  useSyncExternalStore(subscribe, () => version, () => 0);
  return publications.get(viewId) ?? null;
}
