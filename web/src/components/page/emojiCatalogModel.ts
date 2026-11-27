// The emoji picker's catalog as Notion's picker shows it (seen 2026-09-30):
// "최근 사용한 항목", then the groups in Notion's order, each emoji named in
// the reader's language and found by its Korean or English name and words,
// hands and people in the chosen skin tone. A filter ranks what it finds as
// Notion's search does (its code, read 2026-11-02): by the first of the
// emoji's words it matches, a whole word over a word's start over a part,
// earlier words higher, and the person's often used emoji first.

export const EMOJI_GROUPS = ["people", "nature", "food", "activity", "travel", "objects", "symbols", "flags"] as const;
export type EmojiGroup = typeof EMOJI_GROUPS[number];
export type EmojiSectionKey = "recent" | "callout" | EmojiGroup;

const SKIN_TONE_COUNT = 5;
export const SKIN_TONE_STORAGE_KEY = "leafen:emoji-skin-tone";

export type EmojiEntry = {
  emoji: string;
  group: EmojiGroup;
  ko: string;
  en: string;
  /** Its five skin tones, lightest first, when it has them. */
  tones?: readonly string[];
  /** Its names and words, folded, in their ranking order. */
  keywords: readonly string[];
  /** The single words of its keywords, each once. */
  words: readonly string[];
};

export type EmojiCatalog = {
  entries: readonly EmojiEntry[];
  /** Every emoji the catalog draws, its tones included, to its entry. */
  byEmoji: ReadonlyMap<string, EmojiEntry>;
};

/** A section's emoji, and under a filter how well each matched it. */
export type EmojiSection = { key: EmojiSectionKey; items: readonly string[]; ranks?: readonly number[] };

type RawCatalog = { groups: string[]; emoji: Array<[string, number, string, string, string[], string[]?]> };

const fold = (value: string) => value.normalize("NFC").toLowerCase();

export function parseEmojiCatalog(raw: RawCatalog): EmojiCatalog {
  const entries: EmojiEntry[] = [];
  const byEmoji = new Map<string, EmojiEntry>();
  for (const [emoji, groupIndex, ko, en, words, tones] of raw.emoji) {
    const group = raw.groups[groupIndex] as EmojiGroup;
    if (!EMOJI_GROUPS.includes(group)) continue;
    const keywords = words.map(fold);
    const entry: EmojiEntry = {
      emoji, group, ko, en,
      ...(tones?.length === SKIN_TONE_COUNT ? { tones } : {}),
      keywords, words: [...new Set(keywords.flatMap((word) => word.split(" ")).filter(Boolean))],
    };
    entries.push(entry);
    byEmoji.set(emoji, entry);
    for (const toned of entry.tones ?? []) byEmoji.set(toned, entry);
  }
  return { entries, byEmoji };
}

/** The emoji as drawn in a skin tone: 0 is the default yellow. */
export function emojiInTone(entry: EmojiEntry, tone: number) {
  return tone > 0 ? entry.tones?.[tone - 1] ?? entry.emoji : entry.emoji;
}

export function emojiName(entry: EmojiEntry | undefined, language: string) {
  if (!entry) return "";
  return language.toLowerCase().startsWith("ko") ? entry.ko : entry.en;
}

/**
 * A typed filter or a word as Notion's emoji search reads it (its code, read
 * 2026-11-16): lowercase, each "_" a space, a "-" before a letter a space
 * ("woman-boy": "woman boy"), one ending a word after a letter or number
 * dropped, spaces run together and trimmed. The catalog's words are kept so
 * (generate-emoji-catalog.mjs), as Notion's lists are ("-_-": "- -").
 */
export function emojiSearchText(value: string) {
  return value.toLowerCase().replace(/_/g, " ").replace(/([a-z0-9])-$/g, "$1").replace(/-([a-z])/g, " $1")
    .replace(/\s+/g, " ").trim();
}

/**
 * How well an emoji's words match a filter, or undefined where they don't,
 * as Notion's emoji search ranks (its code, read 2026-11-02 and again
 * 2026-11-16): the filter holding the emoji itself, then an emoticon typed
 * after ":", a whole word, a word's start — each the higher the earlier the
 * word, so one late in the list can rank below even a part, as Notion's
 * ("indifferent" finds 🤷‍♂️🤷‍♀️ before 😑, whose word comes later) — then
 * several typed words each starting one of its words, those that are whole words counting
 * twice ("japan flag": 🇯🇵, then 🎏 and 🎌 in their order, as Notion's, seen
 * 2026-11-16 typed either way round), then a part of a word. The filter is
 * read as emojiSearchText reads it ("o_c" is "o c", a part of the clocks'
 * "twelve o clock" and of "tornado cloud"; "t_t" finds 😛 by "stuck out
 * tongue"), the typed words split as typed; from three letters on with no
 * space, a word is matched without its spaces. A filter of marks alone
 * ("_") finds every emoji.
 */
export function emojiMatchRank(emoji: string | undefined, keywords: readonly string[], words: readonly string[], typed: string) {
  const filter = emojiSearchText(typed);
  if (!filter) return 100;
  const at = emoji ? filter.indexOf(emoji) : -1;
  if (at !== -1) return 100 - at;
  const spaced = filter.length < 3 || filter.includes(" ") ? keywords : keywords.map((word) => word.replaceAll(" ", ""));
  const emoticon = spaced.indexOf(`:${filter}`);
  if (emoticon !== -1) return 100 - emoticon;
  const whole = spaced.indexOf(filter);
  if (whole !== -1) return (10 - whole) * 10;
  const start = spaced.findIndex((word) => word.startsWith(filter));
  if (start !== -1) return 10 - start;
  const parts = typed.split(" ").filter(Boolean);
  if (parts.length > 1 && parts.every((part) => words.some((word) => word.startsWith(part)))) {
    return parts.reduce((sum, part) => sum + (words.includes(part) ? 2 : 1), 0);
  }
  if (spaced.some((word) => word.includes(filter))) return 1;
  return undefined;
}

/**
 * The picker's sections: without a filter the recent emoji and every group;
 * with one, the groups holding what it finds, each in rank order (`bonus`
 * raising the person's often used emoji), in their order.
 */
export function emojiSections(catalog: EmojiCatalog, query: string, recent: readonly string[], tone: number,
  bonus: (emoji: string) => number = () => 0, suggested: readonly string[] = []): EmojiSection[] {
  const filter = fold(query.trim());
  const sections: EmojiSection[] = [];
  if (!filter && recent.length > 0) {
    sections.push({ key: "recent", items: recent.map((emoji) => {
      const entry = catalog.byEmoji.get(emoji);
      return entry ? emojiInTone(entry, tone) : emoji;
    }) });
  }
  // What the thing being given an icon suggests comes next, unfiltered: a
  // callout's "콜아웃" row (Notion's, seen 2026-11-06).
  const suggestedItems = filter ? [] : suggested.flatMap((emoji) => {
    const entry = catalog.byEmoji.get(emoji);
    return entry ? [emojiInTone(entry, tone)] : [];
  });
  if (suggestedItems.length > 0) sections.push({ key: "callout", items: suggestedItems });
  for (const group of EMOJI_GROUPS) {
    const found: Array<{ emoji: string; rank: number }> = [];
    for (const entry of catalog.entries) {
      if (entry.group !== group) continue;
      const rank = filter ? emojiMatchRank(entry.emoji, entry.keywords, entry.words, filter) : 0;
      if (rank !== undefined) found.push({ emoji: emojiInTone(entry, tone), rank: rank + (filter ? bonus(entry.emoji) : 0) });
    }
    if (filter) found.sort((left, right) => right.rank - left.rank);
    if (found.length > 0) {
      sections.push({ key: group, items: found.map((item) => item.emoji), ...(filter ? { ranks: found.map((item) => item.rank) } : {}) });
    }
  }
  return sections;
}

/** A stored skin tone, or the default (no tone) for anything else. */
export function skinToneFrom(stored: string | null) {
  const tone = Number(stored);
  return stored !== null && Number.isInteger(tone) && tone >= 0 && tone <= SKIN_TONE_COUNT ? tone : 0;
}

let loading: Promise<EmojiCatalog> | null = null;
let loaded: EmojiCatalog | null = null;

/** The catalog, read once when a picker first opens. */
export function loadEmojiCatalog() {
  loading ??= import("@/emojiCatalog.generated.json")
    .then((module) => (loaded = parseEmojiCatalog(module.default as unknown as RawCatalog)))
    .catch((error: unknown) => {
      loading = null;
      throw error;
    });
  return loading;
}

/** The catalog once read, so a picker opened again draws it in its first frame. */
export function loadedEmojiCatalog() {
  return loaded;
}
