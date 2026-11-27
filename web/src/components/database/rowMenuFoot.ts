import type { GraphRow } from "@/contentGraphWire.generated";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import { type EditedFootActivity, type PageLastEditVia, pageEditedActivity } from "@/lib/pageEditedActivity";
import { rowMenuDocumentStats } from "@/lib/rowMenuDocumentStats";
import { openPageBodyStats } from "@/lib/pageBodyStats";

// A value or line's own text, where it has one.
function ownText(value: unknown): string {
  if (typeof value === "string") return value;
  if (value === null || typeof value !== "object") return "";
  const object = value as Record<string, unknown>;
  return ownText(object.plainText ?? object.text);
}

export type RowMenuFoot = {
  at: string; counts?: string; words?: string;
  /** Who edited last, named on a line of their own, and the activity panel beside the foot (an open row page's "…"). */
  name?: string; editorId?: string | null; activity?: EditedFootActivity;
  /** The connection the last edit came through (usePageLastEditVia). */
  via?: PageLastEditVia;
};

/**
 * The foot of an open row page's "…", as Notion's (seen 2026-10-11): its
 * body's words when it has any — "Count probe" with "ab cd" and "ef  gh"
 * shows "4개의 단어", the title not counted — then when it was last edited.
 * Since 2026-10-20 Notion names the last editor there with their face, as the
 * page's own "…" does, and shows the page's activity beside the foot.
 */
export function rowPageMenuFoot(snapshot: DatabaseRowPageSnapshot, words: (count: number) => string, currentUserId: string): RowMenuFoot {
  // Its body as open on the screen, as typed there (pageBodyStats.ts); its body as read, before it opens.
  const stats = openPageBodyStats(snapshot.row.id)
    ?? rowMenuDocumentStats(snapshot.body.map((block) => ownText(block.content)), snapshot.body.length);
  const activity = pageEditedActivity(snapshot.row, currentUserId);
  return { at: snapshot.row.updatedAt, ...(stats.words > 0 ? { words: words(stats.words) } : {}),
    name: activity.editor.name, editorId: activity.editor.id, activity };
}

/**
 * The foot of a row's menu in its view, as Notion's (seen 2026-10-11): when
 * the row page was last edited, then the words and characters of its title
 * alone — a row with "ab cd" and "ef  gh" in its body under "Count probe"
 * shows "2개의 단어, 11자" — counted as the library's menu counts them.
 * Since 2026-10-31 Notion names the last editor there too, with their face.
 */
export function rowMenuFoot(snapshot: DatabaseRowPageSnapshot, counts: (words: number, chars: number) => string, currentUserId: string): RowMenuFoot {
  const title = Object.values(snapshot.row.values).find((value) => value.type === "title");
  const stats = rowMenuDocumentStats([ownText(title?.value)], snapshot.body.length);
  const { editor } = pageEditedActivity(snapshot.row, currentUserId);
  return { at: snapshot.row.updatedAt, counts: counts(stats.words, stats.characters), name: editor.name, editorId: editor.id };
}

/**
 * The same foot from the row as its view holds it, while its page is still
 * being read: the menu shows its last edit at the press, its title's words
 * and letters with it (`counts`, left out for ⌘/'s, which has none).
 */
export function viewRowMenuFoot(row: GraphRow, counts: ((words: number, chars: number) => string) | null, currentUserId: string): RowMenuFoot {
  const title = Object.values(row.values).find((value) => value.type === "title");
  const stats = counts ? rowMenuDocumentStats([ownText(title?.value)], 0) : null;
  const { editor } = pageEditedActivity(row, currentUserId);
  return { at: row.updatedAt ?? "", ...(counts && stats ? { counts: counts(stats.words, stats.characters) } : {}), name: editor.name, editorId: editor.id };
}
