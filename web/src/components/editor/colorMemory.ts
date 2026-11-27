import { personValue, storePersonValue } from "@/lib/browserStorage";

const LAST_COLOR_KEY = "leafen:last-editor-color";
const RECENT_COLORS_KEY = "leafen:recent-editor-colors";
// Notion's selection menu showed four recent colors, newest first, taking-off
// ones too (seen 2026-10-08); its row holds five.
const RECENT_COLORS_LIMIT = 5;
const LAST_COLOR_RECOVERY_KEY = "leafen:last-editor-color:recovery:v0";
const LAST_COLOR_FORMAT_VERSION = 1;

const COLOR_TOKENS = new Set([
  "default",
  "gray",
  "brown",
  "orange",
  "yellow",
  "green",
  "blue",
  "purple",
  "pink",
  "red",
  "gray_background",
  "brown_background",
  "orange_background",
  "yellow_background",
  "green_background",
  "blue_background",
  "purple_background",
  "pink_background",
  "red_background",
]);

function currentFormatToken(raw: string) {
  try {
    const parsed = JSON.parse(raw);
    if (
      parsed &&
      parsed.version === LAST_COLOR_FORMAT_VERSION &&
      typeof parsed.token === "string" &&
      isEditorColorToken(parsed.token)
    ) {
      return parsed.token;
    }
  } catch {
    /* Legacy v0 is a raw token rather than JSON. */
  }
  return null;
}

function versionedColor(token: string) {
  return JSON.stringify({ version: LAST_COLOR_FORMAT_VERSION, token });
}

// The color last used is the person's, as Notion's "최근 사용" is: another
// account on the same browser keeps its own.
function storedColor() {
  return personValue(LAST_COLOR_KEY, { previous: [LAST_COLOR_KEY] });
}

function preserveLegacyRecovery(legacyToken: string) {
  if (personValue(LAST_COLOR_RECOVERY_KEY, { previous: [LAST_COLOR_RECOVERY_KEY] }) === null) {
    storePersonValue(LAST_COLOR_RECOVERY_KEY, legacyToken);
  }
}

function isEditorColorToken(token: string) {
  return COLOR_TOKENS.has(token);
}

/**
 * The color last used, if one was: the one the block colors offer under
 * "최근 사용" and ⌘⇧H applies, as Notion's do (seen 2026-10-01).
 */
export function storedEditorColor() {
  const raw = storedColor();
  if (raw === null) return null;
  return currentFormatToken(raw) ?? (isEditorColorToken(raw) ? raw : null);
}

/** The colors used lately, newest first, as the selection menu's "최근 사용" offers them. */
export function recentEditorColors(): string[] {
  try {
    const parsed: unknown = JSON.parse(personValue(RECENT_COLORS_KEY, {}) ?? "[]");
    return Array.isArray(parsed)
      ? parsed.filter((token): token is string => typeof token === "string" && isRecentColorToken(token)).slice(0, RECENT_COLORS_LIMIT)
      : [];
  } catch {
    return [];
  }
}

function isRecentColorToken(token: string) {
  return token === "default_background" || isEditorColorToken(token);
}

/** Puts a color first among those used lately. */
export function rememberRecentEditorColor(token: string) {
  if (!isRecentColorToken(token)) return;
  const next = [token, ...recentEditorColors().filter((recent) => recent !== token)].slice(0, RECENT_COLORS_LIMIT);
  storePersonValue(RECENT_COLORS_KEY, JSON.stringify(next));
}

/**
 * A color just used, on words or on blocks: first in "최근 사용", and, unless
 * it takes a color off, the one ⌘⇧H applies. Notion's selection colors list
 * the block menu's colors too (seen 2026-11-15: purple text set from a
 * block's "색" came first in the words' "최근 사용").
 */
export function rememberUsedEditorColor(token: string) {
  rememberRecentEditorColor(token);
  if (!token.startsWith("default")) rememberEditorColor(token);
}

export function rememberEditorColor(token: string) {
  if (!isEditorColorToken(token)) return;
  const legacyToken = storedColor() ?? "";
  if (isEditorColorToken(legacyToken)) preserveLegacyRecovery(legacyToken);
  storePersonValue(LAST_COLOR_KEY, versionedColor(token));
}
