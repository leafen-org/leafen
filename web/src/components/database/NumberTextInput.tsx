"use client";

import { useState } from "react";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";


function parseNumberDraft(value: string): number | null | undefined {
  const normalized = value.trim().replace(/[,\s$€₩%]/g, "");
  if (!normalized) return null;
  const number = Number(normalized);
  return Number.isFinite(number) ? number : undefined;
}

export function NumberTextInput({
  value,
  onChange,
  className,
  placeholder = "Number",
  ariaLabel,
  disabled = false,
  live = false,
}: {
  value: unknown;
  onChange: (value: number | null) => void;
  className?: string;
  placeholder?: string;
  ariaLabel?: string;
  disabled?: boolean;
  /** Takes each number as it is typed, as a filter's field does, keeping what was typed ("1."). */
  live?: boolean;
}) {
  const source = value == null || value === "" ? "" : String(value);
  const [draftState, setDraftState] = useState({ source, draft: source });
  const draft = draftState.source === source ? draftState.draft : source;

  function setDraft(next: string) {
    setDraftState({ source, draft: next });
  }

  function commit(raw = draft) {
    const next = parseNumberDraft(raw);
    // Leaving the field as it was writes nothing: a phone's empty filter,
    // its sheet closing, goes rather than taking an empty value first.
    if (next === undefined || next === parseNumberDraft(source)) {
      setDraftState({ source, draft: source });
      return;
    }
    onChange(next);
    const nextSource = next == null ? "" : String(next);
    setDraftState({ source: nextSource, draft: nextSource });
  }

  // A filter's number takes the phone's plain keyboard, not a number pad, as
  // Notion's filters' fields (iPhone Safari 402pt and Galaxy A90 Chrome 411,
  // 2026-11-15: a quick filter's and an advanced filter's rule's).
  return (
    <input
      className={className}
      type="text"
      value={draft}
      placeholder={placeholder}
      aria-label={ariaLabel}
      disabled={disabled}
      onChange={(e) => {
        const raw = e.target.value;
        const next = live ? parseNumberDraft(raw) : undefined;
        if (next === undefined) return setDraft(raw);
        onChange(next);
        setDraftState({ source: next == null ? "" : String(next), draft: raw });
      }}
      onBlur={() => commit()}
      onKeyDown={(e) => {
        if (imeComposing(e)) return;
        if (fieldEnter(e)) {
          e.preventDefault();
          (e.currentTarget as HTMLInputElement).blur();
        } else if (e.key === "Escape") {
          setDraftState({ source, draft: source });
          (e.currentTarget as HTMLInputElement).blur();
        }
      }}
    />
  );
}
