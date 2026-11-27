import { forwardRef, useImperativeHandle, useState, type KeyboardEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { DbProperty } from "@/lib/types";
import { chipStyle } from "./colors";
import { statusGroupLabelKey } from "./propertyStatusPolicy";
import { StatusGroupLabel } from "./StatusGroupLabel";
import { statusConditionChoices, type StatusConditionValue, type StatusTick } from "./automationStatusCondition";
import styles from "./AutomationStatusConditionList.module.css";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

/**
 * A status trigger's ticks in its desktop window, as Notion's (seen
 * 2026-11-11, 1975px dark): "모든 옵션" 4px in, a line 12px in, then each
 * group — its box 12px in, its mark and 14/500 name in its colour — and its
 * options 8px further in as dotted pills, 28px rows 1px apart. A press ticks
 * or unticks one, a group all its options, a half-ticked "모든 옵션" all;
 * ↑↓ light a row and Enter or Space presses it, "모든 옵션" lit first — keys
 * its window passes on (`keyDown`), wherever in it the focus is
 * (AutomationConditionValuePopover).
 */
export type StatusConditionKeys = { keyDown(event: KeyboardEvent): void };

export const AutomationStatusConditionList = forwardRef<StatusConditionKeys, {
  property: DbProperty;
  condition: StatusConditionValue;
  label: string;
  onChange(next: StatusConditionValue): void;
}>(function AutomationStatusConditionList({ property, condition, label, onChange }, ref) {
  const { t } = useTranslation(["databaseView", "propertyCell"]);
  const [lit, setLit] = useState(0);
  const choices = statusConditionChoices(property, condition);
  const rows: Array<{ key: string; tick: StatusTick; content: ReactNode; press(): StatusConditionValue; nested?: boolean;
    data: Record<string, string> }> = [];
  rows.push({ key: "all", tick: choices.all, content: <span className={styles.words}>{t("databaseView:automationTriggerAllOptions")}</span>,
    press: choices.toggleAll, data: { "data-automation-condition-all": "" } });
  for (const group of choices.groups) {
    const key = statusGroupLabelKey(group.name);
    if (group.id) rows.push({ key: `group:${group.id}`, tick: choices.group(group), press: () => choices.toggleGroup(group),
      content: <span className={styles.group}>
        <StatusGroupLabel stage={group.stage} color={group.color} label={key ? t(`propertyCell:${key}`) : group.name} />
      </span>, data: { "data-automation-condition-group": group.id } });
    for (const option of group.options) rows.push({ key: `option:${option.id}`, tick: choices.option(option.id) ? "on" : "off",
      press: () => choices.toggleOption(option.id), nested: Boolean(group.id), data: { "data-automation-condition-item": option.id },
      content: <span className={styles.pill} style={chipStyle(option.color)} data-status-pill="">{option.name}</span> });
  }
  const shown = Math.min(lit, rows.length - 1);
  const keyDown = (event: KeyboardEvent) => {
    if (event.defaultPrevented || imeComposing(event)) return;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      setLit((shown + (event.key === "ArrowDown" ? 1 : rows.length - 1)) % rows.length);
    } else if (fieldEnter(event) || event.key === " ") {
      event.preventDefault();
      if (rows[shown]) onChange(rows[shown].press());
    }
  };
  useImperativeHandle(ref, () => ({ keyDown }));
  const row = (index: number) => {
    const { key, tick, content, press, nested, data } = rows[index];
    return <div key={key} role="menuitemcheckbox" aria-checked={tick === "mixed" ? "mixed" : tick === "on"} className={styles.row}
      data-nested={nested || undefined} data-lit={index === shown || undefined} {...data}
      onPointerMove={() => { if (index !== shown) setLit(index); }} onPointerDown={(event) => event.preventDefault()}
      onClick={() => onChange(press())}>
      <span className={styles.boxCell} aria-hidden="true">
        <span className={styles.box} data-tick={tick}>
          {tick === "mixed" ? <span className={styles.dash} /> : null}
        </span>
      </span>
      {content}
    </div>;
  };
  return <div role="menu" aria-label={label} className={styles.list} data-automation-status-condition={property.id}>
    <div className={styles.section}>{row(0)}</div>
    <div className={styles.rule} role="separator" />
    <div className={styles.section}>
      {rows.slice(1).map((_, index) => row(index + 1))}
    </div>
  </div>;
});
