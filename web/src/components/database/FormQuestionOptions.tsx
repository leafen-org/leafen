import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { useTranslation } from "react-i18next";
import { Plus, Trash } from "@/icons/leafen";
import { newId } from "@/lib/ids";
import type { SelectOption } from "@/lib/types";
import { newOptionColor } from "./colors";
import styles from "./FormQuestionCard.module.css";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import { optionNameWithoutCommas } from "./optionNameText";

function uniqueName(options: readonly SelectOption[], base: string, except?: string) {
  const names = new Set(options.filter((option) => option.id !== except).map((option) => option.name.trim().toLowerCase()));
  const trimmed = base.trim();
  if (!names.has(trimmed.toLowerCase())) return trimmed;
  for (let i = 2; i < 1000; i += 1) {
    const candidate = `${trimmed} ${i}`;
    if (!names.has(candidate.toLowerCase())) return candidate;
  }
  return `${trimmed} ${newId().slice(0, 4)}`;
}

/**
 * A choice question's options as Notion's builder edits them (seen
 * 2026-10-10): a row each with its mark and its name to type over, and on
 * pointing a way to remove it; Enter on a row goes on to a new empty one,
 * and "+ 옵션 추가" starts one. They are the property's own options, so every
 * view and answer follows them; an empty new row is dropped. A dropdown's
 * options go without marks.
 */
export function FormQuestionOptions({ options, multiple, marks = true, onChange }: {
  options: readonly SelectOption[];
  multiple: boolean;
  marks?: boolean;
  onChange?: (options: SelectOption[]) => void;
}) {
  const { t } = useTranslation("formView");
  const [names, setNames] = useState<Record<string, string>>({});
  const [adding, setAdding] = useState(false);
  const [draft, setDraft] = useState("");
  const addRef = useRef<HTMLInputElement>(null);
  useEffect(() => { if (adding) addRef.current?.focus(); }, [adding]);
  const editable = Boolean(onChange);

  function rename(option: SelectOption) {
    const typed = names[option.id];
    setNames(({ [option.id]: _done, ...rest }) => rest);
    if (typed === undefined || !typed.trim() || typed.trim() === option.name) return;
    onChange?.(options.map((item) => item.id === option.id ? { ...item, name: uniqueName(options, optionNameWithoutCommas(typed), option.id) } : item));
  }
  function add() {
    const name = optionNameWithoutCommas(draft).trim();
    setDraft("");
    if (!name) {
      setAdding(false);
      return;
    }
    onChange?.([...options, { id: newId(), name: uniqueName(options, name), color: newOptionColor() }]);
  }
  const enter = (event: KeyboardEvent<HTMLInputElement>, done: () => void) => {
    if (imeComposing(event)) return;
    if (fieldEnter(event)) {
      event.preventDefault();
      done();
    }
  };
  return (
    <div className={styles.questionChoices} role="list" aria-label={t("questionOptions")} data-form-question-options>
      {options.map((option) => (
        <div key={option.id} role="listitem" className={styles.questionChoice} data-editable={editable || undefined}>
          {marks ? <span className={styles.questionChoiceMark} data-multiple={multiple} aria-hidden="true" /> : null}
          {editable ? <>
            <input className={styles.questionChoiceName} value={names[option.id] ?? option.name} aria-label={t("optionName")}
              onChange={(event) => setNames((current) => ({ ...current, [option.id]: event.target.value }))}
              onBlur={() => rename(option)}
              onKeyDown={(event) => enter(event, () => { rename(option); setAdding(true); })} />
            <button type="button" className={styles.questionChoiceRemove} aria-label={t("removeOption")} title={t("removeOption")}
              onClick={() => onChange?.(options.filter((item) => item.id !== option.id))}>
              <Trash size="small" aria-hidden="true" />
            </button>
          </> : option.name}
        </div>
      ))}
      {adding ? (
        <div role="listitem" className={styles.questionChoice} data-editable>
          {marks ? <span className={styles.questionChoiceMark} data-multiple={multiple} aria-hidden="true" /> : null}
          <input ref={addRef} className={styles.questionChoiceName} value={draft} aria-label={t("optionName")}
            placeholder={t("newOptionPlaceholder")}
            onChange={(event) => setDraft(event.target.value)}
            onBlur={() => { if (!draft.trim()) setAdding(false); else add(); }}
            onKeyDown={(event) => {
              if (event.key === "Escape") { setDraft(""); setAdding(false); return; }
              enter(event, add);
            }} />
        </div>
      ) : null}
      {editable ? (
        <button type="button" className={styles.questionAddOption} onClick={() => setAdding(true)} data-form-add-option>
          <Plus size="small" aria-hidden="true" />{t("addOption")}
        </button>
      ) : null}
    </div>
  );
}
