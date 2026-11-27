import type { Session } from "./types";
import {
  commentBounds,
  commentOperations,
  type NotificationActivitySyncInput,
  type NotificationActivitySyncResponse,
  type NotificationArchiveInput,
  type NotificationArchiveResponse,
  type NotificationInboxResponse,
  type NotificationReadInput,
  type NotificationReadResponse,
  type NotificationStateInput,
  type NotificationStateResponse,
} from "./commentWire.generated";

/** Which of the inbox's notifications a list holds, as Notion's inbox filter. */
export type NotificationInboxState = "inbox" | "unread" | "archived";

type JsonRequest = <T>(
  path: string,
  init?: RequestInit,
  session?: Session | null,
) => Promise<T>;

function workspacePath(template: string, workspaceId: string) {
  return template.replace("{workspaceId}", encodeURIComponent(workspaceId));
}

function settlementPath(template: string, workspaceId: string, settlementId: string) {
  return workspacePath(template, workspaceId).replace(
    "{settlementId}",
    encodeURIComponent(settlementId),
  );
}

function notificationLimit(limit: number | undefined) {
  const value = limit ?? commentBounds.notificationInboxDefault;
  if (
    !Number.isSafeInteger(value)
    || value < 1
    || value > commentBounds.notificationInboxMax
  ) {
    throw new RangeError("Invalid notification inbox limit.");
  }
  return value;
}

export function createNotificationApi(request: JsonRequest) {
  return {
    listNotifications(
      session: Session,
      workspaceId: string,
      limit: number = commentBounds.notificationInboxDefault,
      signal?: AbortSignal,
      /** Personal notifications, as the inbox shows by default, or every workspace update. */
      scope: "personal" | "all" = "all",
      state: NotificationInboxState = "inbox",
      /** Where the page before ended, to read on from there. */
      cursor?: string,
    ) {
      const query = new URLSearchParams({ limit: String(notificationLimit(limit)) });
      if (scope === "personal") query.set("scope", scope);
      if (state !== "inbox") query.set("state", state);
      if (cursor) query.set("cursor", cursor);
      return request<NotificationInboxResponse>(
        `${workspacePath(commentOperations.listNotificationInbox.path, workspaceId)}?${query}`,
        { method: commentOperations.listNotificationInbox.method, signal },
        session,
      );
    },

    syncNotificationActivities(
      session: Session,
      workspaceId: string,
      input: NotificationActivitySyncInput,
      signal?: AbortSignal,
    ) {
      if (input.activities.length > commentBounds.notificationSyncActivities) {
        throw new RangeError("Notification activity sync bound exceeded.");
      }
      return request<NotificationActivitySyncResponse>(
        workspacePath(commentOperations.syncNotificationActivities.path, workspaceId),
        {
          method: commentOperations.syncNotificationActivities.method,
          body: JSON.stringify(input),
          signal,
        },
        session,
      );
    },

    mutateNotificationRead(
      session: Session,
      workspaceId: string,
      input: NotificationReadInput,
      signal?: AbortSignal,
    ) {
      return request<NotificationReadResponse>(
        workspacePath(commentOperations.mutateNotificationRead.path, workspaceId),
        {
          method: commentOperations.mutateNotificationRead.method,
          body: JSON.stringify(input),
          signal,
        },
        session,
      );
    },

    readNotificationSettlement(
      session: Session,
      workspaceId: string,
      settlementId: string,
      signal?: AbortSignal,
    ) {
      return request<NotificationReadResponse>(
        settlementPath(
          commentOperations.getNotificationReadSettlement.path,
          workspaceId,
          settlementId,
        ),
        { method: commentOperations.getNotificationReadSettlement.method, signal },
        session,
      );
    },

    continueNotificationSettlement(
      session: Session,
      workspaceId: string,
      settlementId: string,
      signal?: AbortSignal,
    ) {
      return request<NotificationReadResponse>(
        settlementPath(
          commentOperations.continueNotificationReadSettlement.path,
          workspaceId,
          settlementId,
        ),
        { method: commentOperations.continueNotificationReadSettlement.method, signal },
        session,
      );
    },

    archiveNotifications(
      session: Session,
      workspaceId: string,
      input: NotificationArchiveInput,
      signal?: AbortSignal,
    ) {
      return request<NotificationArchiveResponse>(
        workspacePath(commentOperations.archiveNotifications.path, workspaceId),
        {
          method: commentOperations.archiveNotifications.method,
          body: JSON.stringify(input),
          signal,
        },
        session,
      );
    },

    /** Marks notifications unread again, or takes them back out of the archive. */
    changeNotificationState(
      session: Session,
      workspaceId: string,
      input: NotificationStateInput,
      signal?: AbortSignal,
    ) {
      return request<NotificationStateResponse>(
        workspacePath(commentOperations.changeNotificationState.path, workspaceId),
        {
          method: commentOperations.changeNotificationState.method,
          body: JSON.stringify(input),
          signal,
        },
        session,
      );
    },
  };
}
