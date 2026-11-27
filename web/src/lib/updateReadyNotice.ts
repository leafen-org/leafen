import { i18next } from "@/i18n";
import type { ServiceWorkerUpdateReadyNotifier } from "./serviceWorker";

/**
 * A notice of a new build has its words once i18next has started and holds
 * the notice's namespace. A notice made before then — a first setup or an
 * invitation opens before the words are in — was a box with nothing in it
 * (2026-11-05).
 */
export async function updateNoticeWordsReady() {
  if (!i18next.isInitialized) await new Promise<void>((resolve) => { i18next.on("initialized", () => resolve()); });
  await i18next.loadNamespaces("serviceWorker");
}

type UpdateReadyNotice = Parameters<ServiceWorkerUpdateReadyNotifier>[0];

let pending: UpdateReadyNotice | null = null;
const listeners = new Set<(notice: UpdateReadyNotice) => void>();

/**
 * A new build's worker is ready while this tab runs the old one: the app
 * says so with a reload, once it can show a notice.
 */
export const announceUpdateReady: ServiceWorkerUpdateReadyNotifier = (notice) => {
  pending = notice;
  for (const listener of [...listeners]) listener(notice);
};

export function onUpdateReady(listener: (notice: UpdateReadyNotice) => void) {
  if (pending) listener(pending);
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
