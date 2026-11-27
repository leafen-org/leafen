"use client";

import { useState } from "react";
import { useTranslation } from "react-i18next";

import { SearchClearButton } from "@/components/PageActionSearch";

import { dateKey, formatDateInput, parseDate, parseDateDraft } from "./dateUtils";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

export function DateTextInput({
  value,
  onChange,
  className,
  placeholder,
  ariaLabel,
  disabled = false,
  onFocus,
  clear,
}: {
  value: unknown;
  onChange: (value: string | null) => void;
  className?: string;
  placeholder?: string;
  ariaLabel?: string;
  disabled?: boolean;
  onFocus?: () => void;
  /** Its "ⓧ" after it while it holds any text, typed or set, as Notion's date filter fields (seen 2026-11-15
   *  and 2026-11-16, a "범위 내" rule's 시작일 with one letter typed): pressed, the text and the day go and
   *  the focus with the button. It is Notion's shared "입력 값 지우기" (seen 2026-11-17 at 1440), placed
   *  by the field's `className`, else over the field's end. */
  clear?: { label?: string; className?: string; onClear?(): void };
}) {
  const { t } = useTranslation("databaseView");
  const visiblePlaceholder = placeholder ?? t("defaultPropertyNames.date");
  const parsed = parseDate(value);
  const formatted = formatDateInput(parsed);
  const [draftState, setDraftState] = useState({ source: formatted, draft: formatted });
  const draft = draftState.source === formatted ? draftState.draft : formatted;

  function setDraft(next: string) {
    setDraftState({ source: formatted, draft: next });
  }

  function commit(raw = draft) {
    // A field left as it was writes nothing: its blur as a calendar's day is
    // pressed would write the old day first and lose the press.
    if (raw === formatted) return;
    if (!raw.trim()) {
      onChange(null);
      setDraftState({ source: "", draft: "" });
      return;
    }
    const next = parseDateDraft(raw, parsed?.getFullYear());
    if (!next) {
      setDraft(formatted);
      return;
    }
    onChange(dateKey(next));
    const nextFormatted = formatDateInput(next);
    setDraftState({ source: nextFormatted, draft: nextFormatted });
  }

  const input = (
    <input
      className={className}
      type="text"
      value={draft}
      placeholder={visiblePlaceholder}
      aria-label={ariaLabel}
      disabled={disabled}
      onChange={(e) => setDraft(e.target.value)}
      onFocus={onFocus}
      onBlur={() => commit()}
      onKeyDown={(e) => {
        if (imeComposing(e)) return;
        if (fieldEnter(e)) {
          e.preventDefault();
          (e.currentTarget as HTMLInputElement).blur();
        } else if (e.key === "Escape") {
          setDraft(formatted);
          (e.currentTarget as HTMLInputElement).blur();
        }
      }}
    />
  );
  if (!clear || !draft) return input;
  return (
    <>
      {input}
      <SearchClearButton value={draft} className={clear.className} over label={clear.label} onClear={() => {
        setDraftState({ source: formatted, draft: "" });
        if (formatted) onChange(null);
        clear.onClear?.();
      }} />
    </>
  );
}
