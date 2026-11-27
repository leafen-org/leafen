import { SEARCH_PAIRED } from "@/lib/textMatch";

/**
 * How well a page answers a search by its title, both already folded, lower
 * first: its name, a name starting so, a name holding it, then all its words
 * apart, then some of them — more of them first: a word that fits nothing
 * only lowers a page, as Notion's (seen 2026-11-05: "s8 DB" finds "AB02 s8
 * DB" first, then what holds either word; 2026-11-12: "[[s213 b2 가나" keeps
 * "s213 b2" first). A page whose title holds none of the words is no answer,
 * whatever the pages above it are called, as Notion's move window, "@" and
 * "[[" find pages (seen 2026-11-13: "qv" and "zzk qv" find the pages named
 * so, not the pages under one of them); the quick search goes by the pages
 * above too (the server's). A title holds a word where one of its words
 * starts (titleHolds).
 */
export function pageSearchRank(title: string, query: string) {
  const tokens = query.split(/\s+/).filter(Boolean);
  if (title === query) return 0;
  if (title.startsWith(query)) return 1;
  if (titleHolds(title, query, true)) return 2;
  const count = tokens.length;
  const held = tokens.filter((token, index) => titleHolds(title, token, index === count - 1)).length;
  if (count > 1 && held === count) return 4;
  // Steps below one, so each rank keeps its place.
  if (held) return 5 + (count - held) / (count + 1);
  // A word of letters and digits is words of its own there: ab06 finds AB02,
  // s214 finds s213 (seen 2026-11-05, 2026-11-11), the last of them typed as
  // a start too: "zzqb kv7q" finds "… qxomel", "7q zzqb" only "… q" (seen
  // 2026-11-24 in a test workspace).
  const parts = wordParts(query);
  if (parts.length > tokens.length) {
    const words = wordParts(title);
    const last = parts.length - 1;
    if (parts.some((part, index) => words.some((word) => word === part || index === last && word.startsWith(part)))) return 6;
  }
  return Number.POSITIVE_INFINITY;
}

function wordParts(text: string) {
  return text.split(/[^\p{L}\p{M}\p{N}]+|(?<=\p{L})(?=\p{N})|(?<=\p{N})(?=\p{L})/u).filter(Boolean);
}

const WORD_CHAR = /[\p{L}\p{M}\p{N}]/u;
const NUMBER = /\p{N}/u;

/**
 * Whether a title holds a piece of the search where one of its words
 * starts — or a part of one, its letters and its digits each a part, as
 * "kv7q" is kv, 7 and q — and, but for the last piece typed, where that word
 * or part ends: whole. Never inside a word: "@qx" finds "… qx" and
 * "… qxomel", not "… boqxel" nor "… meqx"; "@zorvel qx dintak" only "… qx";
 * "@2561" finds "w2561", "@561" nothing; "@1234" no "51234", "@7123 zzzq"
 * no "71234 w", "@7123" does — as Notion's "@" menu, page link picker and
 * search find them (seen 2026-11-24 in test workspaces; the server's
 * mentionNameHoldsPiece). A piece of a script written without spaces is held
 * anywhere in a run of it ("@방학" finds "여름방학계획표"), as search finds it
 * (SEARCH_PAIRED; the 2026-10-14 search language decision), where Notion
 * reads Korean by its dictionary's words ("방학" yes, "름방" and a made-up
 * word's inside no).
 */
function titleHolds(title: string, piece: string, last: boolean) {
  const first = String.fromCodePoint(piece.codePointAt(0) ?? 32);
  const end = Array.from(piece).at(-1) ?? "";
  for (let at = title.indexOf(piece); at >= 0; at = title.indexOf(piece, at + 1)) {
    if (!SEARCH_PAIRED.test(first) && !wordEdge(charBefore(title, at), first)) continue;
    if (!last && !SEARCH_PAIRED.test(end) && !wordEdge(charFrom(title, at + piece.length), end)) continue;
    return true;
  }
  return false;
}

/** Whether a word or a part of one ends between `outside` and `inside` (either way round). */
function wordEdge(outside: string, inside: string) {
  const kind = charKind(inside);
  return !kind || charKind(outside) !== kind;
}

/** A character to a word's edge: none (a space, a mark of punctuation, nothing), a digit, a letter. */
function charKind(char: string) {
  if (!char || !WORD_CHAR.test(char)) return 0;
  return NUMBER.test(char) ? 1 : 2;
}

function charBefore(text: string, at: number) {
  return Array.from(text.slice(Math.max(0, at - 2), at)).at(-1) ?? "";
}

function charFrom(text: string, at: number) {
  const code = text.codePointAt(at);
  return code === undefined ? "" : String.fromCodePoint(code);
}

/** When a page was edited last, in milliseconds, 0 where it doesn't say. */
export function pageEditedTime(page: { updatedAt?: string } | undefined) {
  return page?.updatedAt ? Date.parse(page.updatedAt) || 0 : 0;
}

/**
 * The order of what a search finds where pages, databases and database rows
 * are listed together — the "@" and "[[" menus and an action's open list —
 * negative for the one first (Array.sort): the better answer
 * (pageSearchRank), all the words before some of them, and among those
 * answering as well the one edited last; the server's row search picks the
 * rows it gives these lists by the same order (mention_name_match.go).
 * Notion's lists mix rows among pages there, but order those answering as
 * well by a learned ranking (seen 2026-11-24 in two tabs: its search's
 * rankingSignals weigh the time since the last edit, the reader's visits,
 * how deep a page sits and whether it is empty, and pages just edited rose
 * to the top), which no rule reproduces; the last edit is the part every
 * reader sees alike, as the 2026-10-31 search order decision keeps an order
 * the same for everyone.
 */
export function searchAnswerOrder(left: { rank: number; edited: number }, right: { rank: number; edited: number }) {
  return left.rank - right.rank || right.edited - left.edited;
}
