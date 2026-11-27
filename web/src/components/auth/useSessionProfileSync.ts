import { useEffect, useRef } from "react";
import { api } from "@/api";
import { saveSessionIfCurrent } from "@/lib/authSessionStore";
import type { Session } from "@/types";

// A profile read at most this often when the app comes back into view.
const VISIBLE_READ_GAP_MS = 60_000;

/**
 * The signed-in account's name and email, as the server keeps them. They
 * change from this screen, but also from another device or an agent through
 * the API; the session the app holds took them at sign-in, so the settings'
 * account item, the account switcher and the profile list kept the old name
 * until the next sign-in. The app reads the profile as it opens and when it
 * comes back into view, and takes a changed name or email into its session
 * the way saving them on this screen does.
 */
export function useSessionProfileSync(session: Session, onSession: (session: Session) => void): void {
  const held = useRef(session);
  held.current = session;
  const take = useRef(onSession);
  take.current = onSession;
  const userId = session.user.id;
  useEffect(() => {
    let controller: AbortController | null = null;
    let lastRead = 0;
    const read = () => {
      controller?.abort();
      const current = new AbortController();
      controller = current;
      lastRead = Date.now();
      const asked = held.current;
      api.readAccountProfile(asked, current.signal).then(async (profile) => {
        if (current.signal.aborted || profile.id !== asked.user.id) return;
        const name = profile.displayName || asked.user.name;
        const email = profile.email || asked.user.email;
        if (name === asked.user.name && email === asked.user.email) return;
        const next: Session = { ...asked, user: { ...asked.user, name, email } };
        if (await saveSessionIfCurrent(asked, next) && !current.signal.aborted) take.current(next);
      }).catch(() => undefined);
    };
    const onVisible = () => {
      if (document.visibilityState === "visible" && Date.now() - lastRead >= VISIBLE_READ_GAP_MS) read();
    };
    read();
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      document.removeEventListener("visibilitychange", onVisible);
      controller?.abort();
    };
  }, [userId]);
}
