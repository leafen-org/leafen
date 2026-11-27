// The password a person just signed in with, kept in memory only for the
// screen that may follow at once: an administrator's temporary password must
// be changed, and asking for it again right after it was typed was a step too
// many (2026-10-16). Only the same account reads it, within a few minutes; it
// is forgotten once used, and a reload forgets it so the screen asks.

const HANDOFF_MS = 10 * 60 * 1000;
let held: { userId: string; password: string; at: number } | null = null;

export function holdSignInPassword(userId: string, password: string) {
  held = userId && password ? { userId, password, at: Date.now() } : null;
}

export function readSignInPassword(userId: string): string {
  if (!held || held.userId !== userId || Date.now() - held.at >= HANDOFF_MS) return "";
  return held.password;
}

export function forgetSignInPassword() {
  held = null;
}
