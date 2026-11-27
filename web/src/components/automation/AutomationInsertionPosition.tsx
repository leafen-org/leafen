import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { interactiveAutomationBlockInsertionPositions } from "@/interactiveAutomationWire.generated";
import type { InsertBlocksAutomationAction } from "@/lib/types";
import { AutomationCardMenu } from "./AutomationActionMenus";
import styles from "./automationActionEditor.module.css";

/** Position is part of the insertion card's heading, as in the native setup. */
export function AutomationInsertionPosition({ action, number, onChange }: {
 action: InsertBlocksAutomationAction; number: number; onChange(action: InsertBlocksAutomationAction): void;
}) {
 const { t } = useTranslation("databaseView");
 const trigger = useRef<HTMLButtonElement>(null);
 const [open,setOpen] = useState(false);
 return <span className={styles.positionWrap}>
  <button ref={trigger} type="button" className={styles.positionButton}
   aria-label={t("insertPositionForAction",{number})} aria-haspopup="menu" aria-expanded={open}
   onClick={() => setOpen(value => !value)}>
   {t(`insertPosition.${action.placement ?? "button_below"}`)}<span aria-hidden="true">⌄</span>
  </button>
  {open ? <AutomationCardMenu trigger={trigger} onClose={() => setOpen(false)}
   choices={interactiveAutomationBlockInsertionPositions.map(value => ({key:value,label:t(`insertPosition.${value}`),run:()=>onChange({...action,placement:value})}))} /> : null}
 </span>;
}
