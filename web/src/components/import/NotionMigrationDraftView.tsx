import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronRight } from "@/icons/leafen";
import { notionImportStopLabel, notionMigrationRunProgress, notionMigrationTallyAgain, notionMigrationTallyText } from "@/lib/notionMigrationProgress";

import { pageHref } from "../../lib/navigation";
import { usePageNames } from "@/lib/pageNames";
import type { NativeNotionMigrationSummary } from "../../notionImportWire.generated";
import type { Session } from "../../types";
import { useProductPages } from "../product/productPagesContext";
import { CsvLocationPicker } from "./CsvImportPlacePicker";
import { ImportPlaceButton } from "./ImportPlaceButton";
import { NotionFailureLine } from "./NotionFailureLine";
import { NotionImportBlockIssues } from "./NotionImportBlockIssues";
import { NotionMigrationFormatsLeft } from "./NotionMigrationFormatsLeft";
import { NotionMigrationOmittedFiles } from "./NotionMigrationOmittedFiles";
import { NotionMigrationStopLines } from "./NotionMigrationStopLines";
import { NotionMigrationAgendaList, NotionMigrationPlanView, useNotionMigrationPlanChangeList } from "./NotionMigrationPlanView";
import { UploadProgressBar } from "./UploadProgressBar";
import { NotionMigrationTimeLeft } from "./NotionMigrationTimeLeft";
import { NotionMoveFormats, NotionMoveLive, notionMoveWay } from "./NotionMoveLive";
import type { NotionMigrationDraft } from "./useNotionMigrationDraft";
import styles from "./ImportDialog.module.css";

// The direct import's last step: the scan
// going on, then the "as is" draft — what moves, where it goes and what
// can't move — whose "가져오기" is the window's foot (NotionMigrationApprove),
// then the run and its end, which leads to an agent for what Notion didn't
// give (NotionMigrationFormatsLeft).

function moving(summary: NativeNotionMigrationSummary) {
  return { pages: summary.counts.pages, databases: summary.counts.databases, rows: summary.counts.rows,
    rowsCapped: summary.databases.some((database) => database.rowsCapped) };
}

export function NotionMigrationDraftView({ draft, agent = false, session, workspaceId }: {
  draft: NotionMigrationDraft;
  /**
   * The agent's migration: the agent reads, plans with the person and runs
   * it, asking them to approve in the conversation; this shows how far it
   * is, with no approving or picking of its own.
   */
  agent?: boolean;
  session: Session;
  workspaceId: string;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["importDialog", "common"]);
  const productPages = useProductPages();
  const pages = useMemo(() => (productPages?.pages ?? []).filter((page) => page.workspaceId === workspaceId), [productPages, workspaceId]);
  // The place field while its list is open.
  const [picking, setPicking] = useState<HTMLElement | null>(null);
  // The words searched there, kept while the list is closed (CsvLocationPicker).
  const [placeQuery, setPlaceQuery] = useState("");
  const [showPlan, setShowPlan] = useState(false);
  const { migration, summary, plan, job, busy, error } = draft;
  // What the window waits on, at every step: the server away, or a read it keeps not answering.
  const waiting = <MigrationWaiting reconnecting={draft.reconnecting} readsLost={draft.readsLost} />;
  const changed = useNotionMigrationPlanChangeList(plan);
  // Where it goes is the plan's until the person picks a place here.
  const destination = draft.place ?? plan?.plan?.destinationPageId ?? "";
  if (!migration) return null;
  // Another tool's bundle says its tool where Notion's says Notion.
  const tool = migration.source ?? "";
  const scanning = tool ? t("importDialog:tools.scanning", { name: tool }) : t("importDialog:migration.scanning");

  if (migration.status === "failed") {
    return <div className={styles.jobPanel} data-notion-migration-state="failed">
      <strong>{tool ? t("importDialog:tools.scanFailed") : t("importDialog:migration.scanFailed")}</strong>
    </div>;
  }
  if (migration.status === "scanning") {
    return (
      <div className={styles.jobPanel} data-notion-migration-state="scanning">
        <strong>{scanning}</strong>
        {migration.live?.stage === "scanning" ? <NotionMoveLive live={migration.live} way="out" /> : null}
        <UploadProgressBar className={styles.progressTrack} label={scanning}
          progress={{ phase: "preparing", percent: 0 }} />
        {/* The blocks it lists count too: a page of many blocks is read
            with the pages, databases and rows standing still. */}
        <small>{[t("importDialog:migration.counts", migration.counts),
          migration.counts.blocks ? t("importDialog:migration.scanBlocks", { blocks: migration.counts.blocks }) : ""].filter(Boolean).join(" · ")}</small>
        <p data-notion-agent-working={agent || undefined}>{t(agent ? "importDialog:migration.agentWorking" : "importDialog:migration.scanContinues")}</p>
        {waiting}
      </div>
    );
  }
  if (migration.runState === "" && agent) {
    // What moves as the agent's plan has it, and the plan itself to look at.
    const counts = summary ? { ...moving(summary), ...plan?.check?.expected } : migration.counts;
    return (
      <div className={styles.jobPanel} data-notion-migration-state="agent-plan">
        <strong>{t("importDialog:migration.agentPlanning")}</strong>
        <p>{t("importDialog:migration.agentPlanningLead")}</p>
        {/* The settings reading the agent runs in the person's Notion, as it reads. */}
        {migration.live?.stage === "formats" ? <NotionMoveFormats live={migration.live} /> : null}
        <span data-notion-migration-moving>{t("importDialog:migration.moving", { pages: counts.pages, databases: counts.databases, rows: counts.rows })}</span>
        {plan?.estimate ? <p data-notion-migration-estimate>{plan.estimate.say}</p> : null}
        {summary && plan ? (
          <>
            <button type="button" className={styles.planToggle} aria-expanded={showPlan} data-notion-migration-show-plan
              onClick={() => setShowPlan((shown) => !shown)}>
              <ChevronRight size="small" aria-hidden="true" />
              {t("importDialog:plan.show")}
            </button>
            {showPlan ? <NotionMigrationPlanView migration={migration} summary={summary} state={plan} workspaceId={workspaceId} /> : null}
          </>
        ) : null}
        <NotionFailureLine text={error} />
        {waiting}
      </div>
    );
  }
  if (migration.runState === "") {
    if (!summary || !plan) {
      return (
        <div className={styles.jobPanel} data-notion-migration-state="reading">
          <strong>{t("importDialog:migration.reading")}</strong>
          <NotionFailureLine text={error} />
          {waiting}
        </div>
      );
    }
    const counts = { ...moving(summary), ...plan.check?.expected };
    const place = destination ? pages.find((page) => page.id === destination) : null;
    // A saved draft with problems can't run; the plan shows them.
    const blocked = Boolean(plan.plan && plan.check && !plan.check.ok);
    // A saved plan's check keeps what it brings.
    const agenda = plan.plan ? plan.check?.agenda ?? summary.issues : summary.issues;
    return (
      <div className={styles.jobPanel} data-notion-migration-state="draft">
        {/* A plan the agent changed says so, in place of "as is". */}
        {changed.length ? (
          <>
            <strong data-notion-migration-plan-changes={plan.revision}>
              {t("importDialog:plan.changed", { revision: plan.revision, list: changed.join(" · ") })}
            </strong>
            <p>{t("importDialog:plan.changedLead")}</p>
          </>
        ) : (
          <>
            <strong>{t("importDialog:migration.draftTitle")}</strong>
            <p>{tool ? t("importDialog:tools.draftLead", { name: tool }) : t("importDialog:migration.draftLead")}</p>
          </>
        )}
        <span data-notion-migration-moving>
          {t("importDialog:migration.moving", { pages: counts.pages, databases: counts.databases, rows: counts.rows })}
          {counts.rowsCapped ? ` ${t("importDialog:migration.rowsCapped")}` : ""}
        </span>
        {/* Leafen's own reckoning, the sentence the agent says too. */}
        {plan.estimate ? <p data-notion-migration-estimate>{plan.estimate.say}</p> : null}
        <div className={styles.field}>
          <span>{t("importDialog:migration.place")}</span>
          <ImportPlaceButton data-notion-migration-place isPrivate={!place}
            emoji={place?.iconType === "emoji" ? place.icon : null}
            label={place ? pageNames.title(place) : t("importDialog:migration.privatePages")} onClick={(event) => setPicking(event.currentTarget)} />
        </div>
        {picking ? (
          <CsvLocationPicker pages={pages} reader={{ session, workspaceId }} current={destination ? { kind: "page", pageId: destination } : { kind: "private" }} field={picking}
            query={placeQuery} onQuery={setPlaceQuery} onClose={() => setPicking(null)} onChoose={(next) => {
            draft.choosePlace(next.kind === "page" ? next.pageId : "");
            setPicking(null);
          }} />
        ) : null}
        {agenda.length ? (
          <div className={styles.omissionNotice} data-notion-migration-agenda>
            <strong>{t("importDialog:migration.cantMove")}</strong>
            <NotionMigrationAgendaList issues={agenda} />
          </div>
        ) : null}
        <button type="button" className={styles.planToggle} aria-expanded={showPlan || blocked} data-notion-migration-show-plan
          onClick={() => setShowPlan((shown) => !shown)}>
          <ChevronRight size="small" aria-hidden="true" />
          {t("importDialog:plan.show")}
        </button>
        {showPlan || blocked ? <NotionMigrationPlanView migration={migration} summary={summary} state={plan} workspaceId={workspaceId} /> : null}
        <NotionFailureLine text={error} />
        {waiting}
      </div>
    );
  }
  const report = migration.runReport;
  const done = migration.runState === "done";
  // A stopped run says so in its head, with why, not under a full bar.
  const failed = job?.status === "failed";
  const canceled = job?.status === "canceled";
  // Its 완료됨 row says the same stage and count.
  const progress = job && !done && !failed && !canceled ? notionMigrationRunProgress(t, job, tool) : null;
  const leftSay = progress ? progress.leftWhy : report.estimate?.changedAt ? report.estimate.say : null;
  return (
    <div className={styles.jobPanel} data-notion-migration-state={done ? "done" : failed ? "failed" : canceled ? "canceled" : "running"}>
      <strong>{done ? t("importDialog:migration.done") : failed ? t("importDialog:migration.stopped") : canceled ? t("importDialog:migration.canceled") : t("importDialog:migration.running")}</strong>
      {job && progress ? (
        <>
          <NotionMigrationTimeLeft text={progress.left} />
          <span data-notion-migration-phase={job.phase} data-notion-migration-stage={progress.stage}>{progress.phase}</span>
          {migration.live && migration.live.stage !== "scanning" && migration.live.stage !== "formats"
            ? <NotionMoveLive live={migration.live} way={notionMoveWay(migration.live.stage)} /> : null}
          <UploadProgressBar className={styles.progressTrack} label={t("importDialog:stepProgress")} progress={progress.bar} />
          {progress.count ? <small>{progress.count}</small> : null}
        </>
      ) : null}
      {/* The time said before the run stands; a time left far from it says
          why beside it, as the inbox and the agent's status do (the server's
          one line, notion_migration_time_said.go). */}
      {!done && !failed && !canceled && leftSay ? <p data-notion-migration-estimate-changed>{leftSay}</p> : null}
      {/* An agent's run goes on in Leafen alone once the agent's turn ends:
          the window says so, and what the person does at its end (2026-11-19:
          it said "the agent is on it" while the agent was idle for hours). */}
      {!done && !failed && !canceled ? (
        <p data-notion-agent-running={agent || undefined}>{t(agent ? "importDialog:migration.agentRunning" : "importDialog:migration.closeAnytime")}</p>
      ) : null}
      {job?.canCancel && !done ? (
        <button type="button" className={styles.secondary} disabled={busy} data-notion-migration-cancel onClick={() => void draft.cancel()}>
          {notionImportStopLabel(t, job)}
        </button>
      ) : null}
      {canceled ? (
        <>
          {/* A run again canceled leaves what the runs before it moved in
              sight, unlike a first run's (2026-11-16: it said nothing made
              shows, of pages in the workspace for days). */}
          <p data-notion-migration-canceled-lead={migration.everDone ? "again" : "first"}>
            {t(migration.everDone ? "importDialog:migration.canceledAgainLead" : "importDialog:migration.canceledLead")}
          </p>
          {plan?.estimate ? <p data-notion-migration-estimate>{plan.estimate.say}</p> : null}
          <button type="button" className={styles.secondary} disabled={busy} data-notion-migration-run-again onClick={() => void draft.runAgain()}>
            {t("importDialog:migration.runAgain")}
          </button>
        </>
      ) : null}
      {failed ? (
        <>
          <NotionMigrationStopLines job={job} migration={migration} source={tool} />
          <button type="button" className={styles.secondary} disabled={busy} onClick={() => void draft.retry()}>
            {t("importDialog:retry")}
          </button>
        </>
      ) : null}
      {done && job ? (
        <small data-notion-migration-moved>{t("importDialog:migration.moved", plan?.check?.expected ?? summary?.counts ?? job.report)}</small>
      ) : null}
      {/* A run again says what it made new, changed and left as it was. */}
      {done && notionMigrationTallyAgain(report.tally) ? <small data-notion-migration-tally>{notionMigrationTallyText(t, report.tally)}</small> : null}
      {done && report.commentsUnavailable && !tool ? <p>{t("importDialog:migration.commentsUnavailable")}</p> : null}
      {done && !report.commentsUnavailable && report.commentsUnreadable > 0 ? (
        <p data-notion-migration-comments-unreadable>{t("importDialog:migration.commentsUnreadable", { count: report.commentsUnreadable })}</p>
      ) : null}
      {done && report.commentsStopped ? <p data-notion-migration-comments-stopped>{t("importDialog:migration.commentsStopped")}</p> : null}
      {done && report.filesSkipped > 0 ? (
        <p>{tool ? t("importDialog:tools.filesSkipped", { count: report.filesSkipped }) : t("importDialog:migration.filesSkipped", { count: report.filesSkipped })}</p>
      ) : null}
      {/* The agent checks and reports when the person tells it the run ended. */}
      {done && agent ? <p data-notion-agent-done>{t("importDialog:migration.agentDone")}</p> : null}
      {/* What comes after a move from Notion: a run again brings Notion's changes, or Notion is left read-only. */}
      {done && !tool ? <p data-notion-migration-after-done>{t("importDialog:migration.afterDone")}</p> : null}
      {done ? <NotionMigrationOmittedFiles migration={migration} job={job} session={session} workspaceId={workspaceId} /> : null}
      {done && report.reportPageId ? (
        <a className={styles.secondary} href={pageHref(report.reportPageId)} data-notion-migration-report>
          {t("importDialog:migration.openReport")}
        </a>
      ) : null}
      {/* The content opens as soon as it shows, not only at the end. */}
      {migration.importTopPageId && !canceled ? (
        <a className={styles.primary} href={pageHref(migration.importTopPageId)} data-notion-migration-open>
          {t("importDialog:migration.open")}
        </a>
      ) : null}
      {/* The agent matches what Notion didn't give itself (the migration guide's part 6). */}
      {done && !tool && !agent ? <NotionMigrationFormatsLeft migration={migration} session={session} workspaceId={workspaceId} /> : null}
      {done && job ? <NotionImportBlockIssues job={job} source={tool} /> : null}
      <NotionFailureLine text={error} />
      {waiting}
    </div>
  );
}

/**
 * The draft's "가져오기" (approving it saves the plan and runs it), in its
 * window's foot where the steps before it have "계속하기", so a plan opened
 * above it doesn't push it out of sight (2026-11-07); dimmed while a saved
 * plan has problems to fix. Nothing while there is no draft to approve.
 */
export function NotionMigrationApprove({ draft }: { draft: NotionMigrationDraft }) {
  const { t } = useTranslation("importDialog");
  const { migration, summary, plan, busy } = draft;
  if (!migration || migration.status !== "scanned" || migration.runState !== "" || !summary || !plan) return null;
  const blocked = Boolean(plan.plan && plan.check && !plan.check.ok);
  return (
    <button type="button" className={styles.primary} disabled={busy || blocked} data-notion-migration-approve
      onClick={() => void draft.approve()}>
      {t("migration.approve")}
    </button>
  );
}

/**
 * What the window waits on: the server out of reach, or — while it answers
 * the others — a read it keeps not answering, with what comes once it does
 * (2026-11-23: the summary alone refused left the plan step with no "계획
 * 보기" and no reason). Each goes as its read answers.
 */
function MigrationWaiting({ reconnecting, readsLost }: Pick<NotionMigrationDraft, "reconnecting" | "readsLost">) {
  const { t } = useTranslation();
  if (reconnecting) return <MigrationReconnecting />;
  return <>{readsLost.map((read) => (
    <div key={read} className={styles.serverStatus} role="status" data-migration-read-lost={read}>
      {t(`importDialog:migration.readLost.${read}`)}
    </div>
  ))}</>;
}

/** The server out of reach while the import goes on there: the info box's own words, not an error's red. */
export function MigrationReconnecting() {
  const { t } = useTranslation();
  return <div className={styles.serverStatus} role="status" data-migration-reconnecting>{t("importDialog:migration.reconnecting")}</div>;
}
