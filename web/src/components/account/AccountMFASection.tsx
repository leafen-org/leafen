import { type FormEvent, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useCopyNotice } from "@/lib/copyNotice";
import type { useAccountSecurity } from "./useAccountSecurity";
import { AccountSecurityDialog } from "./AccountSecurityDialog";
import { styles } from "./accountSettingsStyles";
import dialogStyles from "./AccountSecurityDialog.module.css";
import { triggerBrowserFileDownload } from "@/lib/browserFileDownload";

type MFAAction = "setup" | "disable" | "recovery";
export function AccountMFASection({ security, onOpenChange }: {
  security: ReturnType<typeof useAccountSecurity>; onOpenChange(open: boolean): void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const copy = useCopyNotice();
  const [action, setAction] = useState<MFAAction | null>(null);
  const enabled = security.snapshot?.mfa.factors.some((factor) => factor.type === "totp" && factor.verified) ?? false;
  const busy = Boolean(security.busy);
  useEffect(() => () => { security.clearMFAInput(); onOpenChange(false); }, []);
  function changeAction(next: MFAAction | null) {
    security.clearMFAInput(); setAction(next); onOpenChange(next !== null);
    if (next === "setup") void security.startEnrollment();
  }
  function submitEnrollment(event: FormEvent) { event.preventDefault(); void security.confirmEnrollment(); }
  async function submitDisable(event: FormEvent) {
    event.preventDefault(); if (await security.disableTOTP()) changeAction(null);
  }
  function submitRecoveryReplacement(event: FormEvent) { event.preventDefault(); void security.regenerateRecoveryCodes(); }
  function downloadRecoveryCodes() {
    const blob = new Blob([`${security.recoveryCodes.join("\n")}\n`], { type: "text/plain;charset=utf-8" });
    void triggerBrowserFileDownload(blob, "leafen-recovery-codes.txt", { revokeAfterMs: 1000 });
  }
  const title = action === "disable" ? t("mfaTurnOff") : action === "recovery" ? t("manageRecoveryTitle") : t("mfaTurnOn");
  return <div className={styles.securityPanel} data-account-mfa>
    <div className={styles.securityRow}>
      <div className={styles.securityInstruction}><strong>{t("mfaTitle")}</strong>
        {/* On or off, and its buttons, are the account read's: none until it is in (2026-11-20). */}
        {security.snapshot ? <span>{enabled ? `${t("mfaOn")} · ${t("mfaEnabledDesc")}` : `${t("mfaOff")} · ${t("mfaDisabledDesc")}`}</span> : null}</div>
      {security.snapshot ? <div className={styles.sectionActions}>
        {enabled ? <button className={styles.secondaryButton} disabled={security.loading || busy} onClick={() => changeAction("recovery")}>{t("manageRecoveryTitle")}</button> : null}
        {/* One button turns it on and off, so the focus its dialog gives back
            finds it either way (2026-10-28: turned off, it went to the settings search). */}
        <button className={styles.secondaryButton} disabled={security.loading || busy} onClick={() => changeAction(enabled ? "disable" : "setup")}>{enabled ? t("mfaTurnOff") : t("mfaTurnOn")}</button>
      </div> : null}
    </div>
    {action ? <AccountSecurityDialog title={title} busy={busy} wide onClose={() => changeAction(null)}>
      {action === "disable" ? <form data-security-form onSubmit={(event) => void submitDisable(event)}>
        <p>{t("mfaEnabledDesc")}</p>
        <input value={security.disableCode} aria-label={t("ariaAuthCodeOrPassword")} placeholder={t("codeOrPasswordPlaceholder")} autoComplete="current-password" onChange={(event) => security.setDisableCode(event.target.value)} disabled={busy} />
        <button type="submit" className={styles.secondaryButton} disabled={busy}>{t("mfaTurnOff")}</button>
      </form> : null}
      {action === "setup" && !security.enrollment && !security.recoveryCodes.length ? <>
        <p>{busy ? t("setupInProgress") : t("mfaSetupIntro")}</p>
        {!busy ? <button className={styles.secondaryButton} onClick={() => void security.startEnrollment()}>{t("mfaTurnOn")}</button> : null}
      </> : null}
              {security.enrollment ? (
                <form className={styles.securitySetup} onSubmit={submitEnrollment} aria-label={t("mfaSetupAriaLabel")}>
                  {/* The dialog's title names the step; it isn't said again inside (2026-10-16). */}
                  <div className={styles.securityInstruction}><span>{t("mfaSetupIntro")}</span></div>
                  <div className={styles.totpStep}>
                    <span className={styles.totpStepMarker}>1</span>
                    <div className={styles.totpStepBody}>
                      <strong>{t("scanQrTitle")}</strong><span>{t("scanQrDesc")}</span>
                      <div className={styles.totpSetupGrid}>
                        <div className={styles.totpQrCard}>
                          {security.qrCodeDataUrl ? <img src={security.qrCodeDataUrl} alt={t("qrAlt")} /> : <span>{security.qrCodeError || t("qrCreating")}</span>}
                        </div>
                        <div className={styles.totpFallback}>
                          <details className={styles.securityDetails}>
                            <summary>{t("qrFallbackSummary")}</summary>
                            <div className={styles.securitySecretRow}>
                              <label className={styles.securitySecret}><span>{t("manualKeyLabel")}</span><input readOnly value={security.enrollment.secret} aria-label={t("ariaAuthSecret")} /></label>
                              <button type="button" className={styles.secondaryButton} onClick={() => void copy(security.enrollment?.secret ?? "")}>{t("copyKey")}</button>
                            </div>
                            <details className={styles.securityDetails}>
                              <summary>{t("advancedUriSummary")}</summary>
                              <div className={styles.securitySecretRow}>
                                <label className={styles.securitySecret}><span>{t("setupUriLabel")}</span><input readOnly value={security.enrollment.qrCodeUri} aria-label={t("ariaAuthSetupUri")} /></label>
                                <button type="button" className={styles.secondaryButton} onClick={() => void copy(security.enrollment?.qrCodeUri ?? "")}>{t("copyUri")}</button>
                              </div>
                            </details>
                          </details>
                        </div>
                      </div>
                    </div>
                  </div>
                  <div className={styles.totpStep}>
                    <span className={styles.totpStepMarker}>2</span>
                    <div className={styles.totpStepBody}>
                      <strong>{t("enterCodeTitle")}</strong><span>{t("enterCodeDesc")}</span>
                      <div className={styles.totpApplyFields}>
                        <input value={security.setupCode} aria-label={t("ariaSetupVerificationCode")} placeholder="123456" inputMode="numeric" autoComplete="one-time-code" maxLength={6} onChange={(event) => security.setSetupCode(event.target.value.replace(/\D/gu, "").slice(0, 6))} disabled={security.busy === "mfa:verify"}
                          aria-invalid={security.error ? true : undefined} aria-describedby={security.error ? "account-mfa-error" : undefined} />
                        <div className={styles.totpActionRow}><button type="submit" className={styles.primaryButton} disabled={security.busy === "mfa:verify"}>{t("applyVerification")}</button></div>
                      </div>
                    </div>
                  </div>
                </form>
              ) : null}

              {security.recoveryCodes.length ? (
                <div className={styles.recoveryCodes} data-account-recovery-codes="true">
                  <div className={styles.recoveryHeader}>
                    <div><strong>{t("recoveryCodesTitle")}</strong><span>{t("recoveryCodesDesc")}</span></div>
                    <span className={styles.recoveryActions}>
                      <button type="button" className={styles.secondaryButton} onClick={() => void copy(security.recoveryCodes.join("\n"))}>{t("copyCodes")}</button>
                      <button type="button" className={styles.secondaryButton} onClick={downloadRecoveryCodes}>{t("saveAsText")}</button>
                    </span>
                  </div>
                  <code>{security.recoveryCodes.join("\n")}</code><span>{t("recoveryCodesKeepSafe")}</span>
                </div>
              ) : null}

              {action === "recovery" && !security.recoveryCodes.length ? (
                <form className={styles.securitySetup} onSubmit={submitRecoveryReplacement}>
                  <div className={styles.securityInstruction}><span>{t("manageRecoveryDesc")}</span></div>
                  <div className={styles.securityInlineForm}>
                    <input value={security.recoveryConfirm} aria-label={t("ariaRecoveryRegenConfirm")} placeholder={t("codeOrPasswordPlaceholder")} autoComplete="current-password" onChange={(event) => security.setRecoveryConfirm(event.target.value)} disabled={security.busy === "mfa:recovery-codes"} />
                    <button type="submit" className={styles.secondaryButton} disabled={security.busy === "mfa:recovery-codes"}>{t("regenerateCodes")}</button>
                  </div>
                </form>
              ) : null}
      {security.error ? <p id="account-mfa-error" role="alert">{security.error}</p> : null}
      <div className={dialogStyles.footer}><button type="button" className={styles.secondaryButton} disabled={busy} onClick={() => changeAction(null)}>{t(security.recoveryCodes.length ? "common:actions.close" : "managedUsersCancel")}</button></div>
    </AccountSecurityDialog> : null}
  </div>;
}
