// A choice made while its control waits for the server's answer shows at
// once, dimmed with the locked control, and after ten seconds without an
// answer the control says the server isn't answering (2026-11-06: on a
// network that stopped, a pick kept showing the value before for 45 seconds
// with no word). Once free again the control shows what its screen holds —
// the choice, or what was there. Switches get the same from
// lib/switchTurns.ts; the words show under the control
// (`data-waiting-note`, app/globals.css), moving nothing.

import { useEffect, useState } from "react";
import { i18next } from "@/i18n";

/** How long a change goes unanswered before its control says so. */
export const WAITING_NOTE_MS = 10_000;

/** What a control waiting longer than that says under it. */
export function waitingNoteText() {
  return i18next.t("settingsErrors:serverNotAnswering");
}

export function usePendingChoice<T>(value: T, disabled: boolean | undefined) {
  const [picked, setPicked] = useState<{ value: T } | null>(null);
  const [slow, setSlow] = useState(false);
  useEffect(() => { if (!disabled && picked !== null) setPicked(null); }, [disabled, picked]);
  useEffect(() => { setPicked(null); }, [value]);
  const waiting = picked !== null && Boolean(disabled);
  useEffect(() => {
    setSlow(false);
    if (!waiting) return undefined;
    const timer = window.setTimeout(() => setSlow(true), WAITING_NOTE_MS);
    return () => window.clearTimeout(timer);
  }, [waiting]);
  return {
    shown: waiting && picked ? picked.value : value,
    pick: (next: T) => setPicked({ value: next }),
    note: waiting && slow ? waitingNoteText() : undefined,
  };
}

/** What a control waiting on its change says once it has waited ten seconds. */
export function useWaitingNote(waiting: boolean) {
  const [slow, setSlow] = useState(false);
  useEffect(() => {
    setSlow(false);
    if (!waiting) return undefined;
    const timer = window.setTimeout(() => setSlow(true), WAITING_NOTE_MS);
    return () => window.clearTimeout(timer);
  }, [waiting]);
  return waiting && slow ? waitingNoteText() : undefined;
}
