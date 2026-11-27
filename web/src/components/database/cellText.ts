import i18next from "i18next";
import { useSyncExternalStore } from "react";

// A table draws a label in each of its cells, most of them the same for a
// whole column ("속성 편집: 이름"). useTranslation gave every cell its own
// fixed t, i18n wrapper and language listener, and every label its own
// i18next lookup (a 30-row, 40-property table: some 3,000 hooks and 2,500
// lookups as it opened). Cells read i18next's t through one listener and
// one store of the labels it gave, both renewed when the language or its
// strings change. Keys name their namespace ("propertyCell:edit").

const labels = new Map<string, string>();
const listeners = new Set<() => void>();
let revision = 0;
// Enough for every label of a few large tables; past it the store starts over.
const MAX_LABELS = 5_000;

function changed() {
  labels.clear();
  revision += 1;
  for (const listener of listeners) listener();
}
let storeHeard = false;
function hearStore() {
  if (storeHeard || !i18next.store) return;
  storeHeard = true;
  i18next.store.on("added", changed);
}
i18next.on("languageChanged", changed);
i18next.on("initialized", () => { hearStore(); changed(); });
hearStore();

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

const currentRevision = () => revision;

/** A cell's label, as i18next's t gives it, kept until the language or its strings change. */
export function cellText(key: string, options?: Record<string, string | number | boolean | null | undefined>): string {
  const id = options ? `${key}\u0000${JSON.stringify(options)}` : key;
  let text = labels.get(id);
  if (text === undefined) {
    text = String(i18next.t(key, options));
    if (labels.size >= MAX_LABELS) labels.clear();
    labels.set(id, text);
  }
  return text;
}

/** cellText, its cell drawn again when the language or its strings change. */
export function useCellText(): typeof cellText {
  useSyncExternalStore(subscribe, currentRevision);
  return cellText;
}
