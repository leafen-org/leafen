import { useEffect, useState } from "react";
import { LeafenApiError, request } from "@/apiTransport";
import { appPlatformErrorCodes, appPlatformOperations, type UsableApp, type UsableAppListResponse } from "@/appPlatformWire.generated";
import { readAppPlatformAvailable } from "@/lib/appPlatformAvailability";
import { announceAppsChanged, onAppsChanged } from "@/lib/appsChangedEvents";
import { answeredUnlessAway, isServerUnreachable, retryWhenServerMayAnswer } from "@/lib/serverReach";
import type { Session } from "@/types";

// A page's app blocks, its menus and the switcher share one reading of the
// list for a few seconds.
const FRESH_MS = 10_000;
let cached: { token: string; at: number; reading: Promise<UsableApp[]> } | null = null;
// The list last read for the person, which stays while the server doesn't answer
// (a session's token may change across it): offline the switcher's apps went
// missing until the page was opened again (2026-11-06).
let lastRead: { person: string; apps: UsableApp[] } | null = null;

function readUsableApps(session: Session): Promise<UsableApp[]> {
  const now = Date.now();
  if (cached && cached.token === session.token && now - cached.at < FRESH_MS) return cached.reading;
  // A build without the app platform has none to list, and isn't asked; the
  // list read last shows at once while the server is away (answeredUnlessAway).
  const reading = readAppPlatformAvailable().then((available) => available
    ? answeredUnlessAway(request<UsableAppListResponse>(appPlatformOperations.usable.path, { method: appPlatformOperations.usable.method }, session))
      .then((listed) => listed.apps)
    : []).then((apps) => { lastRead = { person: session.user.id, apps }; return apps; });
  cached = { token: session.token, at: now, reading };
  reading.catch(() => { if (cached?.reading === reading) cached = null; });
  return reading;
}

onAppsChanged(() => { cached = null; });

/** The list read again now, past the shared few seconds: what a call that changes something checks first. */
export function readUsableAppsNow(session: Session): Promise<UsableApp[]> {
  cached = null;
  return readUsableApps(session);
}

/**
 * Runs `use` with an app as the page's list gave it, and once more with the
 * same app as the list reads now when the server says its install is gone:
 * an administrator removed the app and installed it again, in another window
 * or through the API, since the list was read (2026-11-19: a file pressed
 * then opened the removed install and said "앱을 열지 못했습니다" until the
 * window was left and come back to). Every open place reads its list again,
 * all of them on the one reading this takes; an app removed for good, or
 * not given to the person again, fails as before. An open frame whose call
 * finds its install gone opens again through here (AppPlaceFrame).
 */
export async function withAppInstalledAgain<T>(session: Session, listed: UsableApp, use: (app: UsableApp) => Promise<T>): Promise<T> {
  try {
    return await use(listed);
  } catch (failure) {
    if (!(failure instanceof LeafenApiError) || failure.code !== appPlatformErrorCodes.installNotFound) throw failure;
    announceAppsChanged();
    const now = await readUsableApps(session).catch(() => [] as UsableApp[]);
    const again = now.find((app) => app.appId === listed.appId && app.tenancyKey === listed.tenancyKey && app.installId !== listed.installId);
    if (!again) throw failure;
    return use(again);
  }
}

/** The installed apps the person may use, and whether the list has been read. */
export function useUsableAppList(session: Session | null | undefined, enabled = true): { apps: UsableApp[]; loaded: boolean } {
  const [state, setState] = useState<{ apps: UsableApp[]; loaded: boolean }>({ apps: [], loaded: false });
  useEffect(() => {
    if (!session || !enabled) return;
    let live = true;
    let stopRetrying: (() => void) | null = null;
    const read = () => readUsableApps(session).then((apps) => {
      if (!live) return;
      stopRetrying?.();
      stopRetrying = null;
      setState({ apps, loaded: true });
    }, (failure) => {
      if (!live) return;
      setState({ apps: lastRead?.person === session.user.id ? lastRead.apps : [], loaded: true });
      // Read again once the server may answer (lib/serverReach), as the rest of Leafen does offline.
      if (isServerUnreachable(failure) && !stopRetrying) stopRetrying = retryWhenServerMayAnswer(() => { void read(); });
    });
    void read();
    // An administrator's stop, start or removal reaches the open places: at
    // once from this window, and when the person comes back to it from
    // another (2026-11-09: an open block kept running after an emergency
    // stop, and stayed "stopped" after the app was on again).
    const again = () => { if (document.visibilityState === "visible") void read(); };
    const stopListening = onAppsChanged(() => { void read(); });
    document.addEventListener("visibilitychange", again);
    window.addEventListener("focus", again);
    return () => {
      live = false; stopRetrying?.(); stopListening();
      document.removeEventListener("visibilitychange", again); window.removeEventListener("focus", again);
    };
  }, [enabled, session]);
  return state;
}

/** The installed apps the person may use, for the switcher; none while unread or unavailable. */
export function useUsableApps(session: Session | null | undefined, enabled = true): UsableApp[] {
  return useUsableAppList(session, enabled).apps;
}

/** The app with this id in the organization a page is in, or wherever the person may use it. */
export function usableAppFor(apps: readonly UsableApp[], appId: string, organizationId: string | undefined) {
  const choices = apps.filter((app) => app.appId === appId);
  return choices.find((app) => app.tenancyKey === `org:${organizationId}`) ?? choices[0];
}
