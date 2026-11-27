import { useEffect, useId, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { MenuIcon, X } from "@/icons/leafen";
import { activeDateLocale } from "@/lib/i18n";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { groupPageHistoryVersions, pageHistoryTimeLabel, pageHistoryUnprofiledName, type PageHistoryGrouping } from "@/lib/pageHistoryGroups";
import type { PageHistoryRowMark } from "@/lib/pageHistoryChanges";
import { pagePresenceColor } from "@/lib/pagePresenceModel";
import { readPageHistoryDatabaseAt } from "@/lib/pageHistoryService";
import { userErrorText } from "@/lib/settingsErrors";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import type { PageHistoryDatabase, PageHistoryVersion } from "@/pageHistoryWire.generated";
import type { Session } from "@/types";
import { readWorkspaceActivities } from "@/workspaceActivityApi";
import { workspaceActivityBounds, type WorkspaceActivityActorProfile, type WorkspaceActivityRecord } from "@/workspaceActivityWire.generated";
import { PageHistoryChangeNav } from "./PageHistoryChangeNav";
import { PageHistoryDatabaseTable } from "./PageHistoryDatabaseTable";
import { PageHistoryVersionList } from "./PageHistoryVersionList";
import { PageHistoryTouch, usePageHistoryTouchTimes } from "./PageHistoryTouch";
import { pageHistoryTimeWords } from "./pageHistoryTimeWords";
import { useSidebarLayout } from "./shell/AppShell";
import styles from "./UpdatesPanelHistory.module.css";

/** The inline database opened alone whose own history this is. */
export type DatabaseHistoryTarget = { pageId: string; workspaceId: string; blockId: string; sourceId: string; title: string };

/**
 * An inline database's own version history, as Notion's for one opened alone
 * (seen 2026-11-11): "버전 기록" over the database's name, its own versions
 * only — its rows made and edited, its views and properties, itself — each
 * previewed as its table alone at that time, the rows the version made or
 * changed outlined and stepped through ("1 / 2개 변경 사항"). Its versions
 * are its own updates, by the minute as a page's are.
 */
export function DatabaseHistoryDialog({ database, session, initialId, onClose }: {
  database: DatabaseHistoryTarget;
  session: Session;
  /** The update it opens at, as an update's "버전 보기" asks; the latest otherwise. */
  initialId?: string;
  onClose: () => void;
}) {
  const { t } = useTranslation(["updatesPanel", "common"]);
  const titleId = useId();
  const panelRef = useRef<HTMLDivElement>(null);
  const timelineCloseRef = useRef<HTMLButtonElement>(null);
  const canvasRef = useRef<HTMLDivElement>(null);
  const [items, setItems] = useState<WorkspaceActivityRecord[]>([]);
  const [profiles, setProfiles] = useState<Record<string, WorkspaceActivityActorProfile>>({});
  const [cursor, setCursor] = useState("");
  const [loading, setLoading] = useState(true);
  const [grouping, setGrouping] = useState<PageHistoryGrouping>("minute");
  const [selectedId, setSelectedId] = useState(initialId ?? "");
  const [preview, setPreview] = useState<{ at: number; database: PageHistoryDatabase | null } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [timelineOpen, setTimelineOpen] = useState(true);
  // A touch screen's is the page history's touch "기록" (PageHistoryTouch), with nothing to restore.
  const touch = useSidebarLayout(true).touch;
  const [touchVersionOpen, setTouchVersionOpen] = useState(false);
  const touchTimes = usePageHistoryTouchTimes();
  const touchDoneRef = useRef<HTMLButtonElement>(null);

  const read = (after: string, signal?: AbortSignal) => {
    setLoading(true);
    return readWorkspaceActivities(session, database.workspaceId, after, signal ?? new AbortController().signal,
      { sourceId: database.sourceId, limit: workspaceActivityBounds.maxLimit }).then((result) => {
      setItems((current) => after ? [...current, ...result.items] : result.items);
      setProfiles((current) => ({ ...current, ...Object.fromEntries(result.actorProfiles.map((profile) => [profile.userId, profile])) }));
      setCursor(result.nextCursor);
    }, (caught) => { if (!signal?.aborted) setError(userErrorText(caught, t("updatesPanel:versions.loadFailed"))); })
      .finally(() => { if (!signal?.aborted) setLoading(false); });
  };
  useEffect(() => {
    const controller = new AbortController();
    void read("", controller.signal);
    return () => controller.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [database.sourceId, database.workspaceId, session]);

  // Each update is a version of the database at its time, by whoever made it.
  const versions = useMemo(() => items.map((item): PageHistoryVersion => ({
    id: item.id, pageId: database.pageId, versionCursor: 0, revision: 0, actorId: item.actorId,
    source: item.entityType, createdAt: Date.parse(item.occurredAt), connectionName: item.through ?? "",
    ...(item.integrationsOnly ? { connectionKind: "integration" as const, connectionName: (item.integrations ?? []).join(", ") } : {}),
  })).sort((left, right) => right.createdAt - left.createdAt), [database.pageId, items]);
  const groups = useMemo(() => groupPageHistoryVersions(versions, grouping === "bookmark" ? "minute" : grouping), [grouping, versions]);
  const selectedGroup = groups.find((group) => group.versionIds.includes(selectedId)) ?? (selectedId ? undefined : groups[0]);
  const at = selectedGroup?.version.createdAt ?? 0;

  useEffect(() => {
    if (!at) return;
    const controller = new AbortController();
    setPreview(null);
    readPageHistoryDatabaseAt(session, database.pageId, database.blockId, at, controller.signal).then(
      (result) => setPreview({ at, database: result.database ?? null }),
      (caught) => { if (!controller.signal.aborted) setError(userErrorText(caught, t("updatesPanel:versions.loadFailed"))); },
    );
    return () => controller.abort();
  }, [at, database.blockId, database.pageId, session, t]);

  // The rows the version made or changed, outlined in its table.
  const marks = useMemo(() => {
    const table = preview?.database;
    if (!table || !selectedGroup) return undefined;
    const changes = new Map<string, "added" | "changed">();
    for (const item of items) {
      if (!selectedGroup.versionIds.includes(item.id) || item.entityType !== "page") continue;
      changes.set(item.entityId, item.action === "create" || changes.get(item.entityId) === "added" ? "added" : "changed");
    }
    return (table.rows ?? []).map((row): PageHistoryRowMark => ({ row, change: changes.get(row.id) }));
  }, [items, preview?.database, selectedGroup]);

  // Focus enters at the timeline's "타임라인 닫기", as the page's history window (PageHistoryDialog) and Notion's.
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: panelRef, initialFocus: touch ? touchDoneRef : timelineCloseRef,
    onEscape: touch && touchVersionOpen ? () => setTouchVersionOpen(false) : onClose,
  });
  const locale = activeDateLocale();
  const timeLabel = (createdAt: number) => pageHistoryTimeLabel(createdAt, locale, pageHistoryTimeWords(t));
  const actorLabel = (actorId: string) => profiles[actorId]?.displayName
    || (actorId === session.user.id ? session.user.name || t("updatesPanel:rows.bySelf") : pageHistoryUnprofiledName(actorId));
  const timelineToggle = (open: boolean) => (
    <button ref={open ? undefined : timelineCloseRef} type="button" className={styles.historyTimelineToggle} data-history-timeline-toggle
      aria-label={t(open ? "updatesPanel:versions.openTimeline" : "updatesPanel:versions.closeTimeline")}
      title={t(open ? "updatesPanel:versions.openTimeline" : "updatesPanel:versions.closeTimeline")}
      onClick={() => setTimelineOpen(open)}>
      <MenuIcon size="regular" aria-hidden="true" />
    </button>
  );
  if (typeof document === "undefined") return null;
  const appShell = document.querySelector<HTMLElement>("[data-app-main]")?.parentElement ?? document.body;
  const title = database.title.trim() || untitledPageDisplayTitle({ kind: "database" });
  const current = selectedGroup !== undefined && selectedGroup === groups[0];
  const changeActor = selectedGroup?.actorIds[0];
  const table = preview?.database && preview.at === at ? (
    <PageHistoryDatabaseTable database={preview.database} marks={marks} workspaceId={database.workspaceId}
      changeColor={changeActor ? pagePresenceColor(changeActor) : undefined} />
  ) : null;
  if (touch) {
    return createPortal(
      <PageHistoryTouch panelRef={panelRef} zIndex={zIndex} titleId={titleId} doneRef={touchDoneRef}
        groups={groups} label={touchTimes.line} loading={loading}
        hasMore={Boolean(cursor)} onLoadMore={() => { if (!loading) void read(cursor); }}
        onChoose={(group) => { setSelectedId(group.version.id); setTouchVersionOpen(true); }}
        onClose={onClose}
        version={touchVersionOpen && selectedGroup ? {
          title: touchTimes.title(selectedGroup.version.createdAt), error,
          onDone: () => setTouchVersionOpen(false), preview: table,
        } : null} />,
      appShell,
    );
  }
  return createPortal(
    <div ref={panelRef} className={styles.historyWindow} style={{ zIndex }} data-page-history="true" data-database-history={database.blockId}
      data-history-timeline={timelineOpen ? undefined : "closed"} role="dialog" aria-modal="true" aria-labelledby={titleId}>
      {timelineOpen ? <PageHistoryVersionList timelineToggle={timelineToggle(false)} titleId={titleId} pageTitle={title}
        groups={groups} grouping={grouping} onGrouping={setGrouping} selectedId={selectedGroup?.version.id ?? ""}
        onSelect={setSelectedId} actorLabel={actorLabel} timeLabel={timeLabel} loading={loading}
        hasMore={Boolean(cursor)} onLoadMore={() => { if (!loading) void read(cursor); }}
        bookmarks={{ items: [], selectedId: "", canEdit: false, name: () => "", time: () => "",
          onSelect: () => {}, onBookmarkVersion: () => {}, onRename: () => {}, onDelete: () => {} }} /> : null}
      <section className={styles.historyPreviewPane} aria-label={t("updatesPanel:versions.previewAria")}>
        <div className={styles.historyPreviewHeader}>
          {timelineOpen ? null : timelineToggle(true)}
          <span className={styles.historyPreviewTime}>{selectedGroup ? timeLabel(selectedGroup.version.createdAt) : ""}</span>
          {current ? <span className={styles.historyPreviewState} data-history-preview-kind="current">{t("updatesPanel:versions.currentVersion")}</span> : null}
          {error ? <span className={styles.historyPreviewError} role="alert">{error}</span> : null}
          <span className={styles.historyPreviewActions}>
            <button type="button" className={styles.historyClose} onClick={onClose} aria-label={t("updatesPanel:versions.closeHistory")}>
              <X size="regular" />
            </button>
          </span>
        </div>
        <div ref={canvasRef} className={`${styles.historyPreviewCanvas} nscroll`} data-page-history-preview>
          {table ?? (!loading && groups.length === 0 ? (
            <div className={styles.historyPreviewChoose}><strong>{title}</strong><span>{t("updatesPanel:empty.noPageHistoryYet")}</span></div>
          ) : null)}
        </div>
        {selectedGroup ? <PageHistoryChangeNav canvasRef={canvasRef} versionKey={`${selectedGroup.version.id}:${preview?.at ?? 0}`} /> : null}
      </section>
    </div>,
    appShell,
  );
}
