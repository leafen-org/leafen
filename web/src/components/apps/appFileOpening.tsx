import { useEffect, useSyncExternalStore } from "react";
import type { Session } from "@/types";
import type { UsableApp, UsableAppFileOpener } from "@/appPlatformWire.generated";
import { AppFileOpener, type AppOpenedFile } from "./AppFileOpener";

// The file a person opened with an app. It is kept here, with the product,
// not in the file block or the page that opened it, so the window stays
// while the page's editor draws again or the person goes elsewhere — its
// changes are asked about before they go (2026-10-16: the window vanished,
// unsaved changes and all, when the person's access was lowered to reading).
type AppFileOpening = {
  session: Session; app: UsableApp; opener: UsableAppFileOpener; file: AppOpenedFile;
  /** Called once the window has closed. */
  onClosed?: () => void;
};

let opening: AppFileOpening | null = null;
// Whether the person can edit each page shown, as its view last said.
const editAccess = new Map<string, boolean>();
let version = 0;
const listeners = new Set<() => void>();
const publish = () => {
  version += 1;
  for (const listener of listeners) listener();
};
const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
};

// A page saying when a document on it was saved here.
const savedListeners = new Set<(pageId: string) => void>();

/** A document on this page was saved from this tab: its view reads the page again. */
export function announceAppFileSaved(pageId: string) {
  for (const listener of savedListeners) listener(pageId);
}

export function useAppFileSaved(onSaved: (pageId: string) => void) {
  useEffect(() => {
    savedListeners.add(onSaved);
    return () => { savedListeners.delete(onSaved); };
  }, [onSaved]);
}

export function openAppFile(next: AppFileOpening) {
  opening = next;
  publish();
}

function closeAppFile() {
  const closed = opening;
  opening = null;
  publish();
  closed?.onClosed?.();
}

/** A page's view saying whether the person can edit it now, for a window open on one of its files. */
export function useAppFileEditAccess(pageId: string, canEdit: boolean) {
  useEffect(() => {
    if (editAccess.get(pageId) === canEdit) return;
    editAccess.set(pageId, canEdit);
    if (opening?.file.pageId === pageId) publish();
  }, [pageId, canEdit]);
}

/** The window of the file opened, which can still edit while its page can. */
export function AppFileOpeningHost() {
  useSyncExternalStore(subscribe, () => version, () => 0);
  // Signing out, the window goes with the product.
  useEffect(() => closeAppFile, []);
  if (!opening) return null;
  return <AppFileOpener key={`${opening.file.blockId}:${opening.opener.key}`} session={opening.session} app={opening.app}
    opener={opening.opener} file={opening.file} canEditNow={editAccess.get(opening.file.pageId) ?? true} onClose={closeAppFile} />;
}
