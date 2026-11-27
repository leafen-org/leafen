import { useEffect, useSyncExternalStore } from "react";
import { useTranslation } from "react-i18next";
import { CheckCircleFillIcon, X } from "@/icons/leafen";
import styles from "./PageRestoredNotice.module.css";

// The page a version was just restored on, wherever its history window opened.
let restoredPageId: string | null = null;
const listeners = new Set<() => void>();

function setRestoredPage(next: string | null) {
  if (restoredPageId === next) return;
  restoredPageId = next;
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/** Says a version of the page was restored. */
export function announcePageRestored(pageId: string) {
  setRestoredPage(pageId);
}

/**
 * The band under a page's top bar after a restore, as Notion's (seen
 * 2026-11-05): blue words on a blue wash with a filled check, across the page
 * or its peek, until it is closed or another page is opened.
 */
export function PageRestoredNotice({ pageId }: { pageId: string }) {
  const { t } = useTranslation(["updatesPanel", "common"]);
  const restored = useSyncExternalStore(subscribe, () => restoredPageId);
  useEffect(() => {
    if (restoredPageId !== null && restoredPageId !== pageId) setRestoredPage(null);
  }, [pageId]);
  if (restored !== pageId) return null;
  return (
    <div className={styles.notice} role="status" data-page-restored-notice>
      <CheckCircleFillIcon size="small" className={styles.icon} aria-hidden="true" />
      <span className={styles.text}>{t("updatesPanel:versions.restoredPrevious")}</span>
      <button type="button" className={styles.close} onClick={() => setRestoredPage(null)}
        aria-label={t("common:actions.close")}>
        <X size="tiny" aria-hidden="true" />
      </button>
    </div>
  );
}
