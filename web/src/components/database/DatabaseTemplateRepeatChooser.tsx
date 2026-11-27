import { useRef, useState, type CSSProperties, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import type { DatabaseToolbarTemplateMetadataWire, DatabaseToolbarTemplateRepeatWire } from "@/databaseToolbarConfigWire.generated";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { CheckIcon } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { ScheduleDetailsFields } from "./AutomationScheduleDetails";
import { newScheduleTriggerDraft, nextScheduleRun, SCHEDULE_PRESETS, scheduleChipLabel, schedulePresetOf, withSchedulePreset,
  type SchedulePreset } from "./automationScheduleModel";
import type { ScheduleTriggerDraft } from "./databaseAutomationPanelModel";
import { storedTemplateRepeat, templateRepeatDraft, templateRepeats } from "./templateRepeatModel";
import detailStyles from "./AutomationScheduleDetails.module.css";
import styles from "./DatabaseTemplateRepeatChooser.module.css";

function presetOf(repeat: DatabaseToolbarTemplateRepeatWire | null | undefined) {
  return repeat ? schedulePresetOf(templateRepeatDraft(repeat)) : "";
}

/**
 * What "복제 주기" says for a template: its unit as the schedule's chip says
 * it — "일", "평일", "개월" for the "월" chosen from the list, "3 주" — or
 * "사용 안 함" (Notion's, seen 2026-11-03).
 */
export function templateRepeatLabel(template: DatabaseToolbarTemplateMetadataWire, t: TFunction) {
  if (!templateRepeats(template)) return t("databaseView:templateMenu.repeatOff");
  return scheduleChipLabel(t, templateRepeatDraft(template.repeat));
}

/** The template menu's blue repeat mark's tip, by its unit alone: "매일 복제" every 3 days too (Notion's, seen 2026-11-03). */
export function templateRepeatTip(template: DatabaseToolbarTemplateMetadataWire, t: TFunction) {
  if (!templateRepeats(template)) return "";
  return t(`databaseView:templateMenu.repeatTip.${templateRepeatDraft(template.repeat).frequency}`);
}

/**
 * "복제 주기" as Notion's (seen 2026-10-30): the list of repeats, then "주기
 * 편집" — a recurring automation's fields (ScheduleDetailsFields) under a
 * gray head with a blue "저장하기", with "반복 취소" under them. A new repeat
 * starts tomorrow "자정 전후". A repeat that is on opens straight at its
 * "주기 편집" (seen 2026-11-02). Nothing is kept until "저장하기", which waits
 * for a change and can't keep a repeat with no run left; leaving with
 * changes asks first.
 */
export function DatabaseTemplateRepeatChooser({ template, onSave, onClose, boxRef, placement }: {
  template: DatabaseToolbarTemplateMetadataWire;
  onSave(repeat: DatabaseToolbarTemplateRepeatWire | null): void;
  onClose(): void;
  /** The box its opener places, and where. */
  boxRef?: RefObject<HTMLDivElement | null>;
  placement?: CSSProperties;
}) {
  const { t } = useTranslation(["databaseView", "common"]);
  const [draft, setDraft] = useState<ScheduleTriggerDraft | null>(
    () => (templateRepeats(template) ? templateRepeatDraft(template.repeat) : null));
  const [dirty, setDirty] = useState(false);
  const change = (next: ScheduleTriggerDraft) => { setDraft(next); setDirty(true); };
  const choose = (preset: SchedulePreset) =>
    change(withSchedulePreset(template.repeat ? templateRepeatDraft(template.repeat) : newScheduleTriggerDraft(), preset));
  const stored = draft ? storedTemplateRepeat(draft) : null;
  // Leaving the editor with changes asks first, as Notion's does.
  const leave = async () => {
    if (draft && dirty && !await confirmRepeatDiscard(t)) return;
    onClose();
  };
  // Its own layer: the keys and focus are its while it is open, Escape leaving.
  const ownRef = useRef<HTMLDivElement>(null), rootRef = boxRef ?? ownRef;
  const { zIndex } = useOverlayLayer(true, { kind: "picker", ref: rootRef, restoreFocus: false, onEscape: () => void leave(), onOutsidePress: () => void leave(),
    initialFocus: "first", focusKey: draft ? "edit" : "list" });
  // One row is lit: the first as the list opens, where its focus goes —
  // from the pointer too, as Notion's (seen 2026-11-08: "일") — then the one
  // the pointer or the keys are on.
  const current = templateRepeats(template) ? presetOf(template.repeat) : "";
  const [lit, setLit] = useState<string>(SCHEDULE_PRESETS[0]);
  if (!draft) {
    return (
      <div ref={rootRef} className={styles.list} style={{ ...placement, zIndex }} role="menu" aria-label={t("templateMenu.repeat")} data-template-repeat-list>
        {SCHEDULE_PRESETS.map((preset) => (
          <button key={preset} type="button" role="menuitemradio" aria-checked={current === preset} className={styles.choice}
            data-template-repeat-preset={preset} data-lit={lit === preset ? "true" : undefined}
            onPointerEnter={() => setLit(preset)} onFocus={() => setLit(preset)} onClick={() => choose(preset)}>
            <span>{t(`schedulePresets.${preset}`)}</span>
            {current === preset ? <CheckIcon size="small" aria-hidden="true" /> : null}
          </button>
        ))}
      </div>
    );
  }
  return (
    <div ref={rootRef} className={`${styles.editor} ${detailStyles.details}`} style={{ ...placement, zIndex }} role="dialog"
      aria-label={t("templateRepeat.edit")} data-template-repeat-editor>
      <div className={styles.head}>
        <span>{t("templateRepeat.edit")}</span>
        <button type="button" className={styles.save} disabled={!dirty || !stored || !nextScheduleRun(draft)}
          onClick={() => stored && onSave(stored)}>{t("templateRepeat.save")}</button>
      </div>
      <ScheduleDetailsFields draft={draft} readOnly={false} onChange={change}>
        <button type="button" className={styles.cancel} data-template-repeat-cancel
          onClick={() => (template.repeat ? onSave(null) : onClose())}>{t("templateRepeat.stop")}</button>
      </ScheduleDetailsFields>
    </div>
  );
}

/**
 * Leaving a repeat's editor with changes asks first, as Notion's does on the
 * phone and the desktop (seen 2026-11-09): a trash can over the question, and
 * "삭제" filled red, as its other cards that drop what was made unsaved.
 */
export function confirmRepeatDiscard(t: TFunction) {
  return confirmAction({ discard: true, message: t("templateRepeat.discard"), confirmLabel: t("templateRepeat.discardConfirm") });
}
