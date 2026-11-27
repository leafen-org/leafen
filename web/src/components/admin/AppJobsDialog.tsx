import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import type { AppEvent, AppJobs } from "@/appPlatformWire.generated";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { appText, listAppApprovals, listAppJobs, listAppRuns, setAppSchedule } from "./administratorAppsApi";
import { AppScheduleRow } from "./AppScheduleRow";
import { appEventLabel } from "./appPermissionWords";
import styles from "./AdministratorApps.module.css";
import { displayDateTime } from "@/lib/displayDate";
import { ReadFailed } from "../ReadFailed";

/**
 * An app's work, for the server's administrators (docs/app-platform.md 11·
 * 15): its schedules, each to be turned off or on and given a time zone, the changes it runs on,
 * what waits or failed, its recent runs with their outcome, and its
 * approvals (5) — the latest ones and the hours allowed now. The runs and
 * the approvals go on to the oldest with "더 보기".
 */

// What Leafen itself records of a failed run reads in the person's language;
// anything else is the app's or the runtime's own words, kept as they came.
/** How long a run took, in the seconds people read past the first one. */
function runDuration(ms: number, t: TFunction) {
  return ms < 1000 ? t("apps.jobs.duration", { ms }) : t("apps.jobs.durationSeconds", { seconds: (ms / 1000).toFixed(1) });
}

// A failed run's record keeps a code (appRunFailure on the server); a
// refusal keeps the app's own reason. Records kept before the codes hold the
// worker's words, put the same way where known. No worker's text shows.
function runFailure(error: string, t: TFunction) {
  const at = error.indexOf(":");
  const code = at < 0 ? error : error.slice(0, at);
  const detail = at < 0 ? "" : error.slice(at + 1);
  const limit = (text: string) => {
    const seconds = /^(\d+)s$/u.exec(text);
    return t("apps.jobs.failure.pastLimit", { limit: seconds ? t("apps.jobs.durationSeconds", { seconds: seconds[1] }) : text });
  };
  switch (code) {
    case "refused": return detail;
    case "past_limit": return limit(detail);
    case "busy": return t("apps.jobs.failure.busy");
    case "server_restarted": return t("apps.jobs.failure.serverRestarted");
    case "output_too_large": return t("apps.jobs.failure.outputTooLarge", { bytes: detail });
    case "worker_stopped": return t("apps.jobs.failure.workerStopped");
  }
  const past = /^the call ran past (.+)$/u.exec(error);
  if (past) return limit(past[1]);
  if (error === "the app's workers are busy") return t("apps.jobs.failure.busy");
  if (error === "the server restarted") return t("apps.jobs.failure.serverRestarted");
  const output = /^the output is larger than (\d+) bytes$/u.exec(error);
  if (output) return t("apps.jobs.failure.outputTooLarge", { bytes: output[1] });
  if (error.startsWith("the worker stopped")) return t("apps.jobs.failure.workerStopped");
  return t("apps.jobs.failure.other");
}

export function AppJobsDialog({ session, installId, name, onClose }: {
  session: Session;
  installId: string;
  name: string;
  onClose: () => void;
}) {
  const { t, i18n } = useTranslation(["serverConsole", "common"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const [jobs, setJobs] = useState<AppJobs | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // The first read that failed, said where what it reads shows, with a way to read again.
  const [readFailed, setReadFailed] = useState("");
  const [readTurn, setReadTurn] = useState(0);
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) onClose(); }, initialFocus: closeRef,
  });

  useEffect(() => {
    let live = true;
    listAppJobs(session, installId).then(
      (listed) => { if (live) setJobs(listed); },
      (failure) => { if (live) setReadFailed(settingsErrorMessage(failure, t("apps.readFailed"))); },
    );
    return () => { live = false; };
  }, [readTurn, installId, session, t]);

  // A schedule turned on or off, or moved to another time zone, shows it on
  // its row and says nothing more (2026-11-06, as a saved switch).
  async function update(schedule: string, change: { enabled?: boolean; timeZone?: string }) {
    setBusy(true);
    setError("");
    try {
      setJobs(await setAppSchedule(session, installId, schedule, change));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("apps.changeFailed")));
    } finally {
      setBusy(false);
    }
  }

  // The next window of the runs or the approvals, added under those shown.
  async function more(list: "runs" | "approvals", cursor: string) {
    setBusy(true);
    setError("");
    try {
      if (list === "runs") {
        const page = await listAppRuns(session, installId, cursor);
        setJobs((current) => current && { ...current, runs: [...current.runs, ...page.runs], runsNextCursor: page.nextCursor ?? "" });
      } else {
        const page = await listAppApprovals(session, installId, cursor);
        setJobs((current) => current && {
          ...current, approvals: [...current.approvals, ...page.approvals], approvalsNextCursor: page.nextCursor ?? "",
        });
      }
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("apps.readFailed")));
    } finally {
      setBusy(false);
    }
  }

  const when = (at: number) => displayDateTime(at);
  const actionName = (key: string, name: Record<string, string>) => appText(name, i18n.language) || key;
  // What a data trigger watches, in the filter's words, not its key (2026-11-06).
  const watched = (event: AppEvent) => (event.where?.length
    ? event.where.map((condition) => condition.equals !== undefined ? `${condition.property} = ${condition.equals}`
      : condition.notEquals !== undefined ? `${condition.property} ≠ ${condition.notEquals}`
        : t(condition.empty ? "apps.jobs.whereEmpty" : "apps.jobs.whereNotEmpty", { property: condition.property })).join(" · ")
    : (event.properties ?? []).join(", "));
  const trigger = (value: string) => (["hook", "event", "schedule", "action", "tool", "realtime", "long", "roomLoop", "automation"].includes(value) ? t(`apps.jobs.trigger.${value}`) : value);
  return createPortal(
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }} data-app-jobs>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} disabled={busy} onClick={onClose} />
      <section ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} aria-busy={busy || undefined}>
        <h2 id={titleId} className={styles.dialogTitle}>{t("apps.jobs.title", { name })}</h2>
        {jobs ? (
          <>
            {jobs.waiting || jobs.failed ? <p className={styles.note} data-app-jobs-queue>{t("apps.jobs.queue", { waiting: jobs.waiting, failed: jobs.failed })}</p> : null}
            {jobs.schedules.length ? <h3 className={styles.permissionsTitle}>{t("apps.jobs.schedules")}</h3> : null}
            {jobs.schedules.map((schedule) => (
              <AppScheduleRow key={schedule.key} schedule={schedule} serverZone={jobs.serverTimeZone} busy={busy} when={when}
                onChange={(change) => void update(schedule.key, change)} />
            ))}
            {jobs.events.length ? <h3 className={styles.permissionsTitle}>{t("apps.jobs.events")}</h3> : null}
            {jobs.events.map((event) => (
              <div key={event.key} className={styles.jobRow} data-app-event={event.key}>
                <span className={styles.secretName}>
                  <strong>{event.types.map((type) => appEventLabel(type, t)).join(" · ")}</strong>
                  {watched(event) ? <span>{watched(event)}</span> : null}
                </span>
              </div>
            ))}
            {jobs.longRuns?.length ? <h3 className={styles.permissionsTitle}>{t("apps.jobs.longRuns")}</h3> : null}
            {jobs.longRuns?.map((run) => (
              <div key={`${run.worker}/${run.tenancyKey}`} className={styles.jobRow} data-app-long-run={run.status}>
                <span className={styles.secretName}>
                  <strong>{run.worker}</strong>
                  <span>{[t(`apps.jobs.longStatus.${run.status}`), run.startedAt ? t("apps.jobs.since", { date: when(run.startedAt) }) : "",
                    !run.startedAt && run.nextStart ? t("apps.jobs.next", { date: when(run.nextStart) }) : "",
                    run.restarts ? t("apps.jobs.restarts", { count: run.restarts }) : ""].filter(Boolean).join(" · ")}</span>
                  {run.lastError ? <span className={styles.runError}>{run.lastError}</span> : null}
                </span>
              </div>
            ))}
            {!jobs.schedules.length && !jobs.events.length && !jobs.longRuns?.length ? <p className={styles.note}>{t("apps.jobs.none")}</p> : null}
            <h3 className={styles.permissionsTitle}>{t("apps.jobs.runs")}</h3>
            {jobs.runs.length ? (
              <ul className={styles.runList} data-app-runs>
                {jobs.runs.map((run) => (
                  <li key={run.id} data-status={run.status}>
                    <span>{[when(run.created), trigger(run.trigger), appText(run.name ?? {}, i18n.language) || run.ref || run.function,
                      t(`apps.jobs.status.${run.status}`, { defaultValue: run.status }),
                      runDuration(run.durationMs, t)].join(" · ")}</span>
                    {run.error && run.status !== "halted" ? <span className={styles.runError}>{runFailure(run.error, t)}</span> : null}
                  </li>
                ))}
              </ul>
            ) : <p className={styles.note}>{t("apps.jobs.noRuns")}</p>}
            {jobs.runsNextCursor ? (
              <button type="button" className={`${styles.secondary} ${styles.listMore}`} disabled={busy} data-app-runs-more
                onClick={() => void more("runs", jobs.runsNextCursor ?? "")}>{t("apps.more")}</button>
            ) : null}
            {jobs.approvals?.length || jobs.allowances?.length ? <h3 className={styles.permissionsTitle}>{t("apps.jobs.approvals")}</h3> : null}
            {jobs.allowances?.length ? (
              <ul className={styles.runList} data-app-allowances>
                {jobs.allowances.map((allowance) => (
                  <li key={`${allowance.action}/${allowance.person}/${allowance.way}`}>
                    <span>{t("apps.jobs.allowance", { person: allowance.person, action: actionName(allowance.action, allowance.actionName),
                      way: allowance.way || t("apps.jobs.screen"), date: when(allowance.expiresAt) })}</span>
                  </li>
                ))}
              </ul>
            ) : null}
            {jobs.approvals?.length ? (
              <ul className={styles.runList} data-app-approvals>
                {jobs.approvals.map((approval, index) => (
                  <li key={`${index}/${approval.created}`} data-status={approval.status}>
                    <span>{[when(approval.created), approval.person, actionName(approval.action, approval.actionName), approval.way || t("apps.jobs.screen"),
                      t(`apps.jobs.approval.${approval.status}`, { defaultValue: approval.status }) + (approval.forHour ? ` (${t("apps.jobs.forHour")})` : "")].join(" · ")}</span>
                  </li>
                ))}
              </ul>
            ) : null}
            {jobs.approvalsNextCursor ? (
              <button type="button" className={`${styles.secondary} ${styles.listMore}`} disabled={busy} data-app-approvals-more
                onClick={() => void more("approvals", jobs.approvalsNextCursor ?? "")}>{t("apps.more")}</button>
            ) : null}
          </>
        ) : null}
        {readFailed ? <ReadFailed message={readFailed} onRetry={() => { setReadFailed(""); setReadTurn((turn) => turn + 1); }} /> : null}
        {error ? <div className={styles.error} role="alert">{error}</div> : null}
        <div className={styles.dialogActions}>
          <button ref={closeRef} type="button" className={styles.secondary} disabled={busy} onClick={onClose}>{t("common:actions.close")}</button>
        </div>
      </section>
    </div>,
    document.body,
  );
}
