import { i18next } from "@/i18n";
import { actionMenuScore, blockMenuLine } from "@/lib/menuSearch";
import { blockDefLabel } from "./blockDefinitionLabels";
import { BLOCK_DEFS, blockDefKey, type BlockDef } from "./blockDefinitions";
import { slashOrder } from "./blockDefinitionOrder";
import { colorWords, searchWordsLanguage, searchWordsLine, slashGroupWords, slashWords as slashRowWords, turnWords, type SearchWords } from "./blockSearchWords";

// The menu's groups in its order: the blocks', an app's, then what acts on
// the line (blockDefinitions' SlashGroup).
const GROUP_ORDER: readonly BlockDef["group"][] = [
  "Basic", "Media", "Database", "Advanced", "Inline", "Embed", "Apps", "Import", "TurnInto", "Actions", "TextColor", "BackgroundColor",
];

export type BlockSearchBounds = {
  maxQueryCharacters: number;
  maxQueryTokens: number;
};

function validateBlockSearchBounds(bounds: BlockSearchBounds) {
  if (
    !bounds
    || !Number.isSafeInteger(bounds.maxQueryCharacters)
    || bounds.maxQueryCharacters < 0
    || !Number.isSafeInteger(bounds.maxQueryTokens)
    || bounds.maxQueryTokens < 0
  ) {
    throw new RangeError("Block search requires caller-owned nonnegative safe-integer bounds.");
  }
}

function countQueryTokens(query: string) {
  let count = 0;
  let insideToken = false;
  for (let index = 0; index < query.length; index += 1) {
    const whitespace = /\s/u.test(query[index]);
    if (!whitespace && !insideToken) count += 1;
    insideToken = !whitespace;
  }
  return count;
}

/**
 * Slash-menu results for a query, with what installed apps add, among the
 * items `keep` lets the menu offer. Notion's "/" search is its action menus'
 * (read 2026-11-16 from Notion's web app, and checked against its "/" menu in
 * Chrome): the query, its spaces at the ends trimmed, in lower case and less
 * one "/" it starts with, scores one line of each item's words — its search
 * words, its group's words, its search words again (slashSearchLine) — as
 * actionMenuScore does, and the items it finds list best first, in the
 * menu's order among equals. So a block and its "· 전환" row mix by their
 * words ("/제": 제목1·2·3·토글 제목1, their four "· 전환" rows, then 제목4,
 * whose words have "제목" as a later word); a group's name finds all its
 * items ("/전환"), and a query found nowhere in order finds them weakly
 * ("/inline" ends with 타임라인 보기 and 꺾은선 차트). Untyped, or typed as
 * spaces or a "/" alone, it lists the menu in its order. It lists all it
 * finds; the menu draws them a page at a time (slashMenuPages).
 */
export function matchBlocks(query: string, bounds: BlockSearchBounds, extra: readonly BlockDef[] = [],
  keep: (def: BlockDef) => boolean = () => true): BlockDef[] {
  validateBlockSearchBounds(bounds);
  if (query.length > bounds.maxQueryCharacters) {
    throw new RangeError("Block search query exceeds its caller-owned character bound.");
  }
  const typed = query.trim().toLowerCase();
  const q = typed.startsWith("/") ? typed.slice(1) : typed;
  const defs = (extra.length ? [...BLOCK_DEFS, ...extra] : BLOCK_DEFS).filter((def) => !def.slashHidden && keep(def))
    .map((def, index) => ({ def, index }))
    .sort((a, b) => GROUP_ORDER.indexOf(a.def.group) - GROUP_ORDER.indexOf(b.def.group) || slashOrder(a.def, a.index) - slashOrder(b.def, b.index))
    .map((item) => item.def);
  // Spaces alone list the menu as untyped (Notion "/ ", seen 2026-11-17: 텍스트
  // to 빨간색 배경, 177 rows), drawn under "필터링된 결과" by the menu.
  if (!q) return defs.filter((def) => !def.hiddenWhenEmpty);
  if (countQueryTokens(q) > bounds.maxQueryTokens) {
    throw new RangeError("Block search query exceeds its caller-owned token bound.");
  }
  const language = searchWordsLanguage();
  return defs.map((def, index) => ({ def, index, score: actionMenuScore(q, slashSearchLine(def, language)) }))
    .filter((item) => item.score > 0)
    .sort((a, b) => b.score - a.score || a.index - b.index)
    .map((item) => item.def);
}

/**
 * The line of an item's words the "/" search scores (blockMenuLine): its
 * words, its group's and its words again, as Notion's. Its words are the
 * row's search words (blockSearchWords), Korean then English on a Korean
 * screen, English on an English one and the shown name then English on
 * another, with the shown name at the line's end where it doesn't hold it
 * ("/할 일" finds 할 일 목록, "/토글" 토글 제목4 · 전환); an item
 * without its own words — an app's — is found by its names and the words its
 * "/" item declares (docs/apps.md "Menu lines": "/parrot" finds the item that
 * lists "parrot").
 */
function slashSearchLine(def: BlockDef, language: "ko" | "en" | null) {
  const shown = blockDefLabel(def);
  const own = slashWords(def);
  const words = own ? searchWordsLine(own, language, shown) : def.app ? [def.app.title, def.app.name, ...(def.app.keywords ?? [])].join(" ") : `${shown} ${def.label}`;
  const groupWords = slashGroupWords(def.group);
  const group = groupWords ? searchWordsLine(groupWords, language, i18next.t(`slashMenu:groups.${def.group}`))
    : [i18next.t(`slashMenu:groups.${def.group}`), i18next.t(`slashMenu:groups.${def.group}`, { lng: "en" })].join(" ");
  return blockMenuLine(words, [group], shown);
}

function slashWords(def: BlockDef): SearchWords | undefined {
  if (def.app) return undefined;
  if (def.action === "turn_into") return turnWords(def.type === "column_list" ? `column_list:${def.columnCount ?? 0}` : def.type, def.pageIn);
  if (def.action === "set_color") return colorWords(def.colorToken === "default" && def.group === "BackgroundColor" ? "default_background" : def.colorToken ?? "");
  return slashRowWords(blockDefKey(def));
}

/**
 * Every searchable keyword for a def, English and Korean: what the block
 * menu's search reads for a block type on a screen in another language
 * (blockMenuSearch), where the search words (blockSearchWords) stand for
 * Korean and English.
 */
export function blockDefSearchKeywords(def: BlockDef): string[] {
  return def.koKeywords ? [...def.keywords, ...def.koKeywords] : def.keywords;
}
