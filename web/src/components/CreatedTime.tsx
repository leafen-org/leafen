import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { CalendarIcon } from "@/icons/leafen";
import { placeAround, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { createdAtLabel, shortAgoLabel } from "@/lib/relativeTime";
import { useTipPress } from "@/lib/tipTiming";
import { useMinuteNow } from "@/lib/useMinuteNow";
import styles from "./CreatedTime.module.css";

/**
 * When a comment or a feed post was made, as Notion writes it by the
 * writer's name ("지금", "2분", "13시간", then the date), kept current; the
 * pointer over it shows the exact time in a small white tip 6px under it,
 * a calendar before "생성 일시: …" (seen 2026-10-21). It goes as the app's
 * other tips go (lib/tipTiming.ts): off it or with a press anywhere.
 */
export function CreatedTime({ value, className, suffix }: { value?: string; className?: string; suffix?: string }) {
  const now = useMinuteNow();
  const [anchor, setAnchor] = useState<DOMRect | null>(null);
  const tipRef = useRef<HTMLSpanElement>(null), own = useRef<HTMLTimeElement>(null);
  useTipPress(anchor !== null, () => setAnchor(null), () => own.current);
  const exact = createdAtLabel(value);
  const place = usePopoverPlacement(tipRef, exact && anchor
    ? (size, viewport) => placeAround(anchor, size, viewport, { gap: 6 })
    : null, [anchor, exact], undefined, false);
  return (
    <>
      <time ref={own} className={className} dateTime={value} data-created-time
        onMouseEnter={exact ? (event) => setAnchor(event.currentTarget.getBoundingClientRect()) : undefined}
        onMouseLeave={() => setAnchor(null)}>
        {shortAgoLabel(value, now)}{suffix}
      </time>
      {exact && anchor && typeof document !== "undefined" ? createPortal(
        <span ref={tipRef} className={styles.tip} style={popoverPlacementStyle(place)} role="tooltip" data-created-time-tip>
          <CalendarIcon className={styles.icon} size="micro" aria-hidden="true" />{exact}
        </span>,
        document.body,
      ) : null}
    </>
  );
}
