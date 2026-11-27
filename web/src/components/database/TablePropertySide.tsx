import { useLayoutEffect, useRef, type HTMLAttributes, type PointerEvent, type RefObject } from "react";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { noteBesideList, trackPointer } from "@/lib/submenuHoverHold";
import { useBesideListLeaving } from "@/lib/besideListLeaving";
import { useSubmenuHover } from "../PageMenu";
import styles from "./databaseSheet";

/**
 * The pointer resting on a column menu's "정렬 ›", "계산 ›", "유형 변경 ›" or
 * "속성 편집 ›" opens its list SUBMENU_SETTLE_MS after it came onto the row,
 * as a press does at once; resting that long on another row with a list opens
 * that one in its place, and on a row without one closes it, the focus the
 * list held going to the menu itself and the pointer's row lit; a row passed
 * over sooner, or one crossed on the way to the list, does neither (the one
 * rule of every list beside a row, PageMenu useSubmenuHover). Notion's
 * (measured 2026-11-27 in Chrome at 1440, dpr 1, on a synthetic page in two
 * tabs at different times, judged by the screen and one typed letter after
 * the action, with no script in the page before it: a text column's header
 * pressed, the pointer resting on "정렬" drew its list 125 ms after the
 * pointer came, "A → Z로 정렬" shaded, the focus in the list and "z" in no
 * field — the name kept "메모"; on "계산" 128 ms and on "유형 변경" 126 ms the
 * same, "계산 안함" and "텍스트" shaded; a select column's "속성 편집" opened
 * its settings the same way; moved on onto "그룹화", the list went and
 * "그룹화" lit, the focus on the menu itself and "q" in no field; moved on
 * onto "계산", its list came 126–128 ms after and the one left went some
 * 190 ms after that). A status column's "다음과 같이 표시: ›" opens only on
 * a press (the pointer resting there opened nothing, seen once), and a
 * button's "자동화 편집" opens its window. The menu drawn up over its header
 * where the window ends, a › row coming under the still pointer of the press
 * that opened it opens its list the same way (`onMenuPointerOver`).
 */
export function useTablePropertySideHover<Kind extends string>({ open, menuRef, sideRef, onOpen, onClose, light }: {
  open: Kind | null;
  menuRef: RefObject<HTMLElement | null>;
  sideRef: RefObject<HTMLElement | null>;
  onOpen(kind: Kind, row: HTMLElement): void;
  onClose(): void;
  /** Lights the menu's row by its key. */
  light(key: string): void;
}) {
  // The row whose list the pointer opens takes the menu's light, as the one it closes the list on does, whichever led
  // before (Aria lights a row the pointer comes onto only while the pointer leads).
  const hover = useSubmenuHover(open, (kind, row) => { onOpen(kind as Kind, row); if (row.dataset.key) light(row.dataset.key); }, () => {
    const menu = menuRef.current, row = menu?.querySelector<HTMLElement>("[data-native-menu-item]:hover");
    if (sideRef.current?.contains(document.activeElement)) menu?.focus({ preventScroll: true });
    onClose();
    if (row?.dataset.key) light(row.dataset.key);
  });
  // Noted by its row, for the pointer on its way to it across the menu's other rows (pointerHeadsFor).
  useLayoutEffect(() => {
    const list = sideRef.current;
    const row = open ? menuRef.current?.querySelector(`[data-page-submenu-row="${CSS.escape(open)}"]`) : null;
    if (!list || !row) return undefined;
    trackPointer();
    return noteBesideList(row, list);
  }, [open, menuRef, sideRef]);
  return { onMenuPointerMove: hover.onPointerMove, onMenuPointerOver: hover.onPointerOver, onSidePointerMove: (event: PointerEvent<HTMLElement>) => { if (event.pointerType !== "touch") hover.hold(); } };
}

/**
 * A column menu's list beside it, drawn anew for each row's list (keyed by
 * it): another row's list taking its place, or Escape or ← closing it with
 * the menu staying, the one left fades out (besideListLeaving.ts), as
 * Notion's; gone with the menu, it goes at once.
 */
export function TablePropertySide({ sideRef, row, children, ...attributes }: {
  sideRef: RefObject<HTMLDivElement | null>;
  /** The menu's row the list is beside. */
  row(): Element | null;
} & HTMLAttributes<HTMLDivElement>) {
  useBesideListLeaving(true, () => sideRef.current, row);
  return <div ref={sideRef} tabIndex={-1} {...attributes}>{children}</div>;
}

/** "정렬" beside the column menu: its two directions in words alone, cut at the end where long, as Notion's (seen 2026-11-15 in Chrome and the desktop app, 2026-11-16 in Chrome). */
export function TableSortSideMenu({ label, focusOnOpen, ascending, descending, onSort }: {
  label: string; focusOnOpen: boolean; ascending: string; descending: string; onSort(direction: "asc" | "desc"): void;
}) {
  const menuRef = useRef<HTMLDivElement>(null);
  const directions = [{ key: "asc" as const, label: ascending }, { key: "desc" as const, label: descending }];
  const menu = useNativeMenu({ items: directions, open: true, menuRef, label, focusOnOpen, highlightOnOpen: true,
    keyOf: (direction) => direction.key, textOf: (direction) => direction.label, onAction: (direction) => onSort(direction.key) });
  return (
    <div {...menu.menuProps} ref={menuRef}>
      {directions.map((direction) => (
        <NativeMenuItem key={direction.key} state={menu.state} itemKey={direction.key} className={styles.propertyHeaderItem}>
          {({ labelProps }) => <span {...labelProps} className={styles.propertyHeaderItemText}>{direction.label}</span>}
        </NativeMenuItem>
      ))}
    </div>
  );
}
