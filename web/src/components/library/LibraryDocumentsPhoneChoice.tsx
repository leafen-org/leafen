import { useState, type ReactNode } from "react";
import type { NotionSelectOption } from "@/components/database/NotionSelect";
import { SettingsChoiceSheet } from "@/components/settings/SettingsTouchChoice";
import selectStyles from "@/components/database/NotionSelect.module.css";
import documentStyles from "./LibraryDocuments.module.css";

/**
 * The documents' "형식" or "정렬" on a phone: the row's mark, its 32px
 * square, blue while it chooses other than the first; its choices rise on
 * a sheet from the screen's foot, as the row's "▦ 문서" and the other tabs'
 * "설정" do and as Notion's phone sheets are (SettingsChoiceSheet: the
 * choice's name with "완료", a row a choice, the chosen one ticked); a pick
 * closes it with the list changed.
 */
export function LibraryDocumentsPhoneChoice({ label, icon, chosen, value, options, onChange }: {
  label: string;
  icon: ReactNode;
  /** Whether it chooses other than its first, so the mark is blue. */
  chosen: boolean;
  value: string;
  options: NotionSelectOption[];
  onChange(value: string): void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <span className={`${selectStyles.notionSelect} ${documentStyles.choice} ${chosen ? documentStyles.chosen : ""}`} data-notion-select="">
      <button type="button" className={`${selectStyles.notionSelectButton} ${documentStyles.choiceButton} ${documentStyles.choiceMark}`}
        data-notion-select-button="" aria-label={label} aria-haspopup="dialog" aria-expanded={open} onClick={() => setOpen(true)}>
        {icon}
      </button>
      {open ? <SettingsChoiceSheet title={label} value={value} options={options} onPick={onChange} onClose={() => setOpen(false)} /> : null}
    </span>
  );
}
