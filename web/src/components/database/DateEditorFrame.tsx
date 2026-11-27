import type { ReactNode, RefObject } from "react";
import { useTranslation } from "react-i18next";
import { TouchSheetLayout, useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneSheet } from "../shell/PhoneSheet";
import { CellMenuPortal } from "./PropertyCellMenu";
import { PickerOpened } from "./PropertySelectPicker";
import styles from "./databaseSheet";
import sheet from "./DateEditorSheet.module.css";

/**
 * Where a date property's editor stands: a window hanging from its value,
 * moved up to show whole, 248px wide for its month and as wide as its
 * widest line beyond that (widestLine), as Notion's date window; on a touch screen Notion's
 * phone sheet "날짜", the type's name whatever the property is called (seen
 * 2026-11-15 at 402pt in iOS Safari, twice: "마감일" opens "날짜", as a
 * relation opens "관계형" where a person opens the property's own name):
 * the whole screen, its date field, calendar, settings and "삭제"
 * each a group of their own (DateEditorSheet.module.css), its field not
 * focused, so no keyboard rises until it is pressed.
 */
export function DateEditorFrame({ label, triggerRef, onClose, children }: {
  label: string;
  triggerRef: RefObject<HTMLButtonElement | null>;
  onClose(): void;
  children: ReactNode;
}) {
  const { touch } = useSidebarLayout(true);
  if (touch) return <DateEditorSheet label={label} onClose={onClose}>{children}</DateEditorSheet>;
  return (
    <CellMenuPortal rise under triggerRef={triggerRef} onClose={onClose} ariaLabel={label} className={styles.dateMenu}
      backdropClassName={styles.menuBackdrop} width={248} widestLine>
      {children}
    </CellMenuPortal>
  );
}

/**
 * Every date editor on a touch screen — a date property's, a date mention's
 * (a to-do's due date and a comment's date too) and a form's date answer —
 * as Notion's phone sheet "날짜", whatever the date is called (seen 2026-11-15
 * for a property; a mention's and a form answer's alike, seen 2026-11-16):
 * its groups the date field (data-date-inputs), the calendar, the settings
 * (data-date-options) and "삭제" (data-date-clear), DateEditorSheet.module.css.
 */
export function DateEditorSheet({ label, onClose, children }: { label: string; onClose(): void; children: ReactNode }) {
  const { t } = useTranslation("propertyTypes");
  return (
    <TouchSheetLayout>
      <PhoneSheet title={t("propertyTypes:labels.date")} label={label} tone="settings" onClose={onClose} data={{ "data-date-sheet": "" }}>
        <div className={sheet.sheet}>{children}</div>
      </PhoneSheet>
      <PickerOpened />
    </TouchSheetLayout>
  );
}
