import { type ReactNode, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ChevronDown } from "@/icons/leafen";
import type { NotionRollupFunction } from "@/lib/types";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { PropertySettingButton } from "./PropertySettingRow";
import { rollupCalculationChoices } from "./rollupPolicy";
import { CalculationMenu } from "./TableCalculationMenu";
import type { TableCalculation, TableCalculationGroup } from "./tableCalculationModel";
import styles from "./TableCalculationMenu.module.css";
import { fieldEscape } from "@/lib/fieldKeyBoundary";

// A rollup's calculations go by the table calculate menu's names; its
// "count" is the table's "count_all".
const menuKey = (fn: NotionRollupFunction) => (fn === "count" ? "count_all" : fn);
const rollupFunction = (key: string) => (key === "count_all" ? "count" : key) as NotionRollupFunction;

// A calculation over a status group is its key and the group's.
const GROUP_SEPARATOR = "\u001f";

/**
 * A rollup's "계산" setting, as Notion's (seen 2026-10-10): the row names the
 * calculation, and below it opens Notion's menu — "원본 표시", "고유한 값
 * 표시" (not for a checkbox), then "수", "비율(%)" and, by the target's type,
 * "추가 옵션" or "날짜", each opening its calculations beside the menu. A
 * status's "그룹별 개수" and "그룹별 퍼센트" open its groups in turn (seen
 * 2026-11-12).
 */
export function RollupCalculationSelect({ label, ariaLabel, icon, value, targetType, statusGroups, group, unavailable, disabled = false, unset = false, plainClassName, onChange }: {
  label: string;
  /** Drawn as a plain choice of this class, its value and "⌄", as a cell's rollup form has it. */
  plainClassName?: string;
  ariaLabel: string;
  icon?: ReactNode;
  value: NotionRollupFunction;
  targetType: string | undefined;
  /** A status target's groups, which its counts and shares per group take. */
  statusGroups?: readonly { id: string; label: string }[];
  /** The status group the calculation counts. */
  group?: string;
  /** The calculation is not one the target's type offers. */
  unavailable: boolean;
  /** Until the rollup's relation is chosen, as Notion's dims it. */
  disabled?: boolean;
  /** No relation is chosen yet: a setting row shows no calculation, as Notion's "계산" row (seen 2026-11-19). */
  unset?: boolean;
  onChange(fn: NotionRollupFunction, group?: string): void;
}) {
  const { t } = useTranslation(["propertyTypeConfig", "tableView", "propertyCell"]);
  const [open, setOpen] = useState(false);
  const buttonRef = useRef<HTMLButtonElement>(null), menuRef = useRef<HTMLDivElement>(null), id = useId();
  const calculations = rollupCalculationChoices(targetType);
  const nameOf = (fn: NotionRollupFunction) => fn === "show_original" || fn === "show_unique"
    ? t(`propertyTypeConfig:rollupFunctions.${fn}`) : t(`tableView:calculations.${menuKey(fn)}`);
  // Escape or a pick gives the keys back to its row, ringed, as Notion's
  // (2026-11-15, Chrome, no script before the action; judged by the screen
  // and one typed letter: 속성 편집 › 계산 pressed, Escape — the 계산 row
  // ringed, the letter landing nowhere).
  const close = (restore = false) => {
    if (restore) buttonRef.current?.focus({ preventScroll: true });
    setOpen(false);
  };
  // Under the row at its end, kept under and moved up at the window's foot,
  // as Notion's rollup "계산" list opens (seen 2026-11-02).
  const place = usePopoverPlacement(menuRef, open ? (size, viewport) => placeAround(popoverRectOf(buttonRef.current)!, size, viewport,
    { gap: 0, align: "end", keepSide: true }) : null, [open], undefined,
    { kind: "menu", restoreFocus: false, onEscape: () => close(true), onOutsidePress: () => close(), inside: () => [buttonRef.current] });
  const groups: TableCalculationGroup[] = calculations.groups.map((group) => ({
    key: group.key, items: group.items.map((fn) => menuKey(fn) as TableCalculation),
  }));
  const shown = unavailable ? t("propertyCell:unavailable") : nameOf(value);
  const perGroup = targetType === "status" && statusGroups?.length ? ["count_per_group", "percent_per_group"] : [];
  const choices = Object.fromEntries(perGroup.map((fn) => [fn, (statusGroups ?? []).map((item) => ({
    key: `${fn}${GROUP_SEPARATOR}${item.id}`, label: item.label,
  }))]));
  return <>
    {plainClassName ? <button ref={buttonRef} type="button" className={plainClassName} aria-label={ariaLabel} aria-haspopup="menu"
      aria-expanded={open} aria-controls={open ? id : undefined} disabled={disabled} onClick={() => setOpen(!open)}>
      <span>{shown}</span><ChevronDown size="tiny" aria-hidden="true" />
    </button> : <PropertySettingButton label={label} icon={icon} value={unset ? undefined : shown}
      buttonRef={buttonRef} aria-label={ariaLabel} aria-haspopup="menu" aria-expanded={open} aria-controls={open ? id : undefined}
      disabled={disabled} onClick={() => setOpen(!open)} />}
    {open && createPortal(<div ref={menuRef} id={id} className={styles.popover} style={{ position: "fixed", ...popoverPlacementStyle(place) }}
      onKeyDown={(event) => {
        if (fieldEscape(event)) { event.preventDefault(); event.stopPropagation(); close(true); }
      }}>
      <CalculationMenu label={ariaLabel} focusOnOpen leading={calculations.leading.map((fn) => ({ key: fn, label: nameOf(fn) }))}
        groups={groups} picked={unavailable ? "" : menuKey(value)} choices={choices}
        pickedChoice={group ? `${value}${GROUP_SEPARATOR}${group}` : undefined}
        onPick={(key) => {
          const [fn, picked] = key.split(GROUP_SEPARATOR);
          onChange(rollupFunction(fn), picked);
          close(true);
        }} />
    </div>, document.body)}
  </>;
}
