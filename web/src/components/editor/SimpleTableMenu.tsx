import { type ReactNode, type RefObject, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ArrowDown, ArrowLeft, ArrowRight, ArrowUp, ChevronRight, Copy, PaintRollerIcon, TableHeaderColumnIcon, TableHeaderRowIcon, TableMergeCellsIcon,
  TableUnmergeCellsIcon, Trash, XCircleIcon, XCircleOutlineIcon } from "@/icons/leafen";
import { SwitchMark } from "@/components/Switch";
import { detectKeyboardShortcutPlatform, shortcutCompactLabel, shortcutDisplayKeys } from "@/lib/keyboardShortcuts";
import { NativeActionMenu, NativeActionMenuItem, NativeMenuHeader, NativeMenuSection } from "@/lib/nativeActionMenu";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { placeAround, placeAtPoint, placeBeside, popoverPlacementStyle, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import { inlineEndKey, inlineStartKey, readsRightToLeft } from "@/lib/readingDirection";
import rowMenuStyles from "@/components/RowMenu.module.css";
import { blockItemText } from "./blockItemLabels";
import { BLOCK_HANDLE_BACKGROUND_COLORS, BLOCK_HANDLE_COLORS } from "./blockHandleModel";
import styles from "./editorStyles.generated.module.css";

/** A row's or a column's grip menu, or the menu of the cells a cell's grip holds. */
export type SimpleTableMenuKind = "row" | "column" | "cells";

// A Mac's keys as one mark ("⌘D"), elsewhere joined ("Ctrl+D"), as the block menu's.
function duplicateKeys() {
  const platform = detectKeyboardShortcutPlatform();
  return platform === "mac" ? shortcutCompactLabel("duplicateBlock", platform) : shortcutDisplayKeys("duplicateBlock", platform)[0]?.join("+");
}

/**
 * A simple table's menus, as Notion's (seen 2026-11-09 on a synthetic 3×3
 * table): a row's grip opens, beside it, a search field, "색 ›", insert
 * above and below, duplicate (⌘D), clear contents and delete, then the
 * row's last edit; a column's opens under it the same with left and right
 * and no last edit; a cell's grip opens at the pointer "색 ›", "셀 병합"
 * for a range, "셀 병합 해제" where it holds merged cells, and clear
 * contents (seen 2026-11-10). "색" opens its colors beside the menu,
 * centred on its row — text colors, then backgrounds. The first row's menu
 * leads with "제목 행" and the first column's with "제목 열", switches that
 * keep the menu open (Notion's, seen 2026-11-09).
 */
export function SimpleTableMenu({ kind, anchor, color, canDelete, canAdd, merge, footer, header, onAction, onClose }: {
  kind: SimpleTableMenuKind;
  /** The grip pressed, or for cells the point pressed. */
  anchor: PopoverRect;
  /** The row's, the column's or the first cell's color now; "" for none. */
  color: string;
  canDelete: boolean;
  canAdd: boolean;
  /** For cells: whether they can be merged, and whether they hold merged cells to unmerge. */
  merge?: { merge: boolean; unmerge: boolean };
  footer?: ReactNode;
  /** The first row's or column's header switch: on now, its label, and its turn. */
  header?: { on: boolean; label: string; onToggle(): void };
  onAction: (action: string) => void;
  /** Closed by Escape (what it was for stays chosen) or by a press elsewhere. */
  onClose: (byEscape: boolean) => void;
}) {
  const menuRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const colorRowRef = useRef<HTMLDivElement | HTMLAnchorElement | null>(null);
  const [query, setQuery] = useState("");
  const [colors, setColors] = useState<"pointer" | "keys" | null>(null);
  // Beside a row's grip, its top 3 under the grip's; under a column's, 2 in;
  // at the pointer for cells (Notion, 2026-11-09).
  const place = usePopoverPlacement(menuRef, (size, viewport) => kind === "row"
    ? placeBeside(anchor, size, viewport, { gap: 2, offsetTop: 3 })
    : kind === "column" ? placeAround({ ...anchor, left: anchor.left + 2, right: anchor.right + 2 }, size, viewport, { gap: 3 })
      : placeAtPoint({ x: anchor.left, y: anchor.top + 1 }, size, viewport), [anchor, kind], undefined, false);
  const { zIndex } = useOverlayLayer(true, { kind: "menu", ref: menuRef, onEscape: () => onClose(true), onOutsidePress: () => onClose(false) });
  // Its search takes the keys. Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440: a row's grip
  // pressed, "z" typed went into the menu's search (Leafen the same).
  useEffect(() => { searchRef.current?.focus({ preventScroll: true }); }, []);
  const line = kind !== "cells", rtl = readsRightToLeft();
  const row = kind === "row";
  const entries = [
    ...(header ? [{ key: "header", icon: row ? <TableHeaderRowIcon size="regular" aria-hidden="true" /> : <TableHeaderColumnIcon size="regular" aria-hidden="true" />,
      label: header.label, disabled: false }] : []),
    { key: "colors", icon: <PaintRollerIcon size="regular" aria-hidden="true" />, label: blockItemText("simpleTable.color"), disabled: false },
    // A column before this one stands on its right where the table reads
    // from the right: the labels name the screen's side, the arrows turn over.
    ...(line ? [
      { key: "before", icon: row ? <ArrowUp size="regular" aria-hidden="true" /> : <ArrowLeft size="regular" aria-hidden="true" />,
        label: blockItemText(row ? "simpleTable.insertAbove" : rtl ? "simpleTable.insertRight" : "simpleTable.insertLeft"), disabled: !canAdd },
      { key: "after", icon: row ? <ArrowDown size="regular" aria-hidden="true" /> : <ArrowRight size="regular" aria-hidden="true" />,
        label: blockItemText(row ? "simpleTable.insertBelow" : rtl ? "simpleTable.insertLeft" : "simpleTable.insertRight"), disabled: !canAdd },
      { key: "duplicate", icon: <Copy size="regular" aria-hidden="true" />, label: blockItemText("simpleTable.duplicate"), disabled: !canAdd, hint: duplicateKeys() },
    ] : []),
    ...(merge?.merge ? [{ key: "merge", icon: <TableMergeCellsIcon size="regular" aria-hidden="true" />, label: blockItemText("simpleTable.mergeCells"), disabled: false }] : []),
    ...(merge?.unmerge ? [{ key: "unmerge", icon: <TableUnmergeCellsIcon size="regular" aria-hidden="true" />,
      label: blockItemText("simpleTable.unmergeCells"), disabled: false }] : []),
    { key: "clear", icon: line ? <XCircleIcon size="regular" aria-hidden="true" /> : <XCircleOutlineIcon size="regular" aria-hidden="true" />,
      label: blockItemText("simpleTable.clearContents"), disabled: false },
    ...(line ? [{ key: "delete", icon: <Trash size="regular" aria-hidden="true" />, label: blockItemText("simpleTable.delete"), disabled: !canDelete }] : []),
  ];
  const needle = query.trim().toLocaleLowerCase();
  const shown = entries.filter((entry) => !needle || entry.label.toLocaleLowerCase().includes(needle));
  const label = blockItemText(kind === "row" ? "simpleTable.rowMenu" : kind === "column" ? "simpleTable.columnMenu" : "simpleTable.cellMenu");
  const focusedAction = () => menuRef.current?.querySelector<HTMLElement>('[data-focused="true"]')?.dataset.tableLineAction;
  return createPortal(<>
    <div ref={menuRef} className={`${rowMenuStyles.menu} ${rowMenuStyles.contextMenu}`} tabIndex={-1}
      // 265 wide, as Notion's table menus (seen 2026-11-09).
      style={{ ...popoverPlacementStyle(place), zIndex, width: "min(265px, calc(100vw - 2 * var(--popover-edge)))" }}
      data-row-menu-variant="page" data-table-line-menu={kind}
      onKeyDownCapture={(event) => {
        // → (← in a right-to-left page) on "색" opens its colors, as a submenu row's.
        if (event.key !== inlineEndKey(event.currentTarget) || focusedAction() !== "colors") return;
        event.preventDefault();
        event.stopPropagation();
        setColors("keys");
      }}>
      {line ? <label className={rowMenuStyles.actionSearch}>
        <input ref={searchRef} value={query} placeholder={blockItemText("simpleTable.searchActions")} aria-label={blockItemText("simpleTable.searchActions")}
          onChange={(event) => setQuery(event.target.value)}
          onKeyDown={(event) => {
            if (event.key !== "ArrowDown") return;
            event.preventDefault();
            menuRef.current?.querySelector<HTMLElement>("[data-native-menu-item]:not([aria-disabled='true'])")?.focus();
          }} />
      </label> : null}
      <NativeActionMenu aria-label={label}>
        {shown.map((entry) => (
          <NativeActionMenuItem key={entry.key} type="button" className={rowMenuStyles.item} data-menu-item
            role={entry.key === "header" ? "menuitemcheckbox" : "menuitem"} aria-checked={entry.key === "header" ? header?.on === true : undefined}
            data-table-line-action={entry.key} disabled={entry.disabled} aria-haspopup={entry.key === "colors" ? "menu" : undefined}
            aria-expanded={entry.key === "colors" ? colors !== null : undefined}
            actionRef={entry.key === "colors" ? colorRowRef : undefined}
            onHoverStart={() => setColors((open) => (entry.key === "colors" ? open ?? "pointer" : null))}
            onClick={() => (entry.key === "colors" ? setColors("keys") : entry.key === "header" ? header?.onToggle() : onAction(entry.key))}>
            {entry.icon}
            <span>{entry.label}</span>
            {"hint" in entry && entry.hint ? <span className={rowMenuStyles.itemHint} aria-hidden="true">{entry.hint}</span> : null}
            {entry.key === "colors" ? <ChevronRight className={rowMenuStyles.itemHint} size="small" aria-hidden="true" /> : null}
            {entry.key === "header" ? <SwitchMark on={header?.on === true} className={rowMenuStyles.menuSwitch} /> : null}
          </NativeActionMenuItem>
        ))}
      </NativeActionMenu>
      {/* A search that finds nothing leaves the search alone, its foot gone too, as Notion's (seen 2026-11-15 in two tabs). */}
      {footer && (!needle || shown.length > 0) ? <><div className={rowMenuStyles.divider} role="separator" />{footer}</> : null}
    </div>
    {colors ? <SimpleTableColorMenu main={menuRef} row={colorRowRef} color={color} focusOnOpen={colors === "keys"}
      returnTo={() => searchRef.current ?? menuRef.current}
      onColor={(value) => onAction(`color:${value}`)} onBack={() => setColors(null)} onClose={() => onClose(false)} /> : null}
  </>, document.body);
}

/**
 * "색"'s colors beside the menu, 4px over its edge, centred on the row and
 * at most 70% of the window tall, as Notion's table and block menus open
 * theirs (seen 2026-11-09: 220 wide, "텍스트 색상" — "기본 텍스트" and
 * nine more — then "배경 색상" — "기본 배경" and nine more). Escape or ←
 * goes back to the menu.
 */
function SimpleTableColorMenu({ main, row, color, focusOnOpen, returnTo, onColor, onBack, onClose }: {
  main: RefObject<HTMLDivElement | null>;
  row: RefObject<HTMLDivElement | HTMLAnchorElement | null>;
  color: string;
  focusOnOpen: boolean;
  returnTo: () => HTMLElement | null;
  onColor: (color: string) => void;
  onBack: () => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("blockHandle");
  const ref = useRef<HTMLDivElement>(null);
  const opener = useRef<HTMLElement | null>(null);
  opener.current = returnTo();
  const place = usePopoverPlacement(ref, (_size, viewport) => {
    const box = main.current?.getBoundingClientRect(), at = row.current?.getBoundingClientRect();
    return { anchor: { top: at?.top ?? 0, bottom: at?.bottom ?? 0, left: box?.left ?? 0, right: box?.right ?? 0 },
      placement: "right", gap: -4, options: { cap: Math.round(viewport.height * 0.7) } };
  }, [], undefined, { kind: "menu", inside: () => [main.current], onEscape: onBack, onOutsidePress: onClose,
    returnFocusRef: opener as RefObject<HTMLElement | null> });
  // Opened by the pointer, the menu keeps the keys until they come here.
  useEffect(() => {
    if (!focusOnOpen) opener.current?.focus({ preventScroll: true });
  }, [focusOnOpen]);
  const colorName = (choice: string) => t(`colors.${choice}`, { defaultValue: t("colors.default") });
  const groups = [
    { label: t("colorMenu.textColor"), choices: BLOCK_HANDLE_COLORS.map((choice) => ({ choice, value: choice === "default" ? "" : choice,
      name: t("menu.textColorNamed", { color: colorName(choice) }) })) },
    { label: t("colorMenu.backgroundColor"), choices: (["default_background", ...BLOCK_HANDLE_BACKGROUND_COLORS] as const).map((choice) => ({ choice,
      value: choice === "default_background" ? "" : choice, name: choice === "default_background" ? t("colorMenu.defaultBackground") : colorName(choice) })) },
  ];
  return (
    <div ref={ref} className={`${styles.blockMenu} ${styles.blockSubmenu}`} data-table-color-menu=""
      style={place ? popoverPlacementStyle(place) : { visibility: "hidden" }}
      onKeyDownCapture={(event) => {
        if (event.key !== inlineStartKey(event.currentTarget)) return;
        event.preventDefault();
        event.stopPropagation();
        onBack();
      }}>
      <NativeActionMenu aria-label={blockItemText("simpleTable.color")}>
        {groups.map((group, index) => (
          <NativeMenuSection key={group.label} aria-label={group.label}>
            <NativeMenuHeader className={styles.menuLabel}>{group.label}</NativeMenuHeader>
            {group.choices.map(({ choice, value, name }) => (
              <NativeActionMenuItem key={choice} type="button" className={styles.colorItem} role="menuitemradio"
                aria-checked={color === value && (value !== "" || index === 0)} data-table-line-color={choice}
                onClick={() => onColor(value)}>
                <span className={styles.colorSwatch} data-color={choice} aria-hidden="true"
                  style={choice.endsWith("_background") ? { color: `var(--c-${choice.replace(/_background$/u, "")})` } : undefined}>
                  {index === 0 ? "A" : null}</span>
                <span>{name}</span>
              </NativeActionMenuItem>
            ))}
          </NativeMenuSection>
        ))}
      </NativeActionMenu>
    </div>
  );
}
