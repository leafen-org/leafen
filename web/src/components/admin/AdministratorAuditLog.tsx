import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { userErrorText } from "@/lib/settingsErrors";
import { api } from "../../api";
import type { AdministratorAuditEvent, AdministratorAuditKind } from "../../administratorWire.generated";
import { styles as auditStyles } from "../settings/workspaceSettingsOverviewStyles";
import { administratorAuditDetails, administratorAuditLabelKey } from "./administratorAuditModel";
import styles from "./AdministratorSettingsPage.module.css";
import type { Session } from "../../types";
import { SettingsSectionTitle } from "@/components/settings/SettingsSectionTitle";
import { displayDateTime } from "@/lib/displayDate";
import { AppSelect } from "@/components/AppSelect";
import { EmptyLineReadFailed } from "@/components/settings/SettingsRowNotice";

/**
 * The server audit log: administrator actions on accounts, server setting
 * changes and sign-in attempts, newest first, a window at a time.
 */
export function AdministratorAuditLog({ session }: { session: Session }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [kind, setKind] = useState<AdministratorAuditKind | "">("");
  const [events, setEvents] = useState<AdministratorAuditEvent[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  // A failed read, said where the log shows with a way to read again: the
  // first window in the empty line's place, a further one after the events.
  const [error, setError] = useState("");
  const [failedCursor, setFailedCursor] = useState<string | undefined>();
  const abortRef = useRef<AbortController | null>(null);

  async function load(nextKind: AdministratorAuditKind | "", cursor?: string) {
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    setLoading(true);
    setError("");
    try {
      const page = await api.administratorAuditLog(session, { kind: nextKind || undefined, cursor }, controller.signal);
      if (controller.signal.aborted) return;
      setEvents((current) => cursor ? [...current, ...page.items] : page.items);
      setNextCursor(page.nextCursor);
    } catch (cause) {
      if (!controller.signal.aborted) {
        setError(userErrorText(cause, t("errLoadAuditLog")));
        setFailedCursor(cursor);
        // A first window that failed leaves nothing of the last one, which
        // was another filter's or an older read (2026-11-20: the old filter's
        // events stood over the failure).
        if (!cursor) { setEvents([]); setNextCursor(null); }
      }
    } finally {
      if (!controller.signal.aborted) setLoading(false);
    }
  }

  useEffect(() => {
    void load(kind);
    return () => abortRef.current?.abort();
    // The log reloads when the filter or the session changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kind, session]);

  return (
    <section className={styles.section} aria-labelledby="administrator-audit-title">
      <SettingsSectionTitle id="administrator-audit-title" className={styles.sectionTitle} restates>{t("serverAuditTitle")}</SettingsSectionTitle>
      {/* The filter and the log's own refresh in one row (2026-11-05: the refresh alone in an empty head). */}
      <div className={`${auditStyles.auditTools} ${styles.listTools}`}>
        <AppSelect value={kind} aria-label={t("ariaFilterServerAudit")} onChange={(event) => setKind(event.target.value as AdministratorAuditKind | "")}>
          <option value="">{t("auditAllEvents")}</option>
          <option value="account">{t("navAccountsSignup")}</option>
          <option value="settings">{t("auditSettingsUpdate")}</option>
          <option value="sign_in">{t("auditLoginAttempt")}</option>
        </AppSelect>
        <button type="button" className={styles.secondaryButton} onClick={() => void load(kind)} disabled={loading}>{t("refresh")}</button>
      </div>
      {events.length ? (
        <div className={auditStyles.auditList}>
          {events.map((event) => (
            <div key={`${event.kind}:${event.id}`} className={auditStyles.auditRow} data-outcome={event.outcome || undefined}>
              <span className={auditStyles.auditText}>
                <strong>{t(administratorAuditLabelKey(event))}</strong>
                <span>{administratorAuditDetails(event, t).map(([key, value]) => t(key, { value, policy: value, reason: value })).join(" · ")}</span>
              </span>
              <time dateTime={event.occurredAt}>{displayDateTime(event.occurredAt)}</time>
            </div>
          ))}
        </div>
      ) : error ? null : loading ? (
        <div className={auditStyles.emptyState} role="status">{t("loadingAuditLog")}</div>
      ) : (
        <div className={auditStyles.emptyState}>{kind ? t("noMatchingAudit") : t("noAuditLog")}</div>
      )}
      {error ? <EmptyLineReadFailed onRetry={() => void load(kind, failedCursor)} data-error>{error}</EmptyLineReadFailed> : null}
      {nextCursor && !error ? (
        <button type="button" className={styles.secondaryButton} onClick={() => void load(kind, nextCursor)} disabled={loading}>{t("loadMore")}</button>
      ) : null}
    </section>
  );
}
