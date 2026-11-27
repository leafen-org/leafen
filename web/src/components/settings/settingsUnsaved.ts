import { useEffect, useId, useRef } from "react";
import { i18next } from "@/i18n";
import { confirmAction } from "@/components/ConfirmActionDialog";

// A settings section holding changes it hasn't saved says so here, and the
// settings window asks before they go — closing it, or moving to a section
// that doesn't keep them (2026-10-16: a sender name changed in the server's
// email settings was dropped without a word when the window closed).
/**
 * A section's own question before its changes go, by its words' keys, as
 * Notion asks for the sidebar's presets (seen 2026-11-18: "사이드바 변경 사항을
 * 삭제할까요?", its discard in solid red over "계속 편집").
 */
export type SettingsUnsavedWords = { message: string; detail: string; discard: string; keep: string };
type Unsaved = { stays?: (section: string) => boolean; words?: SettingsUnsavedWords };
const unsaved = new Map<string, Unsaved>();

/**
 * Marks this section's changes as not saved while `dirty`. `stays` names the
 * sections it stays open across (the server's settings are one form);
 * `words` is its own question, the shared one otherwise.
 */
export function useSettingsUnsaved(dirty: boolean, stays?: (section: string) => boolean, words?: SettingsUnsavedWords) {
  const key = useId();
  const staysRef = useRef(stays);
  staysRef.current = stays;
  const wordsRef = useRef(words);
  wordsRef.current = words;
  useEffect(() => {
    if (!dirty) return;
    unsaved.set(key, { stays: (section) => staysRef.current?.(section) === true, words: wordsRef.current });
    const ask = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", ask);
    return () => {
      unsaved.delete(key);
      window.removeEventListener("beforeunload", ask);
    };
  }, [dirty, key]);
}

/** Whether a section holds changes it hasn't saved. */
export function settingsChangesUnsaved(): boolean {
  return unsaved.size > 0;
}
// The question being asked: a link closing the window as its own close does asks once.
let asking: Promise<boolean> | null = null;

/** Whether the window may close, or move to `section`: asks when changes would be lost. */
export async function mayLeaveSettings(section?: string): Promise<boolean> {
  if (asking) return asking;
  const lost = [...unsaved.values()].filter((entry) => section === undefined || !entry.stays?.(section));
  if (!lost.length) return true;
  asking = askLeave(lost);
  try {
    return await asking;
  } finally {
    asking = null;
  }
}

function askLeave(lost: Unsaved[]): Promise<boolean> {
  // One section's changes go with its own question, if it has one.
  const own = lost.length === 1 ? lost[0].words : undefined;
  if (own) {
    return confirmAction({
      message: i18next.t(own.message),
      detail: i18next.t(own.detail),
      confirmLabel: i18next.t(own.discard),
      cancelLabel: i18next.t(own.keep),
      danger: "filled",
    });
  }
  return confirmAction({
    message: i18next.t("workspaceSettingsDialog:unsavedLeave"),
    detail: i18next.t("workspaceSettingsDialog:unsavedLeaveDetail"),
    confirmLabel: i18next.t("workspaceSettingsDialog:unsavedDiscard"),
    danger: true,
  });
}
