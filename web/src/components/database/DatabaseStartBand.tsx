import { type RefObject, useCallback, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Settings, StartSamplesIcon, StartTourIcon, X } from "@/icons/leafen";
import { createDatabaseRowPageService } from "@/lib/databaseRowPageService";
import type { DatabaseStarterBand } from "@/lib/databaseStarterBand";
import { useOptionalProductToast } from "@/lib/productToast";
import type { Session } from "@/types";
import { DatabaseStartTour, databaseTourKnown } from "./DatabaseStartTour";
import styles from "./DatabaseStartBand.module.css";

// Notion's band lies over the page's foot, its top 122px above it, so a
// database with a few rows doesn't scroll (seen 2026-11-06, 696px tall); a
// longer one keeps it under its rows.
const FOOT = 122, GAP = 24;

/**
 * Where the view's own content ends, in the window: a box that reaches the
 * page's foot only to pin its scrollbar there (a wide table's scroll box)
 * ends where its content does, so the band goes by the rows, not the box —
 * by the box it stood under the foot and the page scrolled to it (2026-11-15).
 * The table's own scrollbar, a bar held at the foot below that box, is the
 * foot too, and the box reaching it reaches the foot: with a scrollbar that
 * takes room (a Mac with a mouse), the box ended 16px over the foot, the
 * band stood under it and the page scrolled 77px, its scrollbar coming in a
 * frame after the toolbar was drawn and moving the toolbar 15px under a
 * press (2026-11-15; Notion's page with a few rows doesn't scroll).
 * Each box it reads it watches (`watch`), so new rows move the band.
 */
function contentBottom(element: Element, foot: number, watch: (element: Element) => void): number {
  const style = getComputedStyle(element), contents = style.display === "contents", box = element.getBoundingClientRect();
  // A layer laid over the view (its rows' handles), or a bar held at the foot, is no content.
  if (style.position === "absolute" || style.position === "fixed" || style.position === "sticky") return Number.NEGATIVE_INFINITY;
  if (!contents && Math.abs(box.bottom - foot) > 1) {
    watch(element);
    return box.bottom;
  }
  const bar = [...element.children].find((child) => getComputedStyle(child).position === "sticky"
    && Math.abs(child.getBoundingClientRect().bottom - foot) <= 1);
  const inner = bar ? bar.getBoundingClientRect().top : foot;
  let bottom = contents ? Number.NEGATIVE_INFINITY : box.top;
  for (const child of element.children) bottom = Math.max(bottom, contentBottom(child, child === bar ? foot : inner, watch));
  return bottom;
}

function usePlacedNearFoot(ref: RefObject<HTMLDivElement | null>) {
  useLayoutEffect(() => {
    const band = ref.current;
    let foot = band?.parentElement ?? null;
    while (foot && !/(auto|scroll)/.test(getComputedStyle(foot).overflowY)) foot = foot.parentElement;
    if (!band || !foot) return;
    const scroller = foot;
    let frame = 0;
    const observer = new ResizeObserver(() => { cancelAnimationFrame(frame); frame = requestAnimationFrame(place); });
    const watch = (element: Element) => observer.observe(element);
    function place() {
      const current = parseFloat(band!.style.marginTop) || 0, view = scroller.getBoundingClientRect();
      const before = band!.previousElementSibling;
      const after = before ? contentBottom(before, view.bottom, watch) : band!.getBoundingClientRect().top - current;
      const natural = (Number.isFinite(after) ? after : band!.getBoundingClientRect().top - current) - view.top + scroller.scrollTop;
      const room = scroller.clientHeight - FOOT - natural, pinned = room >= GAP, top = pinned ? room : GAP;
      // Its place under the content, the room between them given as its top margin.
      const flow = band!.getBoundingClientRect().top - current - view.top + scroller.scrollTop;
      band!.style.marginTop = `${top + natural - flow}px`;
      // Over the foot it takes no room, as the page's own space below it is there.
      band!.style.marginBottom = pinned || natural < flow ? `${-(top + natural - flow + band!.offsetHeight)}px` : "";
    }
    place();
    observer.observe(scroller);
    if (band.previousElementSibling) observer.observe(band.previousElementSibling);
    return () => { observer.disconnect(); cancelAnimationFrame(frame); };
  }, [ref]);
}

/**
 * Notion's "시작하기" band on a database page a template made (seen
 * 2026-11-06): × and "시작하기" (12px/500, faint) over 32px pills 8px apart —
 * "샘플 페이지 삭제하기" while its example rows are there (to the trash at once,
 * "샘플 페이지 삭제됨 · 실행 취소"), "투어" where Leafen knows the template's
 * tour, and "맞춤 설정", which opens the view's settings. × closes it for good.
 */
export function DatabaseStartBand({ session, band, rowIds, onChange, onReread, onCustomize }: {
  session: Session;
  band: DatabaseStarterBand;
  /** The rows the database shows now; samples already gone leave the pill out. */
  rowIds: readonly string[];
  onChange(band: DatabaseStarterBand | null): void;
  onReread(): Promise<void>;
  onCustomize(): void;
}) {
  const { t } = useTranslation(["databaseView", "rowMenu"]);
  const toast = useOptionalProductToast();
  const ref = useRef<HTMLDivElement>(null);
  const [busy, setBusy] = useState(false), [touring, setTouring] = useState(false);
  const database = useCallback(() => ref.current?.parentElement?.querySelector<HTMLElement>("[data-database-view]") ?? null, []);
  usePlacedNearFoot(ref);
  const present = band.samples.filter((id) => rowIds.includes(id));

  async function moveSamples(ids: readonly string[], trash: boolean) {
    const service = createDatabaseRowPageService(session, () => {});
    setBusy(true);
    try {
      for (const id of ids) await (trash ? service.trash(id) : service.restore(id));
      await onReread();
      return true;
    } catch {
      toast?.notify(t(trash ? "rowMenu:toast.couldntMoveToTrash" : "rowMenu:toast.couldntRestorePage"), "error");
      return false;
    } finally {
      service.dispose();
      setBusy(false);
    }
  }

  async function deleteSamples() {
    const ids = present;
    if (!ids.length || !await moveSamples(ids, true)) return;
    onChange({ ...band, samples: [] });
    toast?.notify(t("startBand.samplesDeleted"), "success", {
      label: t("startBand.undo"),
      onClick: () => { void moveSamples(ids, false).then((restored) => { if (restored) onChange({ ...band, samples: [...ids] }); }); },
    });
  }

  return (
    <div ref={ref} className={styles.band} contentEditable={false} data-database-start-band>
      <div className={styles.head}>
        <button type="button" className={styles.close} aria-label={t("startBand.close")} title={t("startBand.close")}
          onClick={() => onChange(null)}>
          <X size="small" aria-hidden="true" />
        </button>
        <span className={styles.label}>{t("startBand.label")}</span>
      </div>
      <div className={styles.pills} role="group" aria-label={t("startBand.label")}>
        {present.length ? (
          <button type="button" className={styles.pill} disabled={busy} onClick={() => void deleteSamples()}>
            <StartSamplesIcon aria-hidden="true" />{t("startBand.deleteSamples")}
          </button>
        ) : null}
        {databaseTourKnown(band.tour) ? (
          <button type="button" className={styles.pill} onClick={() => setTouring(true)}>
            <StartTourIcon aria-hidden="true" />{t("startBand.tour")}
          </button>
        ) : null}
        <button type="button" className={styles.pill} onClick={onCustomize}>
          <Settings aria-hidden="true" />{t("startBand.customize")}
        </button>
      </div>
      {touring && band.tour ? <DatabaseStartTour tour={band.tour} database={database} onClose={() => setTouring(false)} /> : null}
    </div>
  );
}
