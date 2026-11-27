import { memo } from "react";
import styles from "./TimelineTicks.module.css";

/**
 * Lines down a timeline's days, one at the start of every `every`th column
 * (timelineLineColumns), laid out on the days' own columns so each stands on
 * the tick its time's label is centred on (TimelineDayHeads). Drawn under the
 * dates, which hide their tops, and under the bars and now's line, down to
 * the area's foot, as Notion's (seen 2026-11-24 in Chrome at 1440, dpr 2, in
 * two tabs: a 1px line from each label's middle down past "+ 새로 만들기").
 */
export const TimelineTickLines = memo(function TimelineTickLines({ count, every, gridTemplate }: { count: number; every: number; gridTemplate: string }) {
  return (
    <div className={styles.lines} style={{ gridTemplateColumns: gridTemplate }} aria-hidden="true">
      {Array.from({ length: Math.ceil(count / every) }, (_, index) => <span key={index} style={{ gridColumnStart: index * every + 1 }} />)}
    </div>
  );
});
