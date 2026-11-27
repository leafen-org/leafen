import { useSyncExternalStore } from "react";
import type { FormViewConfig } from "./types";

/**
 * A form view's builder lends its form to the view's settings panel —
 * Notion's "폼 설정" and its "제출 화면" (seen 2026-11-04) — which changes it
 * through the builder, so the builder's saving owns every change.
 */
export type FormSettingsSource = {
  form: FormViewConfig;
  change(update: (form: FormViewConfig) => FormViewConfig): void;
  /** Opens the builder's "+" choice of a new question. */
  addQuestion(): void;
};

const sources = new Map<string, FormSettingsSource>();
const listeners = new Set<() => void>();

export function lendFormSettings(viewId: string, source: FormSettingsSource | null) {
  if (source) sources.set(viewId, source);
  else sources.delete(viewId);
  for (const listener of listeners) listener();
}

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
};

/** The form the view's builder lends, while it is shown. */
export function useFormSettings(viewId: string) {
  return useSyncExternalStore(subscribe, () => sources.get(viewId) ?? null, () => null);
}
