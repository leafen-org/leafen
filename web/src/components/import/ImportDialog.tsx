import { useEffect } from "react";

import type { Session } from "../../types";
import { useImportSources, type ImportSource } from "./ImportSourcePanels";
import { CsvImportDialog } from "./CsvImportDialog";
import { NotionMigrationWindow } from "./NotionMigrationWindow";

export type { ImportSource } from "./ImportSourcePanels";

/** A file format the slash menu's "가져오기" items import, as Notion's (seen 2026-10-01). */
export type ImportFormat = "csv" | "markdown" | "word" | "zip" | "pdf";

/**
 * Where an import was opened from — a page's own "Import", the slash menu
 * or a database's "CSV와 병합" — whose files go into it, as Notion's do.
 */
export type ImportDestination = {
  pageId: string;
  title: string;
  /** Only files of this format, picked from the slash menu. */
  format?: ImportFormat;
  /** A CSV's rows join the database of this page, from its "CSV와 병합". */
  mergeIntoPage?: boolean;
  /** Which of the page's databases, for an inline database's "CSV와 병합". */
  mergeDataSourceId?: string;
  /** The page took an imported page, which its body shows once read again. */
  onImported?: () => void;
};

type ImportDialogProps = {
  open: boolean;
  session: Session;
  workspaceId: string;
  /** Its name, which Notion's window names. */
  workspaceName?: string;
  initialSource?: ImportSource;
  /** The database page a "CSV와 병합" joins its rows to. */
  destination?: ImportDestination | null;
  onClose: () => void;
};

/**
 * The windows an import opens over a page: "Notion에서 옮기기" and a database's
 * "CSV와 병합". A page's own "가져오기" and the slash menu's open the settings
 * window's, as Notion's do (seen 2026-11-05).
 */
export function ImportDialog({
  open,
  session,
  workspaceId,
  workspaceName,
  initialSource = "file",
  destination = null,
  onClose,
}: ImportDialogProps) {
  const importSources = useImportSources({ active: open, initialSource, session, workspaceId });
  const { setSource } = importSources;

  useEffect(() => {
    if (open) setSource(initialSource);
  }, [initialSource, open, setSource]);

  if (!open) return null;
  if (initialSource === "notion") {
    return (
      <NotionMigrationWindow controller={importSources.notionController} session={session} workspaceId={workspaceId} workspaceName={workspaceName}
        onClose={onClose} />
    );
  }
  if (!destination?.mergeIntoPage) return null;
  return (
    <CsvImportDialog
      session={session}
      workspaceId={workspaceId}
      page={{ pageId: destination.pageId, title: destination.title }}
      merge
      mergeDataSourceId={destination.mergeDataSourceId}
      methodChoice={false}
      initialFile={null}
      onImported={() => destination.onImported?.()}
      onClose={onClose}
    />
  );
}
