import { useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { CheckboxIcon, CheckIcon, FlagIcon, InfoFillIcon, Trash } from "@/icons/leafen";
import type { SelectOption } from "@/lib/types";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "../shell/PhoneSheet";
import { chipStyle, chosenOptionColor, COLOR_NAMES, type ColorName } from "./colors";
import { PhoneMenuChoice } from "./DatabasePhonePickers";
import type { OptionGroupChoice } from "./PropertySelectCellView";
import styles from "./PhoneSelectOptionEditor.module.css";
import sheet from "./databaseSheet";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import { dropOptionNameCommas } from "./optionNameText";

/**
 * An option's editor on a phone, as Notion's (seen 2026-11-11 at 402pt in
 * iOS Safari): a whole sheet headed by the option's chip with "완료", its
 * name in a white row with "ⓘ" at its end, which opens "설명" and its field
 * "설명 추가…"; "삭제" in a group of its own, a status's "기본으로 설정" and
 * "그룹화 ›" with it; then "색", each colour a row with its 24px swatch, the
 * chosen one ticked. Its name is kept as it closes. It opens with the caret
 * at the end of its name and the keyboard up, as Notion's (seen 2026-11-18 on
 * a Galaxy A90 in Chrome at 411, a status's, a select's and a multi-select's
 * option pressed in "속성 편집", each in a tab of its own at two times, no
 * script before the press: the keyboard rose and a letter typed after it
 * ended the name).
 */
export function PhoneSelectOptionEditor({ option, onClose, onRename, onColor, onDescribe, onDelete, onMakeDefault, group }: {
  option: SelectOption;
  onClose(): void;
  onRename(name: string): void;
  onColor(color: ColorName): void;
  onDescribe?(description: string): void;
  /** Asked first (PropertySelectOptionEditor); it then closes. */
  onDelete?(): void;
  onMakeDefault?(): void;
  group?: OptionGroupChoice;
}) {
  const { t } = useTranslation(["propertyCell", "propertyTypeConfig", "common", "databaseView"]);
  const name = useRef(option.name);
  const nameField = useRef<HTMLInputElement>(null);
  // Focused as it is drawn, within the press that opened it, so a phone raises its keyboard.
  useLayoutEffect(() => {
    const field = nameField.current;
    if (!field) return;
    field.focus({ preventScroll: true });
    field.setSelectionRange(field.value.length, field.value.length);
  }, []);
  const [describing, setDescribing] = useState(false), [grouping, setGrouping] = useState(false);
  const close = () => { onRename(name.current); onClose(); };
  const chosen = chosenOptionColor(option);
  const groupLabel = group?.choices.find((choice) => choice.id === group.current)?.label ?? "";
  return (
    <PhoneSheet title={<span className={`${sheet.propertyOptionChip} ${styles.headChip}`} style={chipStyle(option.color)}>{option.name}</span>}
      label={t("propertyCell:edit", { name: option.name })} onClose={close} action={{ label: t("common:actions.done"), onPress: close }}
      data={{ "data-option-editor": option.id, "data-phone-option-editor": "" }}>
      <PhoneSheetGroup>
        <span className={styles.nameRow}>
          <input ref={nameField} className={styles.name} defaultValue={option.name} aria-label={t("propertyCell:optionName")}
            onChange={(event) => { dropOptionNameCommas(event.currentTarget); name.current = event.currentTarget.value; }}
            onBlur={(event) => onRename(event.currentTarget.value)}
            onKeyDown={(event) => {
              if (!fieldEnter(event)) return;
              event.preventDefault();
              event.currentTarget.blur();
            }} />
          {onDescribe ? (
            <button type="button" className={styles.describe} aria-label={t("propertyTypeConfig:addOptionDescription")} aria-expanded={describing}
              data-option-describe onClick={() => setDescribing(true)}>
              <InfoFillIcon size="regular" aria-hidden="true" />
            </button>
          ) : null}
        </span>
      </PhoneSheetGroup>
      {onDescribe && describing ? (
        <PhoneSheetGroup label={t("databaseView:description")}>
          <textarea className={styles.description} defaultValue={option.description ?? ""} rows={1} maxLength={2000} autoFocus
            aria-label={t("propertyTypeConfig:optionDescription")} placeholder={t("propertyTypeConfig:optionDescriptionPlaceholder")} data-option-description
            onBlur={(event) => onDescribe(event.currentTarget.value.trim())} />
        </PhoneSheetGroup>
      ) : null}
      {onDelete || onMakeDefault || group ? (
        <PhoneSheetGroup>
          {onDelete ? <PhoneSheetRow icon={<Trash size="regular" />} label={t("propertyTypeConfig:deleteOption")} more={false}
            onPress={onDelete} data={{ "data-option-delete": "" }} /> : null}
          {onMakeDefault ? <PhoneSheetRow icon={<FlagIcon size="regular" />} label={t("propertyTypeConfig:makeDefaultOption")} more={false}
            onPress={() => { onMakeDefault(); close(); }} data={{ "data-option-make-default": "" }} /> : null}
          {group ? <PhoneSheetRow icon={<CheckboxIcon size="regular" />} label={t("propertyTypeConfig:optionGroup")} value={groupLabel}
            onPress={() => setGrouping(true)} data={{ "data-option-group": "" }} /> : null}
        </PhoneSheetGroup>
      ) : null}
      <PhoneSheetGroup label={t("propertyTypeConfig:optionColorHeading")}>
        {COLOR_NAMES.map((color) => (
          <PhoneSheetRow key={color} icon={<span className={styles.swatch} style={chipStyle(color)} />} more={false}
            label={t(`propertyTypeConfig:optionColors.${color}`)} value={chosen === color ? <CheckIcon size="regular" className={styles.tick} aria-hidden="true" /> : undefined}
            onPress={() => onColor(color)} data={{ "data-option-color": color, "data-active": chosen === color ? "true" : undefined }} />
        ))}
      </PhoneSheetGroup>
      {grouping && group ? (
        <PhoneMenuChoice label={t("propertyTypeConfig:optionGroup")} value={group.current} options={group.choices.map(({ id, label }) => ({ value: id, label }))}
          onPick={group.onMove} onClose={() => setGrouping(false)} />
      ) : null}
    </PhoneSheet>
  );
}
