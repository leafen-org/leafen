import { parseStored, personValue, storePersonValue } from "@/lib/browserStorage";
import { RECENT_ICON_STORAGE_KEY } from "./pageIconPickerModel";

/**
 * How often a person picks each emoji in a workspace, as Notion keeps it
 * (its code, read 2026-11-02): a pick first halves every count once for
 * each week since that emoji's own last pick, then adds one to the picked
 * emoji's, and the hundred most used are kept. "최근 사용한 항목" lists the
 * most used first, the latest among equals, and a filter ranks each emoji a
 * hundred a count higher — the count as kept, not halved again as the filter
 * reads it (its code, read again 2026-11-17). So an emoji picked once stands
 * first under a filter until a pick more than a week after it rounds its
 * count to 0, where it keeps its place in "최근 사용한 항목" but not under a
 * filter: Notion's 👍👎🍏, counted 0, kept their places under "o", "a" and
 * "t_" while ☺️🧡, counted 1, led (its picker, two tabs, 2026-11-17).
 */
export type EmojiUse = Readonly<Record<string, { count: number; at: number }>>;

const STORAGE_KEY = "leafen:emoji-use";
const WEEK = 7 * 24 * 60 * 60 * 1000;
const KEPT = 100;

function storeOptions(workspaceId?: string) {
  return workspaceId ? { part: workspaceId } : {};
}

function mostUsed(use: EmojiUse) {
  return Object.keys(use).sort((left, right) => use[right].count - use[left].count || use[right].at - use[left].at);
}

export function readEmojiUse(workspaceId?: string): EmojiUse {
  const stored = parseStored(personValue(STORAGE_KEY, storeOptions(workspaceId)));
  if (stored && typeof stored === "object" && !Array.isArray(stored)) {
    return Object.fromEntries(Object.entries(stored).filter(([, value]) =>
      typeof value?.count === "number" && typeof value?.at === "number"));
  }
  // The emoji picked before counts were kept, latest first, once each.
  const recent = parseStored(personValue(RECENT_ICON_STORAGE_KEY, { previous: [RECENT_ICON_STORAGE_KEY] }));
  const now = Date.now();
  return Array.isArray(recent)
    ? Object.fromEntries(recent.filter((emoji): emoji is string => typeof emoji === "string").slice(0, KEPT)
      .map((emoji, index) => [emoji, { count: 1, at: now - index }]))
    : {};
}

export function recordEmojiUse(use: EmojiUse, emoji: string, workspaceId?: string, now = Date.now()): EmojiUse {
  const next: Record<string, { count: number; at: number }> = {};
  for (const [key, value] of Object.entries(use)) {
    next[key] = { count: Math.round(value.count * 0.5 ** ((now - value.at) / WEEK)), at: value.at };
  }
  next[emoji] = { count: (next[emoji]?.count ?? 0) + 1, at: now };
  const kept = Object.keys(next).length > KEPT ? Object.fromEntries(mostUsed(next).slice(0, KEPT).map((key) => [key, next[key]])) : next;
  storePersonValue(STORAGE_KEY, JSON.stringify(kept), storeOptions(workspaceId));
  return kept;
}

/** The most used emoji, the latest first among equals. */
export function frequentEmojis(use: EmojiUse, limit: number) {
  return mostUsed(use).slice(0, limit);
}

/** How much higher a filter ranks the emoji for its use. */
export function emojiUseBonus(use: EmojiUse, emoji: string) {
  return 100 * (use[emoji]?.count ?? 0);
}
