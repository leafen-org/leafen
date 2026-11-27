import { useRef, useState, type RefCallback } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { keyCameLast } from "@/lib/lastPress";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { usePageNames } from "@/lib/pageNames";
import type { Page } from "@/lib/types";
import { PageIconGlyph } from "../page/PageIcon";
import { RowTitleText } from "./RichTextCellText";
import styles from "./UndatedRowsMenu.module.css";

type UndatedRowsMenuProps = {
  rows: readonly Page[]; readOnly: boolean; timeline?: boolean; place?: boolean;
  onAssignToday(row: Page): void; onOpen(rowId: string): void;
  rowRef?(rowId: string): RefCallback<HTMLElement>;
};

/** Both date views use their existing authorized row projection and date writer.
 * A map lists its rows with no place the same way, each opening its page, as
 * Notion's "위치 없음(N개)" (seen 2026-11-02: 320px at the button's left).
 * With none it is nothing, its menu not made: made for none, a timeline's
 * view tab's press built it before its first paint (2026-11-15, ~7 ms at CPU ×2).
 * A phone's date views draw no "날짜 없음": Notion's phone calendar and
 * timeline toolbars hold the view's picker, search, "필터 및 정렬", settings
 * and "+ ⌄" alone (calendar seen 2026-11-04; timeline seen 2026-11-12 and
 * 2026-11-15 at 402pt in iOS Safari, three rows undated), so the view's name
 * is not cut. */
export function UndatedRowsMenu(props: UndatedRowsMenuProps) {
  const { phone } = useSidebarLayout(true);
  if (phone && !props.place) return null;
  return props.rows.length ? <RowsMenu {...props} /> : null;
}

function RowsMenu({ rows, readOnly, onAssignToday, onOpen, rowRef, timeline = false, place = false }: UndatedRowsMenuProps) {
  const pageNames = usePageNames();
  const { t } = useTranslation("databaseView");
  const [open, setOpen] = useState(false), [query, setQuery] = useState("");
  const trigger = useRef<HTMLButtonElement>(null), panel = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLDivElement>(null), search = useRef<HTMLInputElement>(null);
  const shown = rows.filter(row => pageNames.title(row).toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()));
  const label = t(place ? "placelessRows.heading" : "undatedRows.heading", { count: rows.length });
  // Escape after a press leaves the page's body focused with no ring, a typed letter landing nowhere, as
  // Notion's (a calendar's "날짜 없음(N)" pressed open, measured twice 2026-11-15 in Chrome 1440 in different
  // tabs, the screen and one typed letter after the key, no script before it). Opened from the keys, it gives
  // its button the keys back, ringed (Notion's not measured there).
  const openedByKey = useRef(false);
  function close(restore = false) { setOpen(false); setQuery(""); if (restore) trigger.current?.focus({ preventScroll: true }); }
  const menu = useNativeMenu<Page>({ items: shown, open, menuRef, focusOwnerRef: search, label,
    keyOf: row => row.id, textOf: row => pageNames.title(row), onClose: () => close(openedByKey.current),
    onAction: row => { if (place || readOnly || row.isLocked) { close(); onOpen(row.id); } else onAssignToday(row); } });
  const placement = usePopoverPlacement(panel, open ? (size, viewport) => placeAround(popoverRectOf(trigger.current)!, size, viewport,
    { gap: 4, align: "start", alignFallback: true }) : null, [open], undefined,
    { kind: "menu", restoreFocus: false, onEscape: () => close(openedByKey.current), onOutsidePress: () => close(), inside: () => [trigger.current] });
  const searchLabel = t(place ? "placelessRows.search" : "undatedRows.search");
  // Every row shows its page's icon (an empty page's blank sheet) and an "열기" that is always there, in a
  // 320px panel from the button's start, as Notion's calendar and timeline "날짜 없음(N)" (seen 2026-11-15 in
  // two tabs, 1440 dark).
  const popup = open && <div ref={panel} className={styles.panel} style={{ position: "fixed", width: 320, ...popoverPlacementStyle(placement) }}>
    <div className={styles.search}><input {...menu.ownerProps} ref={search} value={query} aria-label={searchLabel} placeholder={searchLabel}
      onChange={event => setQuery(event.currentTarget.value)} /></div>
    <div className={styles.list}>{!readOnly && !place && <p>{t(timeline ? "undatedRows.timelineHint" : "undatedRows.hint")}</p>}
    <div {...menu.menuProps} ref={menuRef} className={styles.rows}>{shown.map(row => <div key={row.id}
      ref={rowRef?.(row.id)} className={place ? styles.row : `${styles.row} ${styles.withOpen}`} data-undated-row={row.id}>
      <NativeMenuItem itemKey={row.id} state={menu.state} className={styles.item}>
        {() => <><span className={styles.icon} aria-hidden="true"><PageIconGlyph page={{ icon: row.icon ?? "", iconType: row.iconType, workspaceId: row.workspaceId,
            kind: row.kind, notionIcon: row.notionIcon, hasContent: row.hasContent }} size={18} /></span>
          <RowTitleText row={row} title={pageNames.title(row)} /></>}
      </NativeMenuItem>
      {place ? null : <button type="button" className={styles.open} onKeyDown={event => event.stopPropagation()}
        aria-label={t("undatedRows.openPage", { title: pageNames.title(row) })}
        onClick={event => { event.stopPropagation(); close(); onOpen(row.id); }}>{t("undatedRows.open")}</button>}
    </div>)}</div></div>
  </div>;
  return <><button ref={trigger} type="button" className={styles.trigger} aria-haspopup="menu" aria-expanded={open}
    onClick={() => { if (open) close(); else { openedByKey.current = keyCameLast(); setOpen(true); } }}>{label}</button>
    {popup && createPortal(popup, document.body)}</>;
}
