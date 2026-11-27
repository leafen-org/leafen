"use client";

import { MenuIcon } from "@/icons/leafen";
import { PageHistoryChangeNav } from "./PageHistoryChangeNav";
import type { ProductStatusSink } from "@/lib/productStatus";
import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { useOptionalProductToast } from "@/lib/productToast";
import { userErrorText, writeUncertainMark } from "@/lib/settingsErrors";
import { VersionRestoreIcon, X } from "@/icons/leafen";
import { activeDateLocale } from "@/lib/i18n";
import { pageHistoryChangeTarget } from "@/lib/pageHistoryChangeCards";
import { pageHistoryComparison, pageHistoryEmptyVersion, type PageHistoryComparedVersion } from "@/lib/pageHistoryChanges";
import { pagePresenceColor } from "@/lib/pagePresenceModel";
import { useOverlayLayer } from "@/lib/overlayLayer";
import {
  groupPageHistoryVersions,
  pageHistoryFullTimeLabel,
  pageHistoryGroupOf, pageHistoryGroupSources, pageHistoryUnprofiledName,
  initialPageHistoryVersionId,
  pageHistoryTimeLabel,
  type PageHistoryGroup,
  type PageHistoryGrouping,
} from "@/lib/pageHistoryGroups";
import { type PageHistoryDatabaseKeeps,
  createPageHistoryService,
  type PageHistoryService,
} from "@/lib/pageHistoryService";
import {
  type PageHistoryDatabase,
  pageHistoryErrorCodes,
  type PageHistoryActorProfile,
  type PageHistoryBookmark,
  type PageHistorySnapshotResponse,
  type PageHistoryVersion,
} from "@/pageHistoryWire.generated";
import { LeafenApiError } from "@/apiTransport";
import type { Page, Session } from "@/types";
import { useMadeWithoutAnswer } from "@/lib/uncertainWrites";
import { PageHistoryBookmarkDeleteDialog, PageHistoryBookmarkNameDialog, usePageHistoryBookmarks } from "./PageHistoryBookmarks";
import { PageHistoryChangeList, PageHistoryChangeUnshown, usePageHistoryChanges } from "./PageHistoryChangeList";
import { PageHistoryPreview } from "./PageHistoryPreview";
import { blockPreviewText } from "./pageHistoryPreviewText";
import { PageHistoryRelationTitles, readablePageHistoryValue, type PageHistoryProperty } from "./PageHistoryProperties";
import { usePageHistoryProperties } from "./usePageHistoryProperties";
import { announcePageRestored } from "./product/PageRestoredNotice";
import { PageHistoryRestorePanel } from "./PageHistoryRestorePanel";
import { PageHistoryRevertButtons, pageHistoryRevertStatus } from "./PageHistoryRevertButtons";
import { PageHistoryVersionList } from "./PageHistoryVersionList";
import { PageHistoryTouch, usePageHistoryTouchTimes } from "./PageHistoryTouch";
import { pageHistoryTimeWords } from "./pageHistoryTimeWords";
import { confirmAction } from "./ConfirmActionDialog";
import { useSidebarLayout } from "./shell/AppShell";
import styles from "./UpdatesPanelHistory.module.css";
import { untitledPageDisplayTitle } from "../lib/pageTitle";
import { plainTitleOf } from "@/lib/closedMentions";

function appendSnapshot(
  current: PageHistorySnapshotResponse,
  next: PageHistorySnapshotResponse,
) {
  if (
    current.pageId !== next.pageId
    || current.versionId !== next.versionId
    || current.snapshotFrontier !== next.snapshotFrontier
    || current.revision !== next.revision
  ) throw new Error("Page history preview continuation changed authority.");
  return { ...next, blocks: [...current.blocks, ...next.blocks], databases: current.databases, relationRowTitles: current.relationRowTitles };
}

// The entry before the chosen one, as far as its first blocks.

function profileMap(profiles: PageHistoryActorProfile[]) {
  return Object.fromEntries(profiles.map((profile) => [profile.userId, profile]));
}

/**
 * A page's version history over the whole window, as Notion's (seen
 * 2026-09-30): the versions on the left, grouped by "Group by", and the
 * chosen version on the right under its time, with "복원 시작" opening the
 * restore over it.
 */
const noRelationTitles: Readonly<Record<string, string>> = {};

export function PageHistoryDialog({
  page,
  session,
  onClose,
  onPageChange,
  onStatus,
  properties: givenProperties,
  overPage = false,
  canEdit = true,
  initialVersionId,
}: {
  page: Page;
  session: Session;
  onClose: () => void;
  onPageChange?: (page: Page) => void;
  onStatus?: ProductStatusSink;
  /** A database page's properties as the opener knows them, until its page's are read (usePageHistoryProperties). */
  properties?: readonly PageHistoryProperty[];
  /** It opens over a database page, which draws above the app. */
  overPage?: boolean;
  /** The reader may change the page, which bookmarking its versions needs. */
  canEdit?: boolean;
  /** The version it opens on, as an update's "버전 보기" asks; the latest otherwise. */
  initialVersionId?: string;
}) {
  const { t } = useTranslation(["updatesPanel", "common", "wikiView"]);
  // The page's properties as its page shows them, which each version shows with its values.
  const { properties, wiki } = usePageHistoryProperties(session, page, givenProperties);
  const toast = useOptionalProductToast();
  const titleId = useId();
  const panelRef = useRef<HTMLDivElement>(null);
  const timelineCloseRef = useRef<HTMLButtonElement>(null);
  const startRestoreRef = useRef<HTMLButtonElement>(null);
  const touchDoneRef = useRef<HTMLButtonElement>(null);
  // A touch screen's history is Notion's iPad "기록" (PageHistoryTouch): a
  // list of times, each opening its version over the whole screen.
  const touch = useSidebarLayout(true).touch;
  const [touchVersionOpen, setTouchVersionOpen] = useState(false);
  const touchTimes = usePageHistoryTouchTimes();
  const serviceRef = useRef<PageHistoryService | null>(null);
  const [versions, setVersions] = useState<PageHistoryVersion[]>([]);
  const [actorProfiles, setActorProfiles] = useState<Record<string, PageHistoryActorProfile>>({});
  const [snapshotFrontier, setSnapshotFrontier] = useState<number | null>(null);
  const [versionsCursor, setVersionsCursor] = useState<string | null>(null);
  const [versionsHaveMore, setVersionsHaveMore] = useState(false);
  const [grouping, setGrouping] = useState<PageHistoryGrouping>("minute");
  const [selectedVersionId, setSelectedVersionId] = useState("");
  // A bookmark chosen in the list shows its version under its own time.
  const [selectedBookmarkId, setSelectedBookmarkId] = useState("");
  const [bookmarkDialog, setBookmarkDialog] = useState<
    { mode: "create"; group: PageHistoryGroup } | { mode: "rename" | "delete"; bookmark: PageHistoryBookmark } | null
  >(null);
  const [preview, setPreview] = useState<PageHistorySnapshotResponse | null>(null);
  const [loadingVersions, setLoadingVersions] = useState(true);
  const [loadingPreview, setLoadingPreview] = useState(false);
  const [restoreOpen, setRestoreOpen] = useState(false);
  const [restoreDatabases, setRestoreDatabases] = useState(false);
  const [databaseKeeps, setDatabaseKeeps] = useState<PageHistoryDatabaseKeeps>({});
  const fullPageDatabase = page.kind === "database";
  const [restoringVersion, setRestoringVersion] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // One service a page and signed-in session: a new copy of the same session
  // (the parent drawing again) reads the versions once, not again.
  const sessionKey = session.sessionId ?? `${session.user.id}:${session.token}`;
  const latestSession = useRef(session);
  latestSession.current = session;
  const service = useMemo(
    () => createPageHistoryService(() => latestSession.current, page.id),
    [page.id, sessionKey],
  );
  serviceRef.current = service;
  const groups = useMemo(() => groupPageHistoryVersions(versions, grouping), [versions, grouping]);
  const bookmarks = usePageHistoryBookmarks(session, page.id, setError);
  // A bookmark whose save went without the server's answer and that the
  // list, read again once it answers, shows as made needs no dialog left
  // open to save it again (lib/uncertainWrites.ts).
  const makeBookmark = useMadeWithoutAnswer({
    items: bookmarks.bookmarks,
    idOf: (bookmark) => bookmark.id,
    landed: () => setBookmarkDialog((open) => (open?.mode === "create" ? null : open)),
  });
  // An entry's "›" lists its changes in place of the versions; choosing one
  // shows it in the entry's preview, or says the preview can't.
  const mergeProfiles = useCallback((profiles: PageHistoryActorProfile[]) => (
    setActorProfiles((current) => ({ ...current, ...profileMap(profiles) }))
  ), []);
  const changesFailed = useCallback((caught: unknown) => setError(userErrorText(caught, t("updatesPanel:versions.loadFailed"))), [t]);
  const changeView = usePageHistoryChanges(service, mergeProfiles, changesFailed);
  const canvasRef = useRef<HTMLDivElement>(null);
  const selectedBookmark = bookmarks.bookmarks.find((bookmark) => bookmark.id === selectedBookmarkId);
  const currentVersionId = groups[0]?.version.id ?? "";
  const currentVersionSelected = !selectedBookmark && selectedVersionId === currentVersionId;
  const selectedGroup = groups.find((group) => group.version.id === selectedVersionId);
  // The entry before the chosen one, which the preview is compared with.
  const heldIndex = groups.findIndex((group) => group.versionIds.includes(selectedVersionId));
  const previousVersionId = heldIndex >= 0 ? groups[heldIndex + 1]?.version.id ?? "" : "";
  // The oldest entry of the whole history is compared with an empty page.
  const firstVersion = heldIndex >= 0 && !previousVersionId && !versionsHaveMore && grouping !== "bookmark";
  const [compared, setCompared] = useState<PageHistoryComparedVersion | null>(null);
  // While a restore keeps the page's databases, they show as they are now,
  // as Notion's restore does (seen 2026-11-07).
  const [currentDatabases, setCurrentDatabases] = useState<{ versionId: string; databases: PageHistoryDatabase[] } | null>(null);
  const keepsCurrentDatabases = restoreOpen && !restoreDatabases && !fullPageDatabase && !currentVersionSelected;
  useEffect(() => {
    if (!keepsCurrentDatabases || !currentVersionId || currentDatabases?.versionId === currentVersionId) return;
    let live = true;
    service.readCurrentSnapshot(currentVersionId).then((result) => {
      if (live && result) setCurrentDatabases({ versionId: currentVersionId, databases: result.databases ?? [] });
    }).catch(() => {
      // Without them, the restore shows the version's own.
    });
    return () => { live = false; };
  }, [currentDatabases?.versionId, currentVersionId, keepsCurrentDatabases, service]);
  const pageLocked = page.isLocked;
  const locale = activeDateLocale();
  const timeLabel = (createdAt: number) => pageHistoryTimeLabel(createdAt, locale, pageHistoryTimeWords(t));

  useEffect(() => () => service.dispose(), [service]);

  useEffect(() => {
    let current = true;
    setLoadingVersions(true);
    setError(null);
    service.listPageHistoryVersions().then((result) => {
      if (!current || !result) return;
      setVersions(result.versions);
      setActorProfiles(profileMap(result.actorProfiles));
      setSnapshotFrontier(result.snapshotFrontier);
      setVersionsCursor(result.nextCursor);
      setVersionsHaveMore(result.hasMore);
      setSelectedVersionId(initialPageHistoryVersionId(result.versions, initialVersionId));
    }).catch((caught) => {
      if (current) setError(userErrorText(caught, t("updatesPanel:versions.loadFailed")));
    }).finally(() => {
      if (current) setLoadingVersions(false);
    });
    return () => { current = false; };
  }, [service]);

  // A selection stays on the entry holding it as the grouping changes.
  useEffect(() => {
    if (!selectedVersionId || selectedBookmarkId) return;
    const group = pageHistoryGroupOf(groups, selectedVersionId);
    if (group && group.version.id !== selectedVersionId) setSelectedVersionId(group.version.id);
  }, [groups, selectedBookmarkId, selectedVersionId]);

  useEffect(() => {
    if (!selectedVersionId) {
      setPreview(null);
      return;
    }
    let current = true;
    setLoadingPreview(true);
    setError(null);
    service.readPageHistorySnapshot(selectedVersionId).then((result) => {
      if (current && result) setPreview(result);
    }).catch((caught) => {
      if (current) setError(userErrorText(caught, t("updatesPanel:versions.loadFailed")));
    }).finally(() => {
      if (current) setLoadingPreview(false);
    });
    return () => { current = false; };
  }, [selectedVersionId, service]);

  useEffect(() => {
    if (!previousVersionId) {
      setCompared(firstVersion ? pageHistoryEmptyVersion : null);
      return;
    }
    let current = true;
    service.readComparedSnapshot(previousVersionId).then((result) => {
      if (!current || !result) return;
      setCompared({
        versionId: previousVersionId, blocks: result.blocks, databases: result.databases ?? [], complete: !result.hasMore, title: result.title, values: result.values,
      });
    }).catch(() => {
      // Without the entry before, the preview shows the version unmarked.
    });
    return () => { current = false; };
  }, [firstVersion, previousVersionId, service]);
  const comparing = preview && compared?.versionId === previousVersionId && preview.versionId === selectedVersionId
    ? compared : null;
  const comparison = useMemo(() => pageHistoryComparison(preview, comparing, blockPreviewText, pageHistoryGroupSources(groups[heldIndex], versions)), [comparing, groups, heldIndex, preview, versions]);
  const changeActor = groups[heldIndex]?.actorIds[0];
  const previewBlockIds = useMemo(() => new Set(
    [...(preview?.blocks ?? []), ...(comparison?.removedFrom ?? [])].map(({ id }) => id),
  ), [comparison?.removedFrom, preview?.blocks]);
  const changeTarget = changeView.selected ? pageHistoryChangeTarget(changeView.selected, previewBlockIds) : undefined;
  const changeUnshown = Boolean(preview) && changeTarget === null;
  const changeTargetKey = !changeTarget || changeTarget.kind === "page" ? "" : changeTarget.kind === "title" ? "title"
    : changeTarget.kind === "row" ? `row:${changeTarget.rowId}` : changeTarget.blockId;
  const selectedChangeKey = changeView.view?.selectedKey ?? "";

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || !changeTargetKey) return;
    if (changeTargetKey === "title") canvas.scrollTo({ top: 0 });
    else if (changeTargetKey.startsWith("row:")) {
      canvas.querySelector<HTMLElement>(`[data-history-row="${CSS.escape(changeTargetKey.slice(4))}"]`)?.scrollIntoView({ block: "nearest" });
    } else canvas.querySelector<HTMLElement>(`[data-history-block="${CSS.escape(changeTargetKey)}"]`)?.scrollIntoView({ block: "nearest" });
  }, [changeTargetKey, selectedChangeKey]);

  // A modal layer: Escape closes it after the layers over it; focus enters at its timeline's "타임라인 닫기", as Notion's
  // (seen 2026-11-26 in Chrome at 1440, two tabs at different times, after the action), and returns to its opener.
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: panelRef, initialFocus: touch ? touchDoneRef : timelineCloseRef,
    onEscape: touch && touchVersionOpen ? () => setTouchVersionOpen(false) : onClose,
  });
  const [timelineOpen, setTimelineOpen] = useState(true);
  const timelineToggle = (open: boolean) => (
    <button ref={open ? undefined : timelineCloseRef} type="button" className={styles.historyTimelineToggle} data-history-timeline-toggle
      aria-label={t(open ? "updatesPanel:versions.openTimeline" : "updatesPanel:versions.closeTimeline")}
      title={t(open ? "updatesPanel:versions.openTimeline" : "updatesPanel:versions.closeTimeline")}
      onClick={() => setTimelineOpen(open)}>
      <MenuIcon size="regular" aria-hidden="true" />
    </button>
  );

  async function loadMoreVersions() {
    if (!versionsCursor || loadingVersions) return;
    setLoadingVersions(true);
    try {
      const result = await service.listPageHistoryVersions(versionsCursor);
      if (!result) return;
      setVersions((current) => [...current, ...result.versions]);
      setActorProfiles((current) => ({ ...current, ...profileMap(result.actorProfiles) }));
      setVersionsCursor(result.nextCursor);
      setVersionsHaveMore(result.hasMore);
    } catch (caught) {
      setError(userErrorText(caught, t("updatesPanel:versions.loadFailed")));
    } finally {
      setLoadingVersions(false);
    }
  }

  async function loadMorePreview() {
    if (!preview?.nextCursor || loadingPreview) return;
    setLoadingPreview(true);
    try {
      const next = await service.readPageHistorySnapshot(selectedVersionId, preview.nextCursor);
      if (next) setPreview((current) => current ? appendSnapshot(current, next) : next);
    } catch (caught) {
      setError(userErrorText(caught, t("updatesPanel:versions.loadFailed")));
    } finally {
      setLoadingPreview(false);
    }
  }

  // The restore goes back to the chosen entry's latest version and, as
  // Notion's, closes the history.
  async function restoreSelectedVersion() {
    if (currentVersionSelected || pageLocked || restoringVersion || snapshotFrontier === null) return;
    setRestoringVersion(true);
    setError(null);
    const restoreAt = (frontier: number) => service.restorePageHistoryVersion(
      selectedVersionId, frontier, (fullPageDatabase || restoreDatabases) && (preview?.databases?.length ?? 0) > 0,
      fullPageDatabase ? databaseKeeps : {},
    );
    try {
      let result;
      try {
        result = await restoreAt(snapshotFrontier);
      } catch (caught) {
        // Someone changed the page while its history stood open: as Notion's
        // (seen 2026-11-06), the restore goes ahead over the page as it is
        // now, which the history keeps as its latest version.
        if (!(caught instanceof LeafenApiError) || caught.code !== pageHistoryErrorCodes.revisionChanged) throw caught;
        const latest = await service.listPageHistoryVersions();
        if (!latest) return;
        setVersions(latest.versions);
        setSnapshotFrontier(latest.snapshotFrontier);
        result = await restoreAt(latest.snapshotFrontier);
      }
      if (!result) return;
      onPageChange?.(result.page);
      // Notion's iPad shows the page restored with no band (seen 2026-11-14).
      if (!touch) announcePageRestored(page.id);
      onClose();
    } catch (caught) {
      const message = userErrorText(caught, t("updatesPanel:versions.restoreFailed"));
      setError(message);
      onStatus?.(message);
    } finally {
      setRestoringVersion(false);
    }
  }

  if (typeof document === "undefined") return null;
  const appShell = document.querySelector<HTMLElement>("[data-app-main]")?.parentElement ?? document.body;
  // A database page's title is its title property's value in the version.
  const titleProperty = properties?.find((property) => property.type === "title");
  const versionTitle = titleProperty && preview?.values
    ? readablePageHistoryValue(preview.values[titleProperty.id]?.value)
    : preview?.title;
  const previewTitle = versionTitle || plainTitleOf(page) || untitledPageDisplayTitle(page);
  const pageTitle = plainTitleOf(page) || untitledPageDisplayTitle(page);
  const databases = preview?.databases ?? [];
  // A bookmark's version and a change are named to the second.
  const secondsTimeLabel = (createdAt: number) => pageHistoryTimeLabel(createdAt, locale, pageHistoryTimeWords(t), Date.now(), true);
  const bookmarkTime = (bookmark: PageHistoryBookmark) => secondsTimeLabel(bookmark.versionCreatedAt);
  const fullTime = (createdAt: number) => pageHistoryFullTimeLabel(createdAt, locale);
  const selectedTime = selectedBookmark ? bookmarkTime(selectedBookmark)
    : selectedGroup ? timeLabel(selectedGroup.version.createdAt) : "";
  const failed = (caught: unknown) => setError(userErrorText(caught, t("updatesPanel:versions.changeFailed")));
  const actorLabel = (actorId: string) => (actorProfiles[actorId]?.app
    ? t("updatesPanel:versions.byApp", { name: actorProfiles[actorId].displayName })
    : actorProfiles[actorId]?.displayName)
    || (actorId === session.user.id ? session.user.name || t("updatesPanel:rows.bySelf") : pageHistoryUnprofiledName(actorId));
  const restoreBlocked = currentVersionSelected || pageLocked || !preview || snapshotFrontier === null;
  const nothingChosen = !loadingVersions && !selectedVersionId && !selectedBookmark;
  const restoreHint = currentVersionSelected ? t("updatesPanel:versions.alreadyCurrent") : undefined;
  // A bookmark shows its version as it read, unmarked, as Notion's (seen 2026-11-07).
  const versionPreview = (marked: boolean) => (
    <PageHistoryRelationTitles.Provider value={preview?.relationRowTitles ?? noRelationTitles}>
      <PageHistoryPreview preview={preview} title={previewTitle} properties={properties} wiki={wiki}
        databases={databases} loading={loadingPreview} onLoadMore={() => void loadMorePreview()}
        changes={marked ? comparison?.changes : undefined} returnedBlocks={marked ? comparison?.returnedBlocks : undefined} rowChanges={marked ? comparison?.rowChanges : undefined}
        renamedProperties={marked ? comparison?.renamedProperties : undefined} removedFrom={marked ? comparison?.removedFrom : undefined}
        changedDatabases={marked ? comparison?.changedDatabases : undefined} titleParts={marked ? comparison?.titleParts : undefined} changedValues={marked ? comparison?.changedValues : undefined} fullPageDatabase={fullPageDatabase}
        currentDatabases={!marked && keepsCurrentDatabases && currentDatabases?.versionId === currentVersionId ? currentDatabases.databases : undefined}
        changeColor={changeActor ? pagePresenceColor(changeActor) : undefined}
        changeAuthor={marked && changeActor && groups[heldIndex] ? {
          userId: changeActor, name: actorLabel(changeActor), avatar: actorProfiles[changeActor]?.avatar,
          bot: actorProfiles[changeActor]?.app, time: timeLabel(groups[heldIndex].version.createdAt),
        } : undefined} />
    </PageHistoryRelationTitles.Provider>
  );

  if (touch) {
    return createPortal(
      <PageHistoryTouch panelRef={panelRef} zIndex={zIndex} titleId={titleId} doneRef={touchDoneRef}
        groups={groups} label={touchTimes.line} loading={loadingVersions}
        hasMore={versionsHaveMore && Boolean(versionsCursor)} onLoadMore={() => void loadMoreVersions()}
        onChoose={(group) => {
          setSelectedBookmarkId("");
          setSelectedVersionId(group.version.id);
          setError(null);
          setTouchVersionOpen(true);
        }}
        onClose={onClose}
        version={touchVersionOpen && selectedGroup ? {
          title: touchTimes.title(selectedGroup.version.createdAt),
          restoring: restoringVersion, restoreDisabled: restoreBlocked, error,
          // Asked first in Notion's card, its "복원" red (seen 2026-11-14).
          onRestore: () => void confirmAction({
            message: t("updatesPanel:versions.touchRestoreQuestion"), confirmLabel: t("updatesPanel:versions.touchRestore"),
            cancelLabel: t("common:actions.cancel"), danger: "filled",
          }).then((restore) => { if (restore) void restoreSelectedVersion(); }),
          onDone: () => setTouchVersionOpen(false),
          actions: pageLocked || !selectedGroup.authors.some((author) => author.connectionName) ? null : (
            <PageHistoryRevertButtons session={session} pageId={page.id} group={selectedGroup}
              versions={versions} snapshotFrontier={snapshotFrontier} disabled={!canEdit} actorLabel={actorLabel}
              onError={failed} onReverted={(result, connection) => {
                onPageChange?.(result.page);
                onStatus?.(pageHistoryRevertStatus(t, result, connection), "default");
                onClose();
              }} />
          ),
          // Unmarked, the version as it read, as Notion's touch preview.
          preview: versionPreview(false),
        } : null} />,
      appShell,
    );
  }

  return createPortal(
    <div
      ref={panelRef}
      className={styles.historyWindow}
      style={{ zIndex }}
      data-page-history="true"
      data-history-timeline={timelineOpen ? undefined : "closed"}
      data-layer={overPage ? "over-page" : undefined}
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}

    >
      {!timelineOpen ? null : changeView.view ? (
        <PageHistoryChangeList titleId={titleId} view={changeView.view} onSelect={changeView.select}
          onBack={changeView.close} onLoadMore={changeView.loadMore} actorLabel={actorLabel}
          timeLabel={timeLabel} changeTimeLabel={secondsTimeLabel} />
      ) : <PageHistoryVersionList
        timelineToggle={timelineToggle(false)}
        titleId={titleId}
        pageTitle={pageTitle}
        groups={groups}
        grouping={grouping}
        onGrouping={(next) => {
          // Under "북마크" nothing is chosen until a bookmark is, as Notion's (seen 2026-10-12).
          setGrouping(next);
          if (next === "bookmark" && !selectedBookmarkId && !bookmarks.bookmarks.some((bookmark) => bookmark.versionId === selectedVersionId)) setSelectedVersionId("");
          else if (next !== "bookmark" && !selectedVersionId) setSelectedVersionId(versions[0]?.id ?? "");
        }}
        selectedId={selectedVersionId}
        onSelect={(versionId) => {
          setSelectedBookmarkId("");
          setSelectedVersionId(versionId);
        }}
        onShowChanges={(group) => {
          setSelectedBookmarkId("");
          setSelectedVersionId(group.version.id);
          changeView.open(group);
        }}
        bookmarks={{
          items: bookmarks.bookmarks,
          selectedId: selectedBookmark?.id ?? "",
          // Naming a version leaves the page alone, so a locked page's
          // history still bookmarks, as Notion's (seen 2026-11-06).
          canEdit,
          name: (bookmark) => bookmark.name || fullTime(bookmark.versionCreatedAt),
          time: bookmarkTime,
          onSelect: (bookmark) => {
            setSelectedBookmarkId(bookmark.id);
            setSelectedVersionId(bookmark.versionId);
          },
          onBookmarkVersion: (group) => setBookmarkDialog({ mode: "create", group }),
          onRename: (bookmark) => setBookmarkDialog({ mode: "rename", bookmark }),
          onDelete: (bookmark) => setBookmarkDialog({ mode: "delete", bookmark }),
        }}
        actorLabel={actorLabel}
        timeLabel={timeLabel}
        loading={loadingVersions}
        hasMore={versionsHaveMore && Boolean(versionsCursor)}
        onLoadMore={() => void loadMoreVersions()}
      />}
      <section className={styles.historyPreviewPane} aria-label={t("updatesPanel:versions.previewAria")}>
        <div className={styles.historyPreviewHeader}>
          {timelineOpen ? null : timelineToggle(true)}
          <span className={styles.historyPreviewTime}>{selectedTime}</span>
          {currentVersionSelected && selectedTime ? (
            <span className={styles.historyPreviewState} data-history-preview-kind="current">
              {t("updatesPanel:versions.currentVersion")}
            </span>
          ) : null}
          {error ? <span className={styles.historyPreviewError} role="alert" {...writeUncertainMark(error)}>{error}</span> : null}
          <span className={styles.historyPreviewActions}>
            {/* A locked page's history only shows its versions, as Notion's
                (seen 2026-11-06): nothing there brings one back. */}
            {nothingChosen || pageLocked ? null : <>
            <PageHistoryRevertButtons session={session} pageId={page.id} group={selectedBookmark ? undefined : selectedGroup}
              versions={versions} snapshotFrontier={snapshotFrontier} disabled={pageLocked || !canEdit} actorLabel={actorLabel}
              onError={failed} onReverted={(result, connection) => {
                onPageChange?.(result.page);
                onStatus?.(pageHistoryRevertStatus(t, result, connection), "default");
                onClose();
              }} />
            <span title={restoreHint}>
              <button
                ref={startRestoreRef}
                type="button"
                className={styles.historyStartRestore}
                disabled={restoreBlocked}
                data-page-history-start-restore="true"
                onClick={() => {
                  setRestoreDatabases(false);
                  setDatabaseKeeps({});
                  setRestoreOpen(true);
                }}
              >
                <VersionRestoreIcon size="small" aria-hidden="true" />
                {t("updatesPanel:versions.startRestore")}
              </button>
            </span>
            </>}
            <button
              type="button"
              className={styles.historyClose}
              onClick={onClose}
              aria-label={t("updatesPanel:versions.closeHistory")}
            >
              <X size="regular" />
            </button>
          </span>
        </div>
        <div ref={canvasRef} className={`${styles.historyPreviewCanvas} nscroll`} data-page-history-preview
          data-history-unshown={changeUnshown ? "true" : undefined}>
          {nothingChosen ? <div className={styles.historyPreviewChoose} data-history-choose-version>
            <strong>{previewTitle}</strong><span>{t("updatesPanel:versions.chooseVersion")}</span>
          </div> : versionPreview(!selectedBookmark)}
        </div>
        {nothingChosen ? null : <PageHistoryChangeNav canvasRef={canvasRef} versionKey={selectedBookmark?.id ?? selectedVersionId} />}
        {changeUnshown ? <PageHistoryChangeUnshown /> : null}
      </section>
      {bookmarkDialog?.mode === "delete" ? (
        <PageHistoryBookmarkDeleteDialog onClose={() => setBookmarkDialog(null)} onDelete={async () => {
          try {
            const gone = bookmarkDialog.bookmark;
            await bookmarks.remove(gone);
            setBookmarkDialog(null);
            // Said with its undo, as Notion's "북마크 삭제됨 · 실행 취소" (seen 2026-11-06).
            toast?.notify(t("updatesPanel:versions.bookmarkDeleted"), "default", {
              label: t("updatesPanel:versions.undoBookmarkDelete"), icon: "undo",
              onClick: () => bookmarks.create(gone.versionId, gone.name).then(() => undefined, failed),
            });
          } catch (caught) {
            failed(caught);
          }
        }} />
      ) : bookmarkDialog ? (
        <PageHistoryBookmarkNameDialog
          mode={bookmarkDialog.mode}
          time={fullTime(bookmarkDialog.mode === "create" ? bookmarkDialog.group.version.createdAt : bookmarkDialog.bookmark.versionCreatedAt)}
          initialName={bookmarkDialog.mode === "rename" ? bookmarkDialog.bookmark.name : ""}
          onClose={() => setBookmarkDialog(null)}
          onSubmit={async (name) => {
            // Its failure is told in the dialog, under the field.
            if (bookmarkDialog.mode === "create") {
              const versionId = bookmarkDialog.group.version.id;
              await makeBookmark(() => bookmarks.create(versionId, name));
            } else await bookmarks.rename(bookmarkDialog.bookmark, name);
            setBookmarkDialog(null);
          }}
        />
      ) : null}
      {restoreOpen && !restoreBlocked ? (
        <PageHistoryRestorePanel
          pageTitle={previewTitle}
          timeLabel={selectedTime}
          page={page} versionIcon={preview ?? page} currentTime={groups[0] ? timeLabel(groups[0].version.createdAt) : ""}
          databases={databases}
          restoreDatabases={restoreDatabases}
          onRestoreDatabasesChange={setRestoreDatabases}
          fullPageDatabase={fullPageDatabase}
          databaseKeeps={databaseKeeps}
          onDatabaseKeepsChange={setDatabaseKeeps}
          restoring={restoringVersion}
          error={error}
          onRestore={() => void restoreSelectedVersion()}
          onClose={() => {
            setRestoreOpen(false);
            window.requestAnimationFrame(() => startRestoreRef.current?.focus());
          }}
        >
          {versionPreview(false)}
        </PageHistoryRestorePanel>
      ) : null}
    </div>,
    appShell,
  );
}
