import { useTranslation } from "react-i18next";
import type {
  AdministratorBackupCurrentResult,
  AdministratorBackupJob,
  AdministratorScheduledBackup,
  AdministratorScheduledBackupSnapshot,
} from "../../administratorWire.generated";
import { useCopyNotice } from "@/lib/copyNotice";
import { deviceFolderName } from "@/lib/deviceFolder";
import { psQuoted } from "@/lib/powerShellQuoted";
import styles from "./AdministratorSettingsPage.module.css";
import own from "./BackupRestoreCommand.module.css";

/**
 * A downloaded backup's file name, which carries the local time it was made
 * like the other downloads (a UTC stamp named a file made at 02:48 on the 6th
 * "the 5th"); the restore command below names the file by it.
 */
export function backupFileName(made: AdministratorBackupJob) {
  const at = new Date(made.completedAt ?? made.updatedAt);
  const pad = (value: number) => String(value).padStart(2, "0");
  return `leafen-server-backup-${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())}-${pad(at.getHours())}${pad(at.getMinutes())}${pad(at.getSeconds())}.zip`;
}

export type RestoreWay = Pick<AdministratorBackupCurrentResult, "restoreRuns" | "restoreProgram">;

/**
 * A restore where the server runs, as the server says
 * (administrator_backup_restore_way.go; docs/self-hosting.md "Restore"): the
 * steps in words and the one command to copy — in Leafen's image through
 * Docker Compose; in the desktop app, its program on a computer whose app has
 * no data yet; elsewhere the server's program, into a new data folder. A
 * phone's own server has no terminal, so its words alone say where it
 * restores, with no command.
 */
function RestoreCommand({ runs, words, command }: { runs: string; words: string; command: string }) {
  const { t } = useTranslation("serverConsole");
  const copy = useCopyNotice();
  return (
    <div data-backup-restore-command={runs}>
      <p className={own.way}>{words}</p>
      {runs === "phone" ? null : (
        <p className={own.command}>
          <code className={own.code}>{command}</code>
          <button type="button" className={styles.secondaryButton} onClick={() => void copy(command)}>{t("backup.copyCommand")}</button>
        </p>
      )}
    </div>
  );
}

/**
 * How the downloaded file is restored, its name and SHA-256 filled in, or
 * their places while no backup is made yet, the file beside compose.yaml in
 * Leafen's image (2026-11-21: it said only to use the command line, and no
 * command). The folder goes to the container as "${PWD}:/restore:ro", one
 * word in a POSIX shell and in PowerShell alike: PowerShell split
 * "$PWD":/restore:ro into two arguments (2026-11-25).
 */
export function BackupRestoreCommand({ way, made }: { way: RestoreWay; made: AdministratorBackupJob | null }) {
  const { t } = useTranslation("serverConsole");
  const file = made ? backupFileName(made) : t("backup.restoreFilePlace");
  const sha = made?.artifactSha256 || "<SHA-256>";
  const command = way.restoreRuns === "container"
    ? `docker compose run --rm -v "\${PWD}:/restore:ro" leafen restore --input=/restore/${file} --sha256=${sha}`
    : `${way.restoreProgram}${way.restoreRuns === "server" ? " --dir=leafen-restored" : ""} restore --input=${file} --sha256=${sha}`;
  return <RestoreCommand runs={way.restoreRuns} words={t(`backup.restoreWay_${way.restoreRuns}`)} command={command} />;
}

/**
 * How a backup of the scheduled backup folder is restored, by the same rule:
 * the command names the folder's newest backup by its place in the folder
 * (Leafen backups/snapshots/<name>, scheduled_backup_snapshot.go) — another
 * restores by its name there — and asks for the password where the backups
 * are encrypted. A WebDAV or S3 place's folder is downloaded first, and a
 * folder a phone picked has no path to type, so the command holds their
 * places; in Leafen's image a folder the server writes is the container's
 * own, which the command reaches as it is, and what is downloaded or the
 * password's file goes beside compose.yaml (2026-11-21: the screen gave the
 * command in words, with every path to fill in).
 */
export function BackupFolderRestoreCommand({ way, schedule, newest }: {
  way: RestoreWay;
  schedule: Pick<AdministratorScheduledBackup, "target" | "folder" | "protection">;
  newest: AdministratorScheduledBackupSnapshot | null;
}) {
  const { t } = useTranslation("serverConsole");
  const container = way.restoreRuns === "container";
  // PowerShell's "&" before the program (typedServerProgram) takes its own quotes.
  const powershell = way.restoreProgram.startsWith("& ");
  const quote = (value: string) => (powershell ? psQuoted(value) : `'${value.replaceAll("'", "'\\''")}'`);
  const remote = schedule.target !== "folder";
  const picked = !remote && deviceFolderName(schedule.folder) !== null;
  const separator = !container && /\\/u.test(schedule.folder) && !schedule.folder.includes("/") ? "\\" : "/";
  const root = remote
    ? (container ? "/restore/Leafen backups" : t("restore.folderDownloadedPlace"))
    : `${picked ? t("restore.folderPlace") : schedule.folder.replace(/[\\/]+$/u, "")}${separator}Leafen backups`;
  const input = [root, "snapshots", newest?.name ?? t("restore.folderBackupPlace")].join(separator);
  const encrypted = newest ? newest.sealed : schedule.protection.code || schedule.protection.password;
  const password = encrypted ? ` --password-file=${quote(container ? `/restore/${t("restore.passwordFilePlace")}` : t("restore.passwordFilePlace"))}` : "";
  const command = container
    ? `docker compose run --rm ${remote || encrypted ? `-v "\${PWD}:/restore:ro" ` : ""}leafen restore --input=${quote(input)}${password}`
    : `${way.restoreProgram}${way.restoreRuns === "server" ? " --dir=leafen-restored" : ""} restore --input=${quote(input)}${password}`;
  // What the command needs said goes with it: a phone gives none.
  const words = way.restoreRuns === "phone" ? t("restore.folderWay_phone") : [
    remote ? t(container ? "restore.folderDownloadContainer" : "restore.folderDownload") : "",
    t(`restore.folderWay_${way.restoreRuns}`),
    newest ? t("restore.folderNewest") : "",
    encrypted ? t(container ? "restore.folderSealedContainer" : "restore.folderSealed") : "",
  ].filter(Boolean).join(" ");
  return <RestoreCommand runs={way.restoreRuns} words={words} command={command} />;
}
