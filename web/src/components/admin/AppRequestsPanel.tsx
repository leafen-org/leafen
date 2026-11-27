import { useTranslation } from "react-i18next";
import type { AppRequest } from "@/appPlatformWire.generated";
import { appText } from "./administratorAppsApi";
import styles from "./AdministratorApps.module.css";
import { displayDate } from "@/lib/displayDate";

/**
 * People's requests for apps waiting on the administrators (docs/app-platform.md
 * 3): who asked, in which organization, for which app and why. Approving an
 * app not yet installed opens its review first; installing then lets the
 * person use it.
 */
export function AppRequestsPanel({ requests, busy, onApprove, onDecline }: {
  requests: AppRequest[];
  busy: boolean;
  onApprove: (request: AppRequest) => void;
  onDecline: (request: AppRequest) => void;
}) {
  const { t, i18n } = useTranslation("serverConsole");
  if (!requests.length) return null;
  return (
    <div className={styles.requests} data-app-requests>
      <h3 className={styles.permissionsTitle}>{t("apps.requests.title", { count: requests.length })}</h3>
      {requests.map((request) => (
        <div key={request.id} className={`${styles.jobRow} ${styles.requestRow}`} data-app-request={request.id}>
          <span className={styles.secretName}>
            <strong>{appText(request.appName, i18n.language)}</strong>
            <span>{[request.personName, request.tenancyName,
              displayDate(request.created)].filter(Boolean).join(" · ")}</span>
            {request.reason ? <span className={styles.requestReason}>{request.reason}</span> : null}
          </span>
          <button type="button" className={styles.secondary} disabled={busy} data-app-request-decline
            onClick={() => onDecline(request)}>{t("apps.requests.decline")}</button>
          <button type="button" className={styles.primary} disabled={busy} data-app-request-approve
            onClick={() => onApprove(request)}>{t(request.installedId ? "apps.requests.approve" : "apps.requests.review")}</button>
        </div>
      ))}
    </div>
  );
}
