import { request } from "@/apiTransport";
import { appPlatformOperations, type AppSession, type AppSessionInput } from "@/appPlatformWire.generated";
import type { Session } from "@/types";

// An app's session for the person: what a frame of it opens with — its
// token, its frame's address — in an organization and one of its places.

/** How long before its token runs out a session is opened again. */
export const APP_TOKEN_REFRESH_BEFORE_MS = 60_000;

/** Opens the app for the person in an organization, in one of its places. */
export function openAppSession(session: Session, installId: string, tenancyKey: string, place: Omit<AppSessionInput, "tenancyKey"> = {}) {
  return request<AppSession>(appPlatformOperations.openSession.path.replace("{installId}", encodeURIComponent(installId)), {
    method: appPlatformOperations.openSession.method, body: JSON.stringify({ tenancyKey, ...place }),
  }, session);
}

// Sessions shared by many frames of one place — a database's cells — so a
// table of them opens the app once, until its token is nearly spent.
const sharedSessions = new Map<string, { installId: string; reading: Promise<AppSession>; expiresAt: number }>();

/** The person's session for an app in a place, shared with other frames of that place. */
export function sharedAppSession(session: Session, installId: string, tenancyKey: string, place: Omit<AppSessionInput, "tenancyKey">) {
  const key = `${session.token}\u0000${installId}\u0000${tenancyKey}\u0000${JSON.stringify(place)}`;
  const known = sharedSessions.get(key);
  if (known && known.expiresAt - Date.now() > APP_TOKEN_REFRESH_BEFORE_MS) return known.reading;
  const reading = openAppSession(session, installId, tenancyKey, place);
  const entry = { installId, reading, expiresAt: Date.now() + 5 * 60_000 };
  sharedSessions.set(key, entry);
  reading.then((opened) => { entry.expiresAt = opened.expiresAt ?? 0; }, () => { if (sharedSessions.get(key) === entry) sharedSessions.delete(key); });
  return reading;
}

/**
 * Drops the shared sessions of an install a frame found no longer holding
 * (removed, its token refused): the next frame of the place opens anew
 * rather than with a token the server no longer takes.
 */
export function forgetSharedAppSessions(installId: string) {
  for (const [key, entry] of sharedSessions) if (entry.installId === installId) sharedSessions.delete(key);
}
