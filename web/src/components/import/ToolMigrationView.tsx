import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { notionImportFailure, notionRequestFailureText } from "@/lib/notionImportFailure";
import { durableJobUnreachable } from "@/lib/durableJobPolling";
import { userErrorText } from "@/lib/settingsErrors";

import { api } from "../../api";
import type { BundleSource } from "../../bundleSourcesApi";
import { newId } from "../../lib/ids";
import { pageHref } from "../../lib/navigation";
import type { NativeNotionImportRoot, NativeNotionMigration } from "../../notionImportWire.generated";
import type { Session } from "../../types";
import { MigrationReconnecting, NotionMigrationApprove, NotionMigrationDraftView } from "./NotionMigrationDraftView";
import { UploadProgressBar } from "./UploadProgressBar";
import { useNotionMigrationDraft } from "./useNotionMigrationDraft";
import styles from "./ImportDialog.module.css";
import toolWindow from "./ToolImportWindow.module.css";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";

// The migration of a tool's bundle, once Leafen has it: its top page (or
// the one chosen) handed to an agent that plans the move with the person,
// as from Notion — moves go only through agents (the 2026-10-14 recommended
// structure decision, 6) — or, for a tool moved before, the plan agreed then
// run again on the new bundle. `onDone` says the run is over.

const pollMs = 1500;

export function ToolMigrationView({ source, earlier, session, workspaceId, onDone }: {
  source: BundleSource;
  earlier: NativeNotionMigration | null;
  session: Session;
  workspaceId: string;
  onDone(): void;
}) {
  return earlier
    ? <ToolMigrationAgain migration={earlier} session={session} workspaceId={workspaceId} onDone={onDone} />
    : <ToolMigrationFirst source={source} session={session} workspaceId={workspaceId} onDone={onDone} />;
}

function ToolMigrationFirst({ source, session, workspaceId, onDone }: {
  source: BundleSource; session: Session; workspaceId: string; onDone(): void;
}) {
  const { t } = useTranslation(["importDialog", "common"]);
  const [roots, setRoots] = useState<NativeNotionImportRoot[] | null>(null);
  const [chosen, setChosen] = useState<NativeNotionImportRoot | null>(null);
  const [failed, setFailed] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    api.notionImportScope(session, workspaceId, source.connectionId, "", { signal: controller.signal })
      .then((scope) => {
        setRoots(scope.items);
        if (scope.items.length === 1) setChosen(scope.items[0]);
      })
      .catch((error: unknown) => { if (!controller.signal.aborted) setFailed(notionRequestFailureText(t, error, t("importDialog:cantScanRoots"), true)); });
    return () => controller.abort();
  }, [session, source.connectionId, t, workspaceId]);
  const chosenRoots = useMemo(() => chosen ? [chosen] : [], [chosen]);
  const draft = useNotionMigrationDraft({
    active: Boolean(chosen), connectionId: source.connectionId, follow: true, pick: null, roots: chosenRoots, session, workspaceId, onResume: () => undefined,
  });
  const { start } = draft;
  const begun = useRef(false);
  useEffect(() => {
    if (!chosen || begun.current) return;
    begun.current = true;
    void start();
  }, [chosen, start]);
  const done = draft.migration?.runState === "done";
  useEffect(() => { if (done) onDone(); }, [done, onDone]);

  if (failed) return <div className={styles.serverStatus} role="alert" data-error>{failed}</div>;
  if (!roots) return <div className={styles.jobPanel} role="status"><strong>{t("importDialog:migration.reading")}</strong></div>;
  if (!chosen) {
    // A bundle with more than one top page: the person picks what moves, as from Notion.
    return (
      <fieldset className={styles.scopeList} data-tool-roots>
        <legend>{t("importDialog:tools.chooseRoot")}</legend>
        {roots.map((root) => (
          <label key={root.id} className={styles.scopeOption}>
            <input type="radio" name="tool-root" aria-label={root.title || untitledPageDisplayTitle(root.kind)} onChange={() => setChosen(root)} />
            <span><strong>{root.title || untitledPageDisplayTitle(root.kind)}</strong></span>
          </label>
        ))}
      </fieldset>
    );
  }
  // The draft's "가져오기" keeps to the window's foot, as in the Notion window.
  return (
    <>
      <NotionMigrationDraftView draft={draft} session={session} workspaceId={workspaceId} />
      <footer className={toolWindow.foot}><NotionMigrationApprove draft={draft} /></footer>
    </>
  );
}

function ToolMigrationAgain({ migration: initial, session, workspaceId, onDone }: {
  migration: NativeNotionMigration; session: Session; workspaceId: string; onDone(): void;
}) {
  const { t } = useTranslation(["importDialog", "common"]);
  const [migration, setMigration] = useState(initial);
  const [ran, setRan] = useState(false);
  const [error, setError] = useState("");
  const [reconnecting, setReconnecting] = useState(false);
  // The run is followed until it ends, through a session renewed meanwhile
  // and requests lost while the server is busy or away (2026-11-05: two runs
  // again under heavy load ended with the window still running).
  const sessionRef = useRef(session);
  sessionRef.current = session;
  const begun = useRef(false);
  useEffect(() => {
    if (begun.current) return;
    begun.current = true;
    const controller = new AbortController();
    const signal = controller.signal;
    const stopped = () => signal.aborted;
    const pause = (ms: number) => new Promise((resolve) => window.setTimeout(resolve, ms));
    const ask = async <T,>(call: (current: Session) => Promise<T>) => {
      for (let delay = pollMs; ; delay = Math.min(delay * 2, 5_000)) {
        try {
          const answer = await call(sessionRef.current);
          if (!stopped()) setReconnecting(false);
          return answer;
        } catch (caught) {
          if (stopped() || !durableJobUnreachable(caught)) throw caught;
          setReconnecting(true);
          await pause(delay);
        }
      }
    };
    void (async () => {
      // The new bundle is scanned as Notion's next state would be, then the
      // plan the person agreed to runs again.
      let current = await ask((now) => api.scanNotionMigration(now, workspaceId, initial.id));
      while (!stopped() && current.status === "scanning") {
        setMigration(current);
        setError("");
        setReconnecting(false);
        current = await ask((now) => api.scanNotionMigration(now, workspaceId, initial.id));
      }
      if (stopped()) return;
      const plan = await ask((now) => api.notionMigrationPlan(now, workspaceId, initial.id));
      const requestId = newId();
      current = await ask((now) => api.runNotionMigration(now, workspaceId, initial.id, requestId, plan.revision));
      while (!stopped() && current.runState === "running") {
        setMigration(current);
        setError("");
        setReconnecting(false);
        await pause(pollMs);
        current = await ask((now) => api.notionMigration(now, workspaceId, initial.id));
      }
      if (!stopped()) {
        setMigration(current);
        setError("");
        setReconnecting(false);
        setRan(current.runState === "done");
        // A run whose import failed says why; running again goes on from what moved.
        if (current.runState === "failed") {
          const job = current.importJobId ? await api.notionImportJob(sessionRef.current, workspaceId, current.importJobId).catch(() => null) : null;
          if (!stopped()) setReconnecting(false);
          const failure = notionImportFailure(t, job, current.source ?? "");
          if (!stopped()) setError([t("importDialog:migration.stopped"), failure.text, failure.reference].filter(Boolean).join(" "));
        }
      }
    })().catch((caught: unknown) => { if (!stopped()) setReconnecting(false); if (!stopped()) setError(userErrorText(caught, t("importDialog:migration.readFailed"))); });
    return () => controller.abort();
    // Once: a later session or language reads through the refs and t.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initial.id, workspaceId]);
  const done = ran;
  useEffect(() => { if (done) onDone(); }, [done, onDone]);
  const percent = useMemo(() => (migration.status === "scanning" ? 0 : done ? 100 : 50), [done, migration.status]);
  return (
    <div className={styles.jobPanel} data-tool-migration-again={done ? "done" : "running"}>
      <strong>{done ? t("importDialog:migration.done") : t("importDialog:tools.again")}</strong>
      {!done ? (
        <UploadProgressBar className={styles.progressTrack} label={t("importDialog:tools.again")}
          progress={{ phase: migration.status === "scanning" ? "preparing" : "uploading", percent }} />
      ) : null}
      <p>{t("importDialog:tools.againLead")}</p>
      {done && migration.importTopPageId ? (
        <a className={styles.primary} href={pageHref(migration.importTopPageId)} data-notion-migration-open>
          {t("importDialog:migration.open")}
        </a>
      ) : null}
      {error ? <div className={styles.serverStatus} role="alert" data-error>{error}</div> : null}
      {reconnecting ? <MigrationReconnecting /> : null}
    </div>
  );
}
