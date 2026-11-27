// What the block menu's "작업을 검색하세요" finds, as Notion's (seen
// 2026-10-01): the types a block turns into, under "전환"; the text and
// background colors, under their groups ("빨강" finds "빨간색 텍스트" and
// "빨간색 배경"); the menu's own actions ("복제"); and the apps that open a
// file, under "이 앱으로 열기", each by its name and its app's as a menu's
// app lines are found (appLineScore), all by that row's name. A term matches a name
// in the reader's language or English, or a block type's search words
// ("체크" 할 일 목록, "세로" the columns), and words typed apart find it
// together or each in a word ("제목 3" finds "제목3" and "토글 제목3", seen
// 2026-11-03), letter by letter: the consonant an input method shows while a
// syllable is begun finds what its syllables find ("ㅈ" 제목1 and 주황색
// 텍스트; seen 2026-11-12). Seen 2026-11-13: a term finds a name with it
// inside a word, after a letter ("제" 복제 and 삭제, "red" Numbered list,
// "lic" Duplicate — not "목1" in 제목1 nor "개의" in 2개의 열), and a group's
// name finds all its things ("전" or "urn" every "전환" item, "상" the text
// colors, "배경색" the background colors), after those found by their own.
// In Korean and English, as Notion's search (seen 2026-11-13), a type a block
// turns into and a color are found by their search words — Korean then
// English (blockSearchWords, the same words the "/" menu reads) — and an
// action by its name then its English one, each one
// line followed by its group's name: that line is scored as Notion's action
// menus score theirs, the "/" and page "⋯" menus' too (actionMenuScore; its block
// menu checked 2026-11-16 on "제", "red", "co", "t" and "제목 3") — "h" finds
// 할 일 목록 by "Checkbox", 수학 공식 블록 by "Math", 3개의 열 by "three",
// 기본 텍스트 and 기본 배경 by "White"; "c" the text colors by their group's
// "color" before 기본 텍스트 by "Black"; "t" 제목1 by its group's "Turn
// into", 글머리 기호 목록 after it, by the "t" inside "Bulleted".

import { ACTION_INSIDE, ACTION_START, ACTION_WORD, actionMenuScore, appLineScore, blockMenuLine, searchLetters } from "@/lib/menuSearch";
import { blockDefSearchKeywords } from "./blockDefinitionSearch";
import { BLOCK_DEFS } from "./blockDefinitions";
import { getDef } from "./blockDefinitionOrder";
import { colorWords, searchWordsLanguage, searchWordsLine, turnWords } from "./blockSearchWords";
import type { BlockTurnChoice } from "./blockTurnInto";
import type { BlockType } from "@/lib/types";

const ENGLISH_COLOR_NAMES: Record<string, string> = {
  default: "Default", gray: "Gray", brown: "Brown", orange: "Orange", yellow: "Yellow",
  green: "Green", blue: "Blue", purple: "Purple", pink: "Pink", red: "Red",
};
// A thing its group's name finds lists after every thing found by its own
// (by its names: on actionMenuScore's scale, under one holding the query).
const GROUP_FOUND = ACTION_INSIDE / 2;

/**
 * A thing's names — the shown one first — and its search words; `phrase`,
 * the words Notion's search finds it by, in place of the names, with
 * `shown`, the name it shows (actionMenuScore over blockMenuLine); `appLine`,
 * an app's line — its name and its app's — found as a menu's app lines are
 * (appLineScore), in place of both.
 */
export type BlockMenuSearchNames = { names: readonly (string | undefined)[]; keywords?: readonly string[]; phrase?: string; shown?: string; appLine?: string };

const lettersOf = (text: string) => searchLetters(text).split(/\s+/u).filter(Boolean);

// A word holds a term at its start or, where `inside` and the term has no
// digit, inside it after a letter.
function holds(word: string, term: string, inside: boolean) {
  if (word.startsWith(term)) return true;
  if (!inside || /\p{N}/u.test(term)) return false;
  for (let at = word.indexOf(term, 1); at > 0; at = word.indexOf(term, at + 1)) if (/\p{L}/u.test(word[at - 1]!)) return true;
  return false;
}

/**
 * How well a query finds a thing, on actionMenuScore's scale: an app's line
 * as a menu's app lines are found (appLineScore); one with a
 * `phrase` as Notion's action menus find it in that line, its group's words
 * and the line again, its shown name at the end (actionMenuScore over
 * blockMenuLine); else by its names — a name starts
 * with it (ACTION_START), words of the names start with its words
 * (ACTION_WORD), the names hold them inside a word or the search words start
 * with them (ACTION_INSIDE) — 0 not at all.
 */
export function blockMenuSearchScore(query: string, { names, keywords = [], phrase, shown, appLine }: BlockMenuSearchNames,
  groupWords: readonly string[] = []): number {
  if (appLine !== undefined) return appLineScore(query, appLine);
  if (phrase) return actionMenuScore(query.trim(), blockMenuLine(phrase, groupWords, shown));
  const needle = searchLetters(query.trim());
  if (!needle) return 0;
  const terms = needle.split(/\s+/u), joined = terms.join("");
  const texts = names.flatMap((name) => name ? [searchLetters(name)] : []);
  if (texts.some((text) => text.startsWith(needle) || text.startsWith(joined))) return ACTION_START;
  const words = texts.flatMap((text) => text.split(/\s+/u));
  const found = (list: readonly string[], inside: boolean) => list.some((word) => holds(word, joined, inside))
    || terms.every((term) => list.some((word) => holds(word, term, inside)));
  if (found(words, false)) return ACTION_WORD;
  return found(words, true) || found(keywords.flatMap(lettersOf), false) ? ACTION_INSIDE : 0;
}

/**
 * What a query finds of a group, best first and in the menu's order among
 * equals, and its best score; where it finds the group's name, every thing
 * in the group (GROUP_FOUND).
 */
export function blockMenuFound<T>(query: string, items: readonly T[], namesOf: (item: T) => BlockMenuSearchNames,
  groupNames: readonly string[] = []) {
  const group = groupNames.length > 0 && blockMenuSearchScore(query, { names: groupNames }) > 0;
  const scored = items.map((item, index) => ({ item, index, score: blockMenuSearchScore(query, namesOf(item), groupNames) || (group ? GROUP_FOUND : 0) }))
    .filter((found) => found.score > 0)
    .sort((a, b) => b.score - a.score || a.index - b.index);
  return { items: scored.map((found) => found.item), best: scored[0]?.score ?? 0 };
}

/** The groups a query found, the one with the best match first, in the menu's order among equals. */
export function blockMenuGroupsBestFirst<T extends { best: number }>(groups: readonly T[]): T[] {
  return groups.map((group, index) => ({ group, index })).filter(({ group }) => group.best > 0)
    .sort((a, b) => b.group.best - a.group.best || a.index - b.index).map(({ group }) => group);
}

/**
 * A "전환" choice's English name and search words, as its block type's ("2
 * columns" its count's); in Korean and English, its search words (turnWords:
 * Korean then English in Korean, "페이지 위치:" and "Page in") in place of
 * them, and its shown name ("할 일" finds 할 일 목록, "제목1" 제목1).
 */
export function blockTurnChoiceSearchNames(choice: BlockTurnChoice, shown: string): BlockMenuSearchNames {
  const def = choice.type !== "column_list" ? getDef(choice.type as BlockType)
    : choice.columns ? BLOCK_DEFS.find((candidate) => candidate.columnCount === choice.columns) : undefined;
  const key = choice.type === "column_list" ? `column_list:${choice.columns ?? 0}` : choice.type;
  const language = searchWordsLanguage(), words = turnWords(key, choice.placeIn);
  if (language && words) return { names: [], phrase: searchWordsLine(words, language, shown), shown };
  return { names: [shown, def?.label], keywords: def ? blockDefSearchKeywords(def) : [] };
}

/**
 * A color's names, the shown one first; in Korean and English, its search
 * words (colorWords) and its shown name.
 */
export function blockMenuColorSearchNames(color: string, names: readonly string[]): BlockMenuSearchNames {
  const language = searchWordsLanguage(), words = colorWords(color);
  if (!language || !words) return { names };
  return { names, phrase: searchWordsLine(words, language, names[0] ?? ""), shown: names[0] };
}

/**
 * An action's name and English one; in Korean and English, one line of them,
 * as Notion's search reads its actions. `shown`, what the row shows where
 * that isn't its name ("최근 사용"'s color, found by "하이라이트"), at the
 * line's end (blockMenuLine).
 */
export function blockMenuActionSearchNames(title: string, english: string, shown = title): BlockMenuSearchNames {
  const names = shown === title ? [title, english] : [shown, title, english];
  if (!searchWordsLanguage()) return { names };
  return { names, phrase: title.toLowerCase() === english.toLowerCase() ? title : `${title} ${english}`, shown };
}

/** A color token's English name, as "Red text" or "Red background" reads it. */
export function blockMenuEnglishColorName(color: string) {
  const background = color.endsWith("_background");
  const name = ENGLISH_COLOR_NAMES[background ? color.slice(0, -"_background".length) : color] ?? color;
  return background ? `${name} background` : `${name} text`;
}
