import { type CSSProperties, type ReactNode, memo, useContext, useLayoutEffect, useRef, useState } from "react";
import { menuKeyHint } from "@/lib/menuKeyHint";
import { useTranslation } from "react-i18next";
import { Copy, DotsHorizontal, Trash } from "../icons";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import styles from "./databaseSheet";
import { inlineStartX } from "@/lib/readingDirection";
import { pressedPoint } from "@/lib/pressedPoint";
import { usePageMenuWarm } from "../page/TopBarPageMenu";
import { DatabasePagelessRowsContext } from "./databasePagelessRows";

// Neither "휴지통으로 이동" nor "복제" waits for the table's saves still out:
// the rows leave, or their copies show, at the press, and what goes to the
// server goes after the saves — a copy is asked for once they are saved, the
// server copying the rows as it holds them (nativeTableRowSteps.ts).
type TableSelectionToolbarProps = {
  count: number;
  /** The bar's property buttons. */
  properties?: ReactNode;
  onClear(): void;
  onDelete(): void;
  onDuplicate(): void;
  /** The selected rows' menu, opened from the bar's "…" where it was pressed; false when there is none. */
  onRowMenu?(trigger: HTMLElement, point: { x: number; y: number }): boolean;
};

// As Notion's (seen 2026-10-06), the bar floats over the view's toolbar
// while rows are selected and stays in sight as the page scrolls: the count,
// which clears the selection, the property buttons, "move to trash" and "…",
// the selected rows' menu; rows that are no pages, People's, have no
// "move to trash" (databasePagelessRows.ts). An inline database's covers its title row, its
// view tabs left in sight, 8px before the title (seen 2026-11-02: 407.5 by
// a title at 416, over the title row with tabs under it).
const BAR_HEIGHT = 32;
// With no row chosen it shows nothing, and isn't drawn again as the table is
// (each value typed into it). Once the page's start has settled it is drawn
// ahead, out of sight, with the properties it would show: its buttons are
// measured then (TableSelectionProperties), not in the press that chooses the
// first row — drawn and measured there, a fresh tab's first row ⋮⋮ took
// some 30 ms more to its paint than a later one (2026-11-13, NAS condition,
// CPU ×2). Drawn ahead, it is no toolbar and takes no pointer, focus or keys.
/**
 * How far over the table the view's own rows between its toolbar and the
 * table reach — its filter bar, a banner — which the bar rises past to stay
 * on the toolbar's row, as Notion's (seen 2026-11-17 at 1440, a full-page
 * table with its filter bar shown: "1개 선택됨" over the view tabs' row, the
 * pills under it left in sight and pressable).
 */
function overToolbar(view: HTMLElement, tableTop: number) {
  const chrome = view.querySelector<HTMLElement>(":scope > [data-database-chrome]");
  let top = Infinity;
  for (let part = chrome?.nextElementSibling; part; part = part.nextElementSibling) {
    const boxes = getComputedStyle(part).display === "contents" ? [...part.children] : [part];
    for (const box of boxes) {
      const rect = box.getBoundingClientRect();
      if (rect.height > 0 && rect.top < tableTop) top = Math.min(top, rect.top);
    }
  }
  return Number.isFinite(top) ? Math.max(0, tableTop - top) : 0;
}

export const TableSelectionToolbar = memo(SelectionToolbar, (before, after) => before.count === 0 && after.count === 0);

function SelectionToolbar({
  count,
  properties,
  onClear,
  onDelete,
  onDuplicate,
  onRowMenu,
}: TableSelectionToolbarProps) {
  const [open, setOpen] = useState(false);
  const [warm] = usePageMenuWarm("selection-bar");
  const pageless = useContext(DatabasePagelessRowsContext) !== null;
  const { t } = useTranslation("tableView");
  const anchorRef = useRef<HTMLDivElement>(null);
  const [titleRow, setTitleRow] = useState<{ rise: number; left: number } | null>(null);
  const [over, setOver] = useState(0);
  const selecting = count > 0;
  // Measured again as the view's filter bar comes, goes or wraps under its
  // toolbar while rows stay chosen (the view grows or shrinks with it).
  useLayoutEffect(() => {
    const anchor = anchorRef.current, table = anchor?.parentElement, view = anchor?.closest<HTMLElement>("[data-database-view]");
    if (!selecting || !table) { setTitleRow(null); setOver(0); return; }
    const measure = () => {
      const header = anchor?.closest("[data-inline-database-wrapper]")?.querySelector<HTMLElement>("[data-inline-database-header]");
      const from = table.getBoundingClientRect();
      if (!header) { setTitleRow(null); setOver(view ? overToolbar(view, from.top) : 0); return; }
      const row = header.getBoundingClientRect();
      const title = header.querySelector("[data-inline-database-title]")?.getBoundingClientRect() ?? row;
      setOver(0);
      setTitleRow({ rise: from.top - (row.top + (row.height - BAR_HEIGHT) / 2), left: title.left - 8 - from.left });
    };
    measure();
    if (!view) return;
    const observer = new ResizeObserver(measure);
    observer.observe(view);
    return () => observer.disconnect();
  }, [selecting]);
  const buttonRef = useRef<HTMLButtonElement>(null), menuRef = useRef<HTMLDivElement>(null);
  const shown = open && count > 0;
  usePopoverAppear(shown, menuRef, () => buttonRef.current);
  const layer = useOverlayLayer(shown, { kind: "menu", ref: menuRef, onEscape: () => setOpen(false), onOutsidePress: () => setOpen(false),
    inside: () => [buttonRef.current], returnFocusRef: buttonRef });
  const menu = useNativeMenu<"duplicate" | "delete">({ items: ["duplicate", "delete"], open: shown, menuRef,
    label: t("moreSelectedRowActions"), keyOf: action => action, disabledKeys: [],
    textOf: action => t(action === "duplicate" ? "duplicate" : "moveSelectedToTrash"),
    onAction: action => { setOpen(false); if (action === "duplicate") onDuplicate(); else onDelete(); } });
  if (count < 1 && !warm) return null;
  const ahead = count < 1;
  return (
    <div ref={anchorRef} className={styles.selectionAnchor}
      style={titleRow ? { "--selection-bar-rise": `${titleRow.rise}px`, "--selection-bar-left": `${titleRow.left}px` } as CSSProperties
        : over > 0 ? { "--selection-bar-over": `${over}px` } as CSSProperties : undefined}
      data-title-row={titleRow ? "true" : undefined} data-selection-ahead={ahead ? "true" : undefined} aria-hidden={ahead || undefined} inert={ahead}>
      <div className={styles.selectionBar} data-table-selection-bar={ahead ? undefined : true} role={ahead ? undefined : "toolbar"}
        aria-label={ahead ? undefined : t("selectedRowActions")}>
        <button className={styles.selectionCount} type="button" aria-label={t("selectedCountClear", { count })} onClick={onClear}>
          {t("selectedCount", { count })}
        </button>
        {properties}
        {pageless ? null : <button className={styles.selectionIconAction} type="button" aria-label={t("moveSelectedToTrash")} aria-keyshortcuts="Delete"
          onClick={onDelete}>
          <Trash size="regular" aria-hidden="true" />
        </button>}
        <div className={styles.selectionMoreWrap}>
          <button ref={buttonRef} className={styles.selectionMore} type="button" data-table-selection-more={ahead ? undefined : true} aria-label={t("moreSelectedRowActions")}
            aria-haspopup="menu" aria-expanded={open}
            onClick={(event) => {
              const rect = event.currentTarget.getBoundingClientRect();
              // A key press has no point; its menu opens under the button.
              const point = event.clientX || event.clientY ? pressedPoint(event) : { x: inlineStartX(rect, 0, event.currentTarget), y: rect.bottom };
              if (open || !onRowMenu?.(event.currentTarget, point)) setOpen((value) => !value);
            }}>
            <DotsHorizontal size="regular" aria-hidden="true" />
          </button>
          {open && (
            <div {...menu.menuProps} ref={menuRef} className={styles.selectionMenu} style={{ zIndex: layer.zIndex }}
              onKeyDown={event => { menu.menuProps.onKeyDown(event); layer.onKeyDown(event); }}>
              <NativeMenuItem state={menu.state} itemKey="duplicate" className={styles.selectionMenuItem}>
                {({ labelProps }) => <><Copy size="regular" aria-hidden="true" /> <span {...labelProps}>{t("duplicate")}</span>
                  <kbd className={styles.selectionMenuShortcut}>{menuKeyHint("⌘D")}</kbd></>}
              </NativeMenuItem>
              <NativeMenuItem state={menu.state} itemKey="delete" className={styles.selectionMenuItem}>
                {({ labelProps }) => <><Trash size="regular" aria-hidden="true" /> <span {...labelProps}>{t("moveSelectedToTrash")}</span>
                  <kbd className={styles.selectionMenuShortcut}>Del</kbd></>}
              </NativeMenuItem>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
