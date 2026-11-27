import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { isDeviceStoreLockWaitExceeded } from "@/lib/deviceStoreLock";
import { localEncryptionMode, type LocalEncryptionMode } from "@/lib/localLock";
import type { BrowserLocalLockSettings } from "@/lib/localLockBrowserSettings";
import type { LocalLockChangeResult } from "@/lib/localLockSettings";
import { styles } from "./accountSettingsStyles";
import { AccountSecurityDialog } from "./AccountSecurityDialog";
import dialogStyles from "./AccountSecurityDialog.module.css";
import { useAttemptNotice } from "@/lib/useNoticeFor";
import { thisWindowKey } from "@/lib/thisWindowWords";

export function LocalLockSecurityPanel({
  settings,
}: {
  settings: BrowserLocalLockSettings;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  // "이 브라우저에서는" or, in Leafen's own app window, "이 앱에서는" (thisWindowWords.ts).
  const unavailable = thisWindowKey("workspaceSettingsDialog:lockUnavailable", "workspaceSettingsDialog:lockUnavailableApp");
  const [mode, setMode] = useState<LocalEncryptionMode>(() => localEncryptionMode());
  const [enablePass, setEnablePass] = useState("");
  const [enableConfirm, setEnableConfirm] = useState("");
  const [currentPass, setCurrentPass] = useState("");
  const [nextPass, setNextPass] = useState("");
  const [nextConfirm, setNextConfirm] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useAttemptNotice<string>(enablePass, enableConfirm, currentPass, nextPass, nextConfirm);
  const [notice, setNotice] = useState("");
  // Which window is open: turning the lock on, changing its passphrase, or
  // turning it off — each from its own button, as two-step verification
  // offers its own (2026-10-16: turning it off was only inside the change).
  const [open, setOpen] = useState<"enable" | "change" | "disable" | null>(null);

  // The field a refusal is about, marked with its line (rejected-entry focus).
  const errorField = !error ? "" : error === t("passphraseTooShort") ? "enable" : error === t("passphraseMismatch") ? "enableConfirm"
    : error === t("newPassphraseTooShort") ? "next" : error === t("newPassphraseMismatch") ? "nextConfirm"
      : error === t("enterCurrentPassphrase") || error === t("lockWrongPassphrase") ? "current" : "";
  const refused = (field: string) => errorField === field ? { "aria-invalid": true as const, "aria-describedby": "local-lock-error" } : {};

  function changeOpen(next: "enable" | "change" | "disable" | null) {
    setEnablePass(""); setEnableConfirm(""); setCurrentPass(""); setNextPass(""); setNextConfirm("");
    setError(""); setOpen(next);
  }

  function failure(result: LocalLockChangeResult): string {
    if (result === "pending-changes") return t("lockPendingChanges");
    if (result === "wrong-passphrase") return t("lockWrongPassphrase");
    return t(unavailable);
  }

  async function run(
    operation: () => Promise<LocalLockChangeResult>,
    onSuccess: () => void,
  ) {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const result = await operation();
      if (result !== "ok") {
        setError(failure(result));
        return;
      }
      onSuccess();
      changeOpen(null);
    } catch (caught) {
      // Another tab kept this device's store past the wait (deviceStoreLock.ts).
      setError(t(isDeviceStoreLockWaitExceeded(caught) ? "lockStoreBusy" : unavailable));
    } finally {
      setBusy(false);
    }
  }

  function onEnable(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (enablePass.length < 8) return setError(t("passphraseTooShort"));
    if (enablePass !== enableConfirm) return setError(t("passphraseMismatch"));
    void run(() => settings.enable(enablePass), () => {
      setMode("passphrase");
      setEnablePass("");
      setEnableConfirm("");
      setNotice(t("lockEnabledNotice"));
    });
  }

  function onChangePassphrase(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (nextPass.length < 8) return setError(t("newPassphraseTooShort"));
    if (nextPass !== nextConfirm) return setError(t("newPassphraseMismatch"));
    void run(() => settings.change(currentPass, nextPass), () => {
      setCurrentPass("");
      setNextPass("");
      setNextConfirm("");
      setNotice(t("passphraseChangedNotice"));
    });
  }

  function onDisable() {
    if (!currentPass) return setError(t("enterCurrentPassphrase"));
    void run(() => settings.disable(currentPass), () => {
      setMode("device");
      setCurrentPass("");
      setNotice(t("lockDisabledNotice"));
    });
  }

  return (
    <div className={styles.securityPanel} data-testid="local-lock-panel">
      <div className={styles.securityRow}>
        <div>
          <strong>{t("localLockTitle")}</strong>
          <span>{t(mode === "passphrase" ? "localLockOnDesc" : "localLockOffDesc")}</span>
        </div>
        {mode === "passphrase" ? (
          <div className={styles.sectionActions}>
            <button type="button" className={styles.secondaryButton} disabled={busy} onClick={() => changeOpen("change")}>
              {t("changePassphrase")}
            </button>
            <button type="button" className={styles.secondaryButton} disabled={busy} onClick={() => changeOpen("disable")}
              data-testid="local-lock-off">
              {t("lockOff")}
            </button>
          </div>
        ) : (
          <button type="button" className={styles.secondaryButton} disabled={busy} onClick={() => changeOpen("enable")}>
            {t("lockOn")}
          </button>
        )}
      </div>
      {/* As the page's other notices (2026-11-06: a bare line in the body's 16px). */}
      {notice ? <div className={styles.notice} data-tone="neutral" role="status">{notice}</div> : null}
      {open ? <AccountSecurityDialog title={t(open === "disable" ? "lockOff" : open === "change" ? "changePassphrase" : "localLockTitle")}
        busy={busy} onClose={() => changeOpen(null)}>
      {open !== "disable" ? (
        <div className={styles.notice} data-testid="local-lock-forget-warning">
          {t("localLockForgetWarning")}
        </div>
      ) : null}
      {open === "enable" ? (
        <form className={styles.passwordChangeForm} data-security-form onSubmit={onEnable}>
          <label>
            <span>{t("lockPassphrase")}</span>
            <input
              value={enablePass}
              aria-label={t("ariaLocalLockPassphrase")}
              type="password"
              autoComplete="new-password"
              onChange={(event) => setEnablePass(event.target.value)}
              disabled={busy}
              {...refused("enable")}
            />
          </label>
          <label>
            <span>{t("confirmPassphrase")}</span>
            <input
              value={enableConfirm}
              aria-label={t("ariaConfirmLocalLockPassphrase")}
              type="password"
              autoComplete="new-password"
              onChange={(event) => setEnableConfirm(event.target.value)}
              disabled={busy}
              {...refused("enableConfirm")}
            />
          </label>
          <button type="submit" className={styles.primaryButton} disabled={busy}>
            {t("lockOn")}
          </button>
        </form>
      ) : open === "disable" ? (
        <form className={styles.passwordChangeForm} data-security-form onSubmit={(event) => { event.preventDefault(); onDisable(); }}>
          <label>
            <span>{t("currentPassphrase")}</span>
            <input
              value={currentPass}
              aria-label={t("ariaCurrentLocalLockPassphrase")}
              type="password"
              autoComplete="current-password"
              onChange={(event) => setCurrentPass(event.target.value)}
              disabled={busy}
              {...refused("current")}
            />
          </label>
          <button type="submit" className={styles.primaryButton} data-testid="local-lock-disable" disabled={busy}>
            {t("lockOff")}
          </button>
        </form>
      ) : (
        <form className={styles.passwordChangeForm} data-security-form onSubmit={onChangePassphrase}>
          <label>
            <span>{t("currentPassphrase")}</span>
            <input
              value={currentPass}
              aria-label={t("ariaCurrentLocalLockPassphrase")}
              type="password"
              autoComplete="current-password"
              onChange={(event) => setCurrentPass(event.target.value)}
              disabled={busy}
              {...refused("current")}
            />
          </label>
          <label>
            <span>{t("newPassphrase")}</span>
            <input
              value={nextPass}
              aria-label={t("ariaNewLocalLockPassphrase")}
              type="password"
              autoComplete="new-password"
              onChange={(event) => setNextPass(event.target.value)}
              disabled={busy}
              {...refused("next")}
            />
          </label>
          <label>
            <span>{t("confirmNewPassphrase")}</span>
            <input
              value={nextConfirm}
              aria-label={t("ariaConfirmNewLocalLockPassphrase")}
              type="password"
              autoComplete="new-password"
              onChange={(event) => setNextConfirm(event.target.value)}
              disabled={busy}
              {...refused("nextConfirm")}
            />
          </label>
          <button type="submit" className={styles.primaryButton} disabled={busy}>
            {t("changePassphrase")}
          </button>
        </form>
      )}
      {error ? <p id="local-lock-error" role="alert">{error}</p> : null}
      <div className={dialogStyles.footer}><button type="button" className={styles.secondaryButton} disabled={busy} onClick={() => changeOpen(null)}>{t("managedUsersCancel")}</button></div>
      </AccountSecurityDialog> : null}
    </div>
  );
}
