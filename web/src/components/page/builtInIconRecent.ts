import { parseStored, personValue, storePersonValue } from "@/lib/browserStorage";
import { NATIVE_ICONS } from "@/nativeIconCatalog.generated";

// The Icons tab's "최근 항목", as Notion's (its code, read 2026-11-16, and
// seen after a pick the same day): each icon picked goes first, once, by its
// name alone — the tab draws it in its color of the moment — and two rows of
// the grid are kept. The person's, on this browser.

const STORAGE_KEY = "leafen.recentBuiltInIcons";
/** The rows of them the tab keeps and shows. */
export const RECENT_BUILT_IN_ICON_ROWS = 2;
/** Two rows of the desktop picker's twelve. */
const KEPT = 24;
const known = new Set(NATIVE_ICONS.map(([name]) => name));

export function readRecentBuiltInIcons(): string[] {
  const stored = parseStored(personValue(STORAGE_KEY));
  return Array.isArray(stored)
    ? stored.filter((name): name is string => typeof name === "string" && known.has(name)).slice(0, KEPT)
    : [];
}

export function recordRecentBuiltInIcon(name: string) {
  if (!known.has(name)) return;
  const next = [name, ...readRecentBuiltInIcons().filter((kept) => kept !== name)].slice(0, KEPT);
  storePersonValue(STORAGE_KEY, JSON.stringify(next));
}
