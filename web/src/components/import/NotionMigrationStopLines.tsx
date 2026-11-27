import { useTranslation } from "react-i18next";
import { pageHref } from "@/lib/navigation";
import { notionImportFailure } from "@/lib/notionImportFailure";
import type { NativeNotionImportJob, NativeNotionMigration } from "@/notionImportWire.generated";

// Causes the run's job names as its own code's, which say better what
// stopped it than the code: a page or row someone took away meanwhile, as
// the report's first line and the inbox notice say (notionMigrationFailureReasonKo).
const causeKeys: Record<string, string> = {
  LEAFEN_DATABASE_ROW_PAGE_IN_TRASH: "trashed",
  LEAFEN_PAGE_IN_TRASH: "trashed",
  LEAFEN_DATABASE_ROW_PAGE_NOT_FOUND: "deleted",
  LEAFEN_PAGE_NOT_FOUND: "deleted",
};

/**
 * A stopped migration run as its card, its 완료됨 row and its window say it,
 * in the words of the report's first line and the inbox notice: the step it
 * stopped at with the way to what it was changing, why, and what "다시 시도"
 * does — go on from that step, reading nothing before it from Notion again —
 * apart from "다시 가져오기", which reads all of Notion again (2026-11-16:
 * the card said only that the run stopped, and the two buttons read alike).
 * A run stopped before its stop was kept says why alone, as before.
 */
export function NotionMigrationStopLines({ compact = false, job, migration, source }: {
  /** A row's lines are small, under its name; a card's and a window's are paragraphs. */
  compact?: boolean;
  job: NativeNotionImportJob;
  migration: NativeNotionMigration;
  source: string;
}) {
  const { t } = useTranslation("importDialog");
  const Line = compact ? "small" : "p";
  const failure = notionImportFailure(t, job, source);
  const stop = migration.runReport.failure;
  const cause = stop && stop.code === "LEAFEN_JOB_RUNNER_FAILED" ? causeKeys[stop.causeCode] : undefined;
  const text = cause ? t(`migration.failure.${cause}`) : failure.text;
  const target = stop && stop.targetId && (stop.targetKind === "page" || stop.targetKind === "row") ? stop.targetId : "";
  return (
    <>
      {stop ? (
        <Line role="alert" data-import-notion-migration-stopped-stage={stop.stage}>
          {t("migration.stoppedAt", { stage: t(`migration.stages.${stop.stage}`) })}
          {target ? <> <a href={pageHref(target)} data-import-notion-migration-stopped-target>{t("migration.openStopped")}</a></> : null}
        </Line>
      ) : null}
      <Line role={stop ? undefined : "alert"} data-notion-migration-failure={job.errorCode || undefined}>{text}</Line>
      {failure.reference ? <small data-import-failure-reference>{failure.reference}</small> : null}
      {stop?.retrySafe ? <Line data-import-notion-migration-resume>{t("migration.retryGoesOn")}</Line> : null}
    </>
  );
}
