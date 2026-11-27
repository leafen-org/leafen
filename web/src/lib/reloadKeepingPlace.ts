// A reload the app asks for — a new build's "새로고침", one made at rest —
// comes back where the person was: what is open over the page (the settings
// window and its section) tells this before the page goes, and opens again
// once it is back (2026-11-05: the settings window closed and left the person
// on the last page). A reload of the person's own is left as the browser's.

const KEEP_EVENT = "leafen:keep-place-through-reload";
const KEPT_KEY = "leafen.keptPlace";

/** Reloads the page, keeping what is open over it. */
export function reloadKeepingPlace() {
  window.dispatchEvent(new Event(KEEP_EVENT));
  window.location.reload();
}

/** Keeps a value under a name while the app reloads itself. */
export function onReloadKeepingPlace(name: string, value: () => unknown): () => void {
  const keep = () => {
    const current = value();
    if (current === null || current === undefined) return;
    try {
      const kept = JSON.parse(window.sessionStorage.getItem(KEPT_KEY) ?? "{}") as Record<string, unknown>;
      kept[name] = current;
      window.sessionStorage.setItem(KEPT_KEY, JSON.stringify(kept));
    } catch {
      // No session storage: the page comes back as a reload's would.
    }
  };
  window.addEventListener(KEEP_EVENT, keep);
  return () => window.removeEventListener(KEEP_EVENT, keep);
}

/** Takes what was kept under a name through the last reload, once. */
export function takeKeptPlace<T>(name: string): T | null {
  try {
    const kept = JSON.parse(window.sessionStorage.getItem(KEPT_KEY) ?? "{}") as Record<string, unknown>;
    if (!(name in kept)) return null;
    const value = kept[name] as T;
    delete kept[name];
    if (Object.keys(kept).length) window.sessionStorage.setItem(KEPT_KEY, JSON.stringify(kept));
    else window.sessionStorage.removeItem(KEPT_KEY);
    return value;
  } catch {
    return null;
  }
}
