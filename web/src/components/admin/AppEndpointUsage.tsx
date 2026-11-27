import { useState, type FormEvent } from "react";
import { useAttemptNotice } from "@/lib/useNoticeFor";
import { useTranslation } from "react-i18next";
import type { AppEndpointUsage as Usage } from "@/appPlatformWire.generated";
import { formatFileSize } from "@/components/database/files";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { appText, setAppEndpointLimits, setAppEndpointPublic } from "./administratorAppsApi";
import styles from "./AdministratorApps.module.css";
import usageStyles from "./AppUsageDialog.module.css";

/**
 * The usage window's "엔드포인트" for an app with endpoints: each one's
 * address — a public one with the switch that opens it without signing in —
 * the last day's requests from programs outside Leafen, what was refused,
 * sent and received, what its endpoints keep, and its limits, which the
 * administrator changes here.
 */
export function AppEndpointUsage({ session, installId, usage: first }: { session: Session; installId: string; usage: Usage }) {
  const { t, i18n } = useTranslation("serverConsole");
  const [usage, setUsage] = useState(first);
  const [draft, setDraft] = useState({
    perMinute: String(first.limits.perMinute), concurrent: String(first.limits.concurrent), personStorageMb: String(first.limits.personStorageMb),
  });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useAttemptNotice<string>(draft);
  const [saved, setSaved] = useState(false);

  async function save(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    setSaved(false);
    try {
      setUsage(await setAppEndpointLimits(session, installId, {
        perMinute: Number(draft.perMinute), concurrent: Number(draft.concurrent), personStorageMb: Number(draft.personStorageMb),
      }));
      setSaved(true);
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("apps.usage.endpointLimitsFailed")));
    } finally {
      setBusy(false);
    }
  }

  async function open(key: string, value: boolean) {
    setBusy(true);
    setError("");
    try {
      setUsage(await setAppEndpointPublic(session, installId, key, value));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("apps.usage.endpointPublicFailed")));
    } finally {
      setBusy(false);
    }
  }

  const field = (key: keyof typeof draft, label: string, max: number) => (
    <label className={usageStyles.limitField}>
      <span>{label}</span>
      <input type="number" min={key === "personStorageMb" ? 0 : 1} max={max} required value={draft[key]} disabled={busy}
        onChange={(event) => { setSaved(false); setDraft((current) => ({ ...current, [key]: event.target.value })); }} />
    </label>
  );
  return (
    <>
      <h3 className={styles.permissionsTitle}>{t("apps.usage.endpoints")}</h3>
      <ul className={usageStyles.endpointList} data-app-endpoint-list>
        {usage.endpoints.map((endpoint) => (
          <li key={endpoint.key} data-app-endpoint={endpoint.key}>
            <span>{appText(endpoint.name, i18n.language)}</span>
            <code>{endpoint.address}</code>
            {endpoint.public ? (
              <label className={usageStyles.endpointOpen}>
                <input type="checkbox" checked={endpoint.open} disabled={busy} data-app-endpoint-open
                  onChange={(event) => void open(endpoint.key, event.target.checked)} />
                <span>
                  <strong>{t("apps.usage.endpointPublic")}</strong>
                  <span>{t("apps.usage.endpointPublicNote")}</span>
                </span>
              </label>
            ) : null}
          </li>
        ))}
      </ul>
      <dl className={usageStyles.facts} data-app-usage-endpoints>
        <dt>{t("apps.usage.endpointRequests")}</dt>
        <dd>{t("apps.usage.endpointRequestsValue", { requests: usage.requests.toLocaleString(), refused: usage.refused.toLocaleString() })}</dd>
        <dt>{t("apps.usage.relayBytes")}</dt>
        <dd>{t("apps.usage.bytesValue", { sent: formatFileSize(usage.sentBytes), received: formatFileSize(usage.receivedBytes) })}</dd>
        <dt>{t("apps.usage.endpointStored")}</dt><dd>{formatFileSize(usage.storedBytes)}</dd>
      </dl>
      <form className={usageStyles.limits} onSubmit={(event) => void save(event)} data-app-endpoint-limits>
        {field("perMinute", t("apps.usage.endpointPerMinute"), 60000)}
        {field("concurrent", t("apps.usage.endpointConcurrent"), 64)}
        {field("personStorageMb", t("apps.usage.endpointPersonStorage"), 10 << 20)}
        <p className={styles.note}>{t("apps.usage.endpointLimitsNote", {
          perMinute: usage.defaults.perMinute, concurrent: usage.defaults.concurrent,
        })}</p>
        <div className={usageStyles.limitActions}>
          {saved ? <span className={styles.note} role="status">{t("apps.usage.endpointLimitsSaved")}</span> : null}
          <button type="submit" className={styles.secondary} disabled={busy}>{t("apps.usage.endpointLimitsSave")}</button>
        </div>
      </form>
      {error ? <div className={styles.error} role="alert">{error}</div> : null}
    </>
  );
}
