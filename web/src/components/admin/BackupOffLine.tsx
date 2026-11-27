import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { requestSettingsWindow } from "@/components/settings/settingsWindowRequest";
import { useServerAdministrator } from "@/lib/serverAdministrator";
import { onWindowReturn } from "@/lib/windowReturn";
import type { Session } from "@/types";
import { onScheduledBackupSaved } from "./BackupFailureNotice";
import styles from "./BackupOffLine.module.css";

/**
 * While the server's scheduled backups are off, a line that says so where a
 * lot comes in or its administrator looks — the server's settings and the
 * start of a Notion import — with the way to turn them on, gone once they
 * are on (2026-11-16: a server holding a company's pages and files ran with
 * them off; the band over every page waits a week, BackupFailureNotice).
 * Only an administrator sees it: only they turn backups on, and the schedule
 * is theirs to read. Its "백업 켜기" opens Settings → Server → Backups at the
 * schedule, or the section beside it in the settings (onTurnOn).
 */
export function BackupOffLine({ session, place, onTurnOn }: {
  session: Session;
  /** Where it shows: the server's settings, or the start of an import. */
  place: "settings" | "import";
  onTurnOn?: () => void;
}) {
  const { t } = useTranslation("serverConsole");
  const administrator = useServerAdministrator(session);
  const [off, setOff] = useState(false);
  useEffect(() => {
    if (!administrator) return undefined;
    const controller = new AbortController();
    const read = () => {
      void api.scheduledBackup(session, controller.signal).then(({ schedule }) => {
        setOff(!schedule.enabled && !schedule.setByEnvironment);
      }, () => undefined);
    };
    read();
    const stopSaved = onScheduledBackupSaved(read);
    const stopReturn = onWindowReturn(read);
    return () => {
      controller.abort();
      stopSaved();
      stopReturn();
    };
  }, [administrator, session]);
  if (!administrator || !off) return null;
  const turnOn = onTurnOn ?? (() => requestSettingsWindow({ section: "server-data", part: "scheduled-backup" }));
  const text = t(place === "import" ? "backupOff.import" : "backupOff.settings");
  return (
    <div className={styles.line} role="status" data-backup-off-line={place}>
      <span className={styles.text}>{text}</span>
      <button type="button" className={styles.action} onClick={turnOn} data-backup-off-turn-on>{t("backupOff.turnOn")}</button>
    </div>
  );
}
