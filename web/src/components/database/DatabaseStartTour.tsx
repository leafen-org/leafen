import { useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { X } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import styles from "./DatabaseStartBand.module.css";

type TourStep = { key: string; anchor: (database: HTMLElement) => DOMRect | null };

const rectOf = (element: Element | null) => element?.getBoundingClientRect() ?? null;

/** The rect around a view's tabs, its "+" and "⋯" left out (Notion's ring, seen 2026-11-06). */
function tabsRect(database: HTMLElement) {
  const tabs = [...database.querySelectorAll("[data-view-tabs] [role=tab]")].map((tab) => tab.getBoundingClientRect());
  if (!tabs.length) return rectOf(database.querySelector("[data-view-tabs]"));
  const left = Math.min(...tabs.map(({ left }) => left)), right = Math.max(...tabs.map(({ right }) => right));
  const top = Math.min(...tabs.map(({ top }) => top)), bottom = Math.max(...tabs.map(({ bottom }) => bottom));
  return new DOMRect(left, top, right - left, bottom - top);
}

/** The task tracker's tour, as Notion's (seen 2026-11-06): its new button, its views, its view settings. */
const TOURS: Record<string, readonly TourStep[]> = {
  tasks: [
    { key: "newItem", anchor: (database) => rectOf(database.querySelector("[data-database-new-row]")) },
    { key: "views", anchor: tabsRect },
    { key: "customize", anchor: (database) => rectOf(database.querySelector("[data-database-settings-trigger]")) },
  ],
};

/** Whether Leafen knows this template's tour. */
export function databaseTourKnown(tour: string | undefined) {
  return Boolean(tour && TOURS[tour]);
}

/**
 * A template's tour over its database (Notion's, seen 2026-11-06): one
 * 250px tip at a time 22px under what it points at, joined to it by a line,
 * a ring round that; its title, words, ● ● ● for the steps, "뒤로가기" and
 * "다음" (the last "완료"), and × to leave. Escape leaves too.
 */
export function DatabaseStartTour({ tour, database, onClose }: { tour: string; database: () => HTMLElement | null; onClose: () => void }) {
  const { t } = useTranslation("databaseView");
  const steps = TOURS[tour] ?? [];
  const [index, setIndex] = useState(0);
  const [rect, setRect] = useState<DOMRect | null>(null);
  const tipRef = useRef<HTMLDivElement>(null);
  const step = steps[index];
  const { zIndex } = useOverlayLayer(Boolean(step), { kind: "panel", ref: tipRef, onEscape: onClose, initialFocus: "first" });
  useLayoutEffect(() => {
    if (!step) return;
    const place = () => {
      const root = database(), next = root ? step.anchor(root) : null;
      setRect((old) => old && next && old.x === next.x && old.y === next.y && old.width === next.width && old.height === next.height ? old : next);
    };
    place();
    window.addEventListener("resize", place);
    document.addEventListener("scroll", place, true);
    return () => {
      window.removeEventListener("resize", place);
      document.removeEventListener("scroll", place, true);
    };
  }, [database, step]);
  if (!step || !rect || typeof document === "undefined") return null;
  const last = index === steps.length - 1;
  const center = rect.left + rect.width / 2;
  const left = Math.max(8, Math.min(window.innerWidth - 258, center - 125));
  return createPortal(<>
    <div className={styles.tourRing} style={{ left: rect.left - 4, top: rect.top - 4, width: rect.width + 8, height: rect.height + 8, zIndex }} aria-hidden="true" />
    <div className={styles.tourLine} style={{ left: center, top: rect.bottom + 4, zIndex }} aria-hidden="true" />
    <div ref={tipRef} className={styles.tourTip} style={{ left, top: rect.bottom + 22, zIndex }} role="dialog"
      aria-label={t(`startTour.${step.key}.title`)} data-database-start-tour={step.key}>
      <div className={styles.tourHead}>
        <span className={styles.tourTitle}>{t(`startTour.${step.key}.title`)}</span>
        <button type="button" className={styles.tourClose} aria-label={t("startTour.close")} onClick={onClose}>
          <X size="small" aria-hidden="true" />
        </button>
      </div>
      <p className={styles.tourBody}>{t(`startTour.${step.key}.body`)}</p>
      <div className={styles.tourFoot}>
        <span className={styles.tourDots} aria-label={t("startTour.progress", { current: index + 1, count: steps.length })}>
          {steps.map((item, at) => <span key={item.key} data-current={at === index ? "true" : undefined} />)}
        </span>
        {index > 0 ? <button type="button" className={styles.tourAction} onClick={() => setIndex(index - 1)}>{t("startTour.back")}</button> : null}
        <button type="button" className={styles.tourAction} onClick={() => last ? onClose() : setIndex(index + 1)}>
          {t(last ? "startTour.done" : "startTour.next")}
        </button>
      </div>
    </div>
  </>, document.body);
}
