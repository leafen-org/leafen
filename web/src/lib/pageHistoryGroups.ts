import type { PageHistoryVersion } from "@/pageHistoryWire.generated";
import { personName } from "@/lib/peopleDirectory";

// A page's versions as its history lists them, as Notion's "Group by" does
// (seen 2026-09-30): the changes of one clock minute, quarter hour or hour,
// whoever made them, are one entry, shown by its latest change. The newest
// entry is the current version. "Bookmark" lists the bookmarked versions
// alone (seen 2026-10-01); its entries under it are grouped by the minute.

export type PageHistoryGrouping = "minute" | "quarter" | "hour" | "bookmark";

export const pageHistoryGroupings: readonly PageHistoryGrouping[] = ["minute", "quarter", "hour", "bookmark"];

const GROUPING_SECONDS = { minute: 60, quarter: 900, hour: 3600 } as const;
const SPAN_UNITS = [["hour", 3600], ["minute", 60], ["second", 1]] as const;

/**
 * The span the version history's "Group by" button names, as Notion's: in
 * the language's narrow units and the locale's digits ("1m", "15m", "1h",
 * Arabic "١ د", "١ س"; seen 2026-11-19), where its menu's items keep their words.
 */
export function pageHistoryGroupingSpan(grouping: Exclude<PageHistoryGrouping, "bookmark">, locale: string) {
  let rest: number = GROUPING_SECONDS[grouping];
  const parts = SPAN_UNITS.flatMap(([unit, size]) => {
    const count = Math.floor(rest / size);
    rest -= count * size;
    return count ? [new Intl.NumberFormat(locale, { style: "unit", unit, unitDisplay: "narrow" }).format(count)] : [];
  });
  return new Intl.ListFormat(locale, { type: "unit", style: "narrow" }).format(parts);
}

/**
 * Who made a change: a person, and the connection they made it through, if
 * any (as Notion's history names one, seen 2026-10-01).
 */
export type PageHistoryAuthor = {
  actorId: string;
  connectionName: string;
  /** An integration's changes are named by it alone, as Notion's are (seen 2026-11-05). */
  integration?: boolean;
};

export type PageHistoryGroup = {
  /** The latest version in the group, which the group shows and restores. */
  version: PageHistoryVersion;
  /** The people who made the group's changes, latest first. */
  actorIds: string[];
  /** The people, each with the connection they made changes through, latest first. */
  authors: PageHistoryAuthor[];
  versionIds: string[];
};

/**
 * Who made a version, as the page's history and its updates both name them:
 * the person and the connection — an app's screen, an agent — they made it
 * through, or an integration alone; null when nobody is known.
 */
function pageHistoryAuthorOf(version: PageHistoryVersion): PageHistoryAuthor | null {
  if (!version.actorId) return null;
  return { actorId: version.actorId, connectionName: version.connectionName ?? "",
    ...(version.connectionKind === "integration" ? { integration: true } : {}) };
}

/**
 * A version's author by name where no profile names them: as the people
 * directory does — Leafen's own versions, a migration's and its upkeep's,
 * "Leafen" (peopleDirectory.ts) — else by their id.
 */
export function pageHistoryUnprofiledName(actorId: string) {
  return personName(actorId) || actorId;
}

/**
 * The connection a person made a version through, named beside them; "" by
 * hand, by an integration (named alone) or by an app's own account, which is
 * the app (apps: the actors who are).
 */
export function pageHistoryThrough(version: PageHistoryVersion, apps: ReadonlySet<string> = new Set()): string {
  const author = pageHistoryAuthorOf(version);
  return author && !author.integration && !apps.has(author.actorId) ? author.connectionName : "";
}

function addAuthor(authors: PageHistoryAuthor[], version: PageHistoryVersion) {
  const added = pageHistoryAuthorOf(version);
  if (!added) return;
  if (authors.some((author) => author.actorId === added.actorId && author.connectionName === added.connectionName)) return;
  authors.push(added);
}

function bucketOf(createdAt: number, grouping: PageHistoryGrouping) {
  const date = new Date(createdAt);
  // Under "Bookmark" the entries, which it does not list, keep a minute's.
  const minutes = grouping === "hour" ? 0
    : grouping === "quarter" ? Math.floor(date.getMinutes() / 15) * 15
      : date.getMinutes();
  return `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}-${date.getHours()}-${minutes}`;
}

/** Groups versions listed newest first into entries, newest first. */
export function groupPageHistoryVersions(
  versions: readonly PageHistoryVersion[],
  grouping: PageHistoryGrouping,
): PageHistoryGroup[] {
  const groups: PageHistoryGroup[] = [];
  let bucket = "";
  for (const version of versions) {
    const next = bucketOf(version.createdAt, grouping);
    const current = groups.at(-1);
    if (current && next === bucket) {
      current.versionIds.push(version.id);
      if (version.actorId && !current.actorIds.includes(version.actorId)) current.actorIds.push(version.actorId);
      addAuthor(current.authors, version);
      continue;
    }
    bucket = next;
    const authors: PageHistoryAuthor[] = [];
    addAuthor(authors, version);
    groups.push({ version, actorIds: version.actorId ? [version.actorId] : [], authors, versionIds: [version.id] });
  }
  return groups;
}

/** The group holding a version, for keeping a selection across groupings. */
export function pageHistoryGroupOf(groups: readonly PageHistoryGroup[], versionId: string) {
  return groups.find((group) => group.versionIds.includes(versionId));
}

type HistoryTimeWords = {
  today: (time: string) => string;
  yesterday: (time: string) => string;
  /** The rest of the last week's days by their weekday, where given. */
  weekday?: (weekday: string, time: string) => string;
};

/**
 * A version's time as its history names it, as Notion's version history
 * (its code, read 2026-11-16; seen "토요일 오후 8:25" on a Monday): today's
 * and yesterday's by day word, two to six days back by the weekday where
 * `words.weekday` is given, else by its date.
 */
export function pageHistoryTimeLabel(
  createdAt: number,
  locale: string,
  words: HistoryTimeWords,
  now = Date.now(),
  seconds = false,
  year: "always" | "otherYear" = "otherYear",
) {
  const date = new Date(createdAt);
  const time = date.toLocaleTimeString(locale, { hour: "numeric", minute: "2-digit", ...(seconds ? { second: "2-digit" } : {}) });
  const today = new Date(now);
  const recent = pageHistoryRecentDay(createdAt, now);
  if (recent) return words[recent](time);
  const daysBack = Math.round((new Date(today.getFullYear(), today.getMonth(), today.getDate()).getTime()
    - new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime()) / 86_400_000);
  if (words.weekday && daysBack >= 2 && daysBack <= 6) {
    return words.weekday(date.toLocaleDateString(locale, { weekday: "long" }), time);
  }
  return date.toLocaleString(locale, {
    year: year === "always" || date.getFullYear() !== today.getFullYear() ? "numeric" : undefined,
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
    ...(seconds ? { second: "2-digit" } : {}),
  });
}

/**
 * When a page, row or block was last edited, as a menu's foot names it:
 * today's and yesterday's by day word, an earlier day's date always with its
 * year — Notion's page, sidebar row and block "…" feet ("2026년 11월 9일 오전
 * 9:47", seen 2026-11-14 and 2026-11-16); the history list (pageHistoryTimeLabel) leaves this year's out.
 */
export function menuFootTimeLabel(editedAt: number, locale: string, words: Pick<HistoryTimeWords, "today" | "yesterday">) {
  return pageHistoryTimeLabel(editedAt, locale, words, Date.now(), false, "always");
}

/** A version's whole date and time, as the bookmark dialog names it ("2026년 10월 1일 오전 3:32:24"). */
export function pageHistoryFullTimeLabel(createdAt: number, locale: string, seconds = true) {
  return new Date(createdAt).toLocaleString(locale, {
    year: "numeric", month: "long", day: "numeric", hour: "numeric", minute: "2-digit", ...(seconds ? { second: "2-digit" } : {}),
  });
}

/** Whether a time falls today or yesterday, which the history names by word. */
export function pageHistoryRecentDay(createdAt: number, now = Date.now()): "today" | "yesterday" | null {
  const date = new Date(createdAt);
  const today = new Date(now);
  const yesterday = new Date(now);
  yesterday.setDate(today.getDate() - 1);
  const sameDay = (left: Date, right: Date) => left.getFullYear() === right.getFullYear()
    && left.getMonth() === right.getMonth() && left.getDate() === right.getDate();
  return sameDay(date, today) ? "today" : sameDay(date, yesterday) ? "yesterday" : null;
}

/**
 * A version's day and its time apart, as Notion's touch history lists them
 * ("오늘 · 오전 7:08", "11월 7일 · 오후 5:29", seen 2026-11-14): the day
 * word for today and yesterday, else the month and day, with the year when
 * it is not this one.
 */
export function pageHistoryDayAndTime(
  createdAt: number, locale: string, words: { today: string; yesterday: string }, now = Date.now(),
) {
  const date = new Date(createdAt);
  const recent = pageHistoryRecentDay(createdAt, now);
  const day = recent ? words[recent] : date.toLocaleDateString(locale, {
    year: date.getFullYear() === new Date(now).getFullYear() ? undefined : "numeric", month: "short", day: "numeric",
  });
  return { day, time: date.toLocaleTimeString(locale, { hour: "numeric", minute: "2-digit" }) };
}

/** What made an entry's versions — a restore of an older one ("restore"), a page lock ("page_lock") — by their sources. */
export function pageHistoryGroupSources(group: PageHistoryGroup | undefined, versions: readonly PageHistoryVersion[]): ReadonlySet<string> {
  const ids = new Set(group?.versionIds ?? []);
  return new Set(versions.filter((version) => ids.has(version.id)).map((version) => version.source));
}

// A version named by when one of its page's updates was made rather than by
// its ID: a wiki's update of a view or of one of its pages, whose "버전 보기"
// opens that page's history at that time (PageUpdatesList, PageUpdatesSidePanel).
const VERSION_AT = "at:";

/** The version of a page's history at an update's time, as a history opened at it names it. */
export function pageHistoryVersionAt(pageId: string, at: number): string {
  return `${VERSION_AT}${at}:${pageId}`;
}

/** The page and time a version named by its time (pageHistoryVersionAt) is of. */
export function readPageHistoryVersionAt(id: string | undefined): { pageId: string; at: number } | null {
  if (!id?.startsWith(VERSION_AT)) return null;
  const rest = id.slice(VERSION_AT.length);
  const split = rest.indexOf(":");
  const at = Number(rest.slice(0, split));
  return split > 0 && Number.isFinite(at) && rest.length > split + 1 ? { pageId: rest.slice(split + 1), at } : null;
}

/**
 * The version a history opens at: the one asked for by its ID, or by its
 * time — the newest made by then — and the latest otherwise.
 */
export function initialPageHistoryVersionId(versions: readonly PageHistoryVersion[], initial?: string): string {
  const at = readPageHistoryVersionAt(initial)?.at;
  const asked = at === undefined ? versions.find(({ id }) => id === initial) : versions.find(({ createdAt }) => createdAt <= at);
  return (asked ?? versions[0])?.id ?? "";
}
