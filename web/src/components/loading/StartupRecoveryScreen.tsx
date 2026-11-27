import { useEffect, useState, useRef } from "react";
import { useTranslation } from "react-i18next";
import { useCopyNotice } from "@/lib/copyNotice";
import { userErrorText } from "@/lib/settingsErrors";
import { saveFileToDevice } from "@/lib/deviceFiles";
import { restartPackagedServer } from "@/lib/packagedServerRestart";
import { displayDateTime } from "@/lib/displayDate";
import { canOpenDataFolder, openDataFolder, restoreLastBackup, startupReport } from "@/lib/startupRecovery";
import loading from "./ProductLoadingScreen.module.css";
import styles from "./StartupRecoveryScreen.module.css";
import { pageMarkSource } from "@/lib/pageMark";

/** Why the app's own server couldn't start, as the app names it; none while it is only slow to. */
export type StartupProblem = {
  reason: string;
  message: string;
  backupAt: number | null;
};

const LOG_NAME = "leafen-startup-log.txt";

/**
 * The desktop and phone apps' startup recovery screen (user decision
 * 2026-10-21, after PI-Desktop's StartupRecovery): when the app's own server
 * couldn't start, or is slow to, it says why in words a person acts on and
 * offers what works without the server — trying again, copying the details,
 * saving the log, opening the data folder and, for data that won't open,
 * going back to the last backup with what is there now kept aside. The
 * server's own words go in the details and the log, not on the screen. While
 * it is only slow, the server goes on starting and the app opens by itself.
 */
export function StartupRecoveryScreen({ problem, onStartAgain }: { problem: StartupProblem | null; onStartAgain?: () => void }) {
  const { t } = useTranslation("app");
  const [folderOpens, setFolderOpens] = useState(false);
  const [busy, setBusy] = useState<"" | "retry" | "restore">("");
  // The button keeps its words; drawn before the app, it has no toast, so the note under the buttons says it.
  const notice = useCopyNotice();
  const [restoring, setRestoring] = useState<"" | "confirm" | "secret">("");
  const [secret, setSecret] = useState("");
  const [note, setNoteText] = useState("");
  // A copy said done reads in the quiet color and goes in a moment; a failure stays in red.
  const [noteDone, setNoteDone] = useState(false);
  const doneTimer = useRef<number | undefined>(undefined);
  const setNote = (text: string, done = false) => {
    window.clearTimeout(doneTimer.current);
    setNoteText(text);
    setNoteDone(done);
    if (done && text) doneTimer.current = window.setTimeout(() => setNoteText(""), 3000);
  };
  useEffect(() => () => window.clearTimeout(doneTimer.current), []);

  useEffect(() => {
    void canOpenDataFolder().then(setFolderOpens);
  }, []);

  const kind = problem === null ? "slow"
    : problem.reason === "locked" ? "locked"
      : problem.reason === "damaged" ? "damaged"
        : problem.reason === "disk_full" ? "diskFull" : "failed";
  const restorable = problem?.backupAt != null && (kind === "damaged" || kind === "failed");
  const when = problem?.backupAt != null
    ? displayDateTime(problem.backupAt)
    : "";

  const retry = async () => {
    setBusy("retry");
    setNote("");
    // A start that fails again shows here again, with why.
    await restartPackagedServer().catch(() => undefined);
    onStartAgain?.();
  };
  const restore = async () => {
    setBusy("restore");
    setNote("");
    try {
      const outcome = await restoreLastBackup(secret);
      if (outcome === "needs-secret") {
        setNote(restoring === "secret" ? t("startupRecovery.secretWrong") : "");
        setRestoring("secret");
        setBusy("");
        return;
      }
    } catch (error) {
      setNote(userErrorText(error, t("startupRecovery.restoreFailed")));
      setBusy("");
      return;
    }
    // A server that doesn't start on it shows here again, with why.
    onStartAgain?.();
  };
  const copy = async () => {
    try {
      await notice(await startupReport(), undefined, (message, failed) => setNote(failed ? t("startupRecovery.copyFailed") : message, !failed));
    } catch (error) {
      setNote(userErrorText(error, t("startupRecovery.copyFailed")));
    }
  };
  const save = async () => {
    try {
      await saveFileToDevice(new Blob([await startupReport()], { type: "text/plain" }), LOG_NAME);
    } catch (error) {
      setNote(userErrorText(error, t("startupRecovery.saveFailed")));
    }
  };
  const open = async () => {
    try {
      await openDataFolder();
    } catch (error) {
      setNote(userErrorText(error, t("startupRecovery.openFailed")));
    }
  };

  return (
    <main className={loading.screen} role="alert" data-startup-recovery={kind}>
      <div className={loading.content}>
        <img className={loading.mark} src={pageMarkSource()} alt="" width="48" height="48" />
        <h1 className={loading.alertTitle}>{t(`startupRecovery.${kind}Title`)}</h1>
        <p className={loading.alertText}>{t(`startupRecovery.${kind}Body`)}</p>
        {restorable ? <p className={loading.alertText}>{t("startupRecovery.backup", { when })}</p> : null}
        {kind === "damaged" && !restorable ? <p className={loading.alertText}>{t("startupRecovery.noBackup")}</p> : null}
        {restoring === "confirm" || restoring === "secret" ? (
          <form className={styles.restore} onSubmit={(event) => {
            event.preventDefault();
            void restore();
          }}>
            <p className={loading.alertText}>{t(restoring === "secret" ? "startupRecovery.secretPrompt" : "startupRecovery.restoreConfirm", { when })}</p>
            {restoring === "secret" ? (
              <input className={styles.secret} type="password" autoComplete="off" autoFocus value={secret}
                aria-label={t("startupRecovery.secretLabel")} placeholder={t("startupRecovery.secretLabel")}
                onChange={(event) => setSecret(event.target.value)} />
            ) : null}
            <div className={loading.alertActions}>
              <button type="submit" className={loading.alertPrimary} disabled={busy !== "" || (restoring === "secret" && !secret.trim())}>
                {busy === "restore" ? t("startupRecovery.restoring") : t("startupRecovery.restoreGo")}
              </button>
              <button type="button" className={loading.alertSecondary} disabled={busy !== ""} onClick={() => {
                setRestoring("");
                setSecret("");
                setNote("");
              }}>{t("startupRecovery.cancel")}</button>
            </div>
          </form>
        ) : (
          <div className={loading.alertActions}>
            {problem ? (
              <button type="button" className={loading.alertPrimary} disabled={busy !== ""} onClick={() => void retry()}>
                {busy === "retry" ? t("startupRecovery.retrying") : t("startupRecovery.retry")}
              </button>
            ) : null}
            {restorable ? (
              <button type="button" className={loading.alertSecondary} disabled={busy !== ""}
                onClick={() => setRestoring("confirm")}>{t("startupRecovery.restore")}</button>
            ) : null}
          </div>
        )}
        <div className={loading.alertActions}>
          <button type="button" className={styles.quiet} onClick={() => void copy()}>
            {t("startupRecovery.copy")}
          </button>
          <button type="button" className={styles.quiet} onClick={() => void save()}>{t("startupRecovery.saveLog")}</button>
          {folderOpens ? (
            <button type="button" className={styles.quiet} onClick={() => void open()}>{t("startupRecovery.openFolder")}</button>
          ) : null}
        </div>
        {/* Its line is kept from the start, so a note coming moves nothing on the screen. */}
        <p className={styles.note} role="status" data-note-done={noteDone ? "true" : undefined}>{note}</p>
      </div>
    </main>
  );
}
