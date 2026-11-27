// How Notion's lists rank their things by what is typed in their search, as
// its web app works it out (read 2026-11-14: its time zone list, among
// others, ranks with one function over the "fuzzy" package's match). The
// query and a thing's text are read in compatibility-decomposed form (NFKD:
// 서 is ᄉ+ᅥ, so a lone ㅅ finds it), case aside. A thing answers only when
// the query's letters all come in its text in order; it then scores
// 1,000,000 when its text is the query; 100,000 when its text starts with
// it; where the query first stands after a space, 10,000 (1,000,000 when
// what stands there is an email address); 1,000 when it stands elsewhere;
// 950 when it stands there with accents set aside; a query of words, 900
// less the letters between them where they come in order, each but the
// first after a space (none else); else the best fuzzy score from any place
// the query's first letter stands. A thing under 1 does not answer; the
// rest stand best first, else in their order.
//
// Past Notion's search, a query a thing's text does not answer as typed
// answers as its unaccented forms do, never above 950 (user decision
// 2026-11-21): one typed without a letter that does not come apart
// ("Kobenhavn" for København, "Lodz" for Łódź), or with an accent the text
// lacks ("Zürich" for Zurich). Accents aside, such a letter is its plain
// letter, a curly apostrophe the straight one and an underscore a space, as
// IANA's zone names write one: "Sao Paulo" finds America/Sao_Paulo in a
// list that names it 상파울루.

const EXACT = 1e6, PREFIX = 1e5, WORD_PREFIX = 1e4, INSIDE = 1e3, INSIDE_UNACCENTED = 950, WORDS_IN_ORDER = 900;

/** The things that answer `query`, best first, else in their order, as Notion's list search keeps them. */
export function notionSearch<T>(query: string, things: readonly T[], textOf: (thing: T) => string): T[] {
  return things.map((thing, index) => ({ thing, index, score: notionSearchScore(query, textOf(thing)) }))
    .filter(({ score }) => score >= 1)
    .sort((a, b) => b.score - a.score || a.index - b.index)
    .map(({ thing }) => thing);
}

/** How well a thing's text answers what is typed, as Notion's list search scores it, else accents aside; under 1 when it does not. */
export function notionSearchScore(query: string, text: string): number {
  const typed = query.normalize("NFKD"), read = text.normalize("NFKD");
  const score = scoreOf(typed, read);
  return score >= 1 ? score : Math.min(INSIDE_UNACCENTED, scoreOf(unaccented(typed), unaccented(read)));
}

/** Notion's list search's score of a text for a query, both NFKD; 0 when the query's letters don't all come in it in order. */
function scoreOf(typed: string, read: string): number {
  const matched = fuzzyScore(typed, read);
  if (matched < 0) return 0;
  const needle = typed.toLowerCase(), line = read.toLowerCase();
  if (line === needle) return EXACT;
  const at = line.indexOf(needle);
  if (at === 0) return PREFIX;
  if (line[at - 1] === " ") return isEmail(line.substring(at, at + needle.length)) ? EXACT : WORD_PREFIX;
  if (at > -1) return INSIDE;
  if (unaccented(line).includes(unaccented(needle))) return INSIDE_UNACCENTED;
  if (needle.includes(" ")) {
    const words = needle.trim().replace(/[\\^$.*+?()[\]{}|]/g, "\\$&").replace(/\s+/g, "(.*?) ");
    const found = line.match(new RegExp(words));
    return found ? Math.max(0, WORDS_IN_ORDER - found.slice(1).reduce((between, gap) => between + gap.length, 0)) : 0;
  }
  let best = matched;
  for (let start = 1; start < line.length; start++) {
    if (line.charAt(start) !== needle.charAt(0)) continue;
    best = Math.max(best, fuzzyScore(needle, line.substring(start)));
  }
  return best;
}

/**
 * The fuzzy package's match score (mattyork/fuzzy 0.1.3, MIT): each of the
 * query's letters met in order adds its run, a run growing 1, 3, 7… while
 * they follow one another; -1 when not all are met, Infinity for the text
 * that is the query. Read by UTF-16 unit, as it reads.
 */
function fuzzyScore(pattern: string, text: string): number {
  const wanted = pattern.toLowerCase(), compared = text.toLowerCase();
  let at = 0, run = 0, total = 0;
  for (let index = 0; index < text.length; index++) {
    if (compared[index] === wanted[at]) {
      at++;
      run += 1 + run;
    } else run = 0;
    total += run;
  }
  if (at !== wanted.length) return -1;
  return compared === wanted ? Infinity : total;
}

// Letters that carry their mark inside, which no normal form takes apart, as
// their plain letters; curly apostrophes as the straight one; an underscore
// as a space.
const PLAIN_LETTERS: Readonly<Record<string, string>> = {
  "ø": "o", "æ": "ae", "œ": "oe", "ß": "ss", "ł": "l", "đ": "d", "ð": "d", "ħ": "h", "ı": "i", "ŀ": "l", "þ": "th", "ŧ": "t",
  "\u2019": "'", "\u2018": "'", "\u02bc": "'", "_": " ",
};

/** A text without its accents, lower case, as Notion's search compares them second: "são" is "sao" (and "ø" "o", "_" " "). */
function unaccented(text: string) {
  return text.normalize("NFD").replace(/[\u0300-\u036f]/g, "").toLowerCase()
    .replace(/[øæœßłđðħıŀþŧ\u2019\u2018\u02bc_]/gu, (letter) => PLAIN_LETTERS[letter] ?? letter);
}

// An email address as the email-validator package Notion's search uses reads one.
const EMAIL = /^[-!#$%&'*+/0-9=?A-Z^_a-z{|}~](\.?[-!#$%&'*+/0-9=?A-Z^_a-z`{|}~])*@[a-zA-Z0-9](-*\.?[a-zA-Z0-9])*\.[a-zA-Z](-?[a-zA-Z0-9])+$/;

function isEmail(text: string) {
  if (!text || text.length > 254 || !EMAIL.test(text)) return false;
  const [local, domain] = text.split("@");
  return local.length <= 64 && !domain.split(".").some((part) => part.length > 63);
}
