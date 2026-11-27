import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/api";
import { useTranslation } from "react-i18next";
import type { AdministratorServerWorkspace } from "@/administratorWire.generated";
import { administratorWorkspaceContent } from "@/administratorWorkspaceContentApi";
import { ArrowLeft } from "@/icons/leafen";
import type { Session } from "../../types";
import { formatFileSize } from "../database/files";
import styles from "./AdministratorSettingsPage.module.css";
import own from "./AdministratorWorkspaceContent.module.css";
import { AdministratorWorkspaceContentLog } from "./AdministratorWorkspaceContentLog";
import { AdministratorWorkspaceFiles } from "./AdministratorWorkspaceFiles";
import { AdministratorWorkspacePages } from "./AdministratorWorkspacePages";
import { AdministratorWorkspacePublic } from "./AdministratorWorkspacePublic";

const TABS = ["pages", "files", "public", "log"] as const;

/**
 * A server workspace's summary — its owner, members, pages, files and size —
 * as its row in 서버 → 워크스페이스 and its own page's head both read it
 * (2026-11-13: the row read "페이지: 8" under a head reading "페이지 8개").
 */
export function administratorWorkspaceSummary(
  workspace: AdministratorServerWorkspace,
  t: (key: string, options?: Record<string, unknown>) => string,
) {
  return [
    t("workspaceContent.owner", { name: workspace.ownerName || workspace.ownerEmail || workspace.ownerId }),
    t("workspaceContent.members", { count: workspace.memberCount }),
    t("workspaceContent.pagesCount", { count: workspace.pageCount }),
    t("workspaceContent.filesCount", { count: workspace.fileCount }),
    formatFileSize(workspace.storageBytes),
  ].join(" · ");
}

/**
 * One workspace as a server administrator sees it (Leafen's own): its pages, files and
 * what it made public, and what administrators did in it — everything they
 * open or change here recorded for its owner.
 */
export function AdministratorWorkspaceContent({ session, workspace, onBack, onSummary }: {
  session: Session;
  workspace: AdministratorServerWorkspace;
  onBack: () => void;
  /** The workspace's counts read again, for this head and its row in the list. */
  onSummary?: (workspace: AdministratorServerWorkspace) => void;
}) {
  const { t } = useTranslation("serverConsole");
  const [tab, setTab] = useState<(typeof TABS)[number]>("pages");
  // A file or page removed here changes what the head and the list's row
  // say (2026-11-13: both kept "파일 3개 · 163 B" after one was deleted): the
  // workspace's row is read again from the server, the latest read winning.
  const reading = useRef<AbortController | null>(null);
  useEffect(() => () => reading.current?.abort(), []);
  const changed = useCallback(() => {
    reading.current?.abort();
    const controller = new AbortController();
    reading.current = controller;
    api.administratorWorkspaces(session, { id: workspace.id }, controller.signal).then((list) => {
      const next = list.items.find((item) => item.id === workspace.id);
      if (next && !controller.signal.aborted) onSummary?.(next);
    }, () => undefined);
  }, [onSummary, session, workspace.id]);
  const loadLog = useCallback(
    (cursor?: string, signal?: AbortSignal) => administratorWorkspaceContent.log(session, workspace.id, cursor, signal),
    [session, workspace.id],
  );
  return (
    <section className={styles.section} aria-labelledby="administrator-workspace-content-title" data-admin-workspace={workspace.id}>
      <button type="button" className={own.back} onClick={onBack}>
        <ArrowLeft size="small" aria-hidden="true" />{t("workspaceContent.back")}
      </button>
      <div id="administrator-workspace-content-title" className={styles.sectionTitle}>{workspace.name}</div>
      <div className={own.meta}>
        {administratorWorkspaceSummary(workspace, t)}
      </div>
      <div className={own.notice} role="note">{t("workspaceContent.notice")}</div>
      <div className={own.tabs} role="tablist">
        {TABS.map((name) => (
          <button key={name} type="button" role="tab" className={own.tab} aria-selected={tab === name} onClick={() => setTab(name)}>
            {t(`workspaceContent.tabs.${name}`)}
          </button>
        ))}
      </div>
      <div role="tabpanel">
        {tab === "pages" ? <AdministratorWorkspacePages session={session} workspaceId={workspace.id} onChanged={changed} />
          : tab === "files" ? <AdministratorWorkspaceFiles session={session} workspaceId={workspace.id} onChanged={changed} />
          : tab === "public" ? <AdministratorWorkspacePublic session={session} workspaceId={workspace.id} />
          : <AdministratorWorkspaceContentLog load={loadLog} />}
      </div>
    </section>
  );
}
