// How a menu's search reads what is typed and the names it looks in: letter
// by letter, so the syllable being typed finds the ones it begins ("보"
// finds "복제" and "링크 복사"), and where it stands in a line of a thing's
// words ranks it (actionMenuScore) — the line being it, then a line starting
// with it, then one with a word starting with it, then one holding it, as
// Notion's action menus rank their results (its "/" menu, its page "⋯" menu
// and its block "⋮⋮" menu).

const HANGUL_FIRST = 0xac00;
const HANGUL_LAST = 0xd7a3;
const VOWEL_COUNT = 21;
const TAIL_COUNT = 28;
// The lone consonants an input method shows while a syllable is begun, in
// the order of the syllables' first letters.
const LONE_LEADS = "ㄱㄲㄴㄷㄸㄹㅁㅂㅃㅅㅆㅇㅈㅉㅊㅋㅌㅍㅎ";

/**
 * `text` as letters, lower case: each Hangul syllable as its first letter,
 * vowel and last letter, and a lone consonant as a first letter — so the
 * consonant an input method shows while a syllable is begun ("ㅈ") and a
 * syllable before its last letter ("제" of "젠") find the names they begin,
 * as Notion's "/" and "@" menus do (seen 2026-11-12: "/ㅈ" 제목1 and 지도
 * 보기, "@ㅇ" a name holding "음", "/젬" nothing).
 */
export function searchLetters(text: string): string {
  let out = "";
  for (const char of text.toLowerCase()) {
    const code = char.charCodeAt(0);
    if (code >= HANGUL_FIRST && code <= HANGUL_LAST) {
      const index = code - HANGUL_FIRST;
      const tail = index % TAIL_COUNT;
      out += String.fromCharCode(0x1100 + Math.floor(index / (VOWEL_COUNT * TAIL_COUNT)), 0x1161 + Math.floor((index % (VOWEL_COUNT * TAIL_COUNT)) / TAIL_COUNT));
      if (tail) out += String.fromCharCode(0x11a7 + tail);
      continue;
    }
    const lead = LONE_LEADS.indexOf(char);
    out += lead >= 0 ? String.fromCharCode(0x1100 + lead) : char;
  }
  return out;
}

// The mark that parts a line of words (actionMenuScore): a search's letters
// never run across it, and what follows it starts a word.
export const LINE_PART = "\n";

/**
 * A row's search words with the name it shows after them, parted from them
 * (LINE_PART), where they don't hold it: every menu row here is found by its
 * shown name, where Notion finds some only by words of their own — its page
 * menu's 연결 by "Connections", its block menu's 할 일 목록 by "할일".
 * Coming after them, it leaves what the words themselves find
 * where it was; parted, a search's letters never run on from the words into
 * it, so it adds only what the name finds by itself.
 */
export function withShownName(words: string, shown: string): string {
  const name = shown.trim();
  return !name || words.toLowerCase().includes(name.toLowerCase()) ? words : `${words}${LINE_PART}${name}`;
}

/**
 * The one line of a block menu row's words its search scores
 * (actionMenuScore), as Notion's "/" and "⋮⋮" menus read theirs: the row's
 * words, its group's words and its words again — so two words of the row,
 * or one of the row and one of its group, find it typed in either order —
 * with the name it shows at the very end, parted from them, where the line
 * doesn't hold it (withShownName). There it adds no letters between
 * a row's words and its group's: "red color" finds 빨간색 텍스트 before 빨간색
 * 배경, as Notion's both menus do (seen 2026-11-21), where the shown name
 * between them put the background first. Parted, a search's letters don't
 * run on from the row's words into it: on a Korean screen "/database -",
 * "/heading 제목1" and "/텍스트텍" find no 데이터베이스 - 전체 페이지, 제목1 or
 * text color, as Notion's "/" and "⋮⋮" (seen 2026-11-22).
 */
export function blockMenuLine(words: string, groupWords: readonly string[], shown = ""): string {
  return withShownName([words, ...groupWords, words].join(" "), shown);
}

/** Whether `word` begins with `query`, letter by letter (searchLetters). */
export function startsInLetters(word: string, query: string) {
  return searchLetters(word).startsWith(searchLetters(query));
}

// The fuzzy score of Notion's search: each letter of the query met in order
// adds its run, a run growing 1, 3, 7… as letters follow one another, all
// within one part of the line (LINE_PART).
function fuzzyRuns(needle: string, line: string) {
  let at = 0, run = 0, total = 0;
  for (const letter of line) {
    if (letter === LINE_PART && at < needle.length) {
      at = run = total = 0;
      continue;
    }
    run = letter === needle[at] ? (at++, 1 + 2 * run) : 0;
    total += run;
  }
  return at === needle.length ? total : -1;
}

// How Notion's action menus — the "/" menu, the page "⋯" menu and the block
// "⋮⋮" menu — score a line of a row's words against a search (read
// 2026-11-16 from Notion's web app, its action menu search, and checked in
// Chrome against its page "⋯" menu on "m", "l", "a", "d", "페", "co", "lk",
// "테마" and "page c", its block "⋮⋮" menu on "제", "red", "co", "t" and
// "제목 3", and its "/" menu's lines, 61,596 scores alike: the same rows, in
// the same order, under the same names). Both are taken apart as
// Unicode's NFKD does (a Hangul syllable into its letters, the lone consonant
// an input method shows into a syllable's first letter) and read in lower
// case; the search's letters must all stand in the line in order, and then:
// 1,000,000 the line is the search, 100,000 it starts with it, 10,000 where it
// first stands a word starts, 1,000 inside a word, 950 so once accents are
// dropped, 900 less what lies between for a search of words found in their
// order — else none for a search of words — and else the letters' fuzzy
// score (fuzzyRuns, its best from any of the line's places holding the
// search's first letter), kept from 25.
const ACTION_EXACT = 1_000_000;
export const ACTION_START = 100_000;
export const ACTION_WORD = 10_000;
export const ACTION_INSIDE = 1_000;
const ACTION_UNACCENTED = 950;
const ACTION_IN_ORDER = 900;
const ACTION_LEAST = 25;

const unaccented = (text: string) => text.normalize("NFD").replace(/[̀-ͯ]/g, "").toLowerCase();

/** How well a line of a row's words answers `query` in Notion's action menus: 0 not at all. */
export function actionMenuScore(query: string, line: string): number {
  const needle = query.normalize("NFKD").toLowerCase(), text = line.normalize("NFKD").toLowerCase();
  if (!needle) return 0;
  const fuzzy = fuzzyRuns(needle, text);
  if (fuzzy < 0) return 0;
  if (text === needle) return ACTION_EXACT;
  const at = text.indexOf(needle);
  if (at === 0) return ACTION_START;
  if (at > 0) return text[at - 1] === " " || text[at - 1] === LINE_PART ? ACTION_WORD : ACTION_INSIDE;
  if (unaccented(line).includes(unaccented(query))) return ACTION_UNACCENTED;
  if (needle.includes(" ")) {
    const words = needle.trim().replace(/[\\^$.*+?()[\]{}|]/g, "\\$&").replace(/\s+/g, "(.*?) ");
    const found = new RegExp(words, "u").exec(text);
    return found ? ACTION_IN_ORDER - found.slice(1).reduce((between, gap) => between + gap.length, 0) : 0;
  }
  let best = fuzzy;
  for (let start = 1; start < text.length; start++) if (text[start] === needle[0]) best = Math.max(best, fuzzyRuns(needle, text.slice(start)));
  return best >= ACTION_LEAST ? best : 0;
}

/**
 * How well a query finds an app's line in a menu by its words — its name and
 * its app's (actionMenuScore over that line, a leading "/" set aside): 0 not
 * at all. Notion's menus have no app lines; each menu finds its
 * apps' lines so, as the "/" menu finds an app's item (appLinesFound).
 */
export function appLineScore(query: string, words: string): number {
  const needle = query.trim().toLowerCase().replace(/^\//u, "");
  return needle ? actionMenuScore(needle, words) : 0;
}
