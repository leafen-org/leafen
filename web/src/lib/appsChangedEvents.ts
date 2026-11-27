import { useCallback, useEffect, useRef } from "react";
import { isServerUnreachable, retryWhenServerMayAnswer } from "@/lib/serverReach";
import type { Session } from "@/types";

// An app was installed, removed or given to people, or what the server offers
// changed, on this screen or in a window over it: the screens that list apps
// read them again.

const APPS_CHANGED_EVENT = "leafen:apps-changed";

export function announceAppsChanged() {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new Event(APPS_CHANGED_EVENT));
}

export function onAppsChanged(listener: () => void): () => void {
  if (typeof window === "undefined") return () => {};
  window.addEventListener(APPS_CHANGED_EVENT, listener);
  return () => window.removeEventListener(APPS_CHANGED_EVENT, listener);
}

// The lists about the person's apps last read in this tab, by the person
// and the list: shown at once when "앱" opens again, the server's answer
// replacing them (2026-11-16: offline, "앱 비밀번호", the credentials and
// the schedules were gone until each request gave up, as the app list was
// on 2026-11-15).
const keptLists = new Map<string, unknown>();

function keptKey(session: Session, name: string) {
  return `${session.user.id}/${name}`;
}

/** The list named last read in this tab for the person, if any. */
export function keptAppsList<T>(session: Session | null | undefined, name: string): T | undefined {
  return session ? keptLists.get(keptKey(session, name)) as T | undefined : undefined;
}

/**
 * A list about the person's apps, read when the screen opens and again
 * whenever apps change, the latest reading winning (2026-11-11: an app
 * installed on /apps was missing from its "앱 비밀번호" until the page was
 * opened again). Each list read is kept by its name (keptAppsList), and
 * the returned adopt keeps one a change answered with; a reading that
 * found the server away is read again once the server may answer
 * (retryWhenServerMayAnswer) — the read itself fails at once while the
 * app knows the connection is lost (answeredUnlessAway, personAppsApi.ts).
 */
export function useAppsListReading<T>(session: Session, name: string, read: (session: Session) => Promise<T>, adopt: (list: T) => void,
  failed: (failure: unknown) => void = () => undefined): (list: T) => void {
  const handlers = useRef({ read, adopt, failed });
  useEffect(() => { handlers.current = { read, adopt, failed }; });
  useEffect(() => {
    let live = true;
    let latest = 0;
    let stopRetry: (() => void) | null = null;
    const reading = () => {
      const mine = ++latest;
      handlers.current.read(session).then((list) => {
        keptLists.set(keptKey(session, name), list);
        stopRetry?.();
        stopRetry = null;
        if (live && mine === latest) handlers.current.adopt(list);
      }, (failure) => {
        if (!live || mine !== latest) return;
        handlers.current.failed(failure);
        if (isServerUnreachable(failure) && !stopRetry) stopRetry = retryWhenServerMayAnswer(reading);
      });
    };
    reading();
    const stop = onAppsChanged(reading);
    return () => { live = false; stop(); stopRetry?.(); };
  }, [session, name]);
  return useCallback((list: T) => {
    keptLists.set(keptKey(session, name), list);
    handlers.current.adopt(list);
  }, [session, name]);
}
