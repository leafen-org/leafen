// The titles of pages outside the workspace's page list — database rows and
// templates — whose mentions are on screen, as read once they come there and
// again as the page holding one's database changes elsewhere
// (editor/unlistedMentionedPages.ts): a listed page's mention reads its title
// from the list (peopleDirectory.ts), and one of a row or template reads it
// from here, so it names the page as it is called now, untitled in the reader's
// language, whatever words its writer kept (an API's "New page"), as
// Notion's does. One that reached the reader in marks, closed or missing when
// the server sent it (closedMentions.ts) — a Notion import resolving a
// mention to the row it made, by its format alone, after its words reached
// an open page — names the row from here without a reload, as Notion's does.
//
// Beside them, the mentions on screen whose target closed to the reader or
// went since their words came (useMentionReach.ts): a share, a group, a
// teamspace or a place in the workspace taken from them changes no page
// holding a mention of what it closes, so its words, the target's title as
// the server last sent them, stood until a reload; each such mention reads
// "사용 권한 없음" or "찾을 수 없음" from here until it opens again
// (closedMentions.ts mentionClosedSince).
let titles: ReadonlyMap<string, string> = new Map();
let reaches: ReadonlyMap<string, MentionReachNow> = new Map();
const listeners = new Set<() => void>();

/** How a page's or database's mention stands to its reader, when not open. */
export type MentionReachNow = "closed" | "missing";

function tell() {
  for (const listener of listeners) listener();
}

/** The marked mentions' pages as read now, by their IDs; their mentions follow them. */
export function setMarkedMentionTitles(next: ReadonlyMap<string, string>) {
  if (next.size === titles.size && [...next].every(([id, title]) => titles.get(id) === title)) return;
  titles = next;
  tell();
}

/** A marked mention's page's title as read, while it opens to the reader. */
export function markedMentionTitle(pageId: string) {
  return titles.get(pageId);
}

/** The key a mention's reach is kept by: its kind ("page", "database") and its target's ID. */
export function mentionReachKey(kind: string, id: string) {
  return `${kind}:${id}`;
}

/** The mentions on screen closed to their reader or gone since their words came, by mentionReachKey; their mentions follow them. */
export function setMarkedMentionReaches(next: ReadonlyMap<string, MentionReachNow>) {
  if (next.size === reaches.size && [...next].every(([key, reach]) => reaches.get(key) === reach)) return;
  reaches = next;
  tell();
}

/** How a mention's target stands to its reader as last read, when it closed to them or went; undefined while it opens. */
export function markedMentionReach(kind: string, id: string): MentionReachNow | undefined {
  return reaches.get(mentionReachKey(kind, id));
}

/** Hears of each change of the titles or reaches read (peopleDirectory.ts redraws the mentions). */
export function onMarkedMentionTitles(listener: () => void) {
  listeners.add(listener);
}
