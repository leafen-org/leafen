import type { ReactNode, RefObject } from "react";
import { TouchSheetLayout } from "@/components/shell/AppShell";
import type { DbProperty, SelectOption } from "@/lib/types";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "../shell/PhoneSheet";
import { SlidersIcon } from "../icons";
import { chipStyle } from "./colors";
import { statusGroupLabelKey, type statusGroupColumns } from "./propertyStatusPolicy";
import { useCellText } from "./cellText";
import styles from "./databaseSheet";
import own from "./PropertyStatusSheet.module.css";

/**
 * A status's picker on a touch screen, as Notion's phone sheet (seen
 * 2026-11-12 and 2026-11-15 at 402pt in iOS Safari): the property's name over
 * its groups — "할 일", "진행 중", "완료", 12px — each a group of its
 * options' pills (24 high, 16px, the dot 10), with no search field and so no
 * keyboard, and "속성 편집" in a group of its own at the end where the
 * property may be changed. Pressing an option chooses it and closes the sheet.
 */
export function PropertyStatusSheet({ prop, options, groups, label, sheetRef, onChoose, onEditProperty, onClose, after }: {
  prop: DbProperty;
  options: readonly SelectOption[];
  groups: ReturnType<typeof statusGroupColumns>;
  label: string;
  sheetRef: RefObject<HTMLDivElement | null>;
  onChoose(optionId: string): void;
  /** "속성 편집", where the property's settings may be opened. */
  onEditProperty?: () => void;
  onClose(): void;
  /** What the picker draws beside the sheet (its "opened" signal). */
  after?: ReactNode;
}) {
  const t = useCellText();
  return (
    <TouchSheetLayout>
      <PhoneSheet size="half" title={prop.name} label={label} tone="settings" sheetRef={sheetRef} onClose={onClose} data={{ "data-select-sheet": "", "data-status-sheet": "" }}>
        {groups.map((group) => {
          const shown = options.filter((option) => group.optionIds.includes(option.id));
          if (!shown.length) return null;
          const key = statusGroupLabelKey(group.name);
          return (
            <PhoneSheetGroup key={group.id} label={<span className={own.groupLabel}>{key ? t(`propertyCell:${key}`) : group.name}</span>}>
              {shown.map((option) => (
                <button key={option.id} type="button" className={own.option} data-option-id={option.id} onClick={() => onChoose(option.id)}>
                  <span className={styles.chip} style={chipStyle(option.color)} data-status-chip="">
                    <span className={styles.chipText}>{option.name}</span>
                  </span>
                </button>
              ))}
            </PhoneSheetGroup>
          );
        })}
        {onEditProperty ? (
          <PhoneSheetGroup>
            <PhoneSheetRow icon={<SlidersIcon size="regular" />} label={t("propertyCell:editProperty")} more={false} onPress={onEditProperty}
              data={{ "data-select-edit-property": "" }} />
          </PhoneSheetGroup>
        ) : null}
      </PhoneSheet>
      {after}
    </TouchSheetLayout>
  );
}
