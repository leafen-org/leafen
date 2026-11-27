import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { api } from "@/api";
import { readNotionImportFileOutcomeWindow } from "@/lib/notionImportFileOutcomeContinuation";
import {
  nativeNotionImportErrorCodes,
  type NativeNotionImportJob,
  type NativeNotionMigration,
} from "../../notionImportWire.generated";
import type { Session } from "../../types";
import styles from "./ImportDialog.module.css";

/**
 * The files a finished migration's run couldn't bring because Notion no
 * longer gave them, and besides those earlier runs couldn't copy that this run
 * didn't try, from the move report's one count (filesNotCopied,
 * filesNotCopiedEarlier: notion_migration_report_files.go); those its plan left
 * in Notion are said apart (runReport.filesSkipped). Told when the run ends,
 * a person who then cleaned up Notion lost them (2026-11-14: the window said
 * only that it was done); told this run's alone, they heard two numbers
 * (2026-11-20: the window two files, the report four).
 */
export function notionMigrationFilesOmitted(migration: NativeNotionMigration) {
  const done = migration.runState === "done";
  return { run: done ? migration.filesNotCopied ?? 0 : 0, earlier: done ? migration.filesNotCopiedEarlier ?? 0 : 0 };
}

/** The window's and the import list's line of them, "" for none. */
export function notionMigrationFilesOmittedText(t: TFunction, migration: NativeNotionMigration) {
  const { run, earlier } = notionMigrationFilesOmitted(migration);
  // Those earlier runs couldn't copy are said besides this run's, or alone.
  const before = earlier ? t(run ? "importDialog:migration.filesOmittedBesides" : "importDialog:migration.filesOmittedEarlier", { count: earlier }) : "";
  return [run ? t("importDialog:migration.filesOmitted", { count: run }) : "", before].filter(Boolean).join(" ");
}

/**
 * A finished migration's window says how many files Notion no longer gave,
 * not to delete the Notion originals meanwhile, and copies them again once
 * restored there ("파일 다시 복사", as the import window before migrations).
 */
export function NotionMigrationOmittedFiles({ migration, job, session, workspaceId }: {
  migration: NativeNotionMigration;
  job: NativeNotionImportJob | null;
  session: Session;
  workspaceId: string;
}) {
  const { t } = useTranslation("importDialog");
  // The migration as read again after copying, until the window reads a newer one.
  const [read, setRead] = useState<NativeNotionMigration | null>(null);
  const [busy, setBusy] = useState(false);
  const [said, setSaid] = useState("");
  useEffect(() => { setRead(null); }, [migration]);
  const current = read ?? migration;
  const omitted = notionMigrationFilesOmitted(current);
  const line = notionMigrationFilesOmittedText(t, current);
  const shown = job;
  if (!shown || (!line && !said)) return null;
  async function copyAgain() {
    if (!shown || busy) return;
    setBusy(true);
    setSaid("");
    let copied = 0;
    let left = 0;
    try {
      let cursor = "";
      do {
        const page = await readNotionImportFileOutcomeWindow(session, workspaceId, shown.id, { sourceCursor: cursor });
        for (const outcome of page.items) {
          // Copied ones stand, and those the plan left in Notion stay there.
          if (outcome.status === "copied" || outcome.errorCode === nativeNotionImportErrorCodes.migrationFileSkipped) continue;
          const after = await api.retryNotionImportFile(session, workspaceId, shown.id, outcome.sourceId);
          if (after.status === "copied") copied += 1;
          else left += 1;
        }
        cursor = page.hasMore ? page.nextCursor : "";
      } while (cursor);
      setRead(await api.notionMigration(session, workspaceId, current.id));
      setSaid(t("fileRetryFinished", { copied, skipped: left }));
    } catch {
      setSaid(t("cantRetryFiles"));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className={styles.omissionNotice} data-notion-migration-files-omitted={omitted.run}
      data-notion-migration-files-omitted-earlier={omitted.earlier || undefined}>
      {line ? <p role="status">{line}</p> : null}
      {omitted.run > 0 ? (
        <button type="button" className={styles.secondary} disabled={busy} data-notion-migration-files-retry onClick={() => void copyAgain()}>
          {t("migration.retryFiles")}
        </button>
      ) : null}
      {said ? <small role="status">{said}</small> : null}
    </div>
  );
}
