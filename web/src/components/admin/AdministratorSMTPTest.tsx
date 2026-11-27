import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { authErrorCodes } from "@/authWire.generated";
import { userErrorText } from "@/lib/settingsErrors";
import { useAttemptNotice } from "@/lib/useNoticeFor";
import { api } from "../../api";
import type { Session } from "../../types";
import { SettingField } from "./AdministratorSettingRows";
import styles from "./AdministratorSettingsPage.module.css";

// What a failed test says, by the step the server says failed.
const SMTP_FAILURE_TEXT: Record<string, string> = {
  [authErrorCodes.emailServerUnreachable]: "emailProviderTestUnreachable",
  [authErrorCodes.emailServerTimeout]: "emailProviderTestTimeout",
  [authErrorCodes.emailEncryptionFailed]: "emailProviderTestEncryption",
  [authErrorCodes.emailSignInRefused]: "emailProviderTestSignIn",
  [authErrorCodes.emailSenderRefused]: "emailProviderTestSender",
  [authErrorCodes.emailRecipientRefused]: "emailProviderTestRecipientRefused",
};

/**
 * Sends a test message through the saved SMTP settings, to the address
 * given or else to the signed-in administrator. A failure says which step
 * failed and what to check, as an error (2026-10-26: it said only that it
 * couldn't be tested, in the description's gray). The result holds only
 * while the settings it tested and the address it went to stay as they were.
 */
export function AdministratorSMTPTest({ session, saved, about }: { session: Session; saved: boolean; about: string }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [recipient, setRecipient] = useState("");
  const [sending, setSending] = useState(false);
  const [result, setResult] = useAttemptNotice<{ tone: "neutral" | "error"; message: string; recipientRefused?: boolean }>(about, recipient);
  const resultId = useId();

  async function send() {
    setSending(true);
    setResult(null);
    try {
      const sent = await api.sendAdministratorSMTPTest(session, { recipient: recipient.trim() });
      setResult({
        tone: "neutral",
        message: sent.outcome === "accepted" ? t("emailProviderTestSucceeded") : t("emailProviderTestIncomplete"),
      });
    } catch (cause) {
      const code = (cause as { code?: unknown } | null)?.code;
      const failure = typeof code === "string" ? SMTP_FAILURE_TEXT[code] : undefined;
      const reason = code === "LEAFEN_INVALID_INPUT" ? t("emailProviderTestRecipientInvalid")
        : code === "LEAFEN_ADMINISTRATOR_SETTINGS_INVALID" ? t("emailProviderTestSettingsMissing") : t("emailProviderTestFailed");
      setResult({ tone: "error", message: failure ? t(failure) : userErrorText(cause, reason), recipientRefused: code === "LEAFEN_INVALID_INPUT" });
    } finally {
      setSending(false);
    }
  }

  return (
    <section className={styles.section} aria-labelledby="administrator-smtp-test-title" data-administrator-smtp-test>
      <div id="administrator-smtp-test-title" className={styles.sectionTitle}>{t("emailProviderSendTest")}</div>
      <div className={styles.settingRows}>
        <SettingField title={t("emailProviderTestRecipient")} description={saved ? undefined : t("emailProviderTestUnsaved")}>
          <input type="email" value={recipient} placeholder={session.user.email} autoComplete="off"
            aria-invalid={result?.recipientRefused ? true : undefined} aria-describedby={result?.recipientRefused ? resultId : undefined}
            onChange={(event) => setRecipient(event.target.value)} disabled={sending} />
        </SettingField>
        <div className={styles.settingEnd}>
          {result ? (
            <p id={resultId} className={styles.emailProviderMeta} role={result.tone === "error" ? "alert" : "status"}
              data-error={result.tone === "error" ? "" : undefined}>{result.message}</p>
          ) : null}
          <button type="button" className={styles.secondaryButton} onClick={() => void send()} disabled={sending || !saved}>
            {sending ? t("emailProviderTestSending") : t("emailProviderSendTest")}
          </button>
        </div>
      </div>
    </section>
  );
}
