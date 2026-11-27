import { importFileLine, pageReference, type ImportReferenceMap } from "./importReferences";
import { brokenText, collapseWords, keptLines, spacedText, withoutWordMarks, WORDS_EDGE, WORDS_GAP, type HtmlSpacing } from "./htmlImportSpacing";
import { escapeImportMarkdown } from "./importMarkdownText";
import { htmlBlockColor, htmlMarkColors, htmlUnderline, textStyleMark, underlinedStyle, type TextStyle } from "./htmlImportColors";
import { collapse, wrapInline, type Block } from "./importMarkdownBlocks";
import { boxWords, holdsCheckbox, isCheckbox, TAIL_BOX_HOLDER, TAIL_BREAK, type BoxWords } from "./htmlImportCheckboxes";
import { isElementPart, isLineElement, isMediaElement, lineParts, lineWords, tagOf, type LineWords } from "./htmlImportLineElements";
import { codeLineParts, codeWords, type CodeFormatting } from "./htmlImportInlineCode";
import { blockTypeForPastedAssetUrl } from "@/components/editor/mediaEmbeds";
import { isMath, isMathDrawing, mathWords, notionEquationMarkdown } from "./htmlImportMath";

export { notionEquationMarkdown };

/**
 * The words of an imported HTML document's blocks as the importer's inline
 * Markdown (importDocumentConverters.ts reads the blocks): bold, italic,
 * strikethrough, inline code (<code>, <tt>), quotations (<q>), web links and
 * maths (htmlImportMath.ts), and other text escaped so it
 * reads as written (importMarkdownText.ts), its spaces as the document's
 * style lays them out (htmlImportSpacing.ts); links and images that refer to
 * other imported documents or files become their import references, images
 * and files following the block they appear in (drainInto).
 */

/**
 * breaks: a <br> right inside a paragraph ends it, as Notion's HTML import
 * reads a page's HTML; in Notion's own export it is a line break of the
 * words, as Notion's API import has it (2026-11-13). Blocks' and words'
 * colors are read in an export alone, a heading's in any document
 * (htmlImportColors.ts), an underline in any (htmlUnderline); style is
 * that of the words being read, box how a checkbox in them reads
 * (htmlImportCheckboxes.ts) — none in an export — and lines how a script,
 * a frame and such read in them (htmlImportLineElements.ts).
 */
export type HtmlContext = {
  references: ImportReferenceMap;
  trailing: string[];
  spacing: (element: Element | null) => HtmlSpacing;
  breaks: boolean;
  style: TextStyle;
  box: BoxWords;
  lines: LineWords;
};

/** Words read with line elements read as `lines` reads them, the reading around them after them. */
export function withLineWords<T>(context: HtmlContext, lines: LineWords, read: () => T) {
  const around = context.lines;
  context.lines = lines;
  try {
    return read();
  } finally {
    context.lines = around;
  }
}

/** Words read with their checkboxes read as `box` reads them, the reading around them after them. */
export function withBoxWords<T>(context: HtmlContext, box: BoxWords, read: () => T) {
  const around = context.box;
  context.box = box;
  try {
    return read();
  } finally {
    context.box = around;
  }
}

const FORMATTED = new Set(["A", "B", "STRONG", "I", "EM", "S", "DEL", "STRIKE"]);

export const SKIPPED_TAGS = new Set(["SCRIPT", "STYLE", "NOSCRIPT", "TEMPLATE", "HEAD", "IFRAME", "OBJECT", "SVG", "CANVAS"]);

/**
 * What a document that is no export leaves out of its words, as Notion's HTML
 * import does (seen 2026-11-13): its styles and head; a template is an
 * element that leaves no words, and a <noscript>'s and an <object>'s words
 * are read (htmlImportLineElements.ts reads scripts, frames and drawings).
 */
const UNREAD_TAGS = new Set(["STYLE", "HEAD", "TITLE", "META", "LINK", "BASE"]);

/** Whether an element leaves no words and no line in the document's words. */
export function isSkipped(element: Element, context: HtmlContext) {
  return context.breaks ? UNREAD_TAGS.has(tagOf(element)) : SKIPPED_TAGS.has(element.tagName);
}

const BLOCK_TAGS = new Set([
  "P", "DIV", "SECTION", "ARTICLE", "MAIN", "HEADER", "FOOTER", "ASIDE", "NAV", "FIGURE", "FIGCAPTION",
  "H1", "H2", "H3", "H4", "H5", "H6", "UL", "OL", "LI", "BLOCKQUOTE", "PRE", "HR", "TABLE", "DL", "DT", "DD",
  "BODY", "FORM", "FIELDSET", "ADDRESS", "DETAILS", "SUMMARY",
]);

export function isBlock(node: Node): node is Element {
  return node instanceof Element && BLOCK_TAGS.has(node.tagName);
}

export function hasBlockChild(element: Element) {
  return Array.from(element.children).some((child) => BLOCK_TAGS.has(child.tagName));
}

export function linkText(text: string, href: string | null, context?: HtmlContext) {
  const target = href?.trim() ?? "";
  const reference = context?.references.get(target);
  if (reference?.kind === "page") return `[${text}](${pageReference(reference)})`;
  if (reference && context) {
    context.trailing.push(importFileLine(reference, ""));
    return text;
  }
  if (!LINKED.test(target) || /[\s()]/.test(target) || !collapse(withoutWordMarks(text))) return text;
  if (!context?.breaks) return `[${text}](${target})`;
  const [before, words, after] = textEdges(text);
  return `${before}[${words}](${target})${after}`;
}

/**
 * Words apart from the spaces at their edges, which a document that is no
 * export has outside a link or a style, as Notion's HTML import has them
 * (seen 2026-11-13: "a<a> b</a>" is "a [b]").
 */
function textEdges(words: string): [string, string, string] {
  const match = /^([\s\u0001\u0002]*)([\s\S]*?)([\s\u0001\u0002]*)$/.exec(words)!;
  return [match[1]!, match[2]!, match[3]!];
}

/**
 * The addresses a link of an imported document keeps, as the importer's
 * Markdown links them (workspace_import_markdown.go
 * workspaceImportMarkdownLinkTarget) and Notion's HTML import does (seen
 * 2026-11-13): web, mail and phone addresses, an anchor (#x, which Notion
 * links as about:blank#x), a path (/x) and a web address without its scheme
 * (//host/x). Any other, a relative file or a script, stays its words.
 */
const LINKED = /^(?:https?:\/\/|mailto:|tel:|#|\/(?!\/)|\/\/[^/\s?#]+\.[^/\s?#]+(?:[/?#]|$))/i;

function imageLine(image: Element, context: HtmlContext) {
  const source = image.getAttribute("src")?.trim() ?? "";
  const alt = collapse(image.getAttribute("alt") ?? "");
  const reference = context.references.get(source);
  if (reference && reference.kind !== "page") return importFileLine(reference, alt);
  return /^https?:\/\//i.test(source) && !/[\s()]/.test(source) ? `![${escapeImportMarkdown(alt)}](${source})` : "";
}

/**
 * The block a picture, a frame or a video standing on its own makes: the
 * picture; a frame at a web address the block a pasted address of its kind
 * makes (blockTypeForPastedAssetUrl), else an embed, as Notion's HTML import
 * makes one (seen 2026-11-13: a YouTube frame is a video); a video none.
 */
function mediaLine(element: Element, context: HtmlContext) {
  if (tagOf(element) === "IMG") return imageLine(element, context);
  const address = tagOf(element) === "IFRAME" ? element.getAttribute("src")?.trim() ?? "" : "";
  if (!/^https?:\/\/[^\s<>\\]+$/i.test(address)) return "";
  return `[](leafen-block:${blockTypeForPastedAssetUrl(address) ?? "embed"}) [](<${address}>)`;
}

export function inlineHtmlText(node: Node, context: HtmlContext): string {
  if (node.nodeType === Node.TEXT_NODE) {
    const spacing = context.spacing(node.parentElement);
    const text = context.breaks && spacing === "collapse" ? brokenText(node as Text, (part) => part instanceof Element && FORMATTED.has(part.tagName))
      : spacedText(node.textContent ?? "", spacing);
    return escapeImportMarkdown(text);
  }
  if (node.nodeType === Node.COMMENT_NODE) return WORDS_GAP;
  if (!(node instanceof Element) || isSkipped(node, context) || context.breaks && isMathDrawing(node)) return "";
  if (node.tagName === "BR") return context.box === "tail" ? TAIL_BREAK : "\n";
  if (isCheckbox(node)) return boxWords(node, context.box);
  if (context.breaks && isMath(node)) return mathWords(node);
  const equation = notionEquationMarkdown(node);
  if (equation !== null) return equation;
  const blank = context.breaks ? blankLineWords(node, context) : null;
  if (blank !== null) return blank;
  if (node.tagName === "IMG") {
    const line = imageLine(node, context);
    if (line) context.trailing.push(line);
    return context.breaks ? WORDS_GAP : "";
  }
  if (context.breaks && (isLineElement(node) || isMediaElement(node))) return lineWords(node, context.lines, escapeImportMarkdown);
  if (context.breaks && node.tagName === "TEMPLATE") return WORDS_GAP;
  if (isInlineCode(node)) {
    // Code right after code is one with it, as Notion's import joins them.
    if (node.previousSibling && isInlineCode(node.previousSibling)) return "";
    const keep = context.spacing(node) === "keep";
    let joined: Element = node;
    for (let next = node.nextSibling; next && isInlineCode(next); next = next.nextSibling) {
      if (joined === node) joined = node.cloneNode(true) as Element;
      joined.append(...Array.from(next.childNodes).map((part) => part.cloneNode(true)));
    }
    return codeWords(joined, keep, (element) => isSkipped(element, context), codeFormatting(context));
  }
  // A picture linked to itself, as an export draws one, is the picture once.
  const only = node.tagName === "A" && node.children.length === 1 && node.children[0]!.tagName === "IMG" && !collapse(node.textContent ?? "")
    ? node.children[0]! : null;
  if (only && only.getAttribute("src")?.trim() === node.getAttribute("href")?.trim()) return inlineHtmlText(only, context);
  if (node.tagName === "Q") return quoted(node, context);
  if (node.tagName === "MARK" && !context.breaks) return styledText(node, htmlMarkColors(node, context.style), context);
  if (htmlUnderline(node, !context.breaks)) return styledText(node, underlinedStyle(context.style), context);
  const inner = Array.from(node.childNodes).map((child) => inlineHtmlText(child, context)).join("");
  // A formatted part without words leaves none, its spaces kept as an element's that leaves none.
  if (context.breaks && FORMATTED.has(node.tagName) && !collapse(withoutWordMarks(inner))) return WORDS_GAP;
  // A formatted part a box in a formatted part's line is in takes the spaces before it (tailBoxes).
  const holder = context.box === "tail" && FORMATTED.has(node.tagName) && holdsCheckbox([node]) ? TAIL_BOX_HOLDER : "";
  return holder + formattedText(node, inner, context);
}

function formattedText(node: Element, inner: string, context: HtmlContext) {
  switch (node.tagName) {
    case "A": return linkText(inner, node.getAttribute("href"), context);
    case "B": case "STRONG": return wrapInline(inner, "**");
    case "I": case "EM": return wrapInline(inner, "*");
    case "S": case "DEL": case "STRIKE": return wrapInline(inner, "~~");
    default: return inner ? `${WORDS_EDGE}${inner}${WORDS_EDGE}` : WORDS_GAP;
  }
}

/**
 * An element holding nothing but spaces with a line break, in a document
 * that is no export, as Notion's HTML import reads it (seen 2026-11-13): a
 * formatted part — bold, italic, a link, a strikethrough, an underline,
 * <small> — is an empty line, the spaces around it left out ("자4 <i>⏎</i>나"
 * is "자4", an empty line and "나"); a <label> or a <font> a space; a <span> a
 * line break whose sides keep their spaces (blockLines). Null for any other.
 */
const BLANK_LINE_TAGS = new Set([...FORMATTED, "U", "INS", "SMALL"]);

function blankLineWords(element: Element, context: HtmlContext) {
  const text = element.textContent ?? "";
  if (element.children.length || /\S/.test(text) || !text.includes("\n") || context.spacing(element) !== "collapse") return null;
  if (BLANK_LINE_TAGS.has(element.tagName)) return `\n${WORDS_GAP}\n`;
  return element.tagName === "LABEL" || element.tagName === "FONT" ? " " : null;
}

function isInlineCode(node: Node): boolean {
  return node instanceof Element && (node.tagName === "CODE" || node.tagName === "TT") && !node.closest("pre");
}

/**
 * The formatting inside inline code (htmlImportInlineCode.ts): bold, italic,
 * strikethrough and links, an underline, and in an export a mark's colors
 * (Notion's export underlines inline code inside its <code>).
 */
function codeFormatting(context: HtmlContext): CodeFormatting {
  const marked = (element: Element) => element.tagName === "MARK" && !context.breaks && htmlMarkColors(element, context.style) !== context.style;
  return {
    formats: (element) => FORMATTED.has(element.tagName) || htmlUnderline(element, !context.breaks) || marked(element),
    wrap: (element, words) => {
      if (marked(element)) return styled(words, htmlMarkColors(element, context.style), context.style, context.breaks);
      if (htmlUnderline(element, !context.breaks)) return styled(words, underlinedStyle(context.style), context.style, context.breaks);
      switch (element.tagName) {
        case "A": return linkText(words, element.getAttribute("href"), context);
        case "B": case "STRONG": return wrapInline(words, "**");
        case "I": case "EM": return wrapInline(words, "*");
        default: return wrapInline(words, "~~");
      }
    },
  };
}

/** A <q>'s words between quotation marks, single ones in another, as Notion's HTML import writes them. */
function quoted(q: Element, context: HtmlContext) {
  let depth = 0;
  for (let parent = q.parentElement; parent; parent = parent.parentElement) if (parent.tagName === "Q") depth += 1;
  const [open, close] = depth % 2 ? ["‘", "’"] : ["“", "”"];
  return `${open}${Array.from(q.childNodes).map((child) => inlineHtmlText(child, context)).join("")}${close}`;
}

/** An element's words in a style of their own — a mark's colors, an underline — the style around them after them. */
function styledText(element: Element, style: TextStyle, context: HtmlContext) {
  const around = context.style;
  context.style = style;
  const inner = Array.from(element.childNodes).map((child) => inlineHtmlText(child, context)).join("");
  context.style = around;
  return styled(inner, style, around, context.breaks);
}

/** Words in a style, the style around them after them; as they are when it is the same. */
function styled(words: string, style: TextStyle, around: TextStyle, plain = false) {
  if (style === around || !withoutWordMarks(words).trim()) return words;
  const [before, inner, after] = plain ? textEdges(words) : ["", words, ""];
  return `${before}${textStyleMark(style)}${inner}${textStyleMark(around)}${after}`;
}

/** A block element's color where the document's colors are read (HtmlContext). */
export function blockColor(element: Element | null | undefined, context: HtmlContext) {
  return context.breaks ? "" : htmlBlockColor(element);
}

/**
 * A block's lines: as written where its style keeps its spaces (keptLines),
 * else with their spaces collapsed as collapseWords keeps them, their edges
 * trimmed and blank ones left out — but a line holding an element that
 * leaves no words alone (WORDS_GAP: a script's line, a comment between line
 * breaks), which is an empty line, as Notion's HTML import keeps one, when
 * words stand on each side of it.
 */
function blockLines(text: string, spacing: HtmlSpacing) {
  const lines = text.split("\n");
  if (spacing === "keep") return keptLines(lines.map(withoutWordMarks));
  // A line break inside an element that is no formatting (a <span>) leaves the spaces on its sides as they are,
  // and such line breaks side by side an empty line, as Notion's HTML import has them (seen 2026-11-13).
  const kept = lines.map((line) => ({
    words: collapseWords(line), empty: line.includes(WORDS_GAP) || /^[ \t]*\u0002[ \t\u0002]*$/.test(line),
    lead: /^\u0002+[ \t]/.test(line), tail: /[ \t]\u0002+$/.test(line),
  })).filter(({ words, empty }) => words || empty);
  while (kept.length && !kept[0]!.words) kept.shift();
  while (kept.length && !kept[kept.length - 1]!.words) kept.pop();
  const last = kept.length - 1;
  // A line break next to an empty line is a character reference, as Markdown ends a paragraph at an empty line.
  return kept.map(({ words, lead, tail }, index) => (index === 0 ? "" : words && kept[index - 1]!.words ? "\n" : "&#10;")
    + (words && lead && index > 0 ? "&#32;" : "") + words + (words && tail && index < last ? "&#32;" : "")).join("");
}

/** A block's words; a heading's keep the spaces only an element that leaves no words keeps (htmlImportSpacing.ts). */
export function paragraphText(node: Node, context: HtmlContext, heading = false) {
  const words = inlineHtmlText(node, context);
  return blockLines(heading ? words.split(WORDS_EDGE).join("") : words, context.spacing(node instanceof Element ? node : node.parentElement));
}

export function nodesText(nodes: Node[], context: HtmlContext) {
  return blockLines(nodes.map((node) => inlineHtmlText(node, context)).join(""), context.spacing(nodes[0]?.parentElement ?? null));
}

/** A heading's or a table cell's words on one Markdown line, its line breaks as character references. */
export function oneLine(text: string) {
  return text.replace(/\n/g, "&#10;");
}

/**
 * A paragraph's parts between the <br>s right inside it, where a <br> ends a
 * paragraph (HtmlContext.breaks), and at the elements that end it there
 * (htmlImportLineElements.ts lineParts), each a part of its own.
 */
export function paragraphParts(nodes: Iterable<Node>, context: HtmlContext) {
  if (!context.breaks) return [Array.from(nodes)];
  const parts: Node[][] = [];
  for (const part of lineParts(nodes, true)) {
    if (isElementPart(part)) {
      parts.push(part);
      continue;
    }
    parts.push([]);
    for (const node of part.flatMap(codeLineParts)) {
      if (node instanceof Element && node.tagName === "BR") parts.push([]);
      else parts[parts.length - 1]!.push(node);
    }
  }
  return parts.length ? parts : [[]];
}

/**
 * Parts of words as paragraphs, a picture's or a frame's part its block, the
 * pictures and files each part holds after it.
 */
export function paragraphBlocks(parts: Node[][], indent: number, blocks: Block[], context: HtmlContext, color = "") {
  for (const part of parts) {
    const media = isElementPart(part) && isMediaElement(part[0]!) ? mediaLine(part[0] as Element, context) : null;
    const text = media === null ? nodesText(part, context) : "";
    if (media) blocks.push({ prefix: media, text: "", indent });
    if (text) blocks.push({ prefix: "", text, indent, color });
    drainInto(blocks, context, indent);
  }
}

/**
 * A list item's or a quote's words are what it holds before its first
 * block, or its first paragraph's when it opens with one — up to a <br>
 * that ends the paragraph, the paragraph's next parts then paragraphs under
 * it, as Notion's HTML import reads them; its other blocks are under it, as
 * a Notion export writes an item's blocks inside it (2026-11-13: a line
 * under a bullet ran into the bullet's words).
 */
export function openingParts(parts: Node[], context: HtmlContext) {
  let index = parts.findIndex(isBlock);
  if (index < 0) index = parts.length;
  const own = parts.slice(0, index);
  let under: Node[][] = [];
  const opening = parts[index];
  if (opening instanceof Element && opening.tagName === "P" && !own.some((part) => collapse(part.textContent ?? ""))) {
    const [first, ...next] = paragraphParts(opening.childNodes, context);
    own.push(...first!);
    under = next;
    index += 1;
  }
  return { own, under, rest: parts.slice(index) };
}

/**
 * A list item's own words in a document that is no export, as Notion's HTML
 * import reads them (seen 2026-11-13): its text, the inline elements that
 * format it (ITEM_WORDS) and what a span holds; any other element right in
 * it after words — an <u>, a <mark>, a <label>, a <kbd>, a picture — is a
 * paragraph of its own under the item, the words after it the item's again
 * ("<li>글 <u>밑줄</u> 끝</li>" is the item "글 끝" holding the paragraph
 * "밑줄"); one opening the item is its words, and a math is words. Words of a
 * paragraph the item opens with are all its words. A script, a frame and such
 * stand in its words as htmlImportLineElements.ts reads them.
 */
const ITEM_WORDS = new Set(["B", "STRONG", "I", "EM", "A", "CODE", "S", "DEL", "STRIKE", "SUB", "SUP", "SMALL", "Q", "TIME", "BDO", "TT", "WBR", "BR", "MATH"]);

export function itemWords(nodes: Node[]) {
  const words: Node[] = [];
  const apart: Node[][] = [];
  const said = () => words.some((word) => (word.textContent ?? "").trim());
  const make = (name: string) => nodes[0]!.ownerDocument!.createElement(name);
  const read = (node: Node) => {
    // A script is a line break; a frame, a video and other line elements an empty line, their words or block under the item.
    if (tagOf(node) === "SCRIPT") words.push(make("br"));
    else if (isLineElement(node) || node instanceof Element && tagOf(node) !== "IMG" && isMediaElement(node)) {
      words.push(make("br"), node.ownerDocument!.createComment(""), make("br"));
      apart.push([node]);
    } else if (!(node instanceof Element) || ITEM_WORDS.has(tagOf(node)) || SKIPPED_TAGS.has(node.tagName)) words.push(node);
    else if (node.tagName === "SPAN" && !htmlUnderline(node, false)) node.childNodes.forEach(read);
    else if (said()) {
      apart.push([node]);
      words.push(node.ownerDocument.createComment(""));
    }
    else words.push(node);
  };
  for (const node of nodes) {
    if (node.parentElement?.tagName === "LI") read(node);
    else words.push(node);
  }
  return { words, apart };
}

/** Images and files found in a block follow it as their own lines. */
export function drainInto(blocks: Block[], context: HtmlContext, indent: number) {
  for (const line of context.trailing.splice(0)) blocks.push({ prefix: line, text: "", indent });
}
