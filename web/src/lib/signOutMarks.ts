import type { Session } from "@/types";
import { tokenSessionId } from "./authSessionStore";

// A sign-out the person asked for, in this tab or another of theirs, keeps no
// place to come back to (reauthReturnPath.ts). A sign-in that ended by itself
// — the server no longer takes it, the device was signed out from another —
// comes back where it was, as the sign-in screen says. Another tab learns the
// sign-out was asked for from this mark, as its session goes with it.
//
// Each mark names the sign-in that ended and is read only for it: a new
// sign-in the server ends seconds after a sign-out by hand ended the one
// before is said and keeps its place (2026-11-21: another device ended a
// sign-in made 1.5 s after signing out by hand, and the sign-in screen said
// nothing and opened the start page).
const SIGNED_OUT_BY_HAND_KEY = "leafen:auth:signed-out-by-hand";
// A sign-in the server no longer takes is said on the sign-in screen of every
// tab that held it, not only the one whose request learned it: that tab marks
// it as the stored sign-in goes, and the tabs following the stored sign-in
// away read the mark (App.tsx). A sign-out asked for, here or in another tab,
// says nothing, as above.
const SIGNED_OUT_BY_SERVER_KEY = "leafen:auth:signed-out-by-server";
const MARK_MS = 10_000;

/** The sign-in a mark names: the person and their server sign-in, which a renewed token keeps. */
function signInOf(session: Session) {
  return { userId: session.user.id.trim(), signIn: tokenSessionId(session.token) || session.sessionId || "" };
}

function mark(key: string, session: Session) {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(key, JSON.stringify({ ...signInOf(session), at: Date.now() }));
  } catch {
    // Without storage the other tabs go to the sign-in screen as for a sign-in that ended by itself, or without the reason.
  }
}

function marked(key: string, session: Session): boolean {
  if (typeof window === "undefined") return false;
  try {
    const kept = JSON.parse(window.localStorage.getItem(key) ?? "null") as { userId?: unknown; signIn?: unknown; at?: unknown } | null;
    const { userId, signIn } = signInOf(session);
    return kept?.userId === userId && (kept.signIn ?? "") === signIn && typeof kept.at === "number" && Date.now() - kept.at < MARK_MS;
  } catch {
    return false;
  }
}

/** The person signed out of this sign-in on purpose, which their other tabs that held it learn. */
export function markSignedOutByHand(session: Session) {
  mark(SIGNED_OUT_BY_HAND_KEY, session);
}

/** Whether the person just signed out of this sign-in on purpose, in this tab or another. */
export function signedOutByHand(session: Session): boolean {
  return marked(SIGNED_OUT_BY_HAND_KEY, session);
}

/** The server ended this sign-in: the person's other tabs that held it say so as they follow it away. */
export function markSignedOutByServer(session: Session) {
  mark(SIGNED_OUT_BY_SERVER_KEY, session);
}

/** Whether the server just ended this sign-in, seen from this tab or another, and the person did not sign out of it themselves. */
export function signedOutByServer(session: Session): boolean {
  return !signedOutByHand(session) && marked(SIGNED_OUT_BY_SERVER_KEY, session);
}
