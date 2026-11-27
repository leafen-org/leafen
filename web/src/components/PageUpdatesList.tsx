import { PersonAvatar } from "@/components/people/PersonAvatar";
import { ClockIcon } from "@/icons/leafen";
import { Fragment, type MouseEvent, type ReactNode, useEffect, useRef, useState } from "react";
import { Trans, useTranslation } from "react-i18next";

import { activeDateLocale } from "@/lib/i18n";
import { recentTimeStep } from "@/lib/recentTime";
import { KOREAN_OBJECT_PAIR } from "@/lib/koreanParticles";
import { plainTitleOf } from "@/lib/closedMentions";
import { pageHref } from "@/lib/navigation";
import { pushRoute } from "@/lib/router";
import { listPageLockRecords, readPageHistoryChangesSince, readPageHistorySnapshot } from "@/lib/pageHistoryService";
import { readPageUpdateVersions, readWorkspaceActivitiesUpTo, type PageVersionsReading } from "@/lib/pageUpdatesReading";
import type { WorkspaceActivityRecord } from "@/workspaceActivityWire.generated";
import { databaseActivityHead } from "./PageUpdatesDatabaseActivity";
import { pageHistoryVersionAt } from "@/lib/pageHistoryGroups";
import { PageUpdateChanges } from "./PageUpdateChanges";
import { PageUpdatePermission } from "./PageUpdatePermission";
import { PageUpdateDiscussion, usePageUpdateDiscussions, type PageUpdateDiscussionEntry, type PageUpdateRow } from "./PageUpdatesDiscussion";
import {
  PAGE_UPDATE_GAP_MS,
  groupPageUpdates,
  pageUpdateChanged,
  pageUpdateEntries,
  koreanWithAnd,
  pageUpdateActors,
  pageUpdateDiff,
  pageUpdateKey,
  pageUpdateMadeAt,
  readWholeSnapshot,
  type PageUpdate,
  type PageUpdateDiff,
  type PageUpdateEntry,
} from "@/lib/pageUpdates";
import { pageUpdateRecordDiff, type PageUpdateRecordProperty } from "@/lib/pageUpdateRecord";
import type { PageHistoryActorProfile, PageHistorySnapshotResponse } from "@/pageHistoryWire.generated";
import type { Session } from "@/types";
import { useProductPages } from "./product/productPagesContext";

import styles from "./PageAnalyticsPanel.module.css";
import { untitledPageDisplayTitle } from "../lib/pageTitle";
import { isServerUnreachable } from "@/lib/serverReach";
import { unansweredReadText, useReadAgainWhenAnswered } from "@/lib/unansweredRead";

// The updates the tab lists at most.
const UPDATE_LIMIT = 10;

type Snapshot = Pick<PageHistorySnapshotResponse, "title" | "blocks" | "values">;

/** An inline database whose own locks and updates the list shows, named and linked as itself, where it sits. */
export type PageUpdatesLockTarget = {
  blockId: string;
  title: string;
  href: string;
  pathLinks?: ReadonlyArray<{ id: string; title: string; href?: string }>;
  /** Its source and workspace, whose updates of its rows, views, properties and itself it lists. */
  sourceId?: string;
  workspaceId?: string;
  /** Its title property's name, over a row's new title. */
  titleLabel?: string;
};

/**
 * The database a page is — a wiki's page — whose own updates (its pages'
 * edits, its views, properties and itself) its updates list beside the
 * page's own edits, as Notion's wiki's do (seen 2026-11-17 at 1416, twice:
 * "〈새 page〉 페이지를 편집했습니다 · 〈path〉 / 〈wiki〉" with its title under it,
 * and its views', properties' and collection's making, among the wiki's own
 * "〈wiki〉 페이지를 편집했습니다"). Its path is the path above the page.
 */
export type PageUpdatesSource = Omit<PageUpdatesLockTarget, "blockId" | "sourceId" | "workspaceId"> & { sourceId: string; workspaceId: string };

// A database's own update, listed with its locks; the page's trash moves and permission lines.
type ListedEntry = PageUpdateEntry | { activity: WorkspaceActivityRecord; at: number } | { trash: WorkspaceActivityRecord; at: number }
  | { permission: WorkspaceActivityRecord; at: number };

function readSnapshot(session: Session, pageId: string, versionId: string, signal: AbortSignal): Promise<Snapshot> {
  let values: Snapshot["values"];
  return readWholeSnapshot((cursor) => readPageHistorySnapshot(session, pageId, versionId, { ...(cursor ? { cursor } : {}), signal })
    .then((window) => { values ??= window.values; return window; })).then((whole) => ({ ...whole, values }));
}

// When an update was, in the steps of Notion's recent times (recentTimeStep:
// its updates tab's "1일 전", five days on "11월 11일", seen 2026-11-16).
function updateTime(value: number, now: number, t: (key: string, options?: Record<string, unknown>) => string) {
  const recent = recentTimeStep(value, now, activeDateLocale());
  switch (recent.step) {
    case "now": return t("updates.now");
    case "minutes": return t("updates.minutesAgo", { minutes: recent.count });
    case "hours": return t("updates.hoursAgo", { hours: recent.count });
    case "days": return t("updates.daysAgo", { days: recent.count });
    case "date": return recent.date;
  }
}

/**
 * A page's updates, as Notion's updates tab lists them: who edited the page
 * in a burst of edits, when, where the page is, and what its title and text
 * became, who locked or unlocked it for everyone, who put it in the trash
 * or brought it back, the reader's permission lines of it, and its
 * discussions. It reads them from the page's history, its lock records, its
 * trash moves and permission lines in the workspace's updates and its
 * comments.
 */
export function PageUpdatesList({ pageId, row = false, rowOf, pageTitle, path, pathLinks, titleLabel, properties, lockTarget, source, session, onOpenVersion, onNavigate }: {
  /** The page whose history holds the updates, a database page's own included. */
  pageId: string;
  /** A database row's page, whose comments are read through its row. */
  row?: boolean;
  /** The database page that holds the row, whose property discussions open from its updates. */
  rowOf?: PageUpdateRow;
  pageTitle: string;
  /** Where the page is, as its path names it. */
  path: string;
  pathLinks?: ReadonlyArray<{ id: string; title: string; href?: string }>;
  /** What names a title change: a database row's title property's name, as Notion's. */
  titleLabel?: string;
  /** A database row's properties, whose values its record's updates show (pageUpdateRecord.ts). */
  properties?: readonly PageUpdateRecordProperty[];
  /** The inline database opened alone whose locks are listed instead of the page's own. */
  lockTarget?: PageUpdatesLockTarget;
  /** The database the page is, whose own updates are listed with the page's. */
  source?: PageUpdatesSource;
  session: Session;
  /** Opens the page's history at an update's version, as each of Notion's updates offers ("버전 보기"). */
  onOpenVersion?(versionId: string): void;
  onNavigate?(): void;
}) {
  const { t } = useTranslation("pageAnalytics");
  // A sub-page made or deleted is named as the reader's pages name it; one
  // they can't see isn't listed, as the workspace's updates leave it out.
  const productPages = useProductPages();
  const pages = productPages?.pages;
  const pageName = useRef<(block: Snapshot["blocks"][number]) => string | undefined>(() => undefined);
  pageName.current = (block) => {
    const page = pages?.find(({ id }) => id === String(block.content.childPageId ?? block.id));
    return page ? plainTitleOf(page) || untitledPageDisplayTitle(page) : undefined;
  };
  // Read when an update is, not a reason to read the updates again.
  const recordProperties = useRef(properties);
  recordProperties.current = properties;
  const [entries, setEntries] = useState<ListedEntry[] | null>(null);
  const [profiles, setProfiles] = useState<ReadonlyMap<string, PageHistoryActorProfile>>(new Map());
  const [diffs, setDiffs] = useState<ReadonlyMap<string, PageUpdateDiff>>(new Map());
  const [failed, setFailed] = useState(false);
  // The server not there to ask says so, and is asked again once it may answer (unansweredRead.ts).
  const [unanswered, setUnanswered] = useState(false);
  const [readTurn, setReadTurn] = useState(0);
  useReadAgainWhenAnswered(unanswered, () => setReadTurn((turn) => turn + 1));
  const [now] = useState(() => Date.now());
  // While an update may be unread, the newest time it can be at: an older discussion waits for it, as a lock does.
  const [waitingFrom, setWaitingFrom] = useState<number | null>(null);
  const lockBlockId = lockTarget?.blockId;
  // An inline database opened alone lists its own activity, not its page's discussions.
  const discussions = usePageUpdateDiscussions({ pageId, row, session, enabled: !lockBlockId });
  // Whose own updates are listed: the inline database opened alone, or the database the page is.
  const target = lockTarget ?? source;
  const sourceId = target?.sourceId;
  const sourceWorkspaceId = target?.workspaceId;
  // The workspace whose updates hold the page's trash moves.
  const pageWorkspaceId = pages?.find(({ id }) => id === pageId)?.workspaceId ?? rowOf?.workspaceId ?? "";
  // Everyone in it, on a permission line, is named by it.
  const workspaceName = productPages?.workspaces?.find(({ id }) => id === pageWorkspaceId)?.name ?? "";
  const followLink = (event: MouseEvent<HTMLAnchorElement>) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return;
    event.preventDefault();
    // A link back to this page, its line or the database listed keeps its
    // updates open, as Notion's; an ancestor (or the inline database) changes
    // the page and closes them.
    const target = new URL(event.currentTarget.href).pathname;
    const kept = [pageHref(pageId), ...(lockTarget ? [new URL(lockTarget.href, window.location.href).pathname] : [])];
    if (!kept.includes(target)) onNavigate?.();
    pushRoute(event.currentTarget.href);
  };

  useEffect(() => {
    const controller = new AbortController();
    const { signal } = controller;
    const snapshots = new Map<string, Promise<Snapshot>>();
    const snapshot = (versionId: string) => {
      let read = snapshots.get(versionId);
      if (!read) snapshots.set(versionId, read = readSnapshot(session, pageId, versionId, signal));
      return read;
    };
    setEntries(null);
    setDiffs(new Map());
    setFailed(false);
    // The versions that are typing, whose changes went in one after another.
    const typed = new Set<string>();
    const diffed = new Set<string>();
    // Each update is read in turn, the latest first; one's page before is
    // the next one's after, and a page both have used is let go.
    const readDiffs = async (listed: readonly ListedEntry[]) => {
      for (const entry of listed) {
        if (!("edit" in entry) || diffed.has(pageUpdateKey(entry.edit))) continue;
        const update = entry.edit;
        const key = pageUpdateKey(update);
        diffed.add(key);
        const [before, after, made] = await Promise.all([
          update.before ? snapshot(update.before.id) : Promise.resolve(null),
          snapshot(update.version.id),
          update.made ? snapshot(update.made.id) : Promise.resolve(null),
        ]);
        if (signal.aborted) return;
        snapshots.delete(update.version.id);
        if (update.part === "record") {
          setDiffs((current) => new Map(current).set(key, pageUpdateRecordDiff(before, after, recordProperties.current ?? [], made)));
          continue;
        }
        const named = (block: Snapshot["blocks"][number]) => pageName.current(block);
        let diff = pageUpdateDiff(before, after, undefined, named);
        // Two or more things changed over several versions, or typed in one,
        // go in the order they were made, which the update's changes say
        // (pageUpdateDiff); what one other version did at once keeps the page's order.
        if ((diff.title ? 1 : 0) + diff.lines.length > 1 && (update.first.id !== update.version.id || typed.has(update.version.id))) {
          const changes = await readPageHistoryChangesSince(session, pageId, update.version.id, update.first.id, signal)
            .then((read) => read.changes, () => null);
          if (signal.aborted) return;
          if (changes) diff = pageUpdateDiff(before, after, pageUpdateMadeAt(changes, (id) => typed.has(id)), named);
        }
        // A row's title goes with its record's update.
        if (update.part === "body") diff = { lines: diff.lines };
        setDiffs((current) => new Map(current).set(key, diff));
      }
    };
    void (async () => {
      // An inline database opened alone lists its own activity — its rows,
      // views, properties and itself — and locks, not its page's edits (Notion, seen 2026-11-10, 2026-11-11).
      // Its moves to and from the trash are a line each, as Notion's (seen 2026-11-13: "〈page〉 블록을
      // 삭제했습니다", "… 블록을 복원했습니다", never joined); a page whose updates can't say them lists the rest.
      // The versions are read on while the list is not full (pageUpdatesReading.ts).
      const versionReads = lockBlockId ? null : readPageUpdateVersions(session, pageId, signal);
      const [first, locks, activity, trashMoves] = await Promise.all([
        versionReads?.next(),
        listPageLockRecords(session, pageId, { ...(lockBlockId ? { blockId: lockBlockId } : {}), signal }),
        sourceId && sourceWorkspaceId
          ? readWorkspaceActivitiesUpTo(session, sourceWorkspaceId, signal, { sourceId, limit: UPDATE_LIMIT }) : null,
        lockBlockId || !pageWorkspaceId ? null
          : readWorkspaceActivitiesUpTo(session, pageWorkspaceId, signal, { trashOf: pageId, limit: UPDATE_LIMIT }).catch(() => null),
      ]);
      let reading: PageVersionsReading | null = first && !first.done ? first.value : null;
      let diffing = Promise.resolve();
      for (;;) {
        const actorProfiles = [...reading?.actorProfiles ?? [], ...locks.actorProfiles, ...activity?.actorProfiles ?? [], ...trashMoves?.actorProfiles ?? []];
        const apps = new Set(actorProfiles.filter((profile) => profile.app).map((profile) => profile.userId));
        // A database row's record and body are its updates apart, as Notion's.
        const grouped = reading ? groupPageUpdates(reading.versions, reading.read, PAGE_UPDATE_GAP_MS, apps, row) : { updates: [], waitingFrom: null };
        const waiting = grouped.waitingFrom;
        const listed: ListedEntry[] = [
          ...pageUpdateEntries(grouped.updates, locks.records, waiting, UPDATE_LIMIT, apps),
          // A wiki's page made is listed by its edit alone, as Notion's wiki's (seen 2026-11-17 at 1416, three
          // times: two from the sidebar's "+", one from its table's "새 페이지"); an inline database's row's making is a line.
          ...(activity?.items ?? []).filter((item) => lockBlockId || item.entityType !== "page" || item.action !== "create")
            .map((item) => ({ activity: item, at: Date.parse(item.occurredAt) })),
          // The page's own lines of the workspace's updates: its trash moves and permission lines (PageUpdatePermission).
          ...(trashMoves?.items ?? []).map((item) => item.action === "permission"
            ? { permission: item, at: Date.parse(item.occurredAt) } : { trash: item, at: Date.parse(item.occurredAt) })
            .filter(({ at }) => waiting === null || at >= waiting),
        ].sort((left, right) => right.at - left.at).slice(0, UPDATE_LIMIT);
        const more = reading?.read === "more" && listed.length < UPDATE_LIMIT;
        if (signal.aborted) return;
        // Shown once it holds something, or once it has read all it reads.
        if (listed.length || !more) {
          for (const version of reading?.versions ?? []) if (version.source === "collaboration") typed.add(version.id);
          setProfiles(new Map(actorProfiles.map((profile) => [profile.userId, profile])));
          setWaitingFrom(waiting);
          setEntries(listed);
          setUnanswered(false);
          diffing = diffing.then(() => readDiffs(listed));
          // Its failure is the read's, said once it ends.
          diffing.catch(() => undefined);
        }
        if (!more || !versionReads) break;
        const next = await versionReads.next();
        if (next.done) break;
        reading = next.value;
      }
      await diffing;
    })().catch((error: unknown) => {
      if (signal.aborted) return;
      setFailed(true);
      setUnanswered(isServerUnreachable(error));
    });
    return () => controller.abort();
  }, [pageId, lockBlockId, sourceId, sourceWorkspaceId, pageWorkspaceId, session, readTurn]);

  // Asked again, it keeps saying so until the server answers.
  if (unanswered && !entries) return <p role="status" data-read-unanswered>{unansweredReadText()}</p>;
  if (failed) return <p role="alert">{t("updates.loadError")}</p>;
  if (!entries) return <p>{t("updates.loading")}</p>;
  // A page's making is listed once it shows it brought something; made empty, it lists nothing.
  // Its discussions go among them by when each was last written in.
  const shown: (ListedEntry | PageUpdateDiscussionEntry)[] = [
    ...entries.filter((entry) => !("edit" in entry)
      || entry.edit.before || pageUpdateChanged(diffs.get(pageUpdateKey(entry.edit)))),
    ...discussions.entries.filter((entry) => waitingFrom === null || entry.at >= waitingFrom),
  ].sort((left, right) => right.at - left.at).slice(0, UPDATE_LIMIT);
  if (shown.length === 0) {
    const reading = entries.some((entry) => "edit" in entry && !entry.edit.before && !diffs.has(pageUpdateKey(entry.edit)));
    // An empty page's panel says so in the middle, as Notion's (seen 2026-11-06).
    return reading ? <p>{t("updates.loading")}</p>
      : <div className={styles.updatesEmpty} data-page-updates-empty><span>{t("updates.empty")}</span></div>;
  }
  // When, and where the page is: its top page and its parent by name, "..."
  // for those between, each name cut short while the line is too narrow, as
  // Notion's (seen 2026-11-13; the trash's and the search's places alike, PlaceLine).
  const meta = (at: number, links = pathLinks) => {
    const shown = links && links.length > 2 ? [links[0], null, links[links.length - 1]] : links ?? [];
    return (
      <p className={styles.updateMeta}>
        <span className={styles.updateTime}>{updateTime(at, now, (key, options) => String(t(key, options)))}</span>
        {shown.length || path ? <span className={styles.updatePlace}>
          <span className={styles.updatePlaceGap}>{" · "}</span>
          {shown.length ? shown.map((link, index) => <Fragment key={link?.id ?? "between"}>
            {index ? <span className={styles.updatePlaceGap}>{" / "}</span> : null}
            {link ? <a className={styles.updatePlacePiece} href={link.href ?? pageHref(link.id)} onClick={followLink}>{link.title}</a>
              : <span className={styles.updatePlaceWhole}>...</span>}
          </Fragment>) : <span className={styles.updatePlacePiece}>{path}</span>}
        </span> : null}
      </p>
    );
  };
  // Who made an update and what it did, with their avatar: an edit of the
  // page or a lock of it (or of the inline database listed) for everyone.
  const head = (update: Pick<PageUpdate, "actorIds" | "integrations" | "through">, verb: "edited" | "locked" | "unlocked" | "trashed" | "untrashed",
    name: string, href: string) => {
    const { reader, others, apps } = pageUpdateActors(update.actorIds, profiles, session.user.id, t("updates.someone"));
    // An app's own account reads as a connection does: its name alone and the bot mark.
    const integrations = [...(update.integrations ?? []), ...apps];
    const names = [...(reader ? [t("updates.me")] : []), ...others, ...integrations];
    // A version no account made reads as one an account since gone made: someone's.
    if (names.length === 0) names.push(t("updates.someone"));
    // Made through an app or an agent, it says so as the page's history does.
    const through = update.through ?? "";
    const way = through ? "Through" : "";
    // A lock's record names its one maker, so only an edit is two or more people's.
    const summaryKey = reader && others.length === 0 && integrations.length === 0
      ? `updates.${verb}${way}BySelf`
      : names.length === 1 || verb !== "edited"
        ? integrations.length ? `updates.${verb}ByIntegration` : `updates.${verb}${way}ByOne`
        : names.length === 2
          ? `updates.${verb}${way}ByTwo`
          : `updates.${verb}${way}ByMany`;
    return {
      // The reader's face takes its letter from their name, not from "나", as
      // Notion's (seen 2026-11-11); their profile's, else the directory's.
      avatar: (
        <PersonAvatar className={styles.updateAvatar} name={reader ? profiles.get(session.user.id)?.displayName ?? "" : names[0] ?? ""} size={24}
          bot={!reader && others.length === 0 && integrations.length > 0}
          userId={reader ? session.user.id : others.length ? update.actorIds.find((id) => id !== session.user.id && profiles.get(id)?.app !== true) : null} />
      ),
      summary: (
        <p className={styles.updateSummary}>
          <Trans t={t} i18nKey={summaryKey} values={{ page: name, name: names[0], first: names[0],
            // Two others are listed as Notion's are ("june lee, 〈integration〉님"); with the reader, "나와 …님".
            firstWith: reader ? koreanWithAnd(names[0] ?? "") : `${names[0] ?? ""},`, second: names[1], others: names.length - 1,
            connection: through, object: KOREAN_OBJECT_PAIR }}
            // Trans supplies the translated page title as the link text.
            components={{ page: <a href={href} className={styles.updatePageLink} onClick={followLink} />,
              // Other people and integrations are named in the page's weight, the reader
              // ("본인", "나") in the line's, as Notion's (seen 2026-11-06).
              who: <strong className={styles.updateWho} />,
              first: reader ? <span /> : <strong className={styles.updateWho} /> }} />
        </p>
      ),
    };
  };
  // Its name comes named by its kind (RowMenu, PageUpdatesSidePanel); only a
  // template's draft, a page, comes empty.
  const page = pageTitle || untitledPageDisplayTitle("page");
  const item = (key: string, avatar: ReactNode, body: ReactNode, attributes: Record<string, string> = {}) => (
    <li key={key} className={styles.update} {...attributes}>
      {avatar}
      <div className={styles.updateBody}>{body}</div>
    </li>
  );
  return (
    <>
    <ol className={styles.updateList} aria-label={t("tabs.updates")}>
      {shown.map((entry) => {
        if ("discussion" in entry) {
          return (
            <PageUpdateDiscussion key={`discussion:${entry.discussion.root.id}`} entry={entry} pageId={pageId} page={page}
              meta={meta(entry.at)} actorProfiles={discussions.actorProfiles} session={session} row={row ? rowOf : undefined} onFollow={followLink} />
          );
        }
        if ("activity" in entry) {
          // One of the database's own updates, where it sits.
          const record = entry.activity;
          const database = target?.title ?? "";
          const { avatar, summary, place, value } = databaseActivityHead({ item: record, database, databaseHref: target?.href ?? pageHref(pageId),
            titleLabel: target?.titleLabel, profiles, session, onFollow: followLink, t });
          // A row's update sits under the database, a view's under its page; a
          // property's or the database's own names only its time (Notion, seen 2026-11-11).
          const pageLinks = target?.pathLinks ?? pathLinks ?? [];
          const links = place === "database" && target
            ? [...pageLinks, { id: lockTarget?.blockId ?? pageId, title: database.trim() || untitledPageDisplayTitle({ kind: "database" }), href: target.href }]
            : place === "page" ? pageLinks : [];
          return item(`activity:${record.id}`, avatar, <>{summary}{links.length ? meta(entry.at, links) : (
            <p className={styles.updateMeta}>{updateTime(entry.at, now, (key, options) => String(t(key, options)))}</p>
          )}{value}
            {/* A row's or a view's change opens a history at it, as Notion's "버전 보기": an inline database's own, held by
                its block; a wiki's, its page's history then for a view and the page's own for one of its pages (seen 2026-11-17). */}
            {onOpenVersion && place && target ? (
              <button type="button" className={styles.updateVersion} data-page-update-version={`activity:${record.id}`}
                aria-label={t("updates.viewVersion")} title={t("updates.viewVersion")}
                onClick={() => onOpenVersion(lockTarget ? `activity:${record.id}`
                  : pageHistoryVersionAt(place === "database" ? record.entityId : pageId, entry.at))}><ClockIcon size="small" aria-hidden="true" /></button>
            ) : null}</>,
            { "data-page-update": "true", "data-page-update-database": record.entityType });
        }
        if ("permission" in entry) {
          const record = entry.permission;
          return item(`permission:${record.id}`, (
            <PersonAvatar className={styles.updateAvatar} userId={record.actorId} size={24}
              name={profiles.get(record.actorId)?.displayName ?? ""} />
          ), <PageUpdatePermission record={record} page={page} href={pageHref(pageId)} meta={meta(entry.at)} readerId={session.user.id}
            nameOf={(id) => profiles.get(id)?.displayName ?? ""} workspaceName={workspaceName} onFollow={followLink} />,
          { "data-page-update": "true", "data-page-update-permission": record.id });
        }
        if ("trash" in entry) {
          // Into the trash or back: who and when, where the page is, as its lock's line.
          const record = entry.trash, out = record.action === "delete";
          const { avatar, summary } = head({ actorIds: [record.actorId], integrations: record.integrations, through: record.through },
            out ? "trashed" : "untrashed", page, pageHref(pageId));
          return item(`trash:${record.id}`, avatar, <>{summary}{meta(entry.at)}</>,
            { "data-page-update": "true", "data-page-update-trash": out ? "trashed" : "restored" });
        }
        if (!("edit" in entry)) {
          // A lock for everyone: who and when, where the page or database is (Notion, seen 2026-11-10).
          const { record } = entry.lock;
          const { avatar, summary } = head(entry.lock, record.locked ? "locked" : "unlocked",
            lockTarget ? lockTarget.title || untitledPageDisplayTitle({ kind: "database" }) : page, lockTarget?.href ?? pageHref(pageId));
          return item(`lock:${record.id}`, avatar, <>{summary}{meta(record.createdAt, lockTarget ? lockTarget.pathLinks : pathLinks)}</>,
            { "data-page-update": "true", "data-page-update-lock": record.locked ? "locked" : "unlocked" });
        }
        const update = entry.edit;
        const { avatar, summary } = head(update, "edited", page, pageHref(pageId));
        const diff = diffs.get(pageUpdateKey(update));
        return item(pageUpdateKey(update), avatar, (
          <>
            {summary}
            {meta(update.version.createdAt)}
            {diff ? <PageUpdateChanges diff={diff} pageId={pageId} titleLabel={diff.pageTitle ? undefined : titleLabel} onFollow={followLink} />
              : <div className={styles.updateLoading} aria-busy="true" />}
            {onOpenVersion ? (
              <button type="button" className={styles.updateVersion} data-page-update-version={update.version.id}
                aria-label={t("updates.viewVersion")} title={t("updates.viewVersion")}
                onClick={() => onOpenVersion(update.version.id)}><ClockIcon size="small" aria-hidden="true" /></button>
            ) : null}
          </>
        ), { "data-page-update": "true" });
      })}
    </ol>
    </>
  );
}
