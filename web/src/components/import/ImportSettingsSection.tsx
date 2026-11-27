import { useEffect } from "react";
import type { Session } from "../../types";
import { CsvImportDialog } from "./CsvImportDialog";
import type { ImportDestination, ImportSource } from "./ImportDialog";
import { ImportSourcePanels, useImportSources } from "./ImportSourcePanels";
import type { NotionImportArrival } from "./useNotionImportController";

/**
 * The settings window's "가져오기", as Notion's settings hold their import
 * (seen 2026-10-02): the sources and what each imports into the workspace. A
 * page's own "가져오기" opens it with that page as the place, and a slash
 * menu's kind with that kind's window over it (seen 2026-11-05).
 */
export function ImportSettingsSection({
  session,
  workspaceId,
  workspaceName,
  destination = null,
  initialSource,
  arrival = "",
  onNewWorkspaceFromLeafen,
  onNavigate,
}: {
  session: Session;
  workspaceId: string;
  /** Its name, which Notion's window names. */
  workspaceName?: string;
  /** Files go into this page; without it, to the workspace's top level. */
  destination?: ImportDestination | null;
  initialSource?: ImportSource;
  /** How Notion's window was asked for, when it was. */
  arrival?: NotionImportArrival;
  /** Leaves settings to start a workspace from a Leafen file, the one chosen if any. */
  onNewWorkspaceFromLeafen?: (file: File | null) => void;
  /** Leaves settings for another place in Leafen. */
  onNavigate?: (path: string) => void;
}) {
  const sources = useImportSources({ active: true, initialSource, initialArrival: arrival, session, workspaceId });
  const { setCSVWindow } = sources;
  const openCSV = destination?.format === "csv";
  useEffect(() => { if (openCSV) setCSVWindow(true); }, [openCSV, setCSVWindow]);
  return (
    <>
      <ImportSourcePanels
        sources={sources}
        destination={destination}
        onImported={destination?.onImported}
        onNavigate={onNavigate}
        onNewWorkspaceFromLeafen={onNewWorkspaceFromLeafen}
        session={session}
        workspaceId={workspaceId}
        workspaceName={workspaceName}
      />
      {/* A CSV chosen alone, or the CSV card, opens Notion's CSV window. */}
      {sources.csvFile || sources.csvWindow ? (
        <CsvImportDialog
          session={session}
          workspaceId={workspaceId}
          page={destination ? { pageId: destination.pageId, title: destination.title } : null}
          merge={false}
          // "가져오기 방법" is asked where the window opens with no page to
          // fill — from the settings, or a "/csv" line — and not from a page's
          // own "가져오기", as Notion's (seen 2026-11-09).
          methodChoice={(openCSV || !destination) && !sources.csvFile}
          initialFile={sources.csvFile}
          onImported={() => destination?.onImported?.()}
          onNavigate={onNavigate}
          onClose={() => { sources.setCSVFile(null); sources.setCSVWindow(false); }}
        />
      ) : null}
    </>
  );
}
