import { SwitchInput } from "@/components/Switch";
import { type KeyboardEvent, useLayoutEffect, useRef, useState } from "react";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { isTextField } from "@/lib/menuKeyboard";
import { useTranslation } from "react-i18next";
import { placeBeside, type PopoverRect, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { CheckIcon, ChevronRight } from "../icons";
import { tableCalculationGroups, type TableCalculation, type TableCalculationGroup } from "./tableCalculationModel";
import styles from "./TableCalculationMenu.module.css";
import { inlineEndKey, inlineStartKey } from "@/lib/readingDirection";
import { besideRowOffset } from "@/lib/besideRowOffset";
import { noteBesideList } from "@/lib/submenuHoverHold";
import { submenuRowMarks, useSubmenuHover } from "../PageMenu";

// A group's calculations, 250 wide, 4px over the menu they open from and
// centred on their row where the window allows (besideRowOffset), as
// Notion's (seen 2026-11-16 by w1383 at 1440: "수 ›" beside a 200-wide menu
// at 736–936, its row 493–521, the group 932–1182 from 390.3, 233.4 high).
const GROUP_WIDTH = 250;

/**
 * A table column's calculate menu, as Notion's (seen 2026-09-30): "계산
 * 안함", then "수", "비율(%)" and, by the column's type, "추가 옵션" or
 * "날짜", each opening its calculations beside the menu — on the left where
 * the window ends. The counts come with the switch that shows large counts
 * as 99+.
 */
export function TableCalculationMenu({
  label,
  type,
  calculation,
  limitLargeCounts,
  focusOnOpen = false,
  litOnOpen,
  onPick,
  onLimitLargeCounts,
}: {
  label: string;
  type: string;
  calculation?: TableCalculation;
  limitLargeCounts?: boolean;
  /** Opened from the keyboard, the menu takes the keys at once. */
  focusOnOpen?: boolean;
  /** Its current calculation lit as it opens from the pointer too (a column menu's side, TablePropertyMenu open). */
  litOnOpen?: boolean;
  onPick(calculation: TableCalculation | undefined): void;
  onLimitLargeCounts?(limit: boolean): void;
}) {
  const { t } = useTranslation("tableView");
  return (
    <CalculationMenu label={label} focusOnOpen={focusOnOpen} litOnOpen={litOnOpen} leading={[{ key: "none", label: t("calculations.none") }]}
      groups={tableCalculationGroups(type)} picked={calculation ?? "none"}
      largeCounts={onLimitLargeCounts ? { on: Boolean(limitLargeCounts), set: onLimitLargeCounts } : undefined}
      onPick={(key) => onPick(key === "none" ? undefined : key as TableCalculation)} />
  );
}

/**
 * Calculations as Notion's menus offer them: a few chosen on the spot, then
 * groups that open beside the menu. A table column's calculate menu leads
 * with "계산 안함"; a rollup's with "원본 표시" and "고유한 값 표시". A
 * calculation with choices of its own (a status rollup's "그룹별 개수" and
 * its groups) opens them beside the group in turn.
 */
export function CalculationMenu({
  label,
  leading,
  groups,
  picked,
  choices,
  pickedChoice,
  largeCounts,
  focusOnOpen = false,
  litOnOpen,
  litFirst = false,
  groupWidth = GROUP_WIDTH,
  onPick,
}: {
  label: string;
  leading: readonly { key: string; label: string }[];
  groups: readonly TableCalculationGroup[];
  picked: string;
  /** The choices a calculation opens, by its key; picking one picks its key. */
  choices?: Readonly<Record<string, readonly { key: string; label: string }[]>>;
  pickedChoice?: string;
  largeCounts?: { on: boolean; set(limit: boolean): void };
  focusOnOpen?: boolean;
  /** Its current calculation lit as it opens from the pointer too. */
  litOnOpen?: boolean;
  /** Its first row lit as it opens, wherever the current calculation is (a number chart's "계산"). */
  litFirst?: boolean;
  /** How wide a group's calculations are beside it. */
  groupWidth?: number;
  onPick(key: string): void;
}) {
  const { t } = useTranslation("tableView");
  const menuRef = useRef<HTMLDivElement>(null);
  const groupRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState<{ key: TableCalculationGroup["key"]; anchor: PopoverRect; keyboard: boolean } | null>(null);
  const openGroup = open ? groups.find((group) => group.key === open.key) : undefined;
  // Beside the menu, around its row, on the left where the window ends.
  const groupPlace = usePopoverPlacement(groupRef, open ? (size, viewport) => placeBeside(
    open.anchor, size, viewport, { gap: -4, offsetTop: besideRowOffset(open.anchor, size, viewport) },
  ) : null, [open], undefined, { kind: "inner" });
  type Row = { key: string; label: string; group?: TableCalculationGroup["key"] };
  const rows: Row[] = [...leading,
    ...groups.map((group) => ({ key: group.key, label: t(`calculationGroups.${group.key}`), group: group.key }))];
  const rowOf = (key: string) => menuRef.current?.querySelector<HTMLElement>(`[data-key="${CSS.escape(key)}"]`) ?? null;
  const pickedGroup = groups.find((group) => group.items.some((item) => item === picked))?.key;
  const root = useNativeMenu({ items: rows, open: true, menuRef, label, focusOnOpen, firstKey: litFirst ? rows[0]?.key : pickedGroup ?? picked,
    highlightOnOpen: litOnOpen || undefined,
    keyOf: (row) => row.key, textOf: (row) => row.label, onAction: (row) => {
      const item = rowOf(row.key);
      if (!row.group) onPick(row.key);
      else if (item) show(row.group, item, true);
    } });
  const groupMenu = useNativeMenu({ items: [...(openGroup?.items ?? [])], open: Boolean(openGroup && groupPlace), menuRef: groupRef,
    label: openGroup ? t(`calculationGroups.${openGroup.key}`) : "", focusOnOpen: Boolean(open?.keyboard),
    firstKey: picked, keyOf: (item) => item, textOf: (item) => t(`calculations.${item}`), onAction: (item) => {
      const row = groupRef.current?.querySelector<HTMLElement>(`[data-calculation="${CSS.escape(item)}"]`);
      if (!choices?.[item]) onPick(item);
      else if (row) showChoices(item, row, true);
    } });
  const choiceRef = useRef<HTMLDivElement>(null);
  const [openChoices, setOpenChoices] = useState<{ key: string; anchor: PopoverRect; keyboard: boolean } | null>(null);
  const choiceItems = openChoices ? choices?.[openChoices.key] ?? [] : [];
  const choicePlace = usePopoverPlacement(choiceRef, openChoices ? (size, viewport) => placeBeside(
    openChoices.anchor, size, viewport, { gap: -4, offsetTop: besideRowOffset(openChoices.anchor, size, viewport) },
  ) : null, [openChoices], undefined, { kind: "inner" });
  const choiceMenu = useNativeMenu({ items: [...choiceItems], open: Boolean(openChoices && choicePlace), menuRef: choiceRef,
    label: openChoices ? t(`calculations.${openChoices.key}`) : "", focusOnOpen: Boolean(openChoices?.keyboard),
    firstKey: pickedChoice, keyOf: (choice) => choice.key, textOf: (choice) => choice.label, onAction: (choice) => onPick(choice.key) });
  function showChoices(key: string, item: HTMLElement, keyboard = false) {
    const menu = popoverRectOf(groupRef.current);
    const row = popoverRectOf(item);
    if (!menu || !row) return;
    setOpenChoices({ key, keyboard, anchor: { top: row.top, bottom: row.bottom, left: menu.left, right: menu.right } });
  }
  function closeChoices() {
    const key = openChoices?.key;
    setOpenChoices(null);
    if (!key) return;
    groupMenu.state.selectionManager.setFocused(true); groupMenu.state.selectionManager.setFocusedKey(key);
    groupRef.current?.focus({ preventScroll: true });
  }
  function show(key: TableCalculationGroup["key"], item: HTMLElement, keyboard = false) {
    // The box the menu is drawn in (a column menu's side, a summary's or a rollup's popover), not its rows'.
    const menu = popoverRectOf(menuRef.current?.parentElement ?? menuRef.current);
    const row = popoverRectOf(item);
    if (!menu || !row) return;
    if (open?.key !== key) setOpenChoices(null);
    setOpen({ key, keyboard, anchor: { top: row.top, bottom: row.bottom, left: menu.left, right: menu.right } });
  }
  // Escape or ArrowLeft closes an open group first and gives its row the
  // keys back; the menu around takes the next Escape.
  function closeGroup() {
    const key = open?.key;
    setOpen(null);
    setOpenChoices(null);
    if (!key) return;
    root.state.selectionManager.setFocused(true); root.state.selectionManager.setFocusedKey(key);
    menuRef.current?.focus({ preventScroll: true });
  }
  // A group's calculations, and a calculation's choices, open beside their row by the one rule of every list beside a
  // row (PageMenu useSubmenuHover): once the pointer has rested on the row, the row it first lands on as it comes into
  // a list that came up with its light on another row opening nothing (arrivalHoldsLight; Notion's "계산 ›" "수 ›").
  const groupHover = useSubmenuHover(open?.key ?? null, (key, row) => show(key as TableCalculationGroup["key"], row),
    () => { setOpen(null); setOpenChoices(null); });
  const choiceHover = useSubmenuHover(openChoices?.key ?? null, (key, row) => showChoices(key, row), () => setOpenChoices(null));
  useLayoutEffect(() => {
    const row = open ? rowOf(open.key) : null, list = groupRef.current;
    return row && list ? noteBesideList(row, list) : undefined;
  }, [open?.key]);
  useLayoutEffect(() => {
    const row = openChoices ? groupRef.current?.querySelector(`[data-calculation="${CSS.escape(openChoices.key)}"]`) : null, list = choiceRef.current;
    return row && list ? noteBesideList(row, list) : undefined;
  }, [openChoices?.key]);
  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== "Escape" || !open) return;
    event.preventDefault();
    event.stopPropagation();
    closeGroup();
  }
  return (
    <div {...root.menuProps} ref={menuRef} className={styles.menu} data-table-calculation-menu
      onPointerMove={(event) => {
        // A move in a list beside a row holds the lists it is beside; its own rows open theirs.
        const target = event.target instanceof Node ? event.target : null;
        if (target && choiceRef.current?.contains(target)) { groupHover.hold(); choiceHover.hold(); return; }
        if (target && groupRef.current?.contains(target)) { groupHover.hold(); choiceHover.onPointerMove(event); return; }
        groupHover.onPointerMove(event);
      }}
      onKeyDown={(event) => {
        onKeyDown(event);
        // ArrowRight opens the highlighted group with the keys.
        const focused = root.state.selectionManager.focusedKey;
        const group = groups.find((candidate) => candidate.key === focused)?.key, item = group ? rowOf(group) : null;
        if (!event.defaultPrevented && event.key === inlineEndKey(event.currentTarget) && event.target === menuRef.current && group && item) {
          event.preventDefault(); show(group, item, true); return;
        }
        if (!event.defaultPrevented) root.menuProps.onKeyDown(event);
      }}>
      {rows.map((row) => row.group ? (
        <NativeMenuItem key={row.key} state={root.state} itemKey={row.key} className={styles.item} aria-haspopup="menu"
          aria-expanded={open?.key === row.group} data-calculation-group={row.group}
          data-active={open?.key === row.group ? "true" : undefined} {...submenuRowMarks(row.group)}>
          {({ labelProps }) => <>
            <span {...labelProps}>{row.label}</span>
            <ChevronRight size="small" aria-hidden="true" />
          </>}
        </NativeMenuItem>
      ) : (
        <NativeMenuItem key={row.key} state={root.state} itemKey={row.key} role="menuitemradio" className={styles.item}
          aria-checked={picked === row.key} data-calculation={row.key} {...submenuRowMarks(undefined)}>
          {({ labelProps }) => <>
            <span {...labelProps}>{row.label}</span>
            {picked === row.key && <CheckIcon size="small" aria-hidden="true" />}
          </>}
        </NativeMenuItem>
      ))}
      {open && openGroup && (
        <div {...groupMenu.menuProps} ref={groupRef} className={styles.group} data-calculation-items={openGroup.key}
          style={{ width: groupWidth, ...popoverPlacementStyle(groupPlace) }}
          onKeyDown={(event) => {
            if (openChoices) return;
            if ((event.key === "Escape" || event.key === inlineStartKey(event.currentTarget)) && !isTextField(event.target)) {
              event.preventDefault(); event.stopPropagation(); closeGroup(); return;
            }
            // ArrowRight opens the highlighted calculation's choices with the keys.
            const focused = String(groupMenu.state.selectionManager.focusedKey ?? "");
            const row = choices?.[focused] ? groupRef.current?.querySelector<HTMLElement>(`[data-calculation="${CSS.escape(focused)}"]`) : null;
            if (event.key === inlineEndKey(event.currentTarget) && row) {
              event.preventDefault(); event.stopPropagation(); showChoices(focused, row, true); return;
            }
            groupMenu.menuProps.onKeyDown(event);
          }}>
          {openGroup.key === "count" && largeCounts && (
            <label className={styles.switchRow}>
              <span>
                <span>{t("calculations.limitLargeCounts")}</span>
                <small>{t("calculations.limitLargeCountsHint")}</small>
              </span>
              <SwitchInput checked={largeCounts.on}
                aria-label={t("calculations.limitLargeCounts")}
                onChange={(event) => largeCounts.set(event.target.checked)} />
            </label>
          )}
          {openGroup.items.map((item) => choices?.[item] ? (
            <NativeMenuItem key={item} state={groupMenu.state} itemKey={item} className={styles.item} aria-haspopup="menu"
              aria-expanded={openChoices?.key === item} data-calculation={item} data-active={openChoices?.key === item ? "true" : undefined}
              {...submenuRowMarks(item)}>
              {({ labelProps }) => <>
                <span {...labelProps}>{t(`calculations.${item}`)}</span>
                <ChevronRight size="small" aria-hidden="true" />
              </>}
            </NativeMenuItem>
          ) : (
            <NativeMenuItem key={item} state={groupMenu.state} itemKey={item} role="menuitemradio" className={styles.item}
              aria-checked={picked === item} data-calculation={item} {...submenuRowMarks(undefined)}>
              {({ labelProps }) => <>
                <span {...labelProps}>{t(`calculations.${item}`)}</span>
                {picked === item && <CheckIcon size="small" aria-hidden="true" />}
              </>}
            </NativeMenuItem>
          ))}
        </div>
      )}
      {openChoices && choiceItems.length > 0 && (
        <div {...choiceMenu.menuProps} ref={choiceRef} className={styles.group} data-calculation-choices={openChoices.key}
          style={{ width: groupWidth, ...popoverPlacementStyle(choicePlace) }}
          onKeyDown={(event) => {
            if ((event.key === "Escape" || event.key === inlineStartKey(event.currentTarget)) && !isTextField(event.target)) {
              event.preventDefault(); event.stopPropagation(); closeChoices(); return;
            }
            choiceMenu.menuProps.onKeyDown(event);
          }}>
          {choiceItems.map((choice) => (
            <NativeMenuItem key={choice.key} state={choiceMenu.state} itemKey={choice.key} role="menuitemradio" className={styles.item}
              aria-checked={pickedChoice === choice.key} data-calculation-choice={choice.key}>
              {({ labelProps }) => <>
                <span {...labelProps}>{choice.label}</span>
                {pickedChoice === choice.key && <CheckIcon size="small" aria-hidden="true" />}
              </>}
            </NativeMenuItem>
          ))}
        </div>
      )}
    </div>
  );
}
