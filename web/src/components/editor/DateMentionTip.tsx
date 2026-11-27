import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { CalendarIcon } from "@/icons/leafen";
import { activeDateLocale } from "@/lib/i18n";
import { hoverCardAnchor, listenHoverCardKeys } from "@/lib/hoverCard";
import { placeAround, type PopoverRect, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { dateMentionLabel, safeDateMentionTimeFormat } from "./dateMentions";
import styles from "./DateMentionTip.module.css";

const SHOW_AFTER_MS = 400;
const TIP_GAP = 6;
const MENTION = '[data-mention="date"][data-date]';

type Shown = { element: HTMLElement; anchor: PopoverRect; text: string };

/**
 * A date mention's day in full while the pointer rests on it, as Notion's
 * (seen 2026-11-13, a page's lines and a comment's field alike): after about
 * 0.4s, "🗓 2026년 11월 14일", a time after it with its half of the day last
 * ("2026년 11월 14일 6:00 오후"); leaving the mention, a press or a scroll
 * ends it, a key does not (listenHoverCardKeys).
 */
export function DateMentionTip() {
  const [shown, setShown] = useState<Shown | null>(null);
  const tipRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    let timer = 0, current: HTMLElement | null = null, showing = false;
    const hide = () => {
      showing = false;
      window.clearTimeout(timer);
      current = null;
      setShown(null);
    };
    function onOver(event: MouseEvent) {
      const mention = event.target instanceof Element ? event.target.closest<HTMLElement>(MENTION) : null;
      if (mention === current) return;
      hide();
      if (!mention) return;
      current = mention;
      timer = window.setTimeout(() => {
        const anchor = mention.isConnected ? hoverCardAnchor(mention) : null;
        if (anchor) { showing = true; setShown({ element: mention, anchor, text: dateMentionTipText(mention) }); }
      }, SHOW_AFTER_MS);
    }
    document.addEventListener("mouseover", onOver);
    document.addEventListener("mousedown", hide, true);
    const keys = listenHoverCardKeys(() => (showing ? current : null), hide);
    window.addEventListener("scroll", hide, true);
    return () => {
      window.clearTimeout(timer);
      document.removeEventListener("mouseover", onOver);
      document.removeEventListener("mousedown", hide, true);
      keys();
      window.removeEventListener("scroll", hide, true);
    };
  }, []);
  const place = usePopoverPlacement(tipRef, shown ? (size, viewport) => placeAround(
    shown.anchor, size, viewport, { gap: TIP_GAP, minHeight: size.height },
  ) : null, [shown], undefined, { kind: "hovercard" });
  if (!shown || typeof document === "undefined") return null;
  return createPortal(
    <div ref={tipRef} className={styles.tip} role="tooltip" data-date-mention-tip style={popoverPlacementStyle(place)}>
      <CalendarIcon size="micro" className={styles.icon} aria-hidden="true" />{shown.text}
    </div>,
    document.body,
  );
}

/** The day in full, a time with its half of the day after it, and an end after "→". */
function dateMentionTipText(mention: HTMLElement) {
  const { date = "", dateEnd, dateTimeFormat } = mention.dataset;
  const one = (value: string) => {
    const day = dateMentionLabel(value.slice(0, 10), undefined, "full");
    const time = /[T ](\d{2}):(\d{2})/.exec(value);
    const shown = safeDateMentionTimeFormat(dateTimeFormat);
    if (!time || shown === "hidden") return day;
    if (shown === "24") return `${day} ${Number(time[1])}:${time[2]}`;
    const parts = new Intl.DateTimeFormat(activeDateLocale(), { hour: "numeric", minute: "2-digit", hour12: true })
      .formatToParts(new Date(2000, 0, 1, Number(time[1]), Number(time[2])));
    const half = parts.find((part) => part.type === "dayPeriod")?.value;
    const clock = parts.filter((part) => part.type !== "dayPeriod").map((part) => part.value).join("").trim();
    return `${day} ${clock}${half ? ` ${half}` : ""}`;
  };
  return dateEnd ? `${one(date)} → ${one(dateEnd)}` : one(date);
}
