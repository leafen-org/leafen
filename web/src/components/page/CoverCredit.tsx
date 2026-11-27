import { type ReactNode, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { coverCredit, UNSPLASH_REFERRAL_URL } from "@/lib/coverCredit";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import styles from "./CoverCredit.module.css";

/**
 * A photo from Unsplash names its photographer and Unsplash, as Unsplash's API terms ask: "〈photographer〉 ·
 * Unsplash", small and faint in its picture's bottom end corner, each name a link that opens in a new tab.
 * Notion's covers and pictures show no credit; Leafen's shows only while the pointer is over the picture (the
 * element it stands in), while a key's focus is on one of its links, or once a touch press lands on the picture,
 * till a press lands elsewhere (a touch screen has no pointer resting over anything). `shown` sets it from outside
 * instead of the pointer (the picture view, by its controls); `place="top"` stands it in the top end corner.
 * Anything else draws nothing.
 */
export function CoverCredit({ cover, shown, place }: { cover?: string; shown?: boolean; place?: "top" }) {
  const { t } = useTranslation("pageCover");
  const credit = coverCredit(cover);
  const ref = useRef<HTMLSpanElement>(null);
  const [touched, setTouched] = useState(false);
  const credited = credit !== null;
  useEffect(() => {
    const host = ref.current?.parentElement;
    if (!credited || !host) return;
    const press = (event: PointerEvent) => {
      if (event.pointerType !== "mouse") setTouched(event.target instanceof Node && host.contains(event.target));
    };
    document.addEventListener("pointerdown", press, true);
    return () => document.removeEventListener("pointerdown", press, true);
  }, [credited]);
  if (!credit) return null;
  return <span ref={ref} className={styles.credit} data-cover-credit data-place={place}
    data-controlled={shown === undefined ? undefined : "true"} data-shown={shown || touched ? "true" : undefined}
    // A press or Enter on its links opens them alone: no card opens, no picture view, no drag begins.
    onClick={(event) => event.stopPropagation()} onPointerDown={(event) => event.stopPropagation()}
    onKeyDown={(event) => { if (fieldEnter(event)) event.stopPropagation(); }}>
    <span className={styles.srOnly}>{t("credit.photoBy")} </span>
    <a href={credit.profileUrl} target="_blank" rel="noopener noreferrer" draggable={false}>{credit.author}</a>
    <span aria-hidden="true">{" · "}</span>
    <a href={UNSPLASH_REFERRAL_URL} target="_blank" rel="noopener noreferrer" draggable={false}>Unsplash</a>
  </span>;
}

/**
 * A picture drawn as an <img>, which holds nothing: credited, it stands in a box of its own size that holds the
 * credit too (`fill` where the picture spans its column, a preview's; `className` gives the box the picture's own
 * room round it, where a margin of the picture's would leave the credit off its foot); any other picture stays as
 * it is.
 */
export function CreditedPicture({ cover, fill = false, className, children }: {
  cover?: string; fill?: boolean; className?: string; children: ReactNode;
}) {
  if (!coverCredit(cover)) return children;
  return <span className={className ? `${styles.picture} ${className}` : styles.picture} data-fill={fill ? "true" : undefined}>
    {children}<CoverCredit cover={cover} />
  </span>;
}
