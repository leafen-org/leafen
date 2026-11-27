import type {
  ActorProfile,
  NotificationKind,
  NotificationRecord,
} from "@/lib/types";

export interface NotificationReadContinuation {
  mode: "selected" | "all";
  readAt: string;
  throughOccurredAt?: string;
  throughId?: string;
}

export interface NotificationMutationResult {
  workspaceId: string;
  notifications: NotificationRecord[];
  actorProfiles?: ActorProfile[];
  complete: boolean;
  partial?: boolean;
  notificationReadContinuation?: NotificationReadContinuation;
  synced?: NotificationRecord[];
  updated?: NotificationRecord[];
  unreadCount: number;
  total: number;
}

export interface ListNotificationsInput {
  workspaceId: string;
  includeRead?: boolean;
  kind?: NotificationKind;
  limit?: number;
}
