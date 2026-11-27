import { useEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { i18next } from "@/i18n";
import { CheckCircleFillIcon, Plus, Settings, TableIcon, X } from "@/icons/leafen";
import { CircleHalfFillIcon, TargetFillIcon } from "@/icons/leafen/multiSourceFilterIcons";
import { routeOverlayKeyDown, useOverlayLayer } from "@/lib/overlayLayer";
import { PopoverLayer } from "@/lib/popoverLayer";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { PhoneSheet } from "../shell/PhoneSheet";
import { PropertyTypeIcon } from "./PropertyTypeIcon";
import styles from "./DashboardMultiSourceFilter.module.css";

const t = (key: string, options?: Record<string, unknown>) => i18next.t(`databaseView:${key}`, options);
const NO_RECT = { top: 0, bottom: 0, left: 0, right: 0 };

/** The sources' list's foot, "⚙ 다중 출처 필터링", as Notion's dashboard filter lists end (seen 2026-11-17). */
export function MultiSourceFooter({ onOpen }: { onOpen(): void }) {
  return (
    <div className={styles.footer}>
      <button type="button" className={styles.footerRow} data-multi-source-filtering onClick={onOpen}>
        <Settings size="regular" aria-hidden="true" />
        <span>{t("multiSourceFiltering")}</span>
      </button>
    </div>
  );
}

/** The head of the card and of a filter's window: "다중 출처 필터링" and its round "×". */
export function MultiSourceHead({ onClose }: { onClose(button: HTMLButtonElement): void }) {
  return (
    <div className={styles.head}>
      <span className={styles.title}>{t("multiSourceFiltering")}</span>
      <button type="button" className={styles.close} aria-label={i18next.t("common:actions.close")} data-multi-source-close
        onClick={(event) => onClose(event.currentTarget)}>
        <X size="small" aria-hidden="true" />
      </button>
    </div>
  );
}

/**
 * Closes the window holding `button` as its Escape does: a card drawn in a
 * list's window ("필터 기준", "+ 필터") has no close of its own to call.
 */
export function closeHoldingWindow(button: HTMLElement) {
  routeOverlayKeyDown({ key: "Escape", defaultPrevented: false, preventDefault() {}, target: button });
}

/**
 * What "⚙ 다중 출처 필터링" shows in its list's place, as Notion's (seen
 * twice on 2026-11-17): its head, a picture of a "상태: 진행 중" pill over
 * three tables, a line saying the filter works across several sources' properties, and the blue
 * "+ 필터에 추가하기", which opens the sources' list in a window of its own
 * (`list`); a property picked there starts the filter. A filter whose last
 * property was taken away shows it in its own window, "필터" on its pill.
 */
export function MultiSourceCard({ list, sheet = false, onClose }: {
  /** The sources' list "+ 필터에 추가하기" opens, told how to close. */
  list(close: () => void): ReactNode;
  /** In a phone's sheet, which heads it: no head of its own, its list a sheet over it. */
  sheet?: boolean;
  onClose(button: HTMLButtonElement): void;
}) {
  const [picking, setPicking] = useState(false);
  const addRef = useRef<HTMLButtonElement>(null), cardRef = useRef<HTMLDivElement>(null);
  // In a sheet it takes the place of a list scrolled to its foot: it shows from its top.
  useEffect(() => { if (sheet) cardRef.current?.scrollIntoView({ block: "nearest" }); }, [sheet]);
  return (
    <div ref={cardRef} className={`${styles.card} ${sheet ? styles.sheetCard : ""}`} data-multi-source-card>
      {sheet ? null : <MultiSourceHead onClose={onClose} />}
      <div className={styles.picture} aria-hidden="true">
        <span className={styles.sample}><PropertyTypeIcon type="status" size="small" />{t("multiSourceSample")}</span>
        <span className={styles.tables}>
          {[<CheckCircleFillIcon key="check" style={{ color: "var(--accent-blue)" }} />, <TargetFillIcon key="target" style={{ color: "var(--c-accent-red)" }} />,
            <CircleHalfFillIcon key="half" style={{ color: "var(--c-accent-yellow)" }} />].map((mark) => (
            <span key={mark.key} className={styles.table}><TableIcon size="large" /><span className={styles.badge}>{mark}</span></span>
          ))}
        </span>
      </div>
      <p className={styles.text}>{t("multiSourceIntro")}</p>
      <button ref={addRef} type="button" className={styles.primary} aria-haspopup="dialog" aria-expanded={picking}
        data-multi-source-add onClick={() => setPicking(!picking)}>
        <Plus size="tiny" aria-hidden="true" /><span>{t("multiSourceAdd")}</span>
      </button>
      {picking ? <MultiSourceList sheet={sheet} anchor={addRef} onClose={() => setPicking(false)}>{list(() => setPicking(false))}</MultiSourceList> : null}
    </div>
  );
}

/** The sources' list the card's and a filter's buttons open: its own window, or on a phone a sheet over the one it came from. */
export function MultiSourceList({ sheet, anchor, onClose, children }: {
  sheet: boolean;
  anchor: RefObject<HTMLElement | null>;
  onClose(): void;
  children: ReactNode;
}) {
  if (sheet) {
    return <PhoneSheet title={t("multiSourceFiltering")} onClose={onClose} data={{ "data-multi-source-picker": "" }}>{children}</PhoneSheet>;
  }
  return <MultiSourcePickerWindow anchor={anchor} onClose={onClose}>{children}</MultiSourcePickerWindow>;
}

/**
 * The sources' list "+ 필터에 추가하기" and "+ 더 추가하기" open: a window of
 * its own 4px from its button, under it or over it where it has no room
 * under it, its start on the start of the window holding the button, at most
 * 315px tall (Notion's, seen twice on 2026-11-17).
 */
export function MultiSourcePickerWindow({ anchor, onClose, children }: {
  anchor: RefObject<HTMLElement | null>;
  onClose(): void;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const layer = useOverlayLayer(true, { kind: "picker", ref, onEscape: onClose, onOutsidePress: onClose, inside: () => [anchor.current],
    returnFocusRef: anchor });
  // Its start on the start of the window holding the button, 4px under or over the button (Notion's: the card at 487.5, the list too).
  const place = usePopoverPlacement(ref, (size, viewport) => {
    const button = popoverRectOf(anchor.current) ?? NO_RECT;
    const holder = popoverRectOf(anchor.current?.closest<HTMLElement>('[role="dialog"]') ?? null) ?? button;
    return placeAround({ ...button, left: holder.left, right: holder.right }, size, viewport, { gap: 4, align: "start", whole: true, cap: 315 });
  }, [], undefined, false);
  return (
    <PopoverLayer>
      <div ref={ref} className={styles.pickerWindow} role="dialog" aria-label={t("multiSourceFiltering")} data-multi-source-picker
        style={{ ...popoverPlacementStyle(place), position: "fixed", zIndex: layer.zIndex }} onKeyDown={layer.onKeyDown}>
        {children}
      </div>
    </PopoverLayer>
  );
}
