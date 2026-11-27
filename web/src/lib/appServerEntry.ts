// What a packaged client's shell hands its window for the app's own server
// (the multi-server decision of 2026-10-20: one account flow for every
// server): while the server isn't set up, the code that opens its first
// setup and the name to suggest; while an earlier version's hidden owner
// waits for this window to keep a sign-in, the capability that signs it in
// once more. It stays with this window only.
const APP_SERVER_ENTRY_KEY = "leafen:v2:app-server-entry";

export type AppServerEntry = { capability?: string; setupCode?: string; name?: string };

let held: AppServerEntry | null = null;

/** Keeps what the shell handed this window for its own server. */
export function rememberAppServerEntry(entry: AppServerEntry) {
  held = entry;
  try {
    window.sessionStorage.setItem(APP_SERVER_ENTRY_KEY, JSON.stringify(entry));
  } catch {
    // Held in memory until the window reloads.
  }
}

/** What this window was handed for its own server, if anything. */
export function appServerEntry(): AppServerEntry {
  if (held) return held;
  try {
    const kept = JSON.parse(window.sessionStorage.getItem(APP_SERVER_ENTRY_KEY) ?? "null") as AppServerEntry | null;
    return kept && typeof kept === "object" ? kept : {};
  } catch {
    return {};
  }
}

/** Forgets it: the window works with another server, or the earlier owner settled. */
export function forgetAppServerEntry() {
  held = null;
  try {
    window.sessionStorage.removeItem(APP_SERVER_ENTRY_KEY);
  } catch {
    // Nothing kept there to forget.
  }
}
