import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { AppPersonCredential, AppPersonCredentialList } from "@/appPlatformWire.generated";
import { appText } from "@/components/admin/administratorAppsApi";
import { activeDateLocale } from "@/lib/i18n";
import { keptAppsList, useAppsListReading } from "@/lib/appsChangedEvents";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { readPersonCredentials, removePersonCredential } from "./personAppsApi";
import adminStyles from "@/components/admin/AdministratorApps.module.css";
import styles from "./PersonAppsPage.module.css";

/**
 * What the person gave apps (the mail decision of 2026-10-07): each app's
 * credential with when it was last used, and a button to take it back —
 * the person removes theirs whenever they like.
 */
export function PersonAppCredentials({ session }: { session: Session }) {
  const { t, i18n } = useTranslation("common");
  // The list read last in this tab shows at once, the server's answer replacing it.
  const [credentials, setCredentials] = useState<AppPersonCredential[]>(
    () => keptAppsList<AppPersonCredentialList>(session, "credentials")?.credentials ?? []);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");

  // Read again when an app is installed, removed or given to the person.
  const adopt = useAppsListReading(session, "credentials", readPersonCredentials, (list) => setCredentials(list.credentials));

  async function remove(credential: AppPersonCredential) {
    const id = `${credential.installId}/${credential.key}`;
    setBusy(id);
    setError("");
    try {
      adopt(await removePersonCredential(session, credential.installId, credential.key));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("appPeople.credentials.removeFailed")));
    } finally {
      setBusy("");
    }
  }

  if (!credentials.length && !error) return null;
  const when = (at: number) => at
    ? new Date(at).toLocaleString(activeDateLocale(), { dateStyle: "long", timeStyle: "short" }) : t("appPeople.credentials.unused");
  return (
    <section className={styles.credentials} data-person-app-credentials>
      <h2 className={styles.heading}>{t("appPeople.credentials.title")}</h2>
      <p className={styles.note}>{t("appPeople.credentials.note")}</p>
      {error ? <div className={adminStyles.error} role="alert">{error}</div> : null}
      <ul className={styles.credentialList}>
        {credentials.map((credential) => (
          <li key={`${credential.installId}/${credential.key}`} className={styles.credential}
            data-person-app-credential={`${credential.installId}/${credential.key}`}>
            <span className={styles.credentialText}>
              <span className={styles.credentialName}>{appText(credential.appName, i18n.language)} · {appText(credential.name, i18n.language)}</span>
              <span className={styles.note}>{t("appPeople.credentials.used", { when: when(credential.usedAt) })}</span>
            </span>
            <button type="button" className={adminStyles.secondary} disabled={busy !== ""} data-person-app-credential-remove
              onClick={() => void remove(credential)}>{t("appPeople.credentials.remove")}</button>
          </li>
        ))}
      </ul>
    </section>
  );
}
