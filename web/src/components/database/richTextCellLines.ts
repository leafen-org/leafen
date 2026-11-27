import { blockAnchor } from "@/lib/blockAddress";
import { DOMParser as SchemaParser, type Schema } from "@tiptap/pm/model";
import type { TextSpan } from "@/lib/types";
import { bigFontSize, clipboardHtmlFragment } from "@/lib/clipboardHtml";
import { withoutThreadMarks } from "@/lib/clipboardServer";
import { absolutePageUrl } from "@/lib/navigation";
import { dateMentionCopyText } from "../editor/dateMentions";
import { EDITOR_PASTE_BOUNDS } from "../editor/editorClipboardPolicy";
import { internalPasteIsLine } from "../editor/internalBlockPaste";
import { parseInlineMarkdown, parseInternalPastedBlocks } from "../editor/markdownPaste";
import { fragmentToNativeRichText } from "../editor/nativeTiptapInline";
import { isInlineRichtextElement } from "../editor/richtextHtmlRead";
import { pastedLines } from "../page/titlePaste";
import { pastesTextOverHtml } from "../editor/pastedHtmlChoice";

/**
 * Lines pasted in a field of words that keeps them — a comment box, a
 * database's text — as Notion's comment box and text cell take them (real
 * ⌘V, seen 2026-11-11; the two read a paste alike):
 * - words copied here — within one block, its lines as ⇧Enter put them, or
 *   all of a line's words — keep their marks, mentions and lines;
 * - blocks copied (one or several, Esc ⌘C) are a line each, as Notion reads
 *   the HTML its copy of blocks gives (Leafen's copy, or any app's HTML,
 *   read the same way): a list item led by "• " or its number ("1. "), a
 *   to-do by "[ ]  ", a heading's words bold, a code block's lines code,
 *   another app's empty block an empty line (Leafen's copy, as Notion's,
 *   gives one none); their marks and links stay but no color, and a mention
 *   is its words — a page's a link to it, a date its date with no "@", a
 *   person "@이름" — an equation "$…$"; spaces stay as written (seen
 *   2026-11-11: "[ ]  " and two spaces after a mention kept);
 * - text alone has its Markdown's marks read across its lines (a list's,
 *   heading's or quote's marker stays words).
 * A line of words with no blocks around them reads as the field reads it,
 * its spaces and line breaks as written, as Notion's (seen 2026-11-11:
 * another app's "a  <b>b</b>  c" is "a  b  c", and "x⏎   y" two lines, in a
 * comment box, a text cell, a title cell and a page's line). Null where the
 * field didn't keep the lines as pasted
 * (a title's first line, words cut to a bound) or nothing reads, the words
 * then going in as text.
 */
export function pastedCellLines(data: DataTransfer, text: string, schema: Schema, maxHtml: number): TextSpan[] | null {
  if (pastedLines(data.getData("text/plain")) !== pastedLines(text)) return null;
  try {
    if (!text.includes("\n") && !pastedHtml(data)) return null;
    const spans = readPaste(data, text, schema, maxHtml);
    // Lines the field left out at either end stay out.
    if (!text.startsWith("\n")) while (spans?.[0]?.text === "\n") spans.shift();
    if (!text.endsWith("\n")) while (spans?.at(-1)?.text === "\n") spans.pop();
    return spans?.length ? spans : null;
  } catch (error) {
    if (error instanceof RangeError) return null;
    throw error;
  }
}

/** The paste's HTML, but where its text loses nothing of it and is read alone (pastedHtmlChoice.ts). */
const pastedHtml = (data: DataTransfer) => pastesTextOverHtml(data) ? "" : data.getData("text/html");

function readPaste(data: DataTransfer, text: string, schema: Schema, maxHtml: number): TextSpan[] | null {
  const html = pastedHtml(data);
  if (!html) return parseInlineMarkdown(text, EDITOR_PASTE_BOUNDS);
  // A line's words copied whole here keep their mentions; blocks chosen as blocks are read as blocks.
  const copied = internalPasteIsLine(data, EDITOR_PASTE_BOUNDS) ? ownBlockWords(data) : null;
  if (copied) return copied;
  const root = clipboardHtmlFragment(html, maxHtml);
  const blocks = [...root.querySelectorAll("*")].some(isBlock);
  // A copy of words in one block here (the editor's own HTML) keeps its mentions, and so does a line
  // of words with no blocks, read as the field reads it but for its spaces and line breaks.
  const nativeBlocks = root.querySelectorAll("[data-native-block]").length;
  const words = nativeBlocks === 1 || !nativeBlocks && (root.querySelector("[data-native-text]") !== null || !blocks && !text.includes("\n"));
  // Leafen's own copy of blocks gives an empty block no line, as Notion's copy gives it none (seen 2026-11-11).
  const own = root.querySelector("[data-leafen-copy], [data-native-block]") !== null;
  const lines = readLines(root, { spaces: !blocks, leads: !words, empties: !own });
  const parser = SchemaParser.fromSchema(schema), spans: TextSpan[] = [];
  lines.forEach((line, index) => {
    if (index) spans.push({ text: "\n" });
    // A line at a time: the field's own document holds one.
    const container = document.createElement("div");
    container.append(lineElement(line));
    const block = parser.parseSlice(container).content.firstChild;
    const read = (block?.isTextblock ? fragmentToNativeRichText(block.content) : [])
      .map((span) => span.text.includes(KEPT_SPACE) ? { ...span, text: span.text.replaceAll(KEPT_SPACE, " ") } : span);
    // A line's last break shows no line of its own, as a browser draws it.
    for (let last = read.at(-1); last && /\n$/u.test(last.text); last = read.at(-1)) {
      last.text = last.text.replace(/\n+$/u, "");
      if (last.text) break;
      read.pop();
    }
    spans.push(...(words ? read : read.map(asWords)));
  });
  return withoutThreadMarks(spans);
}

/** A line's words copied whole, as Leafen's copy of its one block gives them (another server's naming what is there). */
function ownBlockWords(data: DataTransfer): TextSpan[] | null {
  const blocks = parseInternalPastedBlocks(data, EDITOR_PASTE_BOUNDS);
  const [block] = blocks;
  const rich = blocks.length === 1 && !block.children?.length ? block.content?.rich : undefined;
  return Array.isArray(rich) && rich.length ? rich as TextSpan[] : null;
}

const MARKS = ["bold", "italic", "underline", "strikethrough", "code"] as const;

/** A mention or equation as several blocks' copy gives it to another app's paste. */
function asWords(span: TextSpan): TextSpan {
  const words: TextSpan = { text: span.text };
  for (const mark of MARKS) if (span[mark]) words[mark] = true;
  if (span.equation) words.text = `$${span.equation}$`;
  else if (span.mention === "date" && span.date) words.text = dateMentionCopyText(span.date, span.dateEnd ?? "", span.dateFormat, span.dateTimeFormat);
  else if (span.mention === "person" && !words.text.startsWith("@")) words.text = `@${words.text}`;
  const page = span.mention === "page" ? span.pageId : span.mention === "database" ? span.databaseId : undefined;
  const link = page ? absolutePageUrl(page) + blockAnchor(page, span.blockId) : span.link;
  if (link) words.link = link;
  return words;
}

const BLOCK_TAGS = new Set(["address", "article", "aside", "blockquote", "dd", "details", "div", "dl", "dt", "figcaption",
  "figure", "footer", "h1", "h2", "h3", "h4", "h5", "h6", "header", "hr", "li", "main", "nav", "ol", "p", "pre", "section",
  "summary", "table", "tbody", "td", "tfoot", "th", "thead", "tr", "ul"]);
const BLOCK_SELECTOR = [...BLOCK_TAGS].join(",");
const HEADING = /^h[1-6]$/u;
const MAX_DEPTH = 64;

/** A workspace emoji drawn as its picture is its name; any other picture none. */
function emojiName(image: Element) {
  return image.hasAttribute("data-inline-custom-emoji") || image.closest("[data-mention]") ? image.getAttribute("alt") ?? "" : "";
}

function isBlock(element: Element) {
  return BLOCK_TAGS.has(element.localName) && !(element instanceof HTMLElement && isInlineRichtextElement(element));
}

type Frame = { prefix?: string; bold?: boolean; code?: boolean };
type Line = Frame & { nodes: Node[] };

/** Whether an element's words keep their spaces and line breaks, by its tag or style; undefined when it doesn't say. */
function keepsSpaces(element: Element): boolean | undefined {
  if (element.localName === "pre" || element.localName === "textarea") return true;
  const space = element instanceof HTMLElement ? element.style.whiteSpace : "";
  return space ? /^(pre|pre-wrap|pre-line|break-spaces)$/u.test(space) : undefined;
}

/** A list item's number in its list, a list going on in the one just after it (Leafen's copy gives each item its own). */
function itemNumber(item: Element) {
  let number = 0;
  for (let at: Element | null = item; at; at = at.previousElementSibling) if (at.localName === "li") number += 1;
  for (let list = item.parentElement?.previousElementSibling; list?.localName === "ol"; list = list.previousElementSibling) {
    number += list.querySelectorAll(":scope > li").length;
  }
  const start = Number(item.parentElement?.getAttribute("start"));
  return number + (Number.isSafeInteger(start) && start > 0 ? start - 1 : 0);
}

/** What leads a line of the editor's own block, by its kind. */
function kindFrame(block: Element): Frame {
  const kind = block.getAttribute("data-kind") ?? "";
  if (/heading_\d$/u.test(kind)) return { bold: true };
  if (kind === "code") return { code: true };
  if (kind === "bulleted_list_item" || kind === "toggle") return { prefix: "• " };
  if (kind === "to_do") return { prefix: /"checked":true/u.test(block.getAttribute("data-props") ?? "") ? "• [x]  " : "• [ ]  " };
  if (kind !== "numbered_list_item") return {};
  let number = 1;
  for (let at = block.previousElementSibling; at?.getAttribute("data-kind") === kind; at = at.previousElementSibling) number += 1;
  return { prefix: `${number}. ` };
}

/** A block in big type (bigFontSize), a heading's bold line, as Notion's text cell reads it (seen 2026-11-13). */
const bigBlock = (element: Element) => element instanceof HTMLElement && bigFontSize(element.style.fontSize);

function blockFrame(element: Element): Frame {
  const parent = element.parentElement?.localName;
  if (element.hasAttribute("data-native-block")) return kindFrame(element);
  if (element.localName === "li") return { prefix: parent === "ol" ? `${itemNumber(element)}. ` : "• " };
  if (HEADING.test(element.localName) || bigBlock(element)) return { bold: true };
  if (element.localName === "pre") return { code: true };
  return {};
}

/**
 * The pasted HTML's lines, each block's words one: `spaces` whether its loose
 * words keep their line breaks (Notion's copy of one block's words), `leads`
 * whether a line takes its block's lead (several blocks'), `empties` whether
 * an empty block is an empty line.
 */
function readLines(root: Node, { spaces, leads, empties }: { spaces: boolean; leads: boolean; empties: boolean }): Line[] {
  const lines: Line[] = [];
  let current: Line | null = null, pending: Frame = {};
  const line = () => {
    if (!current) { current = { ...pending, nodes: [] }; pending = {}; lines.push(current); }
    return current;
  };
  const walk = (parent: Node, keep: boolean, depth: number) => {
    if (depth > MAX_DEPTH) throw new RangeError("Pasted HTML nests past its bound.");
    for (const node of parent.childNodes) {
      if (node.nodeType === Node.TEXT_NODE) {
        if (current || /\S/u.test(node.textContent ?? "")) line().nodes.push(inlineCopy(node, keep, depth));
        continue;
      }
      if (!(node instanceof Element)) continue;
      // A rule or a picture is no words, but for a workspace emoji's name.
      if (node.localName === "hr" || node.localName === "img" && !emojiName(node)) continue;
      // An inline element holding blocks is read as the blocks it holds.
      if (!isBlock(node) && ![...node.querySelectorAll(BLOCK_SELECTOR)].some(isBlock)) {
        line().nodes.push(inlineCopy(node, keep, depth));
        continue;
      }
      current = null;
      // A block's own words (the editor's) lead as its block does; one holding no words of its own passes its lead on.
      pending = node.hasAttribute("data-native-text") ? pending
        : leads ? { ...pending, ...blockFrame(node) } : bigBlock(node) ? { ...pending, bold: true } : pending;
      const before = lines.length;
      walk(node, keepsSpaces(node) ?? keep, depth + 1);
      // A block of no words is an empty line.
      if (empties && lines.length === before && !node.querySelector("*") && node.localName !== "ol" && node.localName !== "ul") line();
      current = null; pending = {};
    }
  };
  walk(root, spaces, 0);
  return lines;
}

/** A copy of inline HTML as the field parses it: line breaks kept where its words keep them, a box ticked or not as words, a heading bold. */
function inlineCopy(node: Node, keep: boolean, depth: number): Node {
  if (depth > MAX_DEPTH) throw new RangeError("Pasted HTML nests past its bound.");
  if (node.nodeType === Node.TEXT_NODE) {
    const text = node.textContent ?? "";
    if (!keep) return document.createTextNode(keptSpaces(text));
    const kept = document.createElement("span");
    kept.style.whiteSpace = "pre-wrap";
    text.split(/\r\n?|\n/u).forEach((part, index) => {
      if (index) kept.append(document.createElement("br"));
      if (part) kept.append(part);
    });
    return kept;
  }
  if (!(node instanceof Element)) return document.createTextNode("");
  if (node.localName === "input" && node.getAttribute("type") === "checkbox") return document.createTextNode(keptSpaces(node.hasAttribute("checked") ? "[x] " : "[ ] ", true));
  if (node.localName === "img") return document.createTextNode(emojiName(node));
  const copy = node.cloneNode(false) as Element;
  const inner = keepsSpaces(node) ?? keep;
  for (const child of node.childNodes) copy.append(inlineCopy(child, inner, depth + 1));
  if (!HEADING.test(node.localName)) return copy;
  const bold = document.createElement("strong");
  bold.append(...copy.childNodes);
  return bold;
}

// A space the field keeps where the HTML's reading would fold it into the one before.
const KEPT_SPACE = "\ue000";

/**
 * A text's spaces as the field keeps them: a line break in the HTML's text
 * (and spaces around it) is one space, as a browser draws it, but several
 * spaces stay, as Notion's paste keeps them; `trailing` keeps a last space
 * that another follows.
 */
function keptSpaces(text: string, trailing = false) {
  const spaced = text.replaceAll(KEPT_SPACE, "").replace(/[ \t]*(?:\r\n?|\n)[ \t\r\n]*/gu, " ").replace(/ {2,}/gu, (run) => KEPT_SPACE.repeat(run.length - 1) + " ");
  return trailing ? spaced.replace(/ $/u, KEPT_SPACE) : spaced;
}

function lineElement(line: Line) {
  const element = document.createElement("p");
  let words: Element = element;
  if (line.prefix) element.append(keptSpaces(line.prefix));
  for (const [on, tag] of [[line.bold, "strong"], [line.code, "code"]] as const) {
    if (!on) continue;
    const mark = document.createElement(tag);
    words.append(mark);
    words = mark;
  }
  words.append(...line.nodes);
  return element;
}
