import { useTranslation } from "react-i18next";
import type { AdministratorSettings, AdministratorSettingsInput } from "../../administratorWire.generated";
import { AppSelect } from "@/components/AppSelect";
import type { Session } from "../../types";
import { AdministratorSMTPTest } from "./AdministratorSMTPTest";
import { SettingField, SettingRow, SettingSwitch } from "./AdministratorSettingRows";
import own from "./AdministratorSettingRows.module.css";
import styles from "./AdministratorSettingsPage.module.css";

export type SmtpFieldError = { field: "host" | "senderAddress" | "password"; message: string };

/** 설정 › 이메일 전송: off, it is one switch; on, what it needs, one row each, and a test once saved. */
export function AdministratorEmailSettings({ session, settings, smtp, busy, fieldError, onChange, saved, about }: {
  session: Session;
  settings: AdministratorSettings;
  smtp: AdministratorSettingsInput["smtp"];
  busy: boolean;
  fieldError: SmtpFieldError | null;
  onChange(change: Partial<AdministratorSettingsInput["smtp"]>): void;
  /** What's shown is what's saved: a test mail tells of this. */
  saved: boolean;
  about: string;
}) {
  const { t } = useTranslation("serverConsole");
  const errorOf = (field: SmtpFieldError["field"]) => fieldError?.field === field ? fieldError.message : "";
  return (
    <>
      <section className={styles.section} aria-labelledby="administrator-email-title">
        <div id="administrator-email-title" className={styles.sectionTitle}>{t("email.smtpSection")}</div>
        <div className={styles.settingRows}>
          <SettingSwitch title={t("email.useSmtp")} description={t("email.useSmtpBody")} on={smtp.enabled} disabled={busy}
            onChange={(enabled) => onChange({ enabled })} data-administrator-smtp-on={smtp.enabled ? "true" : "false"} />
          {/* Verification needs mail: while it is on, its switch stays to turn
              off with SMTP turned off, as the refusal says to (2026-11-06: it
              hid with SMTP, and the refusal named nothing to do). */}
          {smtp.enabled || smtp.requireEmailVerification ? (
            <SettingSwitch title={t("email.requireVerification")} description={t("email.requireVerificationBody")} on={smtp.requireEmailVerification} disabled={busy}
              onChange={(requireEmailVerification) => onChange({ requireEmailVerification })} />
          ) : null}
          {smtp.enabled ? (
            <>
              <SettingField title={t("email.host")} error={errorOf("host")}>
                <input value={smtp.host} placeholder="smtp.example.com" onChange={(event) => onChange({ host: event.target.value })} disabled={busy} />
              </SettingField>
              <SettingField title={t("email.port")}>
                <input className={own.short} type="number" min="1" max="65535" value={smtp.port} onChange={(event) => onChange({ port: Number(event.target.value) })} disabled={busy} />
              </SettingField>
              <SettingField title={t("email.username")} description={t("email.noSignInHint")}>
                <input value={smtp.username} autoComplete="username" onChange={(event) => onChange({ username: event.target.value })} disabled={busy} />
              </SettingField>
              <SettingField title={t("email.password")} description={t("email.secretHelp")} error={errorOf("password")}>
                <input type="password" autoComplete="new-password" value={smtp.password ?? ""} placeholder={settings.smtp.passwordConfigured ? t("providers.secretSet") : ""}
                  onChange={(event) => onChange({ password: event.target.value, clearPassword: false })} disabled={busy} />
              </SettingField>
              {settings.smtp.passwordConfigured ? (
                <SettingSwitch title={t("email.clearPassword")} on={smtp.clearPassword} disabled={busy || smtp.username.trim() !== ""}
                  onChange={(clearPassword) => onChange({ clearPassword, password: null })} />
              ) : null}
              <SettingRow title={t("email.encryption")}>
                <AppSelect look="setting" aria-label={t("email.encryption")} value={smtp.tlsMode} disabled={busy}
                  onChange={(event) => onChange({ tlsMode: event.target.value as AdministratorSettingsInput["smtp"]["tlsMode"] })}>
                  <option value="starttls">STARTTLS</option>
                  <option value="tls">TLS</option>
                  <option value="none">{t("email.encryptionNone")}</option>
                </AppSelect>
              </SettingRow>
              <SettingField title={t("email.senderName")}>
                <input value={smtp.senderName} onChange={(event) => onChange({ senderName: event.target.value })} disabled={busy} />
              </SettingField>
              <SettingField title={t("email.senderAddress")} error={errorOf("senderAddress")}>
                <input type="email" value={smtp.senderAddress} placeholder="noreply@example.com" onChange={(event) => onChange({ senderAddress: event.target.value })} disabled={busy} />
              </SettingField>
            </>
          ) : null}
        </div>
      </section>
      {smtp.enabled ? <AdministratorSMTPTest session={session} saved={saved} about={about} /> : null}
    </>
  );
}
