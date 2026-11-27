import type { Session } from "./types";
import { request } from "./apiTransport";
import { announceCommentFollowingChange } from "./lib/commentRefreshEvents";

export type NotificationSettings = {
  emailActivity: boolean;
  emailAlways: boolean;
  /** A daily digest of changes to pages the person follows or created. */
  emailPageUpdates: boolean;
  /** A weekly digest of the pages most read in the person's workspaces. */
  emailDigest: boolean;
  /** The server can send email at all. */
  emailAvailable: boolean;
  /** Emails can link back to pages. */
  emailLinks: boolean;
  revision: number;
};

export type NotificationSettingsChange = Partial<Pick<
  NotificationSettings, "emailActivity" | "emailAlways" | "emailPageUpdates" | "emailDigest"
>>;

export type PageNotificationLevel = "all_comments" | "replies_and_mentions";

/** Where a page's "알림 받기", or one of its databases', is kept. */
function notificationPreferencePath(pageId: string, database?: string) {
  const page = `/pages/${encodeURIComponent(pageId)}`;
  return `${page}${database ? `/databases/${encodeURIComponent(database)}` : ""}/notification-preference`;
}

export type PageNotificationPreference = {
  pageId: string;
  level: PageNotificationLevel;
};

/** A database row page is followed for all updates, important updates, or replies and @mentions. */
export type DatabaseRowNotificationLevel = "all_updates" | "all_comments" | "replies_and_mentions";

export type DatabaseRowNotificationPreference = {
  rowId: string;
  level: DatabaseRowNotificationLevel;
};

/** The key a browser subscribes to desktop push with. */
/** Slack notifications: whether the server has a Slack bot for them and whether they are on for you. */
export type SlackNotificationState = {
  available: boolean;
  enabled: boolean;
};

export type WebPushState = {
  available: boolean;
  publicKey: string;
};

export type WebPushSubscriptionInput = {
  endpoint: string;
  keys: { p256dh: string; auth: string };
};

function followingChanged<T>(saved: T) {
  announceCommentFollowingChange();
  return saved;
}

export function createNotificationSettingsApi() {
  return {
    readNotificationSettings: (session: Session, signal?: AbortSignal) => request<NotificationSettings>(
      "/account/notifications", { signal }, session,
    ),
    updateNotificationSettings: (session: Session, change: NotificationSettingsChange) => (
      request<NotificationSettings>(
        "/account/notifications", { method: "PATCH", body: JSON.stringify(change) }, session,
      )
    ),
    // With a database on the page, that database's own: an inline database opened alone has its own "알림 받기".
    readPageNotificationPreference: (session: Session, pageId: string, signal?: AbortSignal, database?: string) => (
      request<PageNotificationPreference>(
        `${notificationPreferencePath(pageId, database)}`, { signal }, session,
      )
    ),
    // Which discussions are unread goes by what the person follows (announceCommentFollowingChange).
    savePageNotificationPreference: (session: Session, pageId: string, level: PageNotificationLevel, database?: string) => (
      request<PageNotificationPreference>(
        `${notificationPreferencePath(pageId, database)}`,
        { method: "PUT", body: JSON.stringify({ level }) },
        session,
      ).then(followingChanged)
    ),
    readWebPushState: (session: Session, signal?: AbortSignal) => request<WebPushState>(
      "/account/web-push", { signal }, session,
    ),
    saveWebPushSubscription: (session: Session, subscription: WebPushSubscriptionInput) => request<void>(
      "/account/web-push/subscription", { method: "PUT", body: JSON.stringify(subscription) }, session,
    ),
    deleteWebPushSubscription: (session: Session, endpoint: string) => request<void>(
      "/account/web-push/subscription", { method: "DELETE", body: JSON.stringify({ endpoint }) }, session,
    ),
    readSlackNotificationState: (session: Session, signal?: AbortSignal) => request<SlackNotificationState>(
      "/account/slack-notifications", { signal }, session,
    ),
    saveSlackNotificationState: (session: Session, enabled: boolean) => request<SlackNotificationState>(
      "/account/slack-notifications", { method: "PUT", body: JSON.stringify({ enabled }) }, session,
    ),
    readDatabaseRowNotificationPreference: (session: Session, rowId: string, signal?: AbortSignal) => (
      request<DatabaseRowNotificationPreference>(
        `/database-rows/${encodeURIComponent(rowId)}/notification-preference`, { signal }, session,
      )
    ),
    saveDatabaseRowNotificationPreference: (session: Session, rowId: string, level: DatabaseRowNotificationLevel) => (
      request<DatabaseRowNotificationPreference>(
        `/database-rows/${encodeURIComponent(rowId)}/notification-preference`,
        { method: "PUT", body: JSON.stringify({ level }) },
        session,
      ).then(followingChanged)
    ),
  };
}
