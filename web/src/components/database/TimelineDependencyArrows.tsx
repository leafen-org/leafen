import { useEffect, useState, type PointerEvent as ReactPointerEvent } from "react";
import { afterNextPaint } from "@/lib/afterPress";
import depStyles from "./timelineDeps.module.css";

/** A dependency link as the timeline draws it, from its predecessor's end to its successor's start, along the reading direction. */
export type TimelineArrow = { key: string; predecessor: string; successor: string; x1: number; x2: number; y1: number; y2: number };

/**
 * The timeline's dependency links, as Notion's (read 2026-11-24 in Chrome
 * at 1440, dark, on an owner's synthetic timeline A→B→C, two tabs at
 * different times): a 2px line with its arrowhead, which takes no pointer.
 * For one who may change the timeline each line has a 16px band along it
 * that does: pointed at, its arrowhead turns into a red ✕ in a 16px rounded
 * square, and pressing that takes the link away, with nothing asked; the
 * line itself neither changes nor does anything when pressed. For one who
 * only reads it (a published site's visitor) there is no band: nothing
 * changes under the pointer, as Notion's public timeline.
 *
 * The press hides the line on its own frame and hands the link to `onDelete`
 * after it is painted, which takes it out of the timeline's links and to the
 * server (TimelineView); a link the server keeps comes back with the links.
 */
export function TimelineDependencyArrows({ arrows, markerId, onDelete }: {
  arrows: readonly TimelineArrow[];
  markerId: string;
  onDelete?: (successorRowId: string, predecessorRowId: string) => void;
}) {
  const [pointed, setPointed] = useState<string | null>(null);
  const [gone, setGone] = useState<string | null>(null);
  // The links read anew (the one taken away left them, or came back) show as they are.
  useEffect(() => setGone(null), [arrows]);
  if (!arrows.length) return null;
  const take = (arrow: TimelineArrow) => {
    if (!onDelete) return;
    setGone(arrow.key);
    setPointed(null);
    afterNextPaint(() => onDelete(arrow.successor, arrow.predecessor));
  };
  // The ✕'s press is its own: the lanes under it neither start a bar nor a drag.
  const hold = (event: ReactPointerEvent<SVGGElement>) => { event.stopPropagation(); event.preventDefault(); };
  return (
    <svg className={depStyles.depOverlay} aria-hidden="true">
      <defs><marker id={markerId} viewBox="0 0 8 8" refX="6" refY="4" markerWidth="4.5" markerHeight="4.5" orient="auto-start-reverse"><path className={depStyles.depArrowFill} d="M0,0 L8,4 L0,8 Z" /></marker></defs>
      {arrows.map((arrow) => {
        if (arrow.key === gone) return null;
        const stub = 10;
        const middle = (arrow.x1 + arrow.x2) / 2;
        const path = `M ${arrow.x1} ${arrow.y1} L ${arrow.x1 + stub} ${arrow.y1} C ${middle} ${arrow.y1}, ${middle} ${arrow.y2}, ${arrow.x2 - stub} ${arrow.y2} L ${arrow.x2} ${arrow.y2}`;
        const shown = onDelete && pointed === arrow.key;
        return <g key={arrow.key}>
          <path className={depStyles.depPath} data-timeline-dependency-edge={arrow.key} d={path} markerEnd={shown ? undefined : `url(#${markerId})`} />
          {onDelete ? <g onPointerEnter={() => setPointed(arrow.key)} onPointerLeave={() => setPointed((current) => current === arrow.key ? null : current)}>
            <path className={depStyles.depHit} d={path} data-timeline-dependency-hit={arrow.key} />
            {/* Its ✕ where its arrowhead was, 5 short of its end and 2 above it, as Notion's. */}
            {shown ? <g className={depStyles.depDelete} transform={`translate(${arrow.x2 - 13} ${arrow.y2 - 10})`} data-timeline-dependency-delete={arrow.key}
              onPointerDown={hold} onClick={(event) => { event.stopPropagation(); take(arrow); }}>
              <rect width="16" height="16" rx="5" className={depStyles.depDeleteGround} />
              <circle cx="8" cy="8" r="6" className={depStyles.depDeleteMark} />
              <path d="M5.9 5.9 L10.1 10.1 M10.1 5.9 L5.9 10.1" className={depStyles.depDeleteCross} />
            </g> : null}
          </g> : null}
        </g>;
      })}
    </svg>
  );
}
