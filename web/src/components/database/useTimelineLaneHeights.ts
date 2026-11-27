import { useCallback, useMemo, useState } from "react";
import { timelineLaneHeight, type TimelineLaneKind } from "./timelineLaneProjection";

type Lanes = { kinds: readonly TimelineLaneKind[]; groups: readonly unknown[] };

/**
 * The heights a timeline's canvas lays its lanes out with. Beside its table,
 * the table's lanes' heights as measured (`measure`), which hold for lanes of
 * the same kinds in the same places: a row's value changed draws the timeline
 * once, not again as they are measured the same. Till they are measured, and
 * for a grouped timeline without its table, each lane's kind's height; the
 * table's measured the same, the timeline isn't drawn again for them
 * (2026-11-15: a 46-row timeline opened by its view tab was drawn twice before
 * it showed, ~45 ms at CPU ×2). Without its table and groups, none: its lanes
 * grow with their bars.
 */
export function useTimelineLaneHeights<L extends Lanes>(lanes: L, tableShown: boolean, phone: boolean) {
  const [measured, setMeasured] = useState<{ projection: L | null; heights: readonly number[] }>({ projection: null, heights: [] });
  const heightsHold = (projection: L | null) => projection === lanes
    || (!!projection && projection.kinds.length === lanes.kinds.length && projection.kinds.every((kind, index) => kind === lanes.kinds[index]));
  const kindHeights = useMemo(() => lanes.kinds.map((kind) => timelineLaneHeight(kind, phone)), [lanes.kinds, phone]);
  const measure = useCallback((heights: readonly number[]) => setMeasured((current) => {
    const shown = heightsHold(current.projection) ? current.heights : kindHeights;
    return shown.length === heights.length && shown.every((height, index) => height === heights[index]) ? current : { projection: lanes, heights };
  }), [lanes, kindHeights]);
  const laneHeights = tableShown ? heightsHold(measured.projection) ? measured.heights : kindHeights
    : lanes.groups.length ? kindHeights : undefined;
  return { laneHeights, measure };
}
