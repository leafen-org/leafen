import type { NativeNotionMigration } from "../../notionImportWire.generated";
import { Fragment, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import type { Session } from "../../types";
import { NotionImportAgentFirst, type NotionEntryPhase } from "./NotionImportAgentFirst";
import { NotionFailureLine } from "./NotionFailureLine";
import { NotionMigrationApprove, NotionMigrationDraftView } from "./NotionMigrationDraftView";
import { useNotionMigrationDraft } from "./useNotionMigrationDraft";
import { UploadProgressBar } from "./UploadProgressBar";
import { NotionMigrationTimeLeft } from "./NotionMigrationTimeLeft";
import { api } from "../../api";
import { newId } from "../../lib/ids";
import { notionMigrationRunProgress } from "../../lib/notionMigrationProgress";
import { userErrorText } from "../../lib/settingsErrors";
import {
  notionImportJobGoing,
  type NotionImportArrival,
  type NotionImportController,
  type NotionImportStep,
} from "./useNotionImportController";
import styles from "./ImportDialog.module.css";
import agentFirst from "./NotionImportAgentFirst.module.css";
import rowStyles from "./ImportHistory.module.css";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";

type NotionImportPanelProps = {
  controller: NotionImportController;
  session: Session;
  workspaceId: string;
  /**
   * How the window was come to: by the link an agent gives for the token
   * ("agent", `import_screen`), to put a new token in for a run Notion
   * stopped ("token"), or from Leafen's own entries.
   */
  arrival?: NotionImportArrival;
};

// Notion's page for making a connection's token, its developer tools'
// "Developer connections" (notion.so/profile/integrations goes there, seen
// 2026-10-19 and 2026-11-16), the page the guide's first scene opens.
const notionTokenPageUrl = "https://app.notion.com/developers/connections";

const steps: Array<{
  id: NotionImportStep;
  label: "wizard.stepLabels.0" | "wizard.stepLabels.1" | "wizard.stepLabels.3";
}> = [
  { id: "connect", label: "wizard.stepLabels.0" },
  { id: "scope", label: "wizard.stepLabels.1" },
  { id: "apply", label: "wizard.stepLabels.3" },
];

/**
 * The Notion window, drawn by where the move stands, one thing to do at
 * each step (the user, 2026-11-13): the start hands it to an agent, with
 * "직접 가져오기" small; the token's step and the saved connection
 * (NotionImportAgentFirst); then by hand the roots and the draft, or the
 * agent's migration followed as it reads, plans and moves; and the end.
 * Every way into a Notion import opens this window: the first run's
 * workspace choice, Settings › 가져오기 and its "/" and page menu ways in,
 * and the agent's link.
 */
export function NotionImportPanel({
  controller, session, workspaceId, arrival = "",
}: NotionImportPanelProps) {
  const { t } = useTranslation(["importDialog", "common"]);
  const {
    busy,
    capabilities,
    capabilityError,
    connectNotion,
    chooseRoots,
    connection,
    savedConnection,
    savedConnectionRejected,
    job,
    listQuery,
    loadMoreScope,
    notionError,
    retryScope,
    roots,
    scopeFailure,
    scopeHasMore,
    scopeQuery,
    searchScope,
    selectRoot,
    selectedRootId,
    selectedRoots,
    setStep,
    step,
    token,
  } = controller;
  const saved = connection ?? savedConnection;
  // A saved connection whose token Notion no longer takes asks for a new one in its place.
  const rejected = !connection && savedConnectionRejected;
  // By hand ("직접 가져오기", small at the start's foot) or the agent's way.
  const [direct, setDirect] = useState(arrival === "token");
  // The move's words went to an agent from here: it may start the migration any moment.
  const [handed, setHanded] = useState(false);
  const phase: NotionEntryPhase | null = step !== "connect" ? null
    : !direct && arrival !== "agent" ? "start"
    : !saved || rejected ? "token" : "saved";
  // The import runs as a migration of the chosen roots as they are; one going on
  // shows again when the screen opens, and on the agent's way as soon as the
  // agent starts it. Another tool's move has its own card.
  const pick = useMemo(() => (migration: NativeNotionMigration) => !migration.source, []);
  const draft = useNotionMigrationDraft({
    // An agent may change the plan over MCP while this shows; follow reads it as it changes.
    active: true, connectionId: connection?.id ?? "", follow: true, pick, roots: selectedRoots, session, workspaceId,
    watch: step === "connect" && !direct && (arrival === "agent" || handed || Boolean(saved)),
    onResume: () => setStep("apply"),
  });
  // A run going on has no step to go back to; a draft goes back as far as its
  // connection goes, and one picked up from before has to be connected again.
  const running = step === "apply" && draft.migration?.runState === "running";
  // A finished run has nothing to go back to or approve.
  const finished = step === "apply" && draft.migration?.runState === "done";
  // The agent's migration: the agent reads, plans with the person and runs it; this follows it.
  const agentRun = step === "apply" && Boolean(draft.migration?.agentHandoff);
  // The steps by hand show on the way by hand.
  const wizard = step === "connect" ? direct : !agentRun;
  const currentStepIndex = steps.findIndex((candidate) => candidate.id === step);
  // The scope lists what the connection reaches at the top; words typed here
  // find a page or database at any depth, a moment after typing stops.
  const [query, setQuery] = useState(scopeQuery);
  const search = useRef(searchScope);
  search.current = searchScope;
  useEffect(() => {
    if (step !== "scope" || query.trim() === scopeQuery.trim()) return;
    const timer = window.setTimeout(() => void search.current(query.trim()), 300);
    return () => window.clearTimeout(timer);
  }, [query, scopeQuery, step]);
  const shownRoots = [...selectedRoots.filter((chosen) => !roots.some((root) => root.id === chosen.id)), ...roots];
  const reading = scopeQuery ? t("importDialog:searchingRoots", { query: scopeQuery }) : t("importDialog:readingRoots");
  // A failure shows beside what asked: a read of the roots under the search
  // field (the first read, a search — with the words and "다시 시도"),
  // "더 찾기"'s under it, a run again's under its row, "계속하기"'s over the
  // footer; the token's under its field (NotionImportAgentFirst). The window's
  // foot line keeps the rest. Only the last press's line shows: another press
  // puts the one before away (2026-11-13: "더 찾기"'s line stayed, and a
  // failed "계속하기" stacked a second line under it).
  const [pressed, setPressed] = useState<"list" | "more" | "start" | "rerun">("list");
  const [startFailed, setStartFailed] = useState(false);
  useEffect(() => {
    setPressed("list");
    setStartFailed(false);
  }, [step]);
  const footError = phase === "token" || step === "scope" ? "" : notionError;
  // An import that isn't a migration's — one this browser remembered from
  // before an import by hand ran as a migration (2026-10-12), shown while it
  // goes (useNotionImportController) — says what it read, how far it made it
  // and about how long it still takes, as a migration's window does
  // (notionMigrationRunProgress), not only that the server handles it.
  const jobGoing = job ? notionImportJobGoing(job) : false;
  const jobProgress = job && jobGoing && !draft.migration ? notionMigrationRunProgress(t, job) : null;
  // What moved before, each with the way to bring what changed since: moving
  // it anew would make a separate copy.
  const [moved, setMoved] = useState<NativeNotionMigration[]>([]);
  useEffect(() => {
    if (step !== "scope") return;
    const controller = new AbortController();
    api.notionMigrations(session, workspaceId, { signal: controller.signal })
      .then((list) => setMoved(list.items.filter((item) => !item.source && (item.everDone || item.runState === "done"))))
      .catch(() => undefined);
    return () => controller.abort();
  }, [session, step, workspaceId]);
  const movedOf = (rootId: string) => moved.find((migration) => migration.roots.some((root) => root.id.replaceAll("-", "") === rootId.replaceAll("-", "")));
  const [rerunError, setRerunError] = useState({ rootId: "", text: "" });
  const rerun = async (rootId: string, migration: NativeNotionMigration) => {
    setPressed("rerun");
    setRerunError({ rootId: "", text: "" });
    try {
      const plan = await api.notionMigrationPlan(session, workspaceId, migration.id);
      draft.show(await api.runNotionMigration(session, workspaceId, migration.id, newId(), plan.revision));
      setStep("apply");
    } catch (error) {
      setRerunError({ rootId, text: userErrorText(error, t("importDialog:migration.draftFailed")) });
    }
  };

  return (
    <section className={styles.panel}>
      {!wizard ? null : <nav className={styles.wizardSteps} aria-label={t("importDialog:wizard.stepsAria")}>
        {steps.map((item, index) => (
          <button
            key={item.id}
            type="button"
            className={styles.wizardStep}
            data-active={step === item.id ? "true" : undefined}
            data-done={currentStepIndex > index ? "true" : undefined}
            disabled={running ? item.id !== "apply" : (item.id === "scope" && !connection)
              || (item.id === "apply" && !selectedRootId && !job && !draft.migration)}
            onClick={() => setStep(item.id)}
          >
            <span className={styles.wizardStepBadge}>{index + 1}</span>
            <span className={styles.wizardStepLabel}>
              {item.id === "apply" ? t("importDialog:migration.approve") : t(`importDialog:${item.label}`)}
            </span>
          </button>
        ))}
      </nav>}

      {phase ? (
        <NotionImportAgentFirst controller={controller} phase={phase} byAgent={!direct} session={session} workspaceId={workspaceId}
          tokenPageUrl={notionTokenPageUrl} onHanded={() => setHanded(true)} />
      ) : step === "scope" ? (
        <fieldset className={styles.scopeList}>
          {/* The heading says what the list shows: the top ones, or what words found. */}
          <legend>{t(listQuery ? "importDialog:rootSearchTitle" : "importDialog:rootPickerTitle")}</legend>
          <input type="search" className={styles.scopeSearch} value={query} placeholder={t("importDialog:rootSearch")}
            aria-label={t("importDialog:rootSearch")} onChange={(event) => { setPressed("list"); setQuery(event.target.value); }} data-notion-root-search />
          {pressed === "list" && scopeFailure?.at === "list" ? (
            <NotionFailureLine text={scopeFailure.text} className={agentFirst.fieldFailure}>
              <button type="button" className={agentFirst.directLink} data-notion-root-retry disabled={busy}
                onClick={() => { setPressed("list"); retryScope(); }}>
                {t("common:retry")}
              </button>
            </NotionFailureLine>
          ) : null}
          {/* "No match" only for words Notion answered; a search that failed says so on its line. */}
          {listQuery && !busy && roots.length === 0 && scopeFailure?.at !== "list" ? <p>{t("importDialog:rootSearchEmpty")}</p> : null}
          {/* The step comes at the press; what the connection reaches, or what words find
              when no list shows, fills in as Notion answers. */}
          {busy && (scopeQuery ? roots.length === 0 : shownRoots.length === 0) ? (
            <div className={styles.jobPanel} data-notion-roots-reading>
              <span>{reading}</span>
              <UploadProgressBar className={styles.progressTrack} label={reading} progress={{ phase: "preparing", percent: 0 }} />
            </div>
          ) : null}
          {shownRoots.map((root) => (<Fragment key={root.id}>
            <label className={styles.scopeOption}>
              <input
                type="checkbox"
                name="notion-import-root"
                aria-label={root.title || untitledPageDisplayTitle(root.kind)}
                value={root.id}
                checked={selectedRoots.some((chosen) => chosen.id === root.id)}
                disabled={busy}
                onChange={() => selectRoot(root.id)}
              />
              <span className={rowStyles.named}>
                <strong>{root.title || untitledPageDisplayTitle(root.kind)}</strong>
                <small>{root.kind === "database"
                  ? t("importDialog:rootKindDataSource")
                  : t("importDialog:rootKindPage")}{movedOf(root.id) ? ` · ${t("importDialog:rootAlreadyMoved")}` : ""}</small>
              </span>
              {movedOf(root.id) ? (
                <span className={rowStyles.actions}>
                  <button type="button" className={rowStyles.open} data-notion-root-rerun disabled={busy} onClick={(event) => {
                    event.preventDefault();
                    void rerun(root.id, movedOf(root.id)!);
                  }}>{t("importDialog:history.rerun")}</button>
                </span>
              ) : null}
            </label>
            {pressed === "rerun" && rerunError.rootId === root.id ? <NotionFailureLine text={rerunError.text} /> : null}
          </Fragment>))}
          {scopeHasMore ? (
            <button
              type="button"
              className={styles.secondary}
              disabled={busy}
              data-notion-root-more
              onClick={() => { setPressed("more"); void loadMoreScope(); }}
            >
              {t("importDialog:expand")}
            </button>
          ) : null}
          {pressed === "more" && scopeFailure?.at === "more" ? <NotionFailureLine text={scopeFailure.text} /> : null}
          {pressed === "start" && startFailed ? <NotionFailureLine text={draft.error} /> : null}
        </fieldset>
      ) : (
        <NotionMigrationDraftView draft={draft} agent={agentRun} session={session} workspaceId={workspaceId} />
      )}

      {job && jobProgress ? (
        <div className={styles.jobPanel} data-notion-import-job-progress>
          <NotionMigrationTimeLeft text={jobProgress.left} />
          <span data-notion-migration-phase={job.phase} data-notion-migration-stage={jobProgress.stage}>{jobProgress.phase}</span>
          <UploadProgressBar className={styles.progressTrack} label={t("importDialog:stepProgress")} progress={jobProgress.bar} />
          {jobProgress.count ? <small>{jobProgress.count}</small> : null}
        </div>
      ) : null}
      {/* Before a job starts there is nothing being handled yet; one that
          ended says how. */}
      {footError ? <NotionFailureLine text={footError} /> : capabilityError || job ? (
        <div className={styles.serverStatus} role={capabilityError ? "alert" : "status"} data-error={capabilityError ? "" : undefined}>
          {capabilityError || (!job || jobGoing
            ? capabilities?.executionOwner === "server"
              ? t("importDialog:wizard.serverRunnerStatus")
              : t("importDialog:preparingImport")
            : t(`importDialog:job.${job.status === "canceled" ? "cancelled" : job.status}`)
          )}
        </div>
      ) : null}
      {phase === "start" ? (
        <footer className={styles.wizardFooter} data-notion-agent-way-footer>
          <span className={styles.wizardHint}>
            <button type="button" className={agentFirst.directLink} data-notion-direct onClick={() => setDirect(true)}>
              {t("importDialog:handoff.direct")}
            </button>
          </span>
        </footer>
      ) : controller.connectionsRead && (phase === "token" || (phase === "saved" && direct)) ? (
        <footer className={styles.wizardFooter}>
          <span className={styles.wizardHint}>
            {/* By hand, the agent's way stays a small step away. */}
            {direct && arrival !== "token" ? (
              <button type="button" className={agentFirst.directLink} data-notion-to-agent onClick={() => setDirect(false)}>
                {t("importDialog:handoff.title")}
              </button>
            ) : null}
          </span>
          {phase === "token" ? (
            // Pressed, it says at once that Notion is being asked about the token.
            <button type="button" className={styles.primary} data-notion-save-token disabled={!token.trim() || busy}
              aria-busy={busy || undefined} onClick={() => void connectNotion()}>
              {busy ? t("importDialog:handoff.checkingToken") : t("importDialog:handoff.saveToken")}
            </button>
          ) : (
            <button type="button" className={styles.primary} data-notion-continue disabled={busy}
              onClick={() => void chooseRoots()}>
              {t("importDialog:wizard.chooseRoots")}
            </button>
          )}
        </footer>
      ) : phase || running || finished || agentRun ? null : (
        <footer className={styles.wizardFooter}>
          <span className={styles.wizardHint} />
          <button
            type="button"
            className={styles.secondary}
            data-notion-back
            onClick={() => setStep(step === "apply" && connection ? "scope" : "connect")}
          >
            {t("importDialog:wizard.back")}
          </button>
          {step === "scope" ? (
            <button
              type="button"
              className={styles.primary}
              data-notion-continue
              disabled={!selectedRootId || busy || draft.busy}
              data-notion-migration-start
              onClick={() => {
                setPressed("start");
                setStartFailed(false);
                void draft.start().then((started) => { if (started) setStep("apply"); else setStartFailed(true); });
              }}
            >
              {t("importDialog:wizard.next")}
            </button>
          ) : <NotionMigrationApprove draft={draft} />}
        </footer>
      )}
    </section>
  );
}
