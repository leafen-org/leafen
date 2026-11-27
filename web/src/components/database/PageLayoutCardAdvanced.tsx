import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ArrowUpRightIcon, ChevronRight, Settings } from "@/icons/leafen";
import { placeBeside, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { readsRightToLeft } from "@/lib/readingDirection";
import styles from "./PageLayoutCardPopover.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/**
 * A relation's own card's "고급 ›", as Notion's builder (seen 2026-11-14 at
 * 1440): a row of the card's moves between "속성 그룹에 추가" and "레이아웃에서
 * 제거", a gear before its name and its "›" at its end in the icons' grey.
 * Pointed at or pressed it opens its menu beside the row, level with the
 * row's middle: "↗ 관계형 그룹으로 전환 — 최소화 또는 반응형 페이지 섹션 지원",
 * which turns the card into a relations group holding the relation. Pointing
 * at another move closes it; Escape or ← gives the keys back to the row. After
 * Escape on a menu the pointer opened, the row stays lit with no ring and a
 * typed letter lands nowhere, as Notion's (measured twice on 2026-11-15 in
 * Chrome 1440 in different tabs, no script before the key, the screen and one
 * typed letter after it); from the keys it wears the keyboard's ring.
 */
export function PageLayoutCardAdvanced({ onGroup }: { onGroup(): void }) {
  const { t } = useTranslation("pageLayout");
  const [open, setOpen] = useState(false);
  const row = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const [keyed, setKeyed] = useState(false);
  const back = (quiet = false) => {
    setOpen(false);
    if (quiet && row.current) row.current.dataset.quietRing = "";
    row.current?.focus({ preventScroll: true });
  };
  const place = usePopoverPlacement(menu, open ? (size, viewport) => {
    const at = popoverRectOf(row.current) ?? { top: 0, bottom: 0, left: 0, right: 0 };
    return placeBeside(at, size, viewport, { gap: 0, offsetTop: (at.bottom - at.top - size.height) / 2,
      prefer: row.current && readsRightToLeft(row.current) ? "left" : "right" });
  } : null, [open], undefined, { kind: "picker", restoreFocus: false, onEscape: () => back(!keyed), onOutsidePress: () => setOpen(false), inside: () => [row.current] });
  // Another of the card's moves pointed at closes it, as Notion's.
  useEffect(() => {
    const moves = row.current?.parentElement;
    if (!open || !moves) return undefined;
    const away = (event: PointerEvent) => {
      if (!row.current?.contains(event.target as Node)) setOpen(false);
    };
    moves.addEventListener("pointerover", away);
    return () => moves.removeEventListener("pointerover", away);
  }, [open]);
  // Opened from the keys, its first choice takes them.
  useEffect(() => {
    if (open && keyed && place) menu.current?.querySelector<HTMLElement>("[role=menuitem]")?.focus({ preventScroll: true });
  }, [open, keyed, place]);
  const openFromKeys = (event: KeyboardEvent<HTMLButtonElement>) => {
    const forward = row.current && readsRightToLeft(row.current) ? "ArrowLeft" : "ArrowRight";
    if (event.key !== forward && !fieldEnter(event) && event.key !== " ") return;
    event.preventDefault();
    setKeyed(true);
    setOpen(true);
  };
  const closeFromKeys = (event: KeyboardEvent<HTMLDivElement>) => {
    const backward = row.current && readsRightToLeft(row.current) ? "ArrowRight" : "ArrowLeft";
    if (event.key !== backward) return;
    event.preventDefault();
    back();
  };
  return (
    <>
      <button ref={row} type="button" className={`${styles.action} ${styles.advanced}`} aria-haspopup="menu" aria-expanded={open}
        data-open={open || undefined} data-layout-module-action="advanced"
        onBlur={event => { delete event.currentTarget.dataset.quietRing; }}
        onPointerEnter={() => { setKeyed(false); setOpen(true); }} onClick={() => { setKeyed(false); setOpen(true); }} onKeyDown={openFromKeys}>
        <Settings size="regular" aria-hidden="true" /><span>{t("advanced")}</span>
        <ChevronRight size="small" className={styles.advancedMark} aria-hidden="true" />
      </button>
      {open ? createPortal(
        <div ref={menu} role="menu" aria-label={t("advanced")} className={styles.advancedMenu} style={popoverPlacementStyle(place)}
          onKeyDown={closeFromKeys} data-layout-advanced-menu>
          <button type="button" role="menuitem" className={styles.advancedChoice} onClick={() => { setOpen(false); onGroup(); }}
            data-layout-module-action="relationsGroup">
            <ArrowUpRightIcon size="regular" aria-hidden="true" />
            <span><span>{t("toRelationsGroup")}</span><small>{t("toRelationsGroupNote")}</small></span>
          </button>
        </div>,
        document.body,
      ) : null}
    </>
  );
}
