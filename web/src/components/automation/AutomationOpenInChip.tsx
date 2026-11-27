import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { CenterPeekIcon, ChevronDown, FullPagePeekIcon, SidePeekIcon } from "@/icons/leafen";
import type { InteractiveAutomationOpenIn } from "@/interactiveAutomationWire.generated";
import { AutomationChoicePopover } from "./AutomationChoicePopover";
import styles from "./automationActionEditor.module.css";
import chipStyles from "./automationHeadChip.module.css";

const MODES = [["side_peek", "side", SidePeekIcon], ["center_peek", "center", CenterPeekIcon], ["full_page", "full", FullPagePeekIcon]] as const;

/**
 * Where an opened page or form shows, the "[중앙에서 보기 ⌄] 에서" after
 * Notion's open action names it (seen 2026-11-02, a form's 2026-11-05):
 * beside the page, over it — the default — or in its place, each with its
 * mark and the words a view's page choice has.
 */
export function AutomationOpenInChip({ value, number, onChange }: {
  value?: InteractiveAutomationOpenIn; number: number; onChange(next: InteractiveAutomationOpenIn): void;
}) {
  const { t } = useTranslation("databaseView");
  const [open, setOpen] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const current = value ?? "center_peek";
  const mode = MODES.find(([key]) => key === current)?.[1] ?? "center";
  return <>
    <button ref={trigger} type="button" className={`${styles.targetButton} ${chipStyles.chip}`} aria-label={t("openInForAction", { number })}
      aria-haspopup="menu" aria-expanded={open} data-automation-open-in={current} onClick={() => setOpen((shown) => !shown)}>
      <span>{t(`peekModes.${mode}`)}</span><ChevronDown size="small" aria-hidden="true" />
    </button>
    <span className={styles.actionTitleText} data-head-word>{t("openInAfter")}</span>
    {open ? <AutomationChoicePopover label={t("openPagesIn")} width={280} stacked trigger={trigger.current}
      choices={MODES.map(([key, name, Icon]) => ({ key, text: t(`peekModes.${name}`), detail: t(`peekModeHints.${name}`), chosen: key === current, icon: <Icon size="small" /> }))}
      onPick={(key) => { onChange(key as InteractiveAutomationOpenIn); setOpen(false); }} onClose={() => setOpen(false)} /> : null}
  </>;
}
