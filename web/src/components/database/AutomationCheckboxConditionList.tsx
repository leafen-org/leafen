import { forwardRef, useImperativeHandle, useState, type KeyboardEvent } from "react";
import { useTranslation } from "react-i18next";
import { CheckIcon } from "@/icons/leafen";
import type { DbProperty } from "@/lib/types";
import type { StatusConditionKeys } from "./AutomationStatusConditionList";
import styles from "./AutomationStatusConditionList.module.css";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

/**
 * A checkbox trigger's two states in its desktop window, as Notion's menu
 * (seen 2026-11-18 in Chrome at 1440 dark, from "+ 새 조건" and from the
 * card): under "Done 속성 설정 값:" a line 12px in, then "체크 표시 해제됨"
 * and "체크 표시됨", 28px rows 1px apart 4px in, the words 8px in at 14px
 * and a 16px ✓ 8px from the end of each one ticked, both at first. A press
 * ticks or unticks one; ↑↓ light a row and Enter or Space presses it, the
 * first lit as it opens — keys its window passes on (`keyDown`), as a
 * status's (AutomationStatusConditionList).
 */
export const AutomationCheckboxConditionList = forwardRef<StatusConditionKeys, {
  property: DbProperty;
  label: string;
  /** The states ticked: both while any edit is the condition. */
  states: ReadonlyArray<string | boolean>;
  onToggle(state: boolean): void;
}>(function AutomationCheckboxConditionList({ property, label, states, onToggle }, ref) {
  const { t } = useTranslation("databaseView");
  const [lit, setLit] = useState(0);
  const rows = [false, true] as const;
  const keyDown = (event: KeyboardEvent) => {
    if (event.defaultPrevented || imeComposing(event)) return;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      setLit((lit + 1) % rows.length);
    } else if (fieldEnter(event) || event.key === " ") {
      event.preventDefault();
      onToggle(rows[lit]);
    }
  };
  useImperativeHandle(ref, () => ({ keyDown }));
  return <div role="menu" aria-label={label} className={styles.list} data-automation-checkbox-condition={property.id}>
    <div className={styles.rule} role="separator" />
    <div className={styles.section}>
      {rows.map((state, index) => (
        <div key={String(state)} role="menuitemcheckbox" aria-checked={states.includes(state)} className={styles.row}
          data-lit={index === lit || undefined} data-automation-condition-state={String(state)}
          onPointerMove={() => { if (index !== lit) setLit(index); }} onPointerDown={(event) => event.preventDefault()}
          onClick={() => onToggle(state)}>
          <span className={styles.words}>{t(state ? "automationTriggerCheckbox.checked" : "automationTriggerCheckbox.unchecked")}</span>
          {states.includes(state) ? <CheckIcon size="small" aria-hidden="true" /> : null}
        </div>
      ))}
    </div>
  </div>;
});
