import { ArrowLeft, ServiceIcon } from "@/icons/leafen";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { api } from "../../api";
import type { Session } from "../../types";
import { DirectLeafenArchiveImportPanel } from "./DirectLeafenArchiveImportPanel";
import { DirectMarkdownImportPanel } from "./DirectMarkdownImportPanel";
import { useAppPlatformAvailable } from "@/lib/appPlatformAvailability";
import { takeHeldWorkspaceFile } from "@/lib/openWorkspace";
import { ImportSourceCard } from "./ImportBrowse";
import { ImportHistory } from "./ImportHistory";
import { ImportToolCards } from "./ImportToolCards";
import { NotionMigrationRunCards, notionMigrationRunGoing, readNotionMoves } from "./NotionMigrationRunCards";
import { NotionMigrationWindow } from "./NotionMigrationWindow";
import { useNotionImportController, type NotionImportArrival } from "./useNotionImportController";
import styles from "./ImportDialog.module.css";
import tabStyles from "./ImportSourceTabs.module.css";

// What can be imported, and from where: files, a Notion workspace, or a
// Leafen archive. The settings window's "가져오기" browses them as Notion's
// does, and its Notion card opens the "Notion에서 옮기기" window over it.

export type ImportSource = "file" | "notion" | "leafen";

/** The source shown, a CSV chosen alone, and the Notion import under way. */
export function useImportSources({ active, initialSource = "file", initialArrival = "", session, workspaceId }: {
  active: boolean;
  initialSource?: ImportSource;
  /** How Notion's window was asked for, when it opens at once. */
  initialArrival?: NotionImportArrival;
  session: Session;
  workspaceId: string;
}) {
  // Notion's window asked for at once opens a moment after the window that
  // holds it: opened with it, its layer came first and drew under it
  // (2026-11-09, `leafen setup notion`). While a Notion move runs, the way
  // in shows it in one place — its card on "둘러보기", whose "진행 보기" opens
  // the window — and not the window over it too (2026-11-20: the agent's
  // progress link showed the same run twice). Only a read that says no move
  // runs opens it: one that fails says no such thing, and is read again a few
  // times (readNotionMoves), then left to the Notion card.
  const [source, setSource] = useState<ImportSource>(initialSource === "notion" ? "file" : initialSource);
  const reader = useRef(session);
  reader.current = session;
  const userId = session.user.id;
  useEffect(() => {
    if (initialSource !== "notion") return;
    const controller = new AbortController();
    readNotionMoves(reader.current, workspaceId, controller.signal)
      .then((list) => { if (!list.items.some((item) => !item.source && notionMigrationRunGoing(item))) setSource("notion"); })
      .catch(() => undefined);
    return () => controller.abort();
  }, [initialSource, userId, workspaceId]);
  // How Notion's window was come to, which says where it opens (NotionImportPanel).
  const [arrival, setArrival] = useState<NotionImportArrival>(initialArrival);
  useEffect(() => setArrival(initialArrival), [initialArrival]);
  // A CSV chosen alone, or the CSV card, opens the CSV window in place of the sources.
  const [csvFile, setCSVFile] = useState<File | null>(null);
  const [csvWindow, setCSVWindow] = useState(false);
  const recoverNotion = useCallback(() => setSource("notion"), []);
  const notionController = useNotionImportController({
    active: active && source === "notion",
    onRecover: recoverNotion,
    open: active,
    session,
    workspaceId,
  });
  const selectSource = useCallback((next: ImportSource, nextArrival: NotionImportArrival = "") => {
    setSource(next);
    setArrival(nextArrival);
    if (next === "notion" && !notionController.job) notionController.setStep("connect");
  }, [notionController]);
  return { source, setSource, selectSource, arrival, csvFile, setCSVFile, csvWindow, setCSVWindow, notionController };
}

export type ImportSources = ReturnType<typeof useImportSources>;

export function ImportSourcePanels({
  sources: state,
  destination = null,
  onImported,
  onNavigate,
  onNewWorkspaceFromLeafen,
  session,
  workspaceId,
  workspaceName,
}: {
  sources: ImportSources;
  /** Leaves the import screen to start a workspace from a Leafen file, the one chosen if any. */
  onNewWorkspaceFromLeafen?: (file: File | null) => void;
  /** Leaves the import screen for another place in Leafen. */
  onNavigate?: (path: string) => void;
  destination?: Parameters<typeof DirectMarkdownImportPanel>[0]["destination"];
  onImported?: () => void;
  session: Session;
  workspaceId: string;
  /** Its name, which Notion's window names over its steps. */
  workspaceName?: string;
}) {
  const { t } = useTranslation(["importDialog", "common"]);
  // A build without the app platform has no tools' apps to move content.
  const appPlatform = useAppPlatformAvailable();
  const { arrival, source, selectSource, setCSVFile, setCSVWindow, notionController } = state;
  const [settingsTab, setSettingsTab] = useState<"browse" | "completed">("browse");
  // A Leafen archive dropped on the general way in opens the archive import
  // with it; one this workspace was made for (lib/openWorkspace.ts) goes in at once.
  const [archive, setArchive] = useState<{ file: File; atOnce: boolean } | null>(null);
  useEffect(() => {
    const held = takeHeldWorkspaceFile(workspaceId);
    if (held) setArchive({ file: held, atOnce: true });
  }, [workspaceId]);
  // As Notion's settings "가져오기" (seen 2026-10-08): "둘러보기" and "완료됨";
  // a Notion workspace and a Leafen archive open from their cards, with a way back.
  return (
    <div className={tabStyles.settingsBody} data-import-layout="settings">
      <nav className={tabStyles.tabs} aria-label={t("importDialog:navAria")}>
        {(["browse", "completed"] as const).map((tab) => (
          <button key={tab} type="button" className={styles.navItem} data-active={settingsTab === tab ? "true" : undefined}
            aria-current={settingsTab === tab ? "page" : undefined} data-import-settings-tab={tab}
            onClick={() => { setSettingsTab(tab); selectSource("file"); }}>
            <span>{t(`importDialog:tabs.${tab}`)}</span>
          </button>
        ))}
      </nav>
      {settingsTab === "completed" ? <ImportHistory session={session} workspaceId={workspaceId}
        onOpenNotion={(next) => { setSettingsTab("browse"); selectSource("notion", next); }} />
        : source === "file" || source === "notion" ? (
          <DirectMarkdownImportPanel
            destination={destination}
            onImported={onImported}
            onNavigate={onNavigate}
            onCSVFile={setCSVFile}
            onLeafenArchive={(file) => { setArchive({ file, atOnce: false }); selectSource("leafen"); }}
            session={session}
            workspaceId={workspaceId}
            browse={{ onCSV: () => setCSVWindow(true),
              // A migration running shows here whether or not its window is
              // open, and opens it again (2026-11-07).
              going: <NotionMigrationRunCards session={session} workspaceId={workspaceId}
                readAgain={source === "notion"} onOpen={() => selectSource("notion")} />,
              sources: <>
              {/* An app's card shows the app's own mark, as Notion's import cards each show their app's 24px logo
                  (seen 2026-11-15): Leafen's own; Notion's, another's, in the one-colour frame with its initial
                  every other company's mark takes (judgment principle 4). */}
              <ImportSourceCard icon={<ServiceIcon service="notion" name="Notion" pixels={24} aria-hidden="true" />} title={t("importDialog:notion")}
                text={t("importDialog:browse.notionCard")} data={{ "data-import-source-card": "notion" }} onOpen={() => selectSource("notion")} />
              <ImportSourceCard icon={<img src="/mark-128.png" alt="" width="24" height="24" />} title={t("importDialog:leafen.tab")}
                text={t("importDialog:browse.leafenCard")} data={{ "data-import-source-card": "leafen" }} onOpen={() => selectSource("leafen")} />
            </>, tools: appPlatform ? <ImportToolCards session={session} workspaceId={workspaceId} onNavigate={onNavigate} /> : undefined }}
          />
        ) : (
          <div className={tabStyles.sourceBody}>
            <button type="button" className={tabStyles.back} data-import-back onClick={() => selectSource("file")}>
              <ArrowLeft size="small" aria-hidden="true" />{t(`importDialog:tabs.browse`)}
            </button>
            <DirectLeafenArchiveImportPanel
              createWorkspaceArchiveImport={api.createWorkspaceArchiveImport}
              session={session}
              workspaceId={workspaceId}
              initialFile={archive?.file ?? null}
              importAtOnce={archive?.atOnce === true}
              onNewWorkspace={onNewWorkspaceFromLeafen}
            />
          </div>
        )}
      {settingsTab === "browse" && source === "notion" ? (
        <NotionMigrationWindow controller={notionController} arrival={arrival} session={session} workspaceId={workspaceId}
          workspaceName={workspaceName} onClose={() => selectSource("file")} />
      ) : null}
    </div>
  );
}
