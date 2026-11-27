import { request } from "@/apiTransport";
import {
  deadlineOperations, type DeadlineNotice, type DeadlineNoticeAcknowledged, type DeadlineNoticeList, type DeadlineNoticeStates,
} from "@/deadlineWire.generated";
import { loadSession } from "@/lib/authSessionStore";

// A deadline someone must not miss (the server's deadline_values.go): a
// database date's "deadline", the days before it its notices go, as
// "7,3,1,0". The server sends the notices, again until each is acknowledged.

export const DEFAULT_DEADLINE_DAYS = "7,3,1,0";
const MAX_DEADLINE_NOTICES = 8;
const MAX_DEADLINE_DAYS = 365;

/** The days as the server keeps them, the most first; null when they aren't whole days 0–365, at most 8. */
export function canonicalDeadlineDays(text: string): string | null {
  const days = new Set<number>();
  for (const part of text.split(/[,\s]+/u).filter(Boolean)) {
    if (!/^\d+$/u.test(part)) return null;
    const day = Number(part);
    if (day > MAX_DEADLINE_DAYS) return null;
    days.add(day);
  }
  if (days.size === 0 || days.size > MAX_DEADLINE_NOTICES) return null;
  return [...days].sort((a, b) => b - a).join(",");
}

/** A date value's deadline days, "" when it isn't one. */
export function storedDeadline(value: unknown): string {
  if (!value || typeof value !== "object") return "";
  const { deadline } = value as { deadline?: unknown };
  return typeof deadline === "string" ? canonicalDeadlineDays(deadline) ?? "" : "";
}

/** The notice a deadline notification is about. */
export function deadlineNoticeIdOf(activityKey: string) {
  return activityKey.startsWith("deadline:") ? activityKey.slice("deadline:".length).split(":")[0] ?? "" : "";
}

function sessionOrFail() {
  const session = loadSession();
  if (!session) throw new Error("Login required.");
  return session;
}

export function readDeadlineNotices(rowId: string, propertyId: string, signal?: AbortSignal): Promise<DeadlineNotice[]> {
  const path = deadlineOperations.list.path.replace("{rowId}", encodeURIComponent(rowId)) +
    `?propertyId=${encodeURIComponent(propertyId)}`;
  return request<DeadlineNoticeList>(path, { signal }, sessionOrFail()).then((list) => list.notices);
}

export function acknowledgeDeadlineNotice(noticeId: string): Promise<DeadlineNotice> {
  const path = deadlineOperations.acknowledge.path.replace("{noticeId}", encodeURIComponent(noticeId));
  return request<DeadlineNoticeAcknowledged>(path, { method: "POST" }, sessionOrFail()).then((result) => result.notice);
}

/** Which of the notices the person has acknowledged, and which are gone with their deadline's change. */
export function readDeadlineNoticeStates(ids: string[], signal?: AbortSignal): Promise<DeadlineNoticeStates> {
  return request<DeadlineNoticeStates>(deadlineOperations.states.path,
    { method: "POST", body: JSON.stringify({ ids }), signal }, sessionOrFail());
}
