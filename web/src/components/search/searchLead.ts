import type { CanonicalTextRange } from "@/lib/textMatch";
import type { SearchHit } from "./searchModel";

/**
 * The search window's lead, as Notion's (seen 2026-11-24 in three tabs by
 * w2486 and in two by w2496, its request leaving them out of what the server
 * is asked): up to three pages the person opened lately whose titles answer
 * the words typed stand over the results, with no line of their bodies,
 * their titles marking the words that answered, their preview the page
 * itself (SearchHighlightPanel). Notion's window finds them among the pages
 * its browser keeps; here they are among the person's visits the server
 * keeps (page_visits.go), the same on every device, a page in the trash or
 * under one never among them (user decision 2026-11-24, B).
 *
 * A title answers as Notion's window reads it: its words are the runs
 * between spaces, in lower case, and each run's parts of Latin letters and
 * digits longer than three ("Straße" is "straße" and "stra"); its initials
 * ("w2496 jx plumbert": "wjp") too, read from their third letter from the end
 * on, for a query of one word. A word typed answers a title's word that is
 * it, or that it starts when it is three letters long or more ("613" starts
 * "61234", "lo" starts nothing) — any length when it is the only word typed
 * ("l" starts "longer") — or one it misses by a letter in five, it of five
 * letters or more ("plumbart" answers "plumbert", "stras" "stra"). Every word
 * typed must answer; if no page answers them all, those answering most of
 * the words of three letters or more lead ("lo qa" the pages of "qa",
 * "grindle li" "grindlewick", "grindle line" none). Common English words
 * ("the", "of") count for nothing. A query that reads like an identifier — no
 * space, five characters or more, a digit among them ("w2474") — leads only
 * a title that is it.
 */
export const SEARCH_LEAD_LIMIT = 3;

const SKIPPED = new Set(["a", "an", "and", "are", "as", "at", "be", "but", "by", "for", "if", "in", "is", "it", "of", "on", "or",
  "that", "the", "to", "was", "will", "with"]);
/** Whether a word typed is a common English one the lead passes over ("the", "of"). */
export function searchCommonWord(word: string) {
  return SKIPPED.has(word);
}

// What divides the words counted for a query's sort (one word or several): spaces and these marks.
const COUNTED_APART = /[-./\\()"'‘’“”，。、·～；,;<>~!@#$%^&*|+=[\]{}`?:\s]/u;
const LATIN_PART = /[A-Za-z0-9_]+/g;
const IDENTIFIER = /^[A-Za-z0-9][A-Za-z0-9._:/-]*$/;
const TICKET = /^[A-Z][A-Z0-9-]{1,9}-[0-9]+$/;

type Kind = 0 | 1 | 2 | 3;
const EXACT: Kind = 3, PREFIX: Kind = 2, NEAR: Kind = 1, NONE: Kind = 0;

/** A word of a title as the lead reads it, and where it stands. */
type TitleTerm = { text: string; start: number; end: number };

function titleTerms(title: string): TitleTerm[] {
  const terms: TitleTerm[] = [];
  for (const run of title.matchAll(/\S+/g)) {
    if (SKIPPED.has(run[0])) continue;
    const text = run[0].toLowerCase();
    terms.push({ text, start: run.index, end: run.index + run[0].length });
    for (const part of run[0].matchAll(LATIN_PART)) {
      const lower = part[0].toLowerCase();
      if (part[0].length > 3 && lower !== text) terms.push({ text: lower, start: run.index + part.index, end: run.index + part.index + part[0].length });
    }
  }
  return terms;
}

/** A title's initials, the first letter of each of its words, read from each letter on while three are left. */
function initialTerms(title: string) {
  const initials = title.split(" ").map((word) => Array.from(word).slice(0, 1)).join("").toLowerCase();
  const terms: string[] = [];
  for (let at = 0; at + 3 <= initials.length; at++) terms.push(initials.slice(at));
  return terms;
}

function editDistance(left: string, right: string, most: number) {
  if (Math.abs(left.length - right.length) > most) return most + 1;
  let row = Array.from({ length: right.length + 1 }, (_, index) => index);
  for (let i = 1; i <= left.length; i++) {
    const next = [i];
    let least = i;
    for (let j = 1; j <= right.length; j++) {
      next[j] = Math.min(row[j] + 1, next[j - 1] + 1, row[j - 1] + (left[i - 1] === right[j - 1] ? 0 : 1));
      least = Math.min(least, next[j]);
    }
    if (least > most) return most + 1;
    row = next;
  }
  return row[right.length];
}

/** How a word typed answers a title's word: whole, its start, a letter in five off, or not. */
function answer(word: string, term: string, starts: boolean): Kind {
  if (term === word) return EXACT;
  if (starts && term.startsWith(word)) return PREFIX;
  if (word.length >= 5) {
    const most = Math.min(6, Math.round(word.length * 0.2));
    if (editDistance(word, term, most) <= most) return NEAR;
  }
  return NONE;
}

/** A query as the lead reads it: its words, lower case, the common ones left out; whether it is one word; how many answer at least. */
type LeadQuery = { words: string[]; single: boolean; least: number; identifier?: string };

function readLeadQuery(query: string): LeadQuery {
  const trimmed = query.trim();
  const words = trimmed.split(/\s/).filter((word) => word && !SKIPPED.has(word)).map((word) => word.toLowerCase());
  const counted = trimmed.split(COUNTED_APART).filter(Boolean);
  const identifier = !/\s/.test(trimmed) && trimmed.length >= 5 && trimmed.length <= 64 && IDENTIFIER.test(trimmed)
    && /[0-9]/.test(trimmed) && !TICKET.test(trimmed.toUpperCase()) ? trimmed.normalize().toLowerCase() : undefined;
  return { words, single: counted.length === 1, least: Math.round(0.6 * counted.filter((word) => word.length >= 3).length + 0.5), identifier };
}

/**
 * How a visited title answers the words typed: how many of them, how many of
 * those whole, and whether it is the query itself; none when it doesn't lead.
 * With `every`, it must answer every word, by its words alone; else as many
 * as the query asks (readLeadQuery), its initials counted too — as must one
 * word.
 */
function leadAnswer(title: string, query: LeadQuery, typed: string, every: boolean) {
  const terms = titleTerms(title).map(({ text }) => text);
  const initials = query.single || !every ? initialTerms(title) : [];
  let answered = 0, whole = 0;
  for (const word of query.words) {
    const starts = query.single || word.length >= 3;
    const best = Math.max(NONE, ...[...terms, ...initials].map((term) => answer(word, term, starts)));
    if (best !== NONE) answered++;
    if (best === EXACT) whole++;
  }
  if (every ? answered < query.words.length : answered < query.least) return null;
  return { answered, whole, itself: title.normalize().toLowerCase() === typed.trim().normalize().toLowerCase() };
}

/**
 * The pages that lead a search, of the person's visited ones in the order of
 * their visits, newest first, each by its title: those whose titles answer
 * every word typed or, with none, most of them (leadAnswer); those answering
 * more words first, then more of them whole, then a title that is the query,
 * then the newest visit — the order Notion's window gave its own pages over
 * two tabs (seen 2026-11-24: "plumb" leading the three pages opened last that
 * a word starts, "plumbert zz" the two holding "plumbert" before the newer
 * "plumbers"), where Notion also weighs how often a page was opened, which
 * Leafen doesn't keep. Three at most (SEARCH_LEAD_LIMIT). The search window's
 * lead (searchLeadHits) and the "@" and "[[" menus' (mentionLead.ts), which
 * Notion's lead by the same rule (seen 2026-11-24 in two tabs: "@plive" and
 * "@plivekt" the three pages opened last, "@plivect pa dornux" the one
 * holding two of the words, "@plivect dornux" none of those holding one).
 */
export function searchLeadOf<T>(visited: readonly T[], titleOf: (item: T) => string, query: string): T[] {
  const read = readLeadQuery(query);
  if (!read.words.length) return [];
  const pick = (every: boolean) => visited.flatMap((hit, order) => {
    const title = titleOf(hit).trim();
    if (read.identifier) return title.normalize().toLowerCase() === read.identifier ? [{ hit, order, answered: 1, whole: 1, itself: true }] : [];
    const found = leadAnswer(title, read, query, every);
    return found ? [{ hit, order, ...found }] : [];
  }).sort((left, right) => right.answered - left.answered || right.whole - left.whole
    || Number(right.itself) - Number(left.itself) || left.order - right.order);
  const all = pick(true);
  const found = all.length || read.single || read.identifier ? all : pick(false);
  return found.slice(0, SEARCH_LEAD_LIMIT).map(({ hit }) => hit);
}

/** The search window's lead (searchLeadOf): results found by their titles, marked as leading. */
export function searchLeadHits(visited: readonly SearchHit[], query: string): SearchHit[] {
  return searchLeadOf(visited, (hit) => hit.page.title, query)
    .map((hit) => ({ ...hit, match: "title" as const, preview: "", lead: true as const }));
}

/**
 * Where a leading result's title marks the words typed (searchLeadHits): each
 * word of the title a word typed answers best — whole over its start over a
 * letter off — whole, or the part of Latin letters and digits it answers
 * ("stra" and "stras" mark "[Stra]ße", "longe" "[longer]", "6123 qa"
 * "[qa] [61234]").
 */
export function searchLeadRanges(text: string, query: string): CanonicalTextRange[] {
  const read = readLeadQuery(query);
  if (read.identifier) return text.trim().normalize().toLowerCase() === read.identifier ? [{ start: 0, end: text.length }] : [];
  const terms = titleTerms(text);
  const ranges: CanonicalTextRange[] = [];
  for (const word of read.words) {
    const starts = read.single || word.length >= 3;
    const answers = terms.map((term) => ({ term, kind: answer(word, term.text, starts) }));
    const best = Math.max(NONE, ...answers.map(({ kind }) => kind));
    if (best === NONE) continue;
    for (const { term, kind } of answers) if (kind === best) ranges.push({ start: term.start, end: term.end });
  }
  return ranges.sort((left, right) => left.start - right.start || right.end - left.end);
}
