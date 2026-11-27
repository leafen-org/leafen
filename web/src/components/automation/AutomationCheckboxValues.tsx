import { useTranslation } from "react-i18next";
import { CheckIcon } from "@/icons/leafen";
import type { AutomationAction, EditPropertyAutomationAction } from "@/lib/types";
import { PhoneSheetRow } from "../shell/PhoneSheet";
import styles from "./AutomationEditProperty.module.css";

/**
 * A checkbox's value as Notion lists it (seen 2026-11-10, 2026-11-15):
 * "체크 표시되지 않음" and "체크 표시됨" in flat rows, the one set checked,
 * never a dropdown. On a desktop picking one closes the window; on a phone
 * the rows are the sheet's, as a select's options there, and it stays.
 */
export function AutomationCheckboxValues({ action, label, phone = false, onChange, onDone }: {
  action: EditPropertyAutomationAction; label: string; phone?: boolean;
  onChange(action: AutomationAction): void;
  /** Where the window closes once one is picked. */
  onDone?(): void;
}) {
  const { t } = useTranslation("databaseView");
  const checked = action.value.kind === "literal" && action.value.value === true;
  const pick = (state: boolean) => onChange({ ...action, value: { kind: "literal", value: state } });
  if (phone) {
    return <>{[false, true].map((state) => <PhoneSheetRow key={String(state)} more={false} label={t(state ? "checked" : "unchecked")}
      value={state === checked ? <CheckIcon size="regular" aria-hidden="true" /> : undefined}
      data={{ "data-automation-checkbox": String(state) }} onPress={() => pick(state)} />)}</>;
  }
  return <div role="listbox" aria-label={label} className={styles.optionList}>
    {[false, true].map((state) => (
      <button key={String(state)} type="button" role="option" aria-selected={state === checked} className={styles.optionRow}
        data-automation-checkbox={state} onClick={() => { pick(state); onDone?.(); }}>
        <span>{t(state ? "checked" : "unchecked")}</span>
        {state === checked ? <CheckIcon size="small" aria-hidden="true" /> : null}
      </button>
    ))}
  </div>;
}
