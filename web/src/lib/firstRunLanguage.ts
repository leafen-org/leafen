// The language chosen before there is any account here: on the first screen
// (the multi-server decision of 2026-10-19, 1: the app's first install and a
// server's first setup ask it first, chosen ahead from the device's
// language) or at the sign-in screen's foot (SignInLanguage.tsx, as Notion's
// signed-out login page). The screens before an account read in it — kept in
// this browser through a reload, the app's start and another tab, as
// Notion's login page keeps its choice for the next one opened (seen
// 2026-11-19) — and the first administrator's account takes it, so the
// language step after the name is not asked again. Someone signing in to a
// server meets that server's account language; this only chooses ahead in
// its step. Once an account here has its language, the choice is spent
// (forgetFirstRunLanguage).
//
// No i18n here: the page's start (i18n/index.ts) reads it.

const STORAGE_KEY = "leafen:first-run-language";

let chosen: string | null = null;

/** The language preference chosen before an account in this browser, or "". */
export function firstRunLanguage(): string {
  if (chosen !== null) return chosen;
  try {
    chosen = localStorage.getItem(STORAGE_KEY) ?? "";
  } catch {
    chosen = "";
  }
  return chosen;
}

/** Keeps the choice for this browser's screens before an account. */
export function keepFirstRunLanguage(preference: string): void {
  chosen = preference;
  try {
    localStorage.setItem(STORAGE_KEY, preference);
  } catch {
    // Kept for this page alone where the browser keeps nothing.
  }
}

/** Spends the choice once an account here has its language. */
export function forgetFirstRunLanguage(): void {
  chosen = "";
  try {
    localStorage.removeItem(STORAGE_KEY);
  } catch {
    // Nothing kept where nothing can be.
  }
}
