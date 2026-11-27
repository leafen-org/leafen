import { hasDesktopTabs } from "./desktopMenuCommands";

// A tab of the Mac or Windows app keeps the sign-in it shows — its server
// profile and the account on it — as each tab of Notion's app keeps its
// account (7.42.3, read 2026-11-14: a tab keeps its currentUserId, comes
// back at the next start with it and opens a new tab in the account of the
// tab in front). The page keeps it in its tab's session storage, which lasts
// as long as the tab's page does, through its starting over (a switch of
// account or server); the app keeps it with the tab for its next start and
// gives it to a tab opened from it (desktop/window_tabs_kept.go). Only names
// are kept here: the sign-ins themselves stay in the app's secret storage
// (keptSignIns.ts).

const TAB_SIGN_IN_KEY = "leafen:v2:tab-sign-in";

// How long a page's first start waits for the app to say its tab's sign-in;
// the app answers from memory, so this long is an app that won't.
const ASK_WAIT_MS = 1_000;

/** A tab's sign-in: a server profile and an account on it ("" for the one used last there). */
export type TabSignIn = { profile: string; user: string };

const watchers = new Set<() => void>();

function signInOf(value: string | null | undefined): TabSignIn | null {
  try {
    const parsed = value ? JSON.parse(value) as Partial<TabSignIn> | null : null;
    return parsed && typeof parsed.profile === "string" && parsed.profile !== "" && typeof parsed.user === "string"
      ? { profile: parsed.profile, user: parsed.user }
      : null;
  } catch {
    return null;
  }
}

/** The sign-in this tab of the app shows, or null for a page that isn't one or hasn't said it yet. */
export function tabSignIn(): TabSignIn | null {
  if (!hasDesktopTabs()) return null;
  try {
    return signInOf(sessionStorage.getItem(TAB_SIGN_IN_KEY));
  } catch {
    return null;
  }
}

/** Keeps the sign-in this tab of the app shows now, which it starts in again. */
export function keepTabSignIn(profile: string, user: string): void {
  if (!hasDesktopTabs() || !profile) return;
  const value = JSON.stringify({ profile, user });
  try {
    if (sessionStorage.getItem(TAB_SIGN_IN_KEY) === value) return;
    sessionStorage.setItem(TAB_SIGN_IN_KEY, value);
  } catch {
    return;
  }
  for (const watcher of [...watchers]) watcher();
}

/** Calls `change` as the sign-in this tab keeps changes; returns the way to stop. */
export function watchTabSignIn(change: () => void): () => void {
  watchers.add(change);
  return () => { watchers.delete(change); };
}

/**
 * At a tab's first start — brought back at the app's start, opened again,
 * or opened from another tab — the sign-in the app kept for it becomes the
 * one it starts in. A page that already keeps one (starting over) or that
 * the app tells none keeps what it has.
 */
export async function takeKeptTabSignIn(): Promise<void> {
  if (!hasDesktopTabs() || tabSignIn()) return;
  let wait: number | undefined;
  try {
    const { Call } = await import("@wailsio/runtime");
    const said = await Promise.race([
      Call.ByName("main.WindowTabs.SignIn") as Promise<unknown>,
      new Promise<undefined>((resolve) => { wait = window.setTimeout(resolve, ASK_WAIT_MS); }),
    ]);
    const kept = typeof said === "string" ? signInOf(said) : null;
    if (kept && !tabSignIn()) keepTabSignIn(kept.profile, kept.user);
  } catch {
    // The page starts in the sign-in used last.
  } finally {
    window.clearTimeout(wait);
  }
}
