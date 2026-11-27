import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api";
import type { AdministratorInstanceReport } from "../../administratorWire.generated";
import { newId } from "../../lib/ids";
import { activeNumberLocale } from "../../lib/i18n";
import { displayDateTime } from "@/lib/displayDate";
import type { Session } from "../../types";
import { SettingRow } from "./AdministratorSettingRows";
import styles from "./AdministratorSettingsPage.module.css";
import { formatFileSize } from "../database/files";
import { ReadFailed } from "../ReadFailed";

/** A count older than this is counted again when the page opens. */
const STALE_MS = 60 * 60_000;

/**
 * 설정 › 서버 › 일반's usage: what the server holds, one row each, counted
 * again as the page opens when the last count is an hour old (2026-11-05: a
 * "server report" with a big blue refresh button of its own, on the backup page).
 */
export function AdministratorServerUsage({ session }: { session: Session }) {
  const { t } = useTranslation("serverConsole");
  const [report, setReport] = useState<AdministratorInstanceReport | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "failed">("loading");
  const [readTurn, setReadTurn] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    const count = (expectedRevision: number) => api.refreshAdministratorReport(
      session, { requestId: newId(), expectedRevision }, controller.signal,
    ).then((result) => result.report);
    void api.currentAdministratorReport(session, controller.signal)
      .then(async ({ report: current }) => {
        const stale = !current || Date.now() - new Date(current.createdAt).getTime() > STALE_MS;
        if (current) setReport(current);
        // A count that cannot be made again keeps the last one in sight.
        const fresh = stale ? await count(current?.revision ?? 0).catch(() => current) : current;
        if (controller.signal.aborted) return;
        setReport(fresh);
        setState(fresh ? "ready" : "failed");
      })
      .catch(() => {
        if (!controller.signal.aborted) setState("failed");
      });
    return () => controller.abort();
  }, [readTurn, session]);

  const number = (value: number) => value.toLocaleString(activeNumberLocale());
  return (
    <section className={styles.section} aria-labelledby="administrator-usage-title" data-server-usage={state}>
      <div id="administrator-usage-title" className={styles.sectionTitle}>{t("report.usageTitle")}</div>
      {report ? (
        <div className={styles.settingRows}>
          <SettingRow title={t("report.users")}><span className={styles.settingValue}>{number(report.counts.users)}</span></SettingRow>
          <SettingRow title={t("report.workspaces")}><span className={styles.settingValue}>{number(report.counts.workspaces)}</span></SettingRow>
          <SettingRow title={t("report.sessions")}>
            <span className={styles.settingValue}>{number(report.counts.sessions)}</span>
          </SettingRow>
          <SettingRow title={t("report.files")}><span className={styles.settingValue}>{number(report.counts.files)}</span></SettingRow>
          <SettingRow title={t("report.storage")}><span className={styles.settingValue}>{bytes(report.counts.storageBytes)}</span></SettingRow>
          <SettingRow title={t("report.authActivity")}>
            <span className={styles.settingValue}>{number(report.counts.authActivity)}</span>
          </SettingRow>
          <p className={styles.settingNote}>
            {t("report.countedAt", { time: displayDateTime(report.createdAt) || report.createdAt })}
          </p>
        </div>
      ) : (
        state === "failed" ? <ReadFailed message={t("report.failed")} onRetry={() => { setState("loading"); setReadTurn((turn) => turn + 1); }} />
          : <p className={styles.settingNote} role="status">{t("report.loading")}</p>
      )}
    </section>
  );
}

const bytes = formatFileSize;
