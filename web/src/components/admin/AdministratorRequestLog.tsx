import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api";
import {
  administratorBounds,
  administratorRequestLogSuccessfulModes,
  type AdministratorRequestLog as RequestLog,
  type AdministratorRequestLogSuccessful as Successful,
} from "../../administratorWire.generated";
import { activeNumberLocale } from "../../lib/i18n";
import { productErrorMessage } from "../../lib/productErrorPresentation";
import { useNoticeFor } from "@/lib/useNoticeFor";
import type { Session } from "../../types";
import { AppSelect } from "../AppSelect";
import styles from "./AdministratorSettingsPage.module.css";
import { SettingRow } from "./AdministratorSettingRows";
import dataStyles from "./AdministratorDataManagement.module.css";
import { ReadFailed } from "../ReadFailed";

const RETENTION_DAYS = [1, 3, 5, 7, 14, 30, 90];

/**
 * What the server's request log keeps (2026-10-18, request_log_policy.go):
 * failures, slow requests and administrators' and sign-ins' changes always;
 * of the other successful requests one in a hundred, all, or none; for the
 * days chosen, and at most so many entries.
 */
export function AdministratorRequestLog({ session }: { session: Session }) {
  const { t } = useTranslation("serverConsole");
  const [saved, setSaved] = useState<RequestLog | null>(null);
  const [retentionDays, setRetentionDays] = useState(5);
  const [successful, setSuccessful] = useState<Successful>(administratorRequestLogSuccessfulModes.sample);
  const [busy, setBusy] = useState(false);
  // "Saved" holds while the choices stay as saved.
  const [message, setMessage] = useNoticeFor<string>(`${retentionDays}:${successful}`);
  const [error, setError] = useState("");
  const [readTurn, setReadTurn] = useState(0);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    const controller = new AbortController();
    api.requestLog(session, controller.signal).then((log) => {
      setSaved(log);
      setRetentionDays(log.retentionDays);
      setSuccessful(log.successful);
    }).catch((cause) => {
      if (!controller.signal.aborted) setError(productErrorMessage(cause, "administratorRequestLog.read"));
    });
    return () => {
      mounted.current = false;
      controller.abort();
    };
  }, [readTurn, session]);

  // A choice saves as it is made, as 설정's do (2026-11-05: a blue 저장 in the group's head).
  async function save(next: { retentionDays: number; successful: Successful }) {
    if (busy) return;
    setBusy(true);
    setMessage("");
    setError("");
    try {
      const log = await api.saveRequestLog(session, next);
      if (!mounted.current) return;
      setSaved(log);
    } catch (cause) {
      if (mounted.current) setError(productErrorMessage(cause, "administratorRequestLog.save"));
    } finally {
      if (mounted.current) setBusy(false);
    }
  }

  const days = RETENTION_DAYS.includes(retentionDays) ? RETENTION_DAYS : [...RETENTION_DAYS, retentionDays].sort((a, b) => a - b);
  const count = (value: number) => value.toLocaleString(activeNumberLocale());
  return (
    <section className={styles.section} aria-labelledby="administrator-request-log-title" data-request-log>
      <div id="administrator-request-log-title" className={styles.sectionTitle}>{t("requestLog.title")}</div>
      {saved ? (
        <div className={styles.settingRows}>
          <SettingRow title={t("requestLog.retention")} description={t("requestLog.meta")}>
            <AppSelect look="setting" value={retentionDays} disabled={busy} aria-label={t("requestLog.retention")} data-request-log-retention
              onChange={(event) => { const next = Number(event.target.value); setRetentionDays(next); void save({ retentionDays: next, successful }); }}>
              {days.filter((value) => value >= administratorBounds.requestLogRetentionMinimum && value <= administratorBounds.requestLogRetentionMaximum)
                .map((value) => <option key={value} value={value}>{t("requestLog.days", { count: value })}</option>)}
            </AppSelect>
          </SettingRow>
          <SettingRow title={t("requestLog.successful")} description={t("requestLog.help")}>
            <AppSelect look="setting" value={successful} disabled={busy} aria-label={t("requestLog.successful")} data-request-log-successful
              onChange={(event) => { const next = event.target.value as Successful; setSuccessful(next); void save({ retentionDays, successful: next }); }}>
              <option value={administratorRequestLogSuccessfulModes.sample}>{t("requestLog.sample")}</option>
              <option value={administratorRequestLogSuccessfulModes.all}>{t("requestLog.all")}</option>
              <option value={administratorRequestLogSuccessfulModes.none}>{t("requestLog.none")}</option>
            </AppSelect>
          </SettingRow>
          <p className={styles.settingNote} data-request-log-entries>
            {t("requestLog.entries", { count: saved.entries, entries: count(saved.entries), most: count(saved.maxEntries) })}
          </p>
        </div>
      ) : null}
      {/* Not read yet: what failed, with a way to read it again. */}
      {!saved && error ? <ReadFailed message={error} onRetry={() => { setError(""); setReadTurn((turn) => turn + 1); }} /> : (
        <div className={dataStyles.live} data-error={error ? "true" : undefined} role={error ? "alert" : "status"} aria-live="polite">
          {error || message || (saved ? "" : t("requestLog.loading"))}
        </div>
      )}
    </section>
  );
}
