import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { listSyncFolders, type SyncFolder } from "../../lib/deviceFolder";
import settingsStyles from "./AdministratorSettingsPage.module.css";
import styles from "./ScheduledBackupSyncFolders.module.css";

// The services' own names, which stay in every language.
const SERVICE_NAMES: Record<SyncFolder["service"], string> = {
  "google-drive": "Google Drive", "icloud-drive": "iCloud Drive", dropbox: "Dropbox", onedrive: "OneDrive",
};

/**
 * The cloud drives' folders the desktop app found on this computer, first
 * among the places for backups (2026-10-17): picking one backs up there
 * every day, keeping the last seven, at once. Nothing shows in a browser, on
 * a phone or when none is found.
 */
export function ScheduledBackupSyncFolders({ current, busy, onPick }: {
  /** The folder backups go to now, or "" while they are off. */
  current: string;
  busy: boolean;
  onPick: (path: string) => void;
}) {
  const { t } = useTranslation("serverConsole");
  const [folders, setFolders] = useState<SyncFolder[]>([]);

  useEffect(() => {
    let live = true;
    void listSyncFolders().then((found) => { if (live) setFolders(found); }, () => undefined);
    return () => { live = false; };
  }, []);

  if (!folders.length) return null;
  return (
    <div className={styles.syncFolders} data-scheduled-backup-sync-folders>
      <span className={styles.title}>{t("schedule.syncFolders")}</span>
      <ul className={styles.list}>
        {folders.map((folder) => (
          <li key={folder.path} className={styles.folder} data-sync-folder={folder.service}>
            <span className={styles.names}>
              <span className={styles.service}>
                {SERVICE_NAMES[folder.service]}
                {folder.account ? <span className={styles.account}> · {folder.account}</span> : null}
              </span>
              <span className={styles.path} title={folder.path}>{folder.path}</span>
            </span>
            {folder.path === current ? (
              <span className={styles.inUse}>{t("schedule.syncFolderInUse")}</span>
            ) : (
              <button type="button" className={settingsStyles.secondaryButton} disabled={busy} onClick={() => onPick(folder.path)}>
                {t("schedule.syncFolderUse")}
              </button>
            )}
          </li>
        ))}
      </ul>
      <p className={styles.help}>{t("schedule.syncFoldersHelp")}</p>
    </div>
  );
}
