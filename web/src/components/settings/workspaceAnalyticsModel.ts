import { request } from "@/apiTransport";
import { chosenTimeZone } from "@/lib/dateReminders";
import { dayStartOnClock } from "@/lib/nativeDate";
import type { Session } from "@/types";
import { plainTitleOf } from "@/lib/closedMentions";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import type { DatabaseSourceTitle } from "@/components/editor/databaseTitles";

// Workspace analytics, as Notion's: the owner's overview, members and
// searches, and everyone's content, over the last 7, 30, 90 or 365 days,
// each list exported as CSV.

export const WORKSPACE_ANALYTICS_RANGES = [7, 30, 90, 365] as const;
export type WorkspaceAnalyticsRange = (typeof WORKSPACE_ANALYTICS_RANGES)[number];

export type WorkspaceAnalyticsMember = {
  userId: string;
  name: string;
  email: string;
  role: string;
  pageViews: number;
  pageEdits: number;
  lastActiveAt?: string;
  viewHistoryHidden: boolean;
};

export type WorkspaceAnalyticsPage = {
  pageId: string;
  containerPageId?: string;
  title: string;
  icon: string;
  iconType: string;
  /** What its icon is drawn as without one of its own, as the sidebar draws it, and what it is called with no title. */
  kind?: string;
  /** A full-page database that is a view block, "제목 없음" with no title (untitledPageDisplayTitle). */
  viewBlock?: boolean;
  /** An untitled full-page database's sources while it holds several, which name it "A 및 B" (untitledPageDisplayTitle). */
  sourceTitles?: DatabaseSourceTitle[];
  isWiki?: boolean;
  wikiDataSourceId?: string;
  hasContent?: boolean;
  teamspaceId: string;
  createdAt: string;
  createdBy: string;
  lastEditedAt: string;
  views: number;
  uniqueViewers: number;
};

/** A query members searched: how often, by how many, and how often it opened a result. */
export type WorkspaceAnalyticsSearch = {
  query: string;
  searches: number;
  uniqueSearches: number;
  clicks: number;
};

/** Who viewed or edited a page in the window, and in the window before it. */
export type WorkspaceAnalyticsEngagement = {
  members: number;
  guests: number;
  previousMembers: number;
  previousGuests: number;
};

export type WorkspaceAnalytics = {
  workspaceId: string;
  days: number;
  from: string;
  to: string;
  canSeeMembers: boolean;
  /** How long the organization keeps analytics, 0 when it keeps them. */
  retentionDays: number;
  /** The owner's: missing for everyone else. */
  engagement?: WorkspaceAnalyticsEngagement;
  activeMembers: Array<{ day: string; count: number }>;
  members: WorkspaceAnalyticsMember[];
  membersHasMore: boolean;
  teamspaces: Array<{ teamspaceId: string; name: string; views: number }>;
  content: WorkspaceAnalyticsPage[];
  contentHasMore: boolean;
  searches?: WorkspaceAnalyticsSearch[];
  searchesHasMore?: boolean;
};

export type WorkspaceAnalyticsCreated = "any" | 7 | 30 | 90;

export type WorkspaceAnalyticsContentFilters = {
  query: string;
  teamspace: string;
  createdBy: string;
  created: WorkspaceAnalyticsCreated;
};

export const EMPTY_WORKSPACE_ANALYTICS_FILTERS: WorkspaceAnalyticsContentFilters = {
  query: "", teamspace: "", createdBy: "", created: "any",
};

export type WorkspaceAnalyticsMemberSort = "views" | "edits" | "lastActive" | "name";

export type WorkspaceAnalyticsSearchSort = "searches" | "uniqueSearches" | "clickThroughRate";

/**
 * The analytics request's parameters. Every day in them is on one clock, the
 * reader's (chosenTimeZone: the zone 기본 설정 chose, else the device's): the
 * window the server ends on their today, and "created in the last N days",
 * which starts at the midnight N − 1 days before today there. With Pago Pago
 * chosen on a Seoul device both start at Pago Pago's midnight, not Seoul's
 * (2026-11-26: the filter took the device's while the window took the zone's).
 */
export function workspaceAnalyticsParams(
  days: WorkspaceAnalyticsRange,
  filters: WorkspaceAnalyticsContentFilters,
  now: Date,
) {
  const zone = chosenTimeZone();
  const params = new URLSearchParams({ days: String(days), timeZone: zone });
  if (filters.query.trim()) params.set("q", filters.query.trim());
  if (filters.teamspace) params.set("teamspace", filters.teamspace);
  if (filters.createdBy) params.set("createdBy", filters.createdBy);
  if (filters.created !== "any") {
    params.set("createdAfter", dayStartOnClock(now, zone, filters.created - 1).toISOString());
  }
  return params;
}

export function readWorkspaceAnalytics(
  session: Session,
  workspaceId: string,
  params: URLSearchParams,
  signal?: AbortSignal,
) {
  return request<WorkspaceAnalytics>(
    `/workspaces/${encodeURIComponent(workspaceId)}/analytics?${params}`,
    { signal },
    session,
  );
}

function memberLabel(member: WorkspaceAnalyticsMember) {
  return member.name || member.email || member.userId;
}

/** Orders members by the chosen column, most first. */
export function sortWorkspaceAnalyticsMembers(members: WorkspaceAnalyticsMember[], sort: WorkspaceAnalyticsMemberSort) {
  const time = (member: WorkspaceAnalyticsMember) => (member.lastActiveAt ? Date.parse(member.lastActiveAt) || 0 : 0);
  return [...members].sort((left, right) => {
    switch (sort) {
      case "views":
        return right.pageViews - left.pageViews || memberLabel(left).localeCompare(memberLabel(right));
      case "edits":
        return right.pageEdits - left.pageEdits || memberLabel(left).localeCompare(memberLabel(right));
      case "lastActive":
        return time(right) - time(left) || memberLabel(left).localeCompare(memberLabel(right));
      default:
        return memberLabel(left).localeCompare(memberLabel(right));
    }
  });
}

/** The share of a query's searches that opened a result, from 0 to 1. */
export function workspaceAnalyticsClickThroughRate(search: WorkspaceAnalyticsSearch) {
  return search.searches > 0 ? Math.min(1, search.clicks / search.searches) : 0;
}

/** Orders searches by the chosen column, most first. */
export function sortWorkspaceAnalyticsSearches(searches: WorkspaceAnalyticsSearch[], sort: WorkspaceAnalyticsSearchSort) {
  const measure = (search: WorkspaceAnalyticsSearch) => sort === "searches"
    ? search.searches
    : sort === "uniqueSearches" ? search.uniqueSearches : workspaceAnalyticsClickThroughRate(search);
  return [...searches].sort((left, right) => (
    measure(right) - measure(left) || right.searches - left.searches || left.query.localeCompare(right.query)
  ));
}

export type WorkspaceAnalyticsChange =
  | { kind: "up" | "down"; percent: number }
  | { kind: "same" | "fromNone" | "unknown" };

/**
 * How a count moved against the window before, as Notion's "118% more than
 * previous 90 days". A window the organization didn't keep has no "before".
 */
export function workspaceAnalyticsChange(
  current: number,
  previous: number,
  days: number,
  retentionDays: number,
): WorkspaceAnalyticsChange {
  if (retentionDays > 0 && retentionDays < days * 2) return { kind: "unknown" };
  if (current === previous) return { kind: "same" };
  if (previous === 0) return { kind: "fromNone" };
  const percent = Math.round((Math.abs(current - previous) / previous) * 100);
  return { kind: current > previous ? "up" : "down", percent };
}

/** The members with the most views or edits, leaving out those with none. */
export function topWorkspaceAnalyticsMembers(
  members: WorkspaceAnalyticsMember[],
  key: "pageViews" | "pageEdits",
  count = 5,
) {
  return sortWorkspaceAnalyticsMembers(members, key === "pageViews" ? "views" : "edits")
    .filter((member) => member[key] > 0)
    .slice(0, count);
}

// A cell a spreadsheet would read as a formula is kept as text, and a number,
// negative or signed, stays a number, as the server's CSV files write them
// (csvText in workspace_export_csv.go).
const SPREADSHEET_NUMBER = /^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$/u;

function csvCell(value: string | number) {
  let text = String(value);
  if (/^[=+\-@\t\r]/u.test(text) && !SPREADSHEET_NUMBER.test(text)) text = `'${text}`;
  return /[",\n\r]/u.test(text) ? `"${text.replace(/"/gu, '""')}"` : text;
}

function csvText(rows: Array<Array<string | number>>) {
  return `${rows.map((row) => row.map(csvCell).join(",")).join("\r\n")}\r\n`;
}

export function workspaceAnalyticsMembersCsv(
  members: WorkspaceAnalyticsMember[],
  headers: { name: string; email: string; views: string; edits: string; lastActive: string },
) {
  return csvText([
    [headers.name, headers.email, headers.views, headers.edits, headers.lastActive],
    ...members.map((member) => [member.name, member.email, member.pageViews, member.pageEdits, member.lastActiveAt ?? ""]),
  ]);
}

/** The content list as CSV, a page with no title named as the list names it, by its kind (untitledPageDisplayTitle). */
export function workspaceAnalyticsContentCsv(
  pages: WorkspaceAnalyticsPage[],
  headers: { title: string; views: string; uniqueViewers: string; lastEdited: string; created: string },
) {
  return csvText([
    [headers.title, headers.views, headers.uniqueViewers, headers.lastEdited, headers.created],
    ...pages.map((page) => [plainTitleOf(page) || untitledPageDisplayTitle(page), page.views, page.uniqueViewers, page.lastEditedAt, page.createdAt]),
  ]);
}

export function workspaceAnalyticsSearchesCsv(
  searches: WorkspaceAnalyticsSearch[],
  headers: { query: string; searches: string; uniqueSearches: string; clickThroughRate: string },
) {
  return csvText([
    [headers.query, headers.searches, headers.uniqueSearches, headers.clickThroughRate],
    ...searches.map((search) => [
      search.query, search.searches, search.uniqueSearches,
      `${Math.round(workspaceAnalyticsClickThroughRate(search) * 100)}%`,
    ]),
  ]);
}

export function workspaceAnalyticsExportName(kind: "members" | "content" | "searches", days: number, to: string) {
  return `workspace-analytics-${kind}-${days}d-${to}.csv`;
}
