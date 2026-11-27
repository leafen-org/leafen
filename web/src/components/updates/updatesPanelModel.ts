import type {
  CommentActorProfile,
  NotificationActivityInput,
  NotificationInboxRecord,
} from "@/commentWire.generated";
import { pageHref } from "@/lib/navigation";
import { LEAFEN_ANONYMOUS_USER_ID, LEAFEN_SYSTEM_USER_ID, personAvatarValue, personName } from "@/lib/peopleDirectory";
import type { Page } from "@/types";
import { outOfTrashIn, pageMapOf } from "@/lib/pageIndex";

/**
 * What the inbox lists, as Notion's inbox filter's "상태": the personal
 * notifications still in it, only the unread ones, the archived ones, or
 * every workspace update.
 */
export type InboxState = "inbox" | "unread" | "archived" | "workspace";

export const INBOX_STATES: InboxState[] = ["inbox", "unread", "archived", "workspace"];

/** The kinds of notification Notion's inbox filter's "유형" can hide. */
export type InboxType = "permissions" | "discussions" | "mentions" | "automations" | "updates" | "workspace";

export const INBOX_TYPES: InboxType[] = ["permissions", "discussions", "mentions", "automations", "updates", "workspace"];

export type InboxFilter = { state: InboxState; hidden: ReadonlySet<InboxType> };

const inboxFilterKey = (userId: string) => `leafen:inbox-filter:${userId}`;

/**
 * What the inbox lists and which kinds it hides, kept for the person on
 * this device: Notion's inbox opens with the filter last set, after a
 * reload too (seen 2026-11-06).
 */
export function readInboxFilter(userId: string): InboxFilter {
  try {
    const raw = window.localStorage.getItem(inboxFilterKey(userId));
    const parsed = raw ? JSON.parse(raw) as { state?: unknown; hidden?: unknown } : null;
    const state = INBOX_STATES.find((value) => value === parsed?.state) ?? "inbox";
    const hidden = Array.isArray(parsed?.hidden) ? INBOX_TYPES.filter((type) => (parsed.hidden as unknown[]).includes(type)) : [];
    return { state, hidden: new Set(hidden) };
  } catch {
    return { state: "inbox", hidden: new Set() };
  }
}

export function storeInboxFilter(userId: string, filter: InboxFilter) {
  try {
    window.localStorage.setItem(inboxFilterKey(userId), JSON.stringify({ state: filter.state, hidden: [...filter.hidden] }));
  } catch {
    // Without storage the filter lasts while the inbox is open.
  }
}

// How many of the most recently edited pages one sync of page edits names.
const PAGE_EDIT_SYNC_LIMIT = 40;

export type NotificationActivity = {
  at: number;
  id: string;
  notification: NotificationInboxRecord;
  /** The page the notice names: its database item when it names one (itemId), else its page. */
  page?: Page;
};

/** Whether a notice is a person property naming the reader, its line "Owner: @Ada" (notification_item_now.go). */
export function isPersonPropertyNotice(notification: NotificationInboxRecord) {
  return notification.kind === "mention" && notification.activityKey.startsWith("database-person:");
}

function timestamp(value: string) {
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

export function notificationActivities(
  notifications: NotificationInboxRecord[],
  pages: Page[],
  userId: string,
) {
  // A page in the trash still names its notices with its icon, as Notion's
  // inbox does (seen 2026-11-13); the server keeps them listed.
  const pagesById = pageMapOf(pages);
  return notifications
    // Hide the person's own actions, but not a reminder they set for
    // themselves: the server delivers it to them on purpose, naming them as
    // its actor (a date mention makes one the same way), nor an automation's
    // message, which an unnamed one sends as its maker's. Mirror the server's
    // own rule (notificationShownSQL) so the list matches the unread count.
    .filter((notification) => notification.actorId !== userId || notification.kind === "reminder"
      || notification.activityKey.startsWith("automation-notification:"))
    // A notice about a database item names the item, not the page holding its
    // database, as Notion's inbox does (seen 2026-11-09); the server says which.
    .map<NotificationActivity>((notification) => ({
      at: timestamp(notification.occurredAt),
      id: `notification:${notification.id || notification.activityKey}`,
      notification,
      page: pagesById.get(notification.itemId || notification.pageId),
    }))
    .filter((activity) => activity.at > 0)
    .sort((left, right) => right.at - left.at || right.id.localeCompare(left.id));
}

/** Keeps the notifications whose kind the filter's "유형" leaves checked. */
export function filterInboxTypes(activities: NotificationActivity[], hidden: ReadonlySet<InboxType>) {
  return hidden.size === 0
    ? activities
    : activities.filter((activity) => !hidden.has(inboxTypeOf(activity.notification)));
}

export function groupNotificationActivities<T extends { at: number }>(
  activities: T[],
  sectionFor: (at: number) => string,
) {
  const groups: Array<{ section: string; items: T[] }> = [];
  for (const activity of activities) {
    const section = sectionFor(activity.at);
    const last = groups[groups.length - 1];
    if (last?.section === section) last.items.push(activity);
    else groups.push({ section, items: [activity] });
  }
  return groups;
}

export function actorProfilesById(profiles: CommentActorProfile[]): Record<string, CommentActorProfile> {
  const byId: Record<string, CommentActorProfile> = Object.fromEntries(profiles.map((profile) => [profile.userId, profile]));
  // Leafen's own and an anonymous answer's, which no account holds, name themselves.
  for (const userId of [LEAFEN_SYSTEM_USER_ID, LEAFEN_ANONYMOUS_USER_ID]) {
    byId[userId] ??= { userId, displayName: personName(userId), avatar: personAvatarValue(userId), revision: 0 };
  }
  return byId;
}

/** Pages others edited, out of the trash and from under any page in it (outOfTrashIn), latest first. */
export function buildPageEditActivityInputs(pages: Page[], userId: string) {
  const outOfTrash = outOfTrashIn(pages);
  return pages
    .filter((page) => outOfTrash(page) && page.lastEditedBy !== userId)
    .map((page) => ({ page, at: timestamp(page.updatedAt) }))
    .filter(({ at, page }) => at > 0 && Boolean(page.lastEditedBy))
    .sort((left, right) => right.at - left.at || right.page.id.localeCompare(left.page.id))
    .slice(0, PAGE_EDIT_SYNC_LIMIT)
    .map<NotificationActivityInput>(({ page, at }) => ({
      activityKey: `page:${page.id}:${at}`,
      kind: "page_edit",
      pageId: page.id,
      actorId: page.lastEditedBy,
      title: page.title,
      preview: "",
      target: pageHref(page.id),
      occurredAt: new Date(at).toISOString(),
    }));
}

/**
 * A share of the reader's given, changed or taken away (page_share_permission_notices.go):
 * "page-permission:<page>:<time>:<old role>:<new role>", the old role "" when it was given, the new "" when it went.
 */
const PAGE_PERMISSION_NOTICE = "page-permission:";

/** Which of the filter's types a notification counts as. */
export function inboxTypeOf(notification: NotificationInboxRecord): InboxType {
  switch (notification.kind) {
    case "invite":
    case "access_request":
      return "permissions";
    case "comment":
      return "discussions";
    case "mention":
    case "reminder":
      return "mentions";
    case "page_edit":
    case "link":
      return "updates";
    default:
      // A deadline not to miss is Leafen's own reminder.
      if (notification.activityKey.startsWith("deadline:")) return "mentions";
      // Requests asked of the person — a page's access, an app someone asks for, a teamspace to join — are permission
      // requests, and being added to a workspace is an invitation, as a page's is.
      if (["page-share-access:", "app-request:", "teamspace-join-", "workspace-member-added:", PAGE_PERMISSION_NOTICE]
        .some((kind) => notification.activityKey.startsWith(kind))) return "permissions";
      return notification.activityKey.startsWith("automation-notification:") ? "automations" : "workspace";
  }
}

/**
 * Notion's inbox groups, by the calendar from the week's first day: what came
 * today, the rest of this week, last week, then everything earlier (seen
 * 2026-11-15, a Sunday, and 2026-11-16, a Monday, Sunday-first weeks: on the
 * Monday the Sunday's notices were "이번 주", Sunday 11-08 to Saturday 11-14
 * "지난주" and Saturday 11-07 "이전"). There is no "yesterday": Saturday's went
 * under "지난주" on the Sunday.
 */
export function inboxSection(at: number, now: number, weekStart: 0 | 1): "today" | "thisWeek" | "lastWeek" | "earlier" {
  const start = new Date(now);
  start.setHours(0, 0, 0, 0);
  if (at >= start.getTime()) return "today";
  start.setDate(start.getDate() - ((start.getDay() - weekStart + 7) % 7));
  if (at >= start.getTime()) return "thisWeek";
  start.setDate(start.getDate() - 7);
  return at >= start.getTime() ? "lastWeek" : "earlier";
}

/**
 * How long ago, as Notion's inbox shows it: "8시간", then whole days ("2일")
 * until three days have passed, then the date (seen 2026-11-16 01:08: a notice
 * of 11-13 01:37 read "2일", one of 01:03 "11월 13일"). The workspace's
 * updates keep their days for a week (`relativeDays`; Notion's not measured).
 */
export function inboxAge(at: number, now: number, relativeDays = 3):
  | { unit: "now" }
  | { unit: "minutes" | "hours" | "days"; count: number }
  | { unit: "date" } {
  const minutes = Math.floor(Math.max(0, now - at) / 60_000);
  if (minutes < 1) return { unit: "now" };
  if (minutes < 60) return { unit: "minutes", count: minutes };
  if (minutes < 24 * 60) return { unit: "hours", count: Math.floor(minutes / 60) };
  if (minutes < relativeDays * 24 * 60) return { unit: "days", count: Math.floor(minutes / (24 * 60)) };
  return { unit: "date" };
}

/** The sentence a notification's line reads as, by what it is about. */
export function inboxSentenceKey(notification: NotificationInboxRecord) {
  if (notification.kind === "system") {
    if (notification.activityKey.startsWith("page-share-access:")) return "pageAccess";
    if (notification.activityKey.startsWith("wiki-verification-expired:")) return "wikiExpired";
    if (notification.activityKey.startsWith("automation-notification:")) return "automation";
    if (notification.activityKey.startsWith("app-revoked:")) return "appRevoked";
    if (notification.activityKey.startsWith("app-stopped:")) return "appStopped";
    if (notification.activityKey.startsWith("app-notice:")) return "appNotice";
    if (notification.activityKey.startsWith("app-request:")) return "appRequest";
    if (notification.activityKey.startsWith("app-request-approved:")) return "appRequestApproved";
    if (notification.activityKey.startsWith("app-request-declined:")) return "appRequestDeclined";
    if (notification.activityKey.startsWith("deadline:")) return "deadline";
    if (notification.activityKey.startsWith("teamspace-join-request:")) return "teamspaceJoinRequest";
    if (notification.activityKey.startsWith("teamspace-join-approved:")) return "teamspaceJoinApproved";
    if (notification.activityKey.startsWith("workspace-member-added:")) return "workspaceAdded";
    if (notification.activityKey.startsWith("agent-reconnect:")) return "agentReconnect";
    if (notification.activityKey.startsWith("address-changed:")) return "addressChanged";
    if (notification.activityKey.startsWith("address-move-soon:")) return "addressMoveSoon";
    if (notification.activityKey.startsWith("address-move-ended:")) return "addressMoveEnded";
    if (notification.activityKey.startsWith("address-certificate:")) return "addressCertificate";
    if (notification.activityKey.startsWith("server-disk:")) return "serverDisk";
    if (notification.activityKey.startsWith("sign-in-return:")) return "signInReturn";
    if (notification.activityKey.startsWith(PAGE_PERMISSION_NOTICE)) return "permission";
    return "system";
  }
  // A comment that mentions the reader reads as a comment, as Notion's inbox
  // says it (seen 2026-11-06); "나를 멘션함" is for a mention in a page's text.
  if (notification.kind === "mention" && notification.commentId) return "comment";
  return {
    comment: "comment", mention: "mention", invite: "invite", access_request: "accessRequest",
    reminder: "reminder", link: "link", page_edit: "pageEdit",
  }[notification.kind] ?? "system";
}

/**
 * Whether a notification is about an app, an outside service or the person's
 * agents rather than a page (its page slot holds "app:<install>",
 * "service:<service>" or "agents:…").
 */
export function isAppNotice(notification: NotificationInboxRecord) {
  return notification.pageId.startsWith("app:") || notification.pageId.startsWith("service:") || notification.pageId.startsWith("agents:");
}

/**
 * Whether a notification is about something other than a page: an app's, a
 * service's, the agents', a teamspace's ("teamspace:<id>") or a workspace's
 * ("workspace:<id>") or a share of the reader's ("page-permission:<id>"), which names its page though it is closed to them.
 */
export function isPagelessNotice(notification: NotificationInboxRecord) {
  return isAppNotice(notification) || notification.pageId.startsWith("teamspace:") || isWorkspaceNotice(notification)
    || notification.pageId.startsWith(PAGE_PERMISSION_NOTICE);
}

/** The roles of a share of the reader's given, changed or taken away (PAGE_PERMISSION_NOTICE): from "" given, to "" taken; null for any other notice. */
export function permissionChangeOf(notification: NotificationInboxRecord) {
  if (notification.kind !== "system" || !notification.activityKey.startsWith(PAGE_PERMISSION_NOTICE)) return null;
  const [from = "", to = ""] = notification.activityKey.split(":").slice(3);
  return from || to ? { from, to } : null;
}

/** Whether a notification is about the workspace itself, as being added to it ("workspace:<id>"). */
export function isWorkspaceNotice(notification: NotificationInboxRecord) {
  return notification.pageId.startsWith("workspace:");
}

/**
 * Where opening a notification goes: its target, except that one naming only
 * a window opened over the page (a teamspace's settings) opens it over the
 * page on screen.
 */
export function inboxOpenHref(target: string, current: URL) {
  if (!target.startsWith("/?")) return target;
  const asked = new URLSearchParams(target.slice(2));
  if (!asked.has("teamspace-settings")) return target;
  const next = new URL(current.href);
  for (const [key, value] of asked) next.searchParams.set(key, value);
  return `${next.pathname}${next.search}${next.hash}`;
}

/** The request an access request's notification is about. */
export function accessRequestIdOf(notification: NotificationInboxRecord) {
  return notification.kind === "access_request" && notification.activityKey.startsWith("access-request:")
    ? notification.activityKey.slice("access-request:".length)
    : "";
}


/**
 * The page a line standing for comments on two discussions or more opens,
 * with the top bar's "토론" list, as Notion's (seen 2026-11-11): its target
 * without the comment it names; null for any other line.
 */
export function inboxDiscussionsPage(notification: NotificationInboxRecord) {
  if ((notification.discussions ?? 0) < 2 || !notification.target.startsWith("/p/")) return null;
  const path = notification.target.split("#")[0];
  try {
    const pageId = decodeURIComponent(path.slice("/p/".length).split(/[/?]/u)[0]);
    return pageId ? { path, pageId } : null;
  } catch {
    return null;
  }
}
