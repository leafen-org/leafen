import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { followAcrossTabs, readAndFollow, tellTabs } from "@/lib/followAcrossTabs";
import { activeDateLocale } from "@/lib/i18n";
import { pageHref } from "@/lib/navigation";
import { productErrorMessage } from "@/lib/productErrorPresentation";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import { activePersistentGeneratedLabels } from "@/lib/persistentGeneratedLabels";
import { api } from "../../api";
import type { Session } from "../../types";
import { workspaceImportFormats, workspaceImportStatuses, type WorkspaceImportJob } from "../../workspaceImportWire.generated";
import { newId } from "../../lib/ids";
import {
  notionImportJobTopic, notionImportStopLabel, notionMigrationDurationText, notionMigrationRunProgress, notionMigrationsTopic, notionMigrationTallyAgain,
  notionMigrationTallyText,
} from "../../lib/notionMigrationProgress";
import { nativeNotionImportErrorCodes, nativeNotionImportStatuses, type NativeNotionImportJob, type NativeNotionMigration } from "../../notionImportWire.generated";
import { NotionImportBlockIssues } from "./NotionImportBlockIssues";
import { notionMigrationFilesOmittedText } from "./NotionMigrationOmittedFiles";
import { NotionMigrationStopLines } from "./NotionMigrationStopLines";
import { workspaceImportEndedAs } from "./directLeafenArchiveImportModel";
import { ImportFailureLine } from "./ImportFailureLine";
import { requestSettingsWindow } from "@/components/settings/settingsWindowRequest";
import { IMPORTED_PEOPLE_PART } from "@/lib/importedPeopleMappingView";
import type { NotionImportArrival } from "./useNotionImportController";
import { NotionMigrationTimeLeft } from "./NotionMigrationTimeLeft";
import styles from "./ImportBrowse.module.css";
import { ReadFailed } from "@/components/ReadFailed";
import historyStyles from "./ImportHistory.module.css";

const FINISHED = new Set<string>([workspaceImportStatuses.completed, workspaceImportStatuses.failed, workspaceImportStatuses.cancelled]);

// How often a list with a migration running looks again.
const MIGRATION_POLL_MS = 3000;
// Migrations with a run behind them. One running again stays on the list, with
// what it brought before; one that stopped says why and goes on from here.
// One the list shows stays while it runs again, to end there (2026-11-06: a
// canceled first run started again from its row left the list empty).
const ranMigrations = (items: NativeNotionMigration[], shown: readonly NativeNotionMigration[] = []) =>
  items.filter((migration) => migration.runState === "done" || migration.runState === "failed" || migration.everDone
    // A first run going on is here too, its window closed or never opened
    // (an agent's), and ends here (2026-11-07: a closed window's run showed
    // nowhere until the Notion card was opened again).
    || (migration.status === "scanned" && migration.runState === "running")
    || shown.some((item) => item.id === migration.id));

/**
 * The import screen's "완료됨", as Notion's tab beside "둘러보기" (seen
 * 2026-10-08): the person's finished imports into this workspace, newest
 * first, each opening the page it made.
 */
export function ImportHistory({ onOpenNotion, session, workspaceId }: {
  /** Opens the Notion window: to connect with another token, or to follow a run going on. */
  onOpenNotion?: (arrival?: NotionImportArrival) => void;
  session: Session;
  workspaceId: string;
}) {
  const { t } = useTranslation("importDialog");
  const [jobs, setJobs] = useState<WorkspaceImportJob[] | null>(null);
  // Finished Notion migrations, each of which can bring what changed since.
  const [migrations, setMigrations] = useState<NativeNotionMigration[]>([]);
  const [rerunning, setRerunning] = useState<Record<string, boolean>>({});
  const going = migrations.some((migration) => migration.runState === "running");
  // A failed read of the imports or the migrations, said in the list's place
  // with a way to read again, never as no history (2026-11-20: a failed
  // migrations read left only the imports, or "기록 없음").
  const [readFailed, setReadFailed] = useState("");
  const [readTurn, setReadTurn] = useState(0);
  // A run again that could not start, said under its row.
  const [rerunFailed, setRerunFailed] = useState<Record<string, string>>({});
  useEffect(() => {
    const controller = new AbortController();
    Promise.all([
      api.workspaceImports(session, workspaceId, { signal: controller.signal }),
      api.notionMigrations(session, workspaceId, { signal: controller.signal }),
    ]).then(([list, moved]) => {
      setMigrations(ranMigrations(moved.items));
      setJobs(list.jobs.filter((job) => FINISHED.has(job.status)));
    }, (caught: unknown) => {
      if (!controller.signal.aborted) setReadFailed(settingsErrorMessage(caught, t("history.loadFailed")));
    });
    return () => controller.abort();
  }, [readTurn, session, t, workspaceId]);
  // A run going on ends here too: done, or stopped and why.
  useEffect(() => {
    if (!going) return;
    // One of the person's tabs in sight asks for all of them, and a tab
    // behind asks nothing (followAcrossTabs).
    const controller = new AbortController();
    const topic = notionMigrationsTopic(session.user.id, workspaceId);
    const stop = followAcrossTabs(topic, MIGRATION_POLL_MS,
      () => api.notionMigrations(session, workspaceId, { signal: controller.signal }).then((list) => tellTabs(topic, list.items)),
      (value) => { if (Array.isArray(value)) setMigrations((current) => ranMigrations(value as NativeNotionMigration[], current)); });
    return () => {
      stop();
      controller.abort();
    };
  }, [going, session, workspaceId]);
  // A row that retried or stopped its run reads the list again, so it shows
  // how the run goes on.
  const refresh = useCallback(() => {
    api.notionMigrations(session, workspaceId)
      .then((list) => setMigrations((current) => ranMigrations(list.items, current)))
      .catch(() => undefined);
  }, [session, workspaceId]);
  async function rerun(migration: NativeNotionMigration) {
    setRerunning((current) => ({ ...current, [migration.id]: true }));
    setRerunFailed((current) => ({ ...current, [migration.id]: "" }));
    try {
      const plan = await api.notionMigrationPlan(session, workspaceId, migration.id);
      const running = await api.runNotionMigration(session, workspaceId, migration.id, newId(), plan.revision);
      // The server's migration as it stands: an answer leaves a false or a 0
      // out (scripts/wire-field-tag.mjs), so the last run's end isn't kept.
      setMigrations((current) => current.map((item) => item.id === running.id ? running : item));
    } catch (caught) {
      setRerunFailed((current) => ({ ...current, [migration.id]: productErrorMessage(caught, "workspaceImport.status") }));
    } finally {
      setRerunning((current) => ({ ...current, [migration.id]: false }));
    }
  }
  if (readFailed) return (
    <div className={historyStyles.empty} data-error>
      <ReadFailed message={readFailed} onRetry={() => { setReadFailed(""); setJobs(null); setReadTurn((turn) => turn + 1); }} />
    </div>
  );
  if (!jobs) return <p className={historyStyles.empty} role="status">{t("history.loading")}</p>;
  if (jobs.length === 0 && migrations.length === 0) return <p className={historyStyles.empty} data-import-history-empty>{t("history.empty")}</p>;
  const time = new Intl.DateTimeFormat(activeDateLocale(), { dateStyle: "long", timeStyle: "short" });
  return (
    <ul className={historyStyles.list} data-import-history>
      {migrations.map((migration) => (
        // A run that ended says when it ended; a plan saved since doesn't move it.
        <MigrationRow key={migration.id} migration={migration} rerunning={rerunning[migration.id] ?? false} rerunFailed={rerunFailed[migration.id] ?? ""} session={session}
          when={time.format(migration.runEndedAt || migration.updatedAt)}
          workspaceId={workspaceId} onRerun={() => void rerun(migration)} onChanged={refresh} onOpenNotion={onOpenNotion} />
      ))}
      {jobs.map((job) => {
        const batch = job.format === workspaceImportFormats.fileBatch;
        const title = (batch && job.title) || job.rootPageTitle?.trim() || (job.rootPageId ? activePersistentGeneratedLabels().untitled : t(`history.formats.${job.format}`));
        // How it ended is the one its window said: a batch with nothing in failed.
        const endedAs = workspaceImportEndedAs(job) ?? "failed";
        return (
          <li key={job.id} className={historyStyles.row} data-import-history-job={endedAs}>
            <span className={`${historyStyles.main} ${historyStyles.named}`}>
              <strong>{title}</strong>
              <span>{t(`history.status.${endedAs}`)}{batch ? ` · ${t("job.items", { processed: job.processedItems, total: job.totalItems })}` : ""} · {time.format(job.updated)}</span>
            </span>
            <span className={historyStyles.actions}>
              {endedAs === "completed" && job.rootPageId ? (
                <a className={`${styles.choose} ${historyStyles.open}`} href={pageHref(job.rootPageId)}>{t("history.open")}</a>
              ) : null}
            </span>
            {/* Why it ended unfinished and what to do, as its window says it
                (2026-11-20: an import whose page was deleted for good said
                only "실패" here, or "진행 중" for the page's 30 days). */}
            {endedAs === "failed" ? <ImportFailureLine job={job} session={session} workspaceId={workspaceId} /> : null}
          </li>
        );
      })}
    </ul>
  );
}

/** A migration with a run behind it: how it stands, why it stopped, what it left out. */
function MigrationRow({ migration, onChanged, onOpenNotion, onRerun, rerunFailed, rerunning, session, when, workspaceId }: {
  migration: NativeNotionMigration;
  onOpenNotion?: (arrival?: NotionImportArrival) => void;
  onChanged(): void;
  onRerun(): void;
  /** Why its run again could not start, under it. */
  rerunFailed: string;
  rerunning: boolean;
  session: Session;
  when: string;
  workspaceId: string;
}) {
  const { t } = useTranslation("importDialog");
  const [job, setJob] = useState<NativeNotionImportJob | null>(null);
  const [planRevision, setPlanRevision] = useState(0);
  // How long the next run takes and what it redoes, said before its press:
  // the longer end the server reckons from how long the run before took
  // (notion_migration_estimate_took.go), in hours and minutes.
  const [againMinutes, setAgainMinutes] = useState(0);
  const [acting, setActing] = useState(false);
  const running = rerunning || migration.runState === "running";
  // A run going on says how far it is and can be stopped here, as in its
  // window (2026-11-06: the row said only "다시 가져오는 중…" for half an hour).
  useEffect(() => {
    if (!migration.importJobId) return;
    const controller = new AbortController();
    const stop = readAndFollow(notionImportJobTopic(session.user.id, migration.importJobId), 3000, migration.runState === "running",
      () => api.notionImportJob(session, workspaceId, migration.importJobId, { signal: controller.signal }),
      (value) => { if (value && typeof value === "object") setJob(value as NativeNotionImportJob); });
    return () => {
      stop();
      controller.abort();
    };
  }, [migration.importJobId, migration.runState, session, workspaceId]);
  // A plan an agent changed since the last run is said before the next one.
  useEffect(() => {
    if (running || migration.source) return;
    const controller = new AbortController();
    api.notionMigrationPlan(session, workspaceId, migration.id, { signal: controller.signal })
      .then((plan) => {
        setPlanRevision(plan.revision);
        setAgainMinutes(plan.estimate?.maxMinutes ?? 0);
      })
      .catch(() => undefined);
    return () => controller.abort();
  }, [migration.id, migration.source, running, session, workspaceId]);
  const stopped = !running && migration.runState === "failed";
  const canceled = stopped && job?.status === nativeNotionImportStatuses.canceled;
  const planChanged = !running && planRevision > migration.runPlanRevision && migration.runPlanRevision > 0;
  // A stopped run goes on where it stopped; otherwise one done (or canceled)
  // runs again from the start, but another tool's, from its card.
  const resumeShown = stopped && !canceled && Boolean(job);
  const rerunShown = !resumeShown && !migration.source && !running;
  // A run that never ended done is the first one, not one again (2026-11-06:
  // a first migration retried after it stopped said "다시 가져오는 중…").
  const status = running ? t(migration.everDone ? "history.rerunning" : "migration.running") : canceled ? t("history.status.cancelled") : stopped ? t("history.status.failed") : t("history.status.completed");
  // A run going on says what it is doing and how far, as its Notion window
  // (2026-11-07: "12971개 중 12971개" stood an hour while the comments came);
  // the last run's job, until the new one is read, says nothing.
  const going = job?.status === nativeNotionImportStatuses.queued || job?.status === nativeNotionImportStatuses.running;
  const progress = running && job && going ? notionMigrationRunProgress(t, job, migration.source ?? "") : null;
  const report = migration.runReport;
  const comments = running || migration.runState !== "done" ? ""
    : report.commentsUnavailable ? (migration.source ? "" : t("migration.commentsUnavailable"))
    : report.commentsUnreadable > 0 ? t("migration.commentsUnreadable", { count: report.commentsUnreadable }) : "";
  async function act(call: () => Promise<NativeNotionImportJob>) {
    setActing(true);
    try {
      setJob(await call());
    } catch {
      // The next read says how it stands.
    } finally {
      setActing(false);
      onChanged();
    }
  }
  return (
    <li className={historyStyles.row} data-import-history-migration={running ? "running" : stopped ? "stopped" : "done"}>
      <span className={`${historyStyles.main} ${historyStyles.named}`}>
        <strong>{migration.roots[0]?.title?.trim() || t("history.migration")}{migration.roots.length > 1 ? ` ${t("history.rootsMore", { count: migration.roots.length - 1 })}` : ""}</strong>
        <span>{migration.source ? t("history.toolMigration", { name: migration.source }) : t("history.migration")} · {status} · {when}</span>
      </span>
      <span className={historyStyles.actions}>
        {/* A run going on opens its window, as its card on "둘러보기";
            another tool's has its own window, from its card. */}
        {running && !migration.source && onOpenNotion ? (
          <button type="button" className={`${styles.choose} ${historyStyles.open}`} data-import-history-progress-open
            onClick={() => onOpenNotion?.()}>{t("progressView.open")}</button>
        ) : null}
        {/* Stopping keeps its place for the whole run, and can be pressed
            while the run's job takes it: not before the new run's job is
            read, nor while it publishes (2026-11-13: it came and went under
            the pointer, and "진행 보기" moved onto where it had been). */}
        {running ? (
          <button type="button" className={`${styles.choose} ${historyStyles.open}`} data-import-history-cancel
            disabled={acting || !going || !job?.canCancel}
            onClick={() => { if (job) void act(() => api.cancelNotionImportJob(session, workspaceId, job.id)); }}>{job ? notionImportStopLabel(t, job) : t("cancelImport")}</button>
        ) : null}
        {/* A token Notion no longer takes is replaced where its message says,
            and the run goes on through the new connection (2026-11-06: the row
            said to connect again and gave no way to). */}
        {stopped && !canceled && job?.errorCode === nativeNotionImportErrorCodes.tokenRejected && onOpenNotion ? (
          <button type="button" className={`${styles.choose} ${historyStyles.open}`} data-import-history-connect
            onClick={() => onOpenNotion?.("token")}>{t("useAnotherToken")}</button>
        ) : null}
        {/* A run that failed goes on where it stopped, as its message says. */}
        {resumeShown && job ? (
          <button type="button" className={`${styles.choose} ${historyStyles.open}`} data-import-history-retry disabled={acting}
            onClick={() => void act(() => api.retryNotionImportJob(session, workspaceId, job.id))}>{t("retry")}</button>
        ) : migration.source || running ? null : (
          // A run going on shows how to stop it, not one that can't start
          // (2026-11-06: the disabled button looked like a live link).
          <button type="button" className={`${styles.choose} ${historyStyles.open}`} data-import-history-rerun
            onClick={onRerun}>{stopped && !migration.everDone ? t("migration.runAgain") : t("history.rerun")}</button>
        )}
        {/* The last run's report and top page stay through a run again; a
            first run's come when it is done, not while it runs. */}
        {migration.runReport.reportPageId && (!running || migration.everDone) ? (
          <a className={`${styles.choose} ${historyStyles.open}`} href={pageHref(migration.runReport.reportPageId)} data-import-history-report>{t("history.report")}</a>
        ) : null}
        {migration.importTopPageId && (running ? migration.everDone : !(stopped && !migration.everDone)) ? (
          <a className={`${styles.choose} ${historyStyles.open}`} href={pageHref(migration.importTopPageId)}>{t("history.open")}</a>
        ) : null}
      </span>
      {/* What the run is doing and what it left say so under the name and
          the buttons, across the row, so their changing length never moves
          a button (2026-11-13: the progress line breaking in two moved them). */}
      <span className={historyStyles.details}>
        {progress ? <NotionMigrationTimeLeft text={progress.left} /> : null}
        {progress ? (
          <small data-import-history-progress={progress.stage}>{progress.count ? `${progress.phase} · ${progress.count}` : progress.phase}</small>
        ) : null}
        {progress?.leftWhy ? <small data-notion-migration-left-why>{progress.leftWhy}</small> : null}
        {rerunFailed ? <small className={historyStyles.failed} role="alert" data-import-history-rerun-failed>{rerunFailed}</small> : null}
        {planChanged ? <small data-import-history-plan-changed>{t("history.planChanged")}</small> : null}
        {/* Its press starts the run at once, so how long it takes and what it
            redoes are said before it, as its window and the agent say them
            (2026-11-16: the row's button started a run of an hour unsaid). */}
        {rerunShown && againMinutes > 0 ? (
          <small data-import-history-rerun-lead>{t("history.rerunTakes", { time: notionMigrationDurationText(t, againMinutes) })}</small>
        ) : null}
        {/* The step it stopped at, why, and what trying again does. */}
        {stopped && !canceled && job ? <NotionMigrationStopLines compact job={job} migration={migration} source={migration.source ?? ""} /> : null}
        {/* Comments a finished run left in Notion, as its window and report say
            (2026-11-13: a run that skipped most blocks' comments said only "완료"). */}
        {comments ? <small data-import-history-comments-left>{comments}</small> : null}
        {/* A run again that ended says what it made new, changed and left as it was. */}
        {!running && migration.runState === "done" && notionMigrationTallyAgain(report.tally) ? (
          <small data-import-history-tally>{notionMigrationTallyText(t, report.tally)}</small>
        ) : null}
        {/* Files Notion no longer gave, with not to delete the originals meanwhile; its window copies them again. */}
        {!running && notionMigrationFilesOmittedText(t, migration) ? (
          <small data-import-history-files-omitted>{notionMigrationFilesOmittedText(t, migration)}</small>
        ) : null}
        {/* The Notion people it kept by their Notion name are mapped to
            members under Settings › People, and the row goes there
            (2026-11-19: told "later in Settings' import", a person found
            only this row's buttons here). */}
        {!running && migration.runState === "done" && report.peopleKept > 0 ? (
          <small data-import-history-people-kept>
            {t("history.peopleKept", { count: report.peopleKept })}{" "}
            <button type="button" className={styles.choose} data-import-history-people-open
              onClick={() => requestSettingsWindow({ section: "people", part: IMPORTED_PEOPLE_PART })}>{t("history.peopleKeptOpen")}</button>
          </small>
        ) : null}
        {/* Another tool's next state comes as a new bundle, from its card. */}
        {migration.source && !running ? <small>{t("history.toolAgain", { name: migration.source })}</small> : null}
      </span>
      {/* A canceled run's are not in the report; its window says what it left. */}
      {!running && !canceled && job && job.report.blockIssues + job.report.propertyIssues + job.report.viewIssues > 0 ? (
        <div className={historyStyles.issues}><NotionImportBlockIssues job={job} source={migration.source ?? ""} /></div>
      ) : null}
    </li>
  );
}
