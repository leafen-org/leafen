import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { readAndFollow } from "@/lib/followAcrossTabs";
import { pageHref } from "@/lib/navigation";
import { notionImportJobTopic, notionMigrationRunProgress, notionMigrationsTopic } from "@/lib/notionMigrationProgress";
import { nativeNotionImportStatuses, type NativeNotionImportJob, type NativeNotionMigration } from "@/notionImportWire.generated";
import type { Session } from "@/types";
import { NotionMigrationStopLines } from "./NotionMigrationStopLines";
import { NotionMigrationTimeLeft } from "./NotionMigrationTimeLeft";
import { UploadProgressBar } from "./UploadProgressBar";
import styles from "./ImportDialog.module.css";
import own from "./NotionMigrationRunCards.module.css";

// How often a run going on is read again, as its 완료됨 row reads it.
const READ_EVERY_MS = 3000;

/** Whether a migration's run goes on. */
export const notionMigrationRunGoing = (migration: NativeNotionMigration) => migration.status === "scanned" && migration.runState === "running";
const runGoing = notionMigrationRunGoing;

// How many times the import screen reads a workspace's Notion moves before it
// gives up, 2, 4 and 8 seconds apart: a read that fails says nothing of a move
// going on (2026-11-23: one refused read opened the window to start a move
// over a run going again, and left its card unshown).
const MOVES_READ_TRIES = 4;

/** A workspace's Notion moves, read again a few times while the reads fail. */
export async function readNotionMoves(session: Session, workspaceId: string, signal: AbortSignal) {
  for (let tried = 0; ; tried += 1) {
    try {
      return await api.notionMigrations(session, workspaceId, { signal });
    } catch (error) {
      if (signal.aborted || tried + 1 >= MOVES_READ_TRIES) throw error;
      await new Promise<void>((waited, stopped) => {
        const timer = setTimeout(waited, 2_000 * 2 ** tried);
        signal.addEventListener("abort", () => { clearTimeout(timer); stopped(signal.reason); }, { once: true });
      });
    }
  }
}

/**
 * The import screen's "둘러보기" says the migrations running, as it says the
 * file imports going on (ImportBatchCards): how far each is, and the way back
 * to its window. A run that ends here says how, once; one that ended while
 * away is on "완료됨" (2026-11-07: a migration whose window was closed showed
 * nowhere but in the Notion card).
 */
export function NotionMigrationRunCards({ onOpen, readAgain, session, workspaceId }: {
  /** Opens the Notion window, which picks up the migration going on. */
  onOpen(): void;
  /** Changes when a run may have begun out of sight (the Notion window closed). */
  readAgain: unknown;
  session: Session;
  workspaceId: string;
}) {
  const [shown, setShown] = useState<NativeNotionMigration[]>([]);
  // A card that retried its run reads the list again, to follow it.
  const [retried, setRetried] = useState(0);
  const going = shown.some(runGoing);
  useEffect(() => {
    const controller = new AbortController();
    const stop = readAndFollow(notionMigrationsTopic(session.user.id, workspaceId), READ_EVERY_MS, going,
      () => readNotionMoves(session, workspaceId, controller.signal).then((list) => list.items),
      (value) => {
        if (!Array.isArray(value)) return;
        setShown((current) => (value as NativeNotionMigration[]).filter((item) => runGoing(item)
          // One shown stays to say how it ended.
          || current.some((shownItem) => shownItem.id === item.id)));
      });
    return () => {
      stop();
      controller.abort();
    };
  }, [going, readAgain, retried, session, workspaceId]);
  return <>{shown.map((migration) => (
    <NotionMigrationRunCard key={migration.id} migration={migration} onOpen={onOpen} onRetried={() => setRetried((count) => count + 1)}
      session={session} workspaceId={workspaceId} />
  ))}</>;
}

function NotionMigrationRunCard({ migration, onOpen, onRetried, session, workspaceId }: {
  migration: NativeNotionMigration;
  onOpen(): void;
  onRetried(): void;
  session: Session;
  workspaceId: string;
}) {
  const { t } = useTranslation("importDialog");
  const [job, setJob] = useState<NativeNotionImportJob | null>(null);
  const [acting, setActing] = useState(false);
  const running = runGoing(migration);
  useEffect(() => {
    if (!migration.importJobId) return;
    const controller = new AbortController();
    const stop = readAndFollow(notionImportJobTopic(session.user.id, migration.importJobId), READ_EVERY_MS, running,
      () => api.notionImportJob(session, workspaceId, migration.importJobId, { signal: controller.signal }),
      (value) => { if (value && typeof value === "object") setJob(value as NativeNotionImportJob); });
    return () => {
      stop();
      controller.abort();
    };
  }, [migration.importJobId, running, session, workspaceId]);
  const canceled = !running && job?.status === nativeNotionImportStatuses.canceled;
  // A run canceled is put away where it was canceled.
  if (canceled) return null;
  const done = migration.runState === "done";
  const stopped = migration.runState === "failed";
  const jobGoing = job?.status === nativeNotionImportStatuses.queued || job?.status === nativeNotionImportStatuses.running;
  const progress = running && job && jobGoing ? notionMigrationRunProgress(t, job, migration.source ?? "") : null;
  const title = migration.roots[0]?.title?.trim() || (migration.source ? t("history.toolMigration", { name: migration.source }) : t("history.migration"));
  const heading = done ? t("migration.done") : stopped ? t("migration.stopped") : t(migration.everDone ? "history.rerunning" : "migration.running");
  async function retry() {
    if (!job) return;
    setActing(true);
    try {
      setJob(await api.retryNotionImportJob(session, workspaceId, job.id));
    } catch {
      // The next read says how it stands.
    } finally {
      setActing(false);
      onRetried();
    }
  }
  return (
    <div className={styles.jobPanel} data-import-notion-migration={running ? "running" : done ? "done" : "stopped"}>
      <strong>{title} · {heading}</strong>
      {progress ? (
        <>
          <NotionMigrationTimeLeft text={progress.left} />
          <span data-import-notion-migration-stage={progress.stage}>{progress.phase}</span>
          <UploadProgressBar className={styles.progressTrack} label={t("stepProgress")} progress={progress.bar} />
          {progress.count ? <small>{progress.count}</small> : null}
          {progress.leftWhy ? <small data-notion-migration-left-why>{progress.leftWhy}</small> : null}
        </>
      ) : null}
      {/* The step it stopped at, why, and what trying again does. */}
      {stopped && job ? <NotionMigrationStopLines job={job} migration={migration} source={migration.source ?? ""} /> : null}
      {/* What comes after a move from Notion, as its window says it. */}
      {done && !migration.source ? <small data-notion-migration-after-done>{t("migration.afterDone")}</small> : null}
      <span className={own.actions}>
        {/* Another tool's run has its own window, from its card. */}
        {running && !migration.source ? (
          <button type="button" className={styles.secondary} data-import-notion-migration-open onClick={onOpen}>{t("progressView.open")}</button>
        ) : null}
        {stopped && job ? (
          <button type="button" className={styles.secondary} disabled={acting} onClick={() => void retry()}>{t("retry")}</button>
        ) : null}
        {done && migration.importTopPageId ? (
          <a className={styles.secondary} href={pageHref(migration.importTopPageId)}>{t("migration.open")}</a>
        ) : null}
      </span>
    </div>
  );
}
