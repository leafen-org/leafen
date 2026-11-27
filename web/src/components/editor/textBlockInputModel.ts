import { numberedListMarkerValues } from "@/lib/numberedList";
import { putInByPaste, putInOnePiece, workingByKeyboard } from "./keyboardMode";
import { isSlashCharacter, triggerFor } from "./triggerCharacters";

/**
 * Whether a menu's trigger typed at "start" opens it by what stands before,
 * as Notion's (its editor code read 2026-11-18, and seen the same day in
 * Chrome at 1440, no script before the key): every trigger opens at the
 * line's start. "/" opens after a space (a no-break or ideographic one too,
 * not a line break); never after a digit ("123/") or inside an address
 * ("https:/", "http://"); after anything else — a letter of any script, a
 * mark, a line break (⇧Enter), inline code or a link's words — only when
 * the mouse has moved since the last key (keyboardMode), so "and/or" typed
 * straight on stays words and a "/" typed after a click at a word's end
 * opens the menu. "@" and "+" open after a space, a line break or a bracket
 * ("(@", ")@", "[@"), ":" after a space, a line break, "(", "[" or "{",
 * whatever the mouse did; "[[" opens wherever it is typed.
 */
export function triggerOpensAfter(trigger: string, text: string, start: number) {
  if (start === 0 || trigger === "[[") return true;
  const before = text[start - 1];
  if (trigger === ":") return /[\s([{]/u.test(before);
  if (trigger !== "/") return /[\s()[\]]/u.test(before.normalize("NFKD"));
  if (/\p{Z}/u.test(before)) return true;
  if (/[0-9]/u.test(before) || /https?:\/?$/u.test(text.slice(Math.max(0, start - 10), start))) return false;
  return !workingByKeyboard();
}

/**
 * Whether "+" or ":" opens its menu at the letter typed after it: "+" at a
 * letter of any script, as Notion's page link menu stays only for one (its
 * code, read 2026-11-18, and "x +1" seen to open none the same day); ":" at
 * anything but a space or another ":" (":1" opens the emoji menu there; ":"
 * alone or before a space opens none — "10:30", ":)" after a word neither,
 * seen 2026-11-13).
 */
function opensAtLetter(trigger: string, letter: string | undefined) {
  if (letter === undefined) return false;
  return trigger === "+" ? /\p{L}/u.test(letter) : !/\s/u.test(letter) && letter !== ":";
}

/**
 * The trigger a piece of typed text starting at "at" and running to "end"
 * opens: "/" or "@" as its first letter, "+" or ":" with the letter after it
 * in the piece or — the piece starting with that letter — typed before it,
 * and "[[" by its second "[".
 */
function pieceTrigger(text: string, at: number, end: number): { char: string; start: number } | null {
  const first = triggerFor(text[at]);
  if (first === "[") return text[at - 1] === "[" ? { char: "[[", start: at - 1 } : null;
  if (first === "/" || first === "@") return { char: first, start: at };
  if (first === "+" || first === ":") return at + 1 < end && opensAtLetter(first, text[at + 1]) ? { char: first, start: at } : null;
  const before = triggerFor(text[at - 1]);
  return (before === "+" || before === ":") && opensAtLetter(before, text[at]) ? { char: before, start: at - 1 } : null;
}

/**
 * The trigger ("/", "@", "+", ":" or "[[") typed text just put in starts a
 * menu with, where triggerOpensAfter lets it, or null. Text that came in one
 * piece — an input method's commit, a correction, dictation — opens a menu
 * only by its first letter, as Notion's openers read the letter where the
 * insertion begins (its editor code, read 2026-11-18, and seen the same day
 * in Chrome at 1440 by one insertText each: "/da", "@ab", "+ab" and ":sm"
 * open their menus with the rest as the query; "(+a", "x @ab", "x /da",
 * "(:sm" and "[[ab" open none). Keys pressed one by one that land together
 * (the page busy, as straight after Enter makes a block) are each typed in
 * turn: the first that opens a menu starts it, and the rest is its query.
 * Each letter is read by triggerFor, so a full-width "／", "＠" or "＋" opens
 * as its trigger does. `takes`, a field's triggers (fieldMenuTrigger).
 */
export function typedTrigger(text: string, caret: number, inserted: string, onePiece: boolean, takes?: ReadonlySet<string>) {
  const from = caret - inserted.length;
  if (from < 0 || text.slice(from, caret) !== inserted) return null;
  for (let at = from; at < caret; at += 1) {
    const found = pieceTrigger(text, at, onePiece ? caret : at + 1);
    if (found && (!takes || takes.has(found.char)) && triggerOpensAfter(found.char, text, found.start)) return found;
    if (onePiece) break;
  }
  return null;
}

/**
 * The "/" menu's place and query as a line changes, or null once the menu
 * ends. As Notion's (seen 2026-10-01), it begins as "/" is typed where
 * triggerOpensAfter lets it and filters by all that follows, single spaces too;
 * two spaces in a row, a line break or the caret going back over the "/" end
 * it, and an ended menu does not come back as the line is edited. After
 * words, a space straight after the "/" ends it too ("abc / "), where at a
 * line's start "/ " lists (seen 2026-11-01).
 */
export function slashMenuTrigger(
  open: { start: number } | null,
  text: string,
  caret: number,
  typedSlash: boolean,
): { start: number; end: number; query: string } | null {
  if (open) {
    const slash = isSlashCharacter(text[open.start]) ? open.start : open.start + 1;
    if (caret <= slash || !isSlashCharacter(text[slash])) return null;
    const query = text.slice(slash + 1, caret);
    if (/^\s/u.test(query) && text.slice(text.lastIndexOf("\n", slash - 1) + 1, slash).trim()) return null;
    return /\s\s|\n/u.test(query) ? null : { start: slash, end: caret, query };
  }
  if (!typedSlash || !isSlashCharacter(text[caret - 1]) || !triggerOpensAfter("/", text, caret - 1)) return null;
  return { start: caret - 1, end: caret, query: "" };
}

/**
 * The "@" menu's place and query as a line changes, or null once the menu
 * ends. As Notion's (seen 2026-10-01), it begins as "@" is typed where
 * triggerOpensAfter lets it and filters by all that follows, spaces and words
 * too; a line break or the caret going back over the "@" ends it, and an
 * ended menu does not come back as the line is edited.
 */
export function mentionMenuTrigger(
  open: { start: number } | null,
  text: string,
  caret: number,
  typedAt: boolean,
): { start: number; end: number; query: string } | null {
  if (open) {
    if (caret <= open.start || triggerFor(text[open.start]) !== "@") return null;
    const query = text.slice(open.start + 1, caret);
    return /\n/u.test(query) ? null : { start: open.start, end: caret, query };
  }
  if (!typedAt || triggerFor(text[caret - 1]) !== "@" || !triggerOpensAfter("@", text, caret - 1)) return null;
  return { start: caret - 1, end: caret, query: "" };
}

/**
 * "[[" opens the page link menu wherever it is typed, after a letter too, as
 * Notion's does (seen 2026-11-05: "abc[[s14"); what follows it is the query.
 */
export function pageLinkMenuTrigger(
  open: { start: number } | null,
  text: string,
  caret: number,
  typedBracket: boolean,
): { start: number; end: number; query: string } | null {
  if (open) {
    const opener = text.slice(open.start, open.start + 2) === "[[" ? 2 : triggerFor(text[open.start]) === "+" ? 1 : 0;
    if (!opener || caret < open.start + opener) return null;
    const query = text.slice(open.start + opener, caret);
    return /\n/u.test(query) ? null : { start: open.start, end: caret, query };
  }
  const start = caret - 2;
  if (!typedBracket || start < 0 || text.slice(start, caret) !== "[[") return null;
  return { start, end: caret, query: "" };
}

/**
 * "+" opens the page link menu at the first letter typed after it, where
 * triggerOpensAfter lets it, as Notion's (seen 2026-11-05): "+ " is a list
 * item, and "+1" stays as typed (opensAtLetter).
 */
export function plusPageLinkTrigger(text: string, caret: number) {
  const start = caret - 2;
  if (start < 0 || triggerFor(text[start]) !== "+" || !opensAtLetter("+", text[caret - 1]) || !triggerOpensAfter("+", text, start)) return null;
  return { start, end: caret, query: text[caret - 1] };
}

export type FieldMenu = { kind: "mention" | "page-link" | "emoji"; start: number; end: number; query: string; newPageFirst?: boolean };

const FIELD_TRIGGERS = {
  mention: new Set(["@"]), links: new Set(["@", "[[", ":"]), all: new Set(["@", "[[", ":", "+"]),
} as const;

/**
 * The menu a field's text opens or goes on with at its caret, by a line's rules;
 * "typed" is the text just put in, opening a menu as a line's does (typedTrigger:
 * text put in at once by its first letter, as Notion's database cell and comment,
 * seen 2026-11-18). A field has no "/" menu, a comment no "+", and an automation
 * message or description only "@" (a database cell and a comment seen 2026-11-05).
 */
export function fieldMenuTrigger(
  open: { kind: FieldMenu["kind"]; start: number } | null,
  text: string,
  caret: number,
  typed: string,
  rules: "mention" | "links" | "all",
): FieldMenu | null {
  if (open?.kind === "page-link") {
    const going = pageLinkMenuTrigger(open, text, caret, false);
    if (going) return { ...going, kind: "page-link", newPageFirst: triggerFor(text[going.start]) === "+" };
  }
  if (open?.kind === "emoji" && text[open.start] === ":" && caret > open.start && !text.slice(open.start, caret).includes("\n")) {
    return { kind: "emoji", start: open.start, end: caret, query: text.slice(open.start + 1, caret) };
  }
  const going = open?.kind === "mention" ? mentionMenuTrigger(open, text, caret, false) : null;
  if (going) return { ...going, kind: "mention" };
  // Pasted words open none, as a line's paste (Notion's, seen 2026-11-18).
  const started = typed && !putInByPaste() ? typedTrigger(text, caret, typed, putInOnePiece(typed), FIELD_TRIGGERS[rules]) : null;
  if (!started) return null;
  const at = { start: started.start };
  if (started.char === "@") {
    const mention = mentionMenuTrigger(at, text, caret, false);
    return mention && { ...mention, kind: "mention" };
  }
  if (started.char === ":") {
    const query = text.slice(started.start + 1, caret);
    return query && !/\s/u.test(query) ? { kind: "emoji", start: started.start, end: caret, query } : null;
  }
  const link = pageLinkMenuTrigger(at, text, caret, false);
  return link && { ...link, kind: "page-link", newPageFirst: started.char === "+" };
}

// The Markdown typed at a paragraph's start that turns it into a block, as
// Notion's input rules have it (its own table, seen 2026-11-01): the first
// spelling of each is the one the slash menu shows; the others follow —
// "+ " and "• " bullets, "| " and curled quotes, "[ ] " and checked to-dos,
// and the full-width forms a Japanese keyboard types, without a space.
const BLOCK_SHORTCUTS = [
  ["# ", "heading_1"], ["## ", "heading_2"], ["### ", "heading_3"], ["#### ", "heading_4"],
  ["- ", "bulleted_list_item"], ["* ", "bulleted_list_item"], ["1. ", "numbered_list_item"],
  ["[] ", "to_do"], ["> ", "toggle"], ['" ', "quote"], ["```", "code"],
  ["＃", "heading_1"], ["＃＃", "heading_2"], ["＃＃＃", "heading_3"], ["＃＃＃＃", "heading_4"],
  ["+ ", "bulleted_list_item"], ["• ", "bulleted_list_item"], ["＊", "bulleted_list_item"],
  ["ー", "bulleted_list_item"], ["＋", "bulleted_list_item"], ["・", "bulleted_list_item"],
  ["[ ] ", "to_do"], ["「」　", "to_do"], ["[x] ", "to_do"], ["[X] ", "to_do"], ["「x」　", "to_do"], ["「X」　", "to_do"],
  ["＞", "toggle"], ["| ", "quote"], ["“ ", "quote"], ["”", "quote"], ["´´´", "code"], ["｀｀｀", "code"],
] as const;
const BLOCK_SHORTCUT_TYPES = new Map(BLOCK_SHORTCUTS);

export function blockShortcutForText(text: string) {
  return BLOCK_SHORTCUT_TYPES.get(text as never) ?? null;
}

/** Whether the to-do a shortcut makes starts checked ("[x] "), as Notion's. */
export function blockShortcutChecked(text: string) {
  return /^(\[[xX]\] |「[xX]」　)$/u.test(text);
}

const ROMAN_ONE = "i";
const NUMBERED_SHORTCUT = /^(\d+|[０-９]+|[a-z]+)(\. |。|．|\) )$/u;

/**
 * The numbered list a typed marker makes, as Notion's (seen 2026-11-10): any
 * number with ". ", ") ", "。" or "．" (half or full width) starts a list from
 * it; "i." starts a roman list and "a." a lettered one, other letters stay as
 * typed. Right under a numbered list (next, the number it comes to next), a
 * marker goes on with that list only when it reads as that number — digits,
 * letters ("c." for 3) or roman numerals ("iv." for 4) — and takes the format
 * it names but no start (follows); any other stays as typed ("1. ", "2. " or
 * "a. " under "1. 2.", "i. " under "a.").
 */
export function numberedListShortcut(text: string, next?: number | null):
  { start: number; format?: "letters" | "roman"; follows?: true } | null {
  const head = NUMBERED_SHORTCUT.exec(text)?.[1];
  if (!head) return null;
  if (/^[a-z]+$/u.test(head)) {
    if (next) {
      const { letters, roman } = numberedListMarkerValues(head);
      return letters === next ? { start: next, format: "letters", follows: true }
        : roman === next ? { start: next, format: "roman", follows: true } : null;
    }
    return head === ROMAN_ONE ? { start: 1, format: "roman" } : head === "a" ? { start: 1, format: "letters" } : null;
  }
  const start = Number(head.replace(/[０-９]/gu, (digit) => String(digit.charCodeAt(0) - 0xff10)));
  if (!Number.isSafeInteger(start) || start < 1) return null;
  if (next) return start === next ? { start, follows: true } : null;
  return { start };
}

/** Whether typed text has a numbered list marker's shape, which may make a list where it stands. */
export function numberedListShortcutTyped(text: string) {
  return NUMBERED_SHORTCUT.test(text);
}

const TOGGLE_HEADING_TYPES = ["toggle_heading_1", "toggle_heading_2", "toggle_heading_3", "toggle_heading_4"] as const;

/**
 * The toggle heading a heading or a toggle turns into, as Notion's (seen
 * 2026-10-01): "> " typed at a heading's start makes it a toggle heading of
 * its size, and "# " to "#### " at a toggle's start a toggle heading of
 * that size.
 */
export function toggleHeadingShortcutFor(blockType: string, typed: string) {
  const heading = /^heading_([1-4])$/u.exec(blockType);
  if (heading && typed === "> ") return TOGGLE_HEADING_TYPES[Number(heading[1]) - 1];
  const level = /^(#{1,4}) $/u.exec(typed)?.[1].length;
  if (blockType === "toggle" && level) return TOGGLE_HEADING_TYPES[level - 1];
  return null;
}

/** The first Markdown that makes a block of this type, without its space. */
function blockShortcutForType(type: string) {
  // A toggle heading is its heading's Markdown and then a toggle's.
  const toggleHeading = /^toggle_heading_([1-4])$/u.exec(type);
  if (toggleHeading) return `${"#".repeat(Number(toggleHeading[1]))} >`;
  return BLOCK_SHORTCUTS.find(([, shortcutType]) => shortcutType === type)?.[0].trim();
}

/** The typed prefix displayed beside a slash-menu block. */
export function typedBlockShortcutHint(type: string) {
  return type === "divider" ? "---" : blockShortcutForType(type);
}
