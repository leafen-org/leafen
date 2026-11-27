import { useEffect, useRef, useState } from "react";
import { useAttemptNotice } from "@/lib/useNoticeFor";
import { useTranslation } from "react-i18next";
import { api } from "../../api";
import { administratorErrorCodes, type AdministratorScheduledBackup as Schedule } from "../../administratorWire.generated";
import { LeafenApiError } from "../../apiTransport";
import { pickedFoldersOnly } from "../../lib/deviceFolder";
import { productErrorMessage } from "../../lib/productErrorPresentation";
import type { Session } from "../../types";
import styles from "./AdministratorSettingsPage.module.css";
import { SettingRow } from "./AdministratorSettingRows";
import { protectionRequiredText } from "./scheduledBackupFolderWords";
import dataStyles from "./AdministratorDataManagement.module.css";
import { useCopyNotice } from "@/lib/copyNotice";

type Translate = (key: string, options?: Record<string, unknown>) => string;

/**
 * The backups' protection (2026-10-18): what opens the backups — a recovery
 * code the server makes and shows once, a password, or both — each wrapping
 * the backups' own key, so either opens them and changing one seals nothing
 * again. A place off this computer takes no backup until one exists; a
 * caller that needs one first (backing up to a cloud folder) asks for the
 * code and goes on once it is saved.
 */
export function BackupProtection({ session, schedule, onSchedule, askCode, onCodeSaved, onCodeCancelled }: {
  session: Session;
  schedule: Schedule;
  onSchedule: (schedule: Schedule) => void;
  /** Make a recovery code now, as a caller that needs one asks. */
  askCode?: boolean;
  onCodeSaved?: () => void;
  onCodeCancelled?: () => void;
}) {
  const { t } = useTranslation("serverConsole");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [code, setCode] = useState("");
  // Whether the code shown is the first way to open the backups.
  const [codeFirst, setCodeFirst] = useState(false);
  const copy = useCopyNotice();
  const [choosing, setChoosing] = useState(false);
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useAttemptNotice<string>(password, confirm);
  const errorId = "backup-protection-error";
  const mounted = useRef(true);
  const protection = schedule.protection;
  const protectedNow = protection.code || protection.password;
  // Backups made before stay unsealed until enough new ones replace them.
  const firstSealed = schedule.snapshots > 0
    ? t("protection.firstSealedOlder", { count: schedule.keep }) : t("protection.firstSealed");

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  useEffect(() => {
    if (askCode && !code && !busy) void makeCode();
    // Asked once each time the caller asks.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [askCode]);

  // A code made because the caller asked comes into sight with the focus: the
  // press that asked was elsewhere, and its button goes.
  const copyRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (!code || !askCode) return;
    const button = copyRef.current;
    button?.scrollIntoView({ block: "nearest" });
    button?.focus({ preventScroll: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [code]);

  async function run(action: () => Promise<Schedule | null>, done?: string) {
    if (busy) return false;
    setBusy(true);
    setError("");
    setMessage("");
    try {
      const next = await action();
      if (!mounted.current) return false;
      if (next) onSchedule(next);
      if (done) setMessage(done);
      return true;
    } catch (cause) {
      if (mounted.current) setError(protectionMessage(cause, t));
      return false;
    } finally {
      if (mounted.current) setBusy(false);
    }
  }

  async function makeCode() {
    const first = !protectedNow;
    await run(async () => {
      const result = await api.createBackupRecoveryCode(session);
      setCode(result.code);
      setCodeFirst(first);
      return result.schedule;
    });
  }

  async function savePassword() {
    if (password !== confirm) {
      setError(t("protection.passwordMismatch"));
      return;
    }
    const saved = await run(async () => (await api.setBackupPassword(session, password)).schedule,
      protection.password ? t("protection.passwordChanged") : protectedNow ? t("protection.passwordAdded") : firstSealed);
    if (saved) {
      setPassword("");
      setConfirm("");
      setChoosing(false);
    }
  }

  async function copyCode() {
    await copy(code);
  }

  function printCode() {
    // Not "noopener": that makes window.open answer null, and this page
    // writes the code into the window it opens. Its tie back to this page is
    // cut as soon as it is open.
    const page = window.open("", "_blank", "width=480,height=360");
    if (!page) return;
    page.opener = null;
    page.document.title = t("protection.printTitle");
    const heading = page.document.createElement("h1");
    heading.textContent = t("protection.printTitle");
    const shown = page.document.createElement("pre");
    shown.textContent = code;
    shown.style.fontSize = "20px";
    const note = page.document.createElement("p");
    note.textContent = t("protection.codeHelp");
    page.document.body.append(heading, shown, note);
    page.print();
  }

  function codeSaved() {
    setCode("");
    setMessage(codeFirst ? firstSealed : t("protection.codeReplaced"));
    onCodeSaved?.();
  }

  if (schedule.setByEnvironment) return null;
  // 설정's rows: what opens the backups, each with its button (2026-11-05: a box of
  // "설정됨/없음" over a row of up to five buttons).
  const dropping = Boolean(protection.code && protection.password);
  return (
    <section className={styles.section} aria-labelledby="backup-protection-title" data-backup-protection={protectedNow ? "on" : "off"}>
      <div id="backup-protection-title" className={styles.sectionTitle}>{t("protection.title")}</div>
      <div className={styles.settingRows}>
        <SettingRow title={t("protection.code")} description={<span data-backup-protection-code={protection.code ? "on" : "off"}>{protection.code ? t("protection.kept") : t("protection.none")}</span>}>
          <span className={styles.settingEnd}>
            {dropping ? (
              <button type="button" className={styles.secondaryButton} disabled={busy}
                onClick={() => void run(async () => (await api.dropBackupProtection(session, "code")).schedule)} data-backup-drop-code>
                {t("protection.dropCode")}
              </button>
            ) : null}
            <button type="button" className={styles.secondaryButton} disabled={busy || Boolean(code)}
              onClick={() => void makeCode()} data-backup-make-code>
              {protection.code ? t("protection.newCode") : t("protection.makeCode")}
            </button>
          </span>
        </SettingRow>
        <SettingRow title={t("protection.password")} description={<span data-backup-protection-password={protection.password ? "on" : "off"}>{protection.password ? t("protection.kept") : t("protection.none")}</span>}>
          <span className={styles.settingEnd}>
            {dropping ? (
              <button type="button" className={styles.secondaryButton} disabled={busy}
                onClick={() => void run(async () => (await api.dropBackupProtection(session, "password")).schedule)} data-backup-drop-password>
                {t("protection.dropPassword")}
              </button>
            ) : null}
            <button type="button" className={styles.secondaryButton} disabled={busy || choosing}
              onClick={() => { setChoosing(true); setError(""); }} data-backup-choose-password>
              {protection.password ? t("protection.changePassword") : t("protection.choosePassword")}
            </button>
          </span>
        </SettingRow>
      {code ? (
        <div className={styles.tunnelFields} data-backup-recovery-code>
          <p className={styles.emailProviderMeta}>{t("protection.codeShownOnce")}</p>
          <code className={styles.recoveryCode} data-backup-recovery-code-value>{code}</code>
          <div className={dataStyles.actions}>
            <button ref={copyRef} type="button" className={styles.secondaryButton} onClick={() => void copyCode()} data-backup-recovery-code-copy>
              {t("protection.copy")}
            </button>
            <button type="button" className={styles.secondaryButton} onClick={printCode} data-backup-recovery-code-print>
              {t("protection.print")}
            </button>
            <button type="button" className={styles.primaryButton} onClick={codeSaved} data-backup-recovery-code-saved>
              {t("protection.saved")}
            </button>
          </div>
          <p className={styles.emailProviderMeta}>{t("protection.codeHelp")}</p>
        </div>
      ) : choosing ? (
        <div className={styles.tunnelFields} data-backup-password-form>
          <label>
            <span>{t("protection.newPassword")}</span>
            <input type="password" autoComplete="new-password" value={password} disabled={busy}
              onChange={(event) => setPassword(event.target.value)} data-backup-password
              {...(error === t("protection.weak") ? { "aria-invalid": true as const, "aria-describedby": "backup-protection-error" } : {})} />
          </label>
          <label>
            <span>{t("protection.confirmPassword")}</span>
            <input type="password" autoComplete="new-password" value={confirm} disabled={busy}
              onChange={(event) => setConfirm(event.target.value)} data-backup-password-confirm
              {...(error === t("protection.passwordMismatch") ? { "aria-invalid": true as const, "aria-describedby": "backup-protection-error" } : {})} />
          </label>
          <p className={styles.emailProviderMeta}>{t("protection.passwordHelp")}</p>
          <div className={dataStyles.actions}>
            <button type="button" className={styles.primaryButton} disabled={busy || !password || !confirm}
              onClick={() => void savePassword()} data-backup-password-save>
              {t("protection.setPassword")}
            </button>
            <button type="button" className={styles.secondaryButton} disabled={busy}
              onClick={() => { setChoosing(false); setPassword(""); setConfirm(""); }}>
              {t("protection.cancel")}
            </button>
          </div>
        </div>
      ) : null}
        {askCode && !code && onCodeCancelled ? (
          <div><button type="button" className={styles.secondaryButton} disabled={busy} onClick={onCodeCancelled}>{t("protection.cancel")}</button></div>
        ) : null}
        {!protectedNow && protection.required ? (
          <p className={styles.errorText} role="alert" data-error data-backup-protection-required>{protectionRequiredText(t)}</p>
        ) : null}
        <p className={styles.settingNote}>{protectedNow ? t("protection.help") : t(pickedFoldersOnly() ? "protection.offHelpDevice" : "protection.offHelp")}</p>
        {message ? <p className={styles.settingNote} role="status">{message}</p> : null}
        {error ? <p id={errorId} className={styles.errorText} role="alert" data-error>{error}</p> : null}
      </div>
    </section>
  );
}

/**
 * Backups at the place this server can't open yet — another server's, being
 * restored here — opened with their recovery code or password, which this
 * server then keeps to open and go on writing them.
 */
export function BackupOpener({ session, onSchedule }: { session: Session; onSchedule: (schedule: Schedule) => void }) {
  const { t } = useTranslation("serverConsole");
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useAttemptNotice<string>(secret);
  const errorId = "backup-opener-error";
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  async function open() {
    if (busy || !secret) return;
    setBusy(true);
    setError("");
    try {
      const result = await api.openBackups(session, secret);
      if (!mounted.current) return;
      setSecret("");
      onSchedule(result.schedule);
    } catch (cause) {
      if (mounted.current) setError(protectionMessage(cause, t));
    } finally {
      if (mounted.current) setBusy(false);
    }
  }

  return (
    <form className={styles.tunnelFields} data-backup-opener onSubmit={(event) => { event.preventDefault(); void open(); }}>
      <label>
        <span>{t("protection.openLabel")}</span>
        <input type="password" autoComplete="off" spellCheck={false} value={secret} disabled={busy}
          onChange={(event) => setSecret(event.target.value)} data-backup-opener-secret
          {...(error === t("protection.openerWrong") ? { "aria-invalid": true as const, "aria-describedby": "backup-opener-error" } : {})} />
      </label>
      <div className={dataStyles.actions}>
        <button type="submit" className={styles.secondaryButton} disabled={busy || !secret} data-backup-opener-open>
          {t("protection.open")}
        </button>
      </div>
      <p className={styles.emailProviderMeta}>{t("protection.openHelp")}</p>
      {error ? <p id={errorId} className={styles.emailProviderMeta} role="alert" data-error>{error}</p> : null}
    </form>
  );
}

function protectionMessage(error: unknown, t: Translate) {
  if (error instanceof LeafenApiError) {
    if (error.code === administratorErrorCodes.backupOpenerWrong) return t("protection.openerWrong");
    if (error.code === administratorErrorCodes.backupPasswordWeak) return t("protection.weak");
    if (error.code === administratorErrorCodes.backupProtectionLast) return t("protection.last");
  }
  return productErrorMessage(error, "administratorScheduledBackup.save");
}
