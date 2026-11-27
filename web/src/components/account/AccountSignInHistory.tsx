import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { request } from "@/apiTransport";
import type { AuthAuditEvent, AuthAuditHistory } from "@/authWire.generated";
import { authAuditMethodText, authAuditReasonText, authAuditSignInText } from "@/lib/authAuditText";
import { displayDateTime } from "@/lib/displayDate";
import { userErrorText } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { sessionDevice } from "./AccountDevicesSection";
import { styles } from "./accountSettingsStyles";
import { ListNotice } from "@/components/settings/SettingsRowNotice";

const SHOWN = 10;

// As the devices beside it (2026-10-26: "2026. 10. 26. 오후 10:23" here, "2026년 10월 26일 오후 10:23" there).
function moment(value: string) {
  return displayDateTime(value) || value;
}

/**
 * One's own sign-ins (the audit decision of 2026-10-21 20:05, 2), beside the
 * devices: each attempt's way, whether it went in and why not, the device
 * and when, newest first (the server's auth_audit.go). It reads again when
 * the devices beside it do (2026-10-26: a device that joined showed in the
 * table and not here until the settings opened again).
 */
export function AccountSignInHistory({ session, devices }: { session: Session; devices?: unknown }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [events, setEvents] = useState<AuthAuditEvent[]>([]);
  const [cursor, setCursor] = useState("");
  const [loaded, setLoaded] = useState(false);
  // A failed read, with the window it was for, said with a way to read it again.
  const [message, setMessage] = useState<{ text: string; after: string } | null>(null);

  const load = (after: string, signal?: AbortSignal) => request<AuthAuditHistory>(
    `/account/auth-audit?${new URLSearchParams({ limit: String(SHOWN), ...(after ? { cursor: after } : {}) })}`, { signal }, session,
  ).then((page) => {
    setEvents((current) => (after ? [...current, ...page.events] : page.events));
    setCursor(page.nextCursor ?? "");
    setLoaded(true);
    setMessage(null);
  }, (caught) => { if (!signal?.aborted) setMessage({ text: userErrorText(caught, t("signInHistory.failed")), after }); });

  useEffect(() => {
    const controller = new AbortController();
    void load("", controller.signal);
    return () => controller.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session, devices]);

  return (
    <section className={styles.section} role="region" aria-label={t("signInHistory.title")} data-sign-in-history>
      <div className={styles.sectionTitle}>{t("signInHistory.title")}</div>
      <div className={styles.organizationMeta}>{t("signInHistory.description")}</div>
      {message ? (
        <ListNotice role="alert" onRetry={() => { setMessage(null); void load(message.after); }} data-sign-in-read-failed>{message.text}</ListNotice>
      ) : null}
      <div className={styles.securityPanel}>
        {loaded && events.length === 0 ? <div className={styles.organizationMeta}>{t("signInHistory.empty")}</div> : null}
        {events.map((event) => (
          <div key={event.id} className={styles.securityRow} data-sign-in={event.outcome}>
            <div className={styles.securityInstruction}>
              <strong>{authAuditSignInText(t, event.outcome, event.phase)}{" · "}{authAuditMethodText(t, event.method)}</strong>
              <span>{[event.reasonCode ? t("accountHistory.reason", { reason: authAuditReasonText(t, event.method, event.reasonCode, event.reasonStatus) }) : "",
                sessionDevice(event.userAgent ?? "")?.name ?? "", event.ip ?? ""].filter(Boolean).join(" · ")}</span>
            </div>
            <time className={styles.organizationMeta} dateTime={event.occurredAt}>{moment(event.occurredAt)}</time>
          </div>
        ))}
      </div>
      {cursor ? (
        <button type="button" className={styles.secondaryButton} onClick={() => void load(cursor)} data-sign-in-more>{t("signInHistory.more")}</button>
      ) : null}
    </section>
  );
}
