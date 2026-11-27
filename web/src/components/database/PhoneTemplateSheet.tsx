import { useState, type ReactNode, type Ref } from "react";
import { useTranslation } from "react-i18next";
import type { DatabaseToolbarTemplateMetadataWire, DatabaseToolbarTemplateRepeatWire } from "@/databaseToolbarConfigWire.generated";
import { CheckIcon, ComposeIcon, Copy, DotsHorizontal, FileBlank, FlagIcon, MenuIcon, Plus, RepeatIcon, Trash } from "@/icons/leafen";
import { phoneScreen } from "@/lib/screenKind";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetMenu, PhoneSheetRow, phoneSheetStyles } from "../shell/PhoneSheet";
import { PhoneScheduleSheet } from "./PhoneAutomationSchedule";
import { confirmRepeatDiscard, templateRepeatLabel } from "./DatabaseTemplateRepeatChooser";
import { newScheduleTriggerDraft, nextScheduleRun, SCHEDULE_PRESETS, schedulePresetOf, withSchedulePreset,
  type SchedulePreset } from "./automationScheduleModel";
import type { ScheduleTriggerDraft } from "./databaseAutomationPanelModel";
import { databaseViewLabels } from "./databaseViewLabels";
import { TemplateIconGlyph } from "./databaseViewShared";
import { useTemplateDisplayName } from "./untitledRowName";
import { storedTemplateRepeat, templateRepeatDraft, templateRepeats } from "./templateRepeatModel";
import scheduleStyles from "./PhoneAutomationSchedule.module.css";
import styles from "./PhoneTemplateSheet.module.css";

/**
 * The templates on a phone, as Notion's half sheet (seen 2026-10-30 at
 * 375px): each template with "기본" when it is the default and its "⋯", the
 * empty page as "비어 있음", then "+ 새 템플릿"; a database without templates
 * shows a gray line saying what they are instead. A template's "⋯" holds
 * "복제 주기 [사용 안 함 ›]", 기본으로 설정, 편집, 복제 and 삭제 (the empty
 * page's 기본으로 설정 alone); "복제 주기" lists the repeats at the bottom,
 * and picking one opens "주기 편집" — a recurring automation's phone sheet
 * under a gray head with a blue "저장하기", "반복 취소" in red under its rows.
 * Nothing is kept until "저장하기"; leaving with changes asks first.
 * A wide touch screen's sheet ends with "사이드바에 단축키 추가" in a group of
 * its own, as the desktop's menu does and Notion's iPad (seen 2026-11-14 at
 * 1210: under "+ 새 템플릿", its sidebar mark and words dark); a phone's has
 * no sidebar to add to and shows none (Notion's iPhone, seen 2026-11-14 at 402).
 */
export function PhoneTemplateSheet({ templates, defaultTemplate, writable, sheetRef, zIndex, onClose, onCreateRow, onCreateTemplate,
  onChooseDefault, onEditTemplate, onDuplicateTemplate, onDeleteTemplate, onSetRepeat, onAddShortcut }: {
  templates: DatabaseToolbarTemplateMetadataWire[];
  defaultTemplate?: DatabaseToolbarTemplateMetadataWire;
  writable: boolean;
  sheetRef: Ref<HTMLDivElement>;
  zIndex?: number;
  onClose(): void;
  onCreateRow(templateId?: string): void;
  onCreateTemplate(): void;
  onChooseDefault(templateId: string | null): void;
  onEditTemplate(templateId: string): void;
  onDuplicateTemplate(templateId: string): void;
  onDeleteTemplate(templateId: string): void;
  onSetRepeat(templateId: string, repeat: DatabaseToolbarTemplateRepeatWire | null): void;
  onAddShortcut?(): void;
}) {
  const labels = databaseViewLabels();
  const nameOf = useTemplateDisplayName(templates);
  const { t } = useTranslation("databaseView");
  // The "⋯" open: a template's id, or "" for the empty page.
  const [actionsFor, setActionsFor] = useState<string | null>(null);
  const [choosing, setChoosing] = useState(false);
  const [draft, setDraft] = useState<ScheduleTriggerDraft | null>(null);
  const [dirty, setDirty] = useState(false);
  const template = actionsFor ? templates.find((candidate) => candidate.id === actionsFor) : undefined;
  const closeActions = () => { setActionsFor(null); setChoosing(false); setDraft(null); setDirty(false); };
  const run = (action: () => void) => { closeActions(); action(); };
  const choose = (preset: SchedulePreset) => {
    if (!template) return;
    setDraft(withSchedulePreset(template.repeat ? templateRepeatDraft(template.repeat) : newScheduleTriggerDraft(), preset));
    setDirty(true); setChoosing(false);
  };
  const leave = async () => {
    if (dirty && !await confirmRepeatDiscard(t)) return;
    setDraft(null); setDirty(false);
  };
  const save = (repeat: DatabaseToolbarTemplateRepeatWire | null) => {
    if (!template) return;
    const id = template.id;
    closeActions();
    onSetRepeat(id, repeat);
  };
  const stored = draft ? storedTemplateRepeat(draft) : null;
  const row = (key: string, icon: ReactNode, name: string, isDefault: boolean, onUse: () => void) => (
    <div key={key} className={styles.item} data-phone-template-row={key}>
      <PhoneSheetRow icon={icon} label={name} more={false} onPress={onUse}
        value={isDefault ? <span className={styles.badge}>{t("templateMenu.default")}</span> : undefined} />
      {writable ? <button type="button" className={styles.more} aria-label={t("templateMenu.actions", { name })}
        data-phone-template-more={key} onClick={() => setActionsFor(key)}>
        <DotsHorizontal size="regular" aria-hidden="true" />
      </button> : null}
    </div>
  );
  return (
    <PhoneSheet title={labels.sheetTemplates} label={labels.sheetTemplates} size="half" sheetRef={sheetRef} zIndex={zIndex}
      onClose={onClose} data={{ "data-database-template-menu": "" }}>
      {templates.length ? <PhoneSheetGroup>
        {templates.map((candidate) => row(candidate.id, <TemplateIconGlyph icon={candidate.icon} empty={!candidate.hasContent} />,
          nameOf(candidate), candidate.id === defaultTemplate?.id, () => onCreateRow(candidate.id)))}
        {row("", <FileBlank size="regular" />, t("templateMenu.empty"), !defaultTemplate, () => onCreateRow(""))}
      </PhoneSheetGroup> : <PhoneSheetGroup><p className={phoneSheetStyles.groupNote}>{labels.sheetTemplatesHint}</p></PhoneSheetGroup>}
      {writable ? <PhoneSheetGroup>
        <PhoneSheetRow icon={<Plus size="regular" />} label={labels.newTemplate} more={false} muted onPress={onCreateTemplate} />
      </PhoneSheetGroup> : null}
      {onAddShortcut && !phoneScreen() ? <PhoneSheetGroup>
        <PhoneSheetRow icon={<MenuIcon size="regular" />} label={t("templateMenu.addShortcut")} more={false}
          onPress={() => { onClose(); onAddShortcut(); }} data={{ "data-template-menu-add-shortcut": "" }} />
      </PhoneSheetGroup> : null}
      {actionsFor !== null ? (
        <PhoneSheet title={template ? nameOf(template) : t("templateMenu.empty")} head={false}
          label={t("templateMenu.actions", { name: template ? nameOf(template) : t("templateMenu.empty") })}
          onClose={closeActions} data={{ "data-phone-template-actions": actionsFor }}>
          <div className={styles.actions}>
          {template ? <PhoneSheetRow icon={<RepeatIcon size="regular" />} label={t("templateMenu.repeat")}
            value={templateRepeatLabel(template, t)} onPress={() => setChoosing(true)} data={{ "data-phone-template-action": "repeat" }} /> : null}
          <PhoneSheetRow icon={<FlagIcon size="regular" />} label={t("templateMenu.setDefault")} more={false} data={{ "data-phone-template-action": "default" }}
            // Notion asks how far it reaches, on the default too.
            onPress={() => run(() => onChooseDefault(template ? template.id : null))} />
          {template ? <>
            <PhoneSheetRow icon={<ComposeIcon size="regular" />} label={t("templateMenu.edit")} more={false} onPress={() => run(() => onEditTemplate(template.id))} />
            <PhoneSheetRow icon={<Copy size="regular" />} label={t("templateMenu.duplicate")} more={false} onPress={() => run(() => onDuplicateTemplate(template.id))} />
            <PhoneSheetRow icon={<Trash size="regular" />} label={t("templateMenu.delete")} more={false} onPress={() => run(() => onDeleteTemplate(template.id))} />
          </> : null}
          </div>
        </PhoneSheet>
      ) : null}
      {template && choosing ? (
        <PhoneSheetMenu label={t("templateMenu.repeat")} onClose={() => setChoosing(false)}>
          {SCHEDULE_PRESETS.map((preset) => (
            <PhoneSheetRow key={preset} label={t(`schedulePresets.${preset}`)} more={false} onPress={() => choose(preset)}
              value={templateRepeats(template) && schedulePresetOf(templateRepeatDraft(template.repeat)) === preset
                ? <CheckIcon size="regular" aria-hidden="true" /> : undefined}
              data={{ "data-phone-template-preset": preset }} />
          ))}
        </PhoneSheetMenu>
      ) : null}
      {template && draft ? (
        <PhoneScheduleSheet draft={draft} label={t("templateRepeat.edit")} onClose={() => void leave()}
          onChange={(next) => { setDraft(next); setDirty(true); }}
          head={<div className={styles.editHead}>
            <span>{t("templateRepeat.edit")}</span>
            <button type="button" className={styles.save} disabled={!stored || !nextScheduleRun(draft)} data-phone-template-save
              onClick={() => stored && save(stored)}>{t("templateRepeat.save")}</button>
          </div>}>
          <button type="button" className={`${scheduleStyles.row} ${styles.stop}`} data-phone-template-stop
            onClick={() => (template.repeat ? save(null) : void leave())}>{t("templateRepeat.stop")}</button>
        </PhoneScheduleSheet>
      ) : null}
    </PhoneSheet>
  );
}
