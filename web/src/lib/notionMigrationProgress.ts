import type { TFunction } from "i18next";
import { activeDateLocale } from "@/lib/i18n";
import {
  nativeNotionImportPhases, nativeNotionImportStatuses, type NativeNotionImportJob, type NativeNotionMigrationRunTally,
} from "../notionImportWire.generated";

/** Where a migration's run is: reading Notion, making pages, asking each
 *  block for its comments, or settling the rest with nothing left to count. */
export type NotionMigrationRunStage = "reading" | "making" | "comments" | "finishing";

export type NotionMigrationRunProgress = {
  stage: NotionMigrationRunStage;
  /** What the run is doing, as "댓글 가져오는 중…". */
  phase: string;
  /** How far, where the stage counts: found so far, made of all, or blocks
   *  asked of all; null while it finishes. */
  count: string | null;
  /** About how long it still takes, said apart near its head, large, where
   *  it is seen at a glance: "약 54분 남음", "곧 끝남"; null with no reckoning
   *  (2026-11-20: at the end of the count's long grey line it went unseen). */
  left: string | null;
  /** Why the time left is far from the time said, beside it — the run's
   *  pace ends it later, or well sooner — as the server words it for the
   *  inbox and the agent alike (notion_migration_time_said.go); null when
   *  it isn't (2026-11-23: a move read 16 hours in the inbox and 6 in its
   *  status, with no word why). */
  leftWhy: string | null;
  /** The bar's fill: a migration's run by the share of its time gone, the
   *  same reckoning as its time left, through all its stages; otherwise the
   *  stage's own count; "preparing" where there is no end to measure. */
  bar: { phase: "preparing" | "uploading"; percent: number };
};

/** How a migration's run stands, said the same in its Notion window and on
 *  its 완료됨 row (2026-11-07: the row stood at "12971개 중 12971개" for the
 *  hour the window counted the blocks' comments). Another tool's migration
 *  (`source`, the tool's name) reads from that tool, not Notion
 *  (2026-11-12: an Obsidian vault's run said "Notion에서 읽는 중…"). */
export function notionMigrationRunProgress(t: TFunction, job: NativeNotionImportJob, source = ""): NotionMigrationRunProgress {
  return { ...notionMigrationRunStage(t, job, source), leftWhy: job.progress?.leftSay || null };
}

function notionMigrationRunStage(t: TFunction, job: NativeNotionImportJob, source: string): Omit<NotionMigrationRunProgress, "leftWhy"> {
  const left = notionMigrationLeftText(t, job.progress?.minutesLeft ?? 0);
  const shared = notionMigrationRunBar(job);
  // Reading its source says what it has found, and how many of the bodies
  // there are it has read (2026-11-20: a bar sliding with no end looked
  // empty at 171 of 1,081 bodies).
  if (job.phase === nativeNotionImportPhases.scope) {
    // An import no migration runs fills by its reads: the bodies, then what
    // they hold (2026-11-20: full at "41 of 41 bodies" for the 17 minutes
    // the blocks inside them were read).
    const reads = (job.progress?.bodiesTotal ?? 0) + (job.progress?.innerTotal ?? 0);
    const done = (job.progress?.bodiesRead ?? 0) + (job.progress?.innerRead ?? 0);
    return { stage: "reading", phase: source ? t("importDialog:tools.phaseReading", { name: source }) : t("importDialog:migration.phaseReading"),
      count: notionMigrationReadingText(t, job, source), left,
      bar: shared ?? (reads > 0 ? { phase: "uploading", percent: (done / reads) * 100 } : { phase: "preparing", percent: 0 }) };
  }
  // All of it made, the run still settles it and writes its report
  // (2026-11-07: "13030개 중 13030개" stood 4 minutes under a full bar).
  const made = job.totalItems > 0 && job.processedItems >= job.totalItems;
  // A run again counts what it made new, changed and left as it was, from
  // its making on (2026-11-16: a run again said "13391 of 13391 made" and
  // reported 0).
  const tally = job.progress?.tally ? notionMigrationTallyText(t, job.progress.tally) : "";
  const withTally = (count: string) => [count, tally].filter(Boolean).join(" · ");
  if (!made) {
    return { stage: "making", phase: t("importDialog:migration.phaseMaking"),
      count: withTally(t("importDialog:ofTotal", { done: job.processedItems, total: job.totalItems })), left,
      bar: shared ?? { phase: job.status === nativeNotionImportStatuses.queued ? "preparing" : "uploading",
        percent: (job.processedItems / Math.max(job.totalItems, 1)) * 100 } };
  }
  // Then the comments, by the job's one stage (2026-11-16: the pages'
  // comments, asked first, read "마무리하는 중…"): the pages and rows asked
  // of all, then the blocks — each counted as asked, so the line moves
  // while either is (2026-11-20: it stood with no count while the pages
  // were asked).
  const asked = job.commentBlocksDone;
  const blocks = job.commentBlocksTotal;
  if (job.stage === "comments" || (!job.stage && blocks > 0 && asked < blocks)) {
    const pages = job.commentPagesTotal ?? 0;
    const count = blocks > 0 ? t("importDialog:migration.commentsBlocks", { done: asked, total: blocks })
      : pages > 0 ? t("importDialog:migration.commentsPages", { done: job.commentPagesDone ?? 0, total: pages }) : "";
    return { stage: "comments", phase: t("importDialog:migration.phaseComments"),
      count: withTally(count) || null, left,
      bar: shared ?? (blocks > 0 ? { phase: "uploading", percent: (asked / blocks) * 100 } : { phase: "preparing", percent: 100 }) };
  }
  // After making, the step it is at while the made count stands: the second
  // pass for what the plan merges, the links, showing the pages, the plan's
  // changes (2026-11-20: "마무리하는 중…" stood with no count through them).
  const step = job.stage === "creating" ? notionMigrationStepPhase(t, job.step ?? "") : null;
  if (step) {
    return { stage: "making", phase: step, count: tally || null, left, bar: shared ?? { phase: "preparing", percent: 100 } };
  }
  if (job.stage === "verifying") {
    return { stage: "finishing", phase: t("importDialog:migration.phaseVerifying"), count: tally || null, left,
      bar: shared ?? { phase: "preparing", percent: 100 } };
  }
  return { stage: "finishing", phase: t("importDialog:migration.phaseFinishing"), count: tally || null, left,
    bar: shared ?? { phase: "preparing", percent: 100 } };
}

/** What a migration's run does at a step of making after its pages are
 *  made, as its line says it (the server's step, notionImportJobStageStep);
 *  null for a step it says otherwise. */
function notionMigrationStepPhase(t: TFunction, step: string): string | null {
  switch (step) {
    case "merge": return t("importDialog:migration.stepMerge");
    case "link": return t("importDialog:migration.stepLink");
    case "publish": return t("importDialog:migration.stepPublish");
    case "plan": return t("importDialog:migration.stepPlan");
    case "moves": return t("importDialog:migration.stepMoves");
    case "people": return t("importDialog:migration.stepPeople");
    case "rerun": return t("importDialog:migration.stepRerun");
    case "formats": return t("importDialog:migration.stepFormats");
  }
  return null;
}

/** A migration's run's bar, by the share of its time gone (the server's
 *  reckoning, notionMigrationTimeShare), so it never empties as a stage
 *  begins; null for an import no migration runs, or before its time is kept. */
function notionMigrationRunBar(job: NativeNotionImportJob): NotionMigrationRunProgress["bar"] | null {
  const share = job.progress?.timeShare ?? 0;
  return share > 0 ? { phase: "uploading", percent: share / 10 } : null;
}

/** What a run again made new, changed and left as it was, as its window
 *  and its report count them; a first run makes all of it new. */
export function notionMigrationTallyText(t: TFunction, tally: NativeNotionMigrationRunTally): string {
  const total = (counts: NativeNotionMigrationRunTally["made"]) => counts.pages + counts.databases + counts.rows + counts.blocks;
  return t("importDialog:migration.tally", { made: total(tally.made), changed: total(tally.changed), kept: total(tally.kept) });
}

/** Whether a run's counts are a run again's: one that changed or left
 *  something as it was. */
export function notionMigrationTallyAgain(tally: NativeNotionMigrationRunTally | null | undefined): tally is NativeNotionMigrationRunTally {
  if (!tally) return false;
  const { changed, kept } = tally;
  return changed.pages + changed.databases + changed.rows + changed.blocks + kept.pages + kept.databases + kept.rows + kept.blocks > 0;
}

/** What stopping a running import does, as its button says: while a
 *  migration's run brings its comments, the content is done and stopping
 *  ends only the comments; before its pages show, it cancels the import. */
export function notionImportStopLabel(t: TFunction, job: NativeNotionImportJob): string {
  return notionMigrationRunProgress(t, job).stage === "comments" ? t("importDialog:migration.stopComments") : t("importDialog:cancelImport");
}

/** A time in minutes as a migration's every time says it, the time left and
 *  a run again's alike: in hours and minutes past an hour ("8시간 30분"), as
 *  the server's sentences say it (notionMigrationDuration). The Completed
 *  row's run again once said "약 510분" beside "8시간 30분" (2026-11-21). */
export function notionMigrationDurationText(t: TFunction, minutes: number): string {
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  return [hours ? t("importDialog:migration.durationHours", { count: hours }) : "",
    rest ? t("importDialog:migration.durationMinutes", { count: rest }) : ""].filter(Boolean).join(" ");
}

/** A run's time left as its window, its card and its row say it: a
 *  migration's goes at the pace the run has gone, never past the end it
 *  promised, the server's one reckoning (notion_migration_time_left.go), in
 *  hours and minutes past an hour; its last minute is almost done; null
 *  with none. */
export function notionMigrationLeftText(t: TFunction, left: number): string | null {
  if (left <= 0) return null;
  if (left === 1) return t("importDialog:migration.leftAlmostDone");
  return t("importDialog:migration.leftAbout", { time: notionMigrationDurationText(t, left) });
}

/** What a migration reading Notion, or another tool's bundle, says it has:
 *  the pages, rows and blocks read, the bodies and databases still to read,
 *  the source's files it is copying, and how long nothing moved (about how
 *  long it takes is said apart, notionMigrationLeftText) — a first move's
 *  reading makes nothing in Leafen yet (2026-11-12: 37 minutes of "0 of
 *  13,277" left the person asking whether it had stopped); a run again's
 *  leaves what moved before as it is. */
function notionMigrationReadingText(
  t: TFunction, job: Pick<NativeNotionImportJob, "totalItems" | "filesPending" | "progress">, source: string, now = Date.now(),
): string {
  const progress = job.progress;
  const parts = progress
    ? [t("importDialog:migration.phaseRead", { pages: progress.readPages, rows: progress.readRows, blocks: progress.readBlocks })]
    : [t("importDialog:migration.phaseFound", { count: job.totalItems })];
  if (progress?.readTrashedRows) parts.push(t("importDialog:migration.phaseTrashedRows", { count: progress.readTrashedRows }));
  // A database's templates are no rows: Notion counts them apart.
  if (progress?.readTemplates) parts.push(t("importDialog:migration.phaseTemplates", { count: progress.readTemplates }));
  // The bodies read of all there are: it only goes up (2026-11-16: "bodies
  // left" went 226, 343, 546, 700 as reading found more).
  if (progress?.bodiesTotal) parts.push(t("importDialog:migration.phaseBodies", { read: progress.bodiesRead ?? 0, total: progress.bodiesTotal }));
  // Then the blocks inside them that hold more, read a level at a time, by
  // the reckoning its time left goes by (notion_migration_reading_work.go):
  // once some are read, or every body is (2026-11-20: "41 of 41 bodies
  // read" stood still while four reads in five went on).
  const inner = progress?.innerTotal ?? 0;
  if (inner > 0 && ((progress?.innerRead ?? 0) > 0 || (progress?.bodiesRead ?? 0) >= (progress?.bodiesTotal ?? 0))) {
    parts.push(t("importDialog:migration.phaseInner", { read: progress?.innerRead ?? 0, total: inner }));
  }
  if (progress?.waitingDatabases) parts.push(t("importDialog:migration.phaseWaitingDatabases", { count: progress.waitingDatabases }));
  if (job.filesPending > 0) {
    parts.push(source ? t("importDialog:tools.phaseCopying", { name: source, count: job.filesPending })
      : t("importDialog:migration.phaseCopying", { count: job.filesPending }));
  }
  const still = progress?.changedAt ? Math.floor((now - progress.changedAt) / 60_000) : 0;
  if (still >= 5) parts.push(t("importDialog:migration.phaseStill", { time: notionMigrationDurationText(t, still) }));
  // A run again leaves what the runs before moved as it is, whole as of the
  // last that finished, and changes only what the source changed since — the
  // server's one judgement (notion_import_filling_again.go; 2026-11-23: a run
  // again read as if nothing had come yet).
  const movedAsOf = progress?.movedAsOf ?? 0;
  if (movedAsOf > 0) {
    const time = new Intl.DateTimeFormat(activeDateLocale(), { dateStyle: "long", timeStyle: "short" }).format(movedAsOf);
    return `${parts.join(" · ")}. ${source ? t("importDialog:tools.phaseChangesLater", { name: source, time })
      : t("importDialog:migration.phaseChangesLater", { time })}`;
  }
  return `${parts.join(" · ")}. ${t("importDialog:migration.phaseMadeLater")}`;
}

/** The followed state of a workspace's Notion migrations, heard by every screen that shows them (followAcrossTabs). */
export const notionMigrationsTopic = (userId: string, workspaceId: string) => `notion-migrations:${userId}:${workspaceId}`;

/** The followed state of one Notion import run's job (followAcrossTabs). */
export const notionImportJobTopic = (userId: string, jobId: string) => `notion-import-job:${userId}:${jobId}`;
