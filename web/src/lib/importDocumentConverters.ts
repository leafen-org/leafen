import { importFileLine, isRelativeImportHref, type ImportReferenceMap } from "./importReferences";
import { blockTypeForPastedAssetUrl } from "@/components/editor/mediaEmbeds";
import { exportedCodeLanguageFence } from "@/components/editor/codeLanguages";
import { documentSpacing, KEPT_SPACES_MARK, keptLines, withoutWordMarks } from "./htmlImportSpacing";
import { escapeImportMarkdown } from "./importMarkdownText";
import { calloutMark, htmlCallout } from "./htmlImportCallouts";
import { boxMode, boxParts, holdsCheckbox, itemCheckbox, leadBox, tailBoxes, todoWords, withoutBoxGaps, type ItemCheckbox } from "./htmlImportCheckboxes";
import { htmlBlockColor, PLAIN_TEXT } from "./htmlImportColors";
import { blocksToMarkdown, collapse, wrapInline, type Block } from "./importMarkdownBlocks";
import {
  blockColor, drainInto, hasBlockChild, inlineHtmlText, isBlock, isSkipped, itemWords, nodesText, oneLine, openingParts, paragraphBlocks,
  notionEquationMarkdown, paragraphParts, paragraphText, SKIPPED_TAGS, withBoxWords, withLineWords, type HtmlContext,
} from "./htmlImportText";
import { lineParts } from "./htmlImportLineElements";
import { holdsImageTable, imageTableBlocks } from "./htmlImportImageTables";
import { withoutPageHead } from "./htmlImportPageHead";
import { columnListBlocks, holdsIndented, isColumnList, pageLinkBlock } from "./htmlImportLayout";

/**
 * Turns HTML documents into the block Markdown that the workspace importer
 * reads (importMarkdownBlocks.ts; a Word document's, docxImportConverter.ts):
 * headings (a fifth level and below become the fourth, as in Notion's
 * import, seen 2026-11-05), paragraphs, bulleted, numbered and to-do lists,
 * toggles, quotes, code, dividers, tables, and images, their words as
 * htmlImportText.ts reads them. A block's blocks stay under it.
 *
 * What Markdown has no form for, the importer reads from a link with no
 * words to an address of its own (workspace_import_markdown_marks.go): a
 * list item opening with [](leafen-toggle:) is a toggle, and with
 * [](leafen-toggle:2) a toggle heading of that level, its blocks under it,
 * and with [](leafen-callout:<color>,<icon>) a callout, its words its first
 * line (htmlImportCallouts.ts); a table whose first cell opens with [](leafen-table:header-column) has the
 * headers it names, none when it names none (a Markdown table has a header
 * row only); a line opening with [](leafen-block:<kind>) and holding a link
 * is a block of that kind at the link's address, a file named by the link's
 * words, and [](leafen-block:table_of_contents) alone an export's table of
 * contents (contentsLine); a line opening with [](leafen-caption:) right
 * after a code block, an image, a file or such a block is that block's
 * caption; words opening with [](leafen-spaces:) keep the spaces at their
 * ends (keptLines);
 * and, in an export, words opening with [](leafen-color:<color>) are a block
 * of that color, and ![](leafen-text-color:<color>,<background>) gives the
 * words after it those colors, and an underline too with ",underline" after
 * them (htmlImportColors.ts).
 */

const NO_REFERENCES: ImportReferenceMap = new Map();
const CAPTION_MARK = "[](leafen-caption:) ";
const CONTENTS_MARK = "[](leafen-block:table_of_contents)";

function htmlBlocks(nodes: Iterable<Node>, indent: number, blocks: Block[], context: HtmlContext) {
  let inline: Node[] = [];
  const drain = () => drainInto(blocks, context, indent);
  const flush = () => {
    if (boxedWordsBlocks(inline, inline[0]?.parentElement ?? null, false, indent, blocks, context)) {
      // Boxes made the words' blocks.
    } else if (context.breaks) paragraphBlocks(lineParts(inline, true), indent, blocks, context);
    else {
      const text = nodesText(inline, context);
      if (text) blocks.push({ prefix: "", text, indent });
    }
    inline = [];
    drain();
  };
  // The paragraph last read, which a box of its blocks may follow (holdsIndented).
  let held: { element: Element; block: Block } | null = null;
  for (const child of nodes) {
    if (!isBlock(child)) {
      if (!(child instanceof Element) || !isSkipped(child, context)) inline.push(child);
      continue;
    }
    flush();
    const tag = child.tagName, before = blocks.length;
    if (!context.breaks && holdsIndented(child, held, blocks)) {
      htmlBlocks(child.childNodes, indent + 1, blocks, context);
    } else if (!context.breaks && isColumnList(child)) {
      columnListBlocks(child, indent, blocks, (inside, at) => htmlBlocks(inside, at, blocks, context));
    } else if (!context.breaks && tag === "FIGURE" && child.classList.contains("link-to-page")) {
      const line = pageLinkBlock(child, paragraphText(child, context), indent);
      if (line) blocks.push(line);
    } else if (/^H[1-6]$/.test(tag)) {
      const level = Math.min(Number(tag[1]), 4);
      blocks.push({ prefix: `${"#".repeat(level)} `, text: oneLine(headingText(child, context)), indent, color: htmlBlockColor(child) });
    } else if (tag === "UL" || tag === "OL") {
      for (const item of Array.from(child.children).filter((candidate) => candidate.tagName === "LI")) {
        listItemBlocks(item, tag === "OL", indent, blocks, context);
      }
    } else if (tag === "DETAILS") {
      toggleBlocks(child, indent, blocks, context);
    } else if (tag === "ASIDE" || child.classList.contains("callout") && !context.breaks) {
      asideBlocks(child, indent, blocks, context);
    } else if (tag === "FIGURE" && (webBlockLine(child) || Array.from(child.children).some((part) => part.tagName === "FIGCAPTION"))) {
      figureBlocks(child, indent, blocks, context);
    } else if (tag === "BLOCKQUOTE") {
      quoteBlocks(child, indent, blocks, context);
    } else if (tag === "PRE") {
      const code = child.querySelector("code");
      const language = exportedCodeLanguageFence(/language-([\w+-]+)/.exec(code?.className ?? child.className)?.[1] ?? "");
      blocks.push({ code: (code ?? child).textContent ?? "", language, indent });
    } else if (tag === "HR" || notionEquationMarkdown(child)) {
      blocks.push({ prefix: tag === "HR" ? "---" : "", text: tag === "HR" ? "" : notionEquationMarkdown(child)!, indent });
    } else if (tag === "TABLE") {
      tableBlock(child, indent, blocks, context);
    } else if (contentsLine(child, context)) {
      blocks.push({ prefix: CONTENTS_MARK, text: "", indent, color: htmlBlockColor(child) });
    } else if (hasBlockChild(child)) {
      htmlBlocks(child.childNodes, indent, blocks, context);
    } else if (!loneFileLink(child, context) && !boxedWordsBlocks(Array.from(child.childNodes), child, tag === "P", indent, blocks, context)) {
      const text = tag === "P" || context.breaks ? "" : paragraphText(child, context);
      // An export's empty paragraph is one, as an export writes its line
      // (Notion's "<p id=…>⏎</p>") and Notion's API import keeps it; one of
      // no block's ID is none: its page's empty description, or the one a
      // reader makes of the "</p>" closing Notion's paragraph that holds its
      // indented blocks.
      if (tag === "P" && !context.breaks && child.id && !child.children.length && !child.textContent?.trim()) {
        blocks.push({ prefix: "", text: KEPT_SPACES_MARK, indent, color: blockColor(child, context) });
      } else if (tag === "P") paragraphBlocks(paragraphParts(child.childNodes, context), indent, blocks, context, blockColor(child, context));
      else if (context.breaks) paragraphBlocks(lineParts(child.childNodes, false), indent, blocks, context);
      else if (text) blocks.push({ prefix: "", text, indent });
    }
    held = tag === "P" && blocks.length === before + 1 ? { element: child, block: blocks[before]! } : null;
    drain();
  }
  flush();
}

/**
 * Words a checkbox is in, of a document that is no export, as the block
 * holding them makes them (htmlImportCheckboxes.ts boxMode): split at their
 * boxes into paragraphs — a paragraph's at its <br>s too — and empty to-dos,
 * code after a formatted part read in its line a code block (boxParts);
 * a to-do of them when a box opens them, its other boxes line breaks; else
 * their paragraph, each box its mark on a line, or left out in a toggle. A
 * paragraph's words are its own; others are words between blocks, or a
 * block's that holds no block. False when they hold no box.
 */
function boxedWordsBlocks(nodes: Node[], holder: Element | null, paragraph: boolean, indent: number, blocks: Block[], context: HtmlContext) {
  if (!context.breaks || !holdsCheckbox(nodes)) return false;
  const words = (part: Node[], at = indent) => {
    if (paragraph) paragraphBlocks(paragraphParts(part, context), at, blocks, context);
    else paragraphBlocks([part], at, blocks, context);
  };
  const mode = boxMode(holder);
  const lead = mode === "todo" ? leadBox(nodes) : null;
  if (mode === "split") {
    for (const part of boxParts(nodes, holder?.tagName === "BODY" ? "body" : paragraph ? "paragraph" : "other")) {
      if (Array.isArray(part) && part.tail) {
        const text = tailBoxes(withBoxWords(context, "tail", () => nodesText(part, context)));
        if (text) blocks.push({ prefix: "", text, indent });
        drainInto(blocks, context, indent);
      } else if (Array.isArray(part) && part.code) {
        const code = part[0]?.textContent ?? "";
        if (code.trim()) blocks.push({ code, language: "", indent });
      } else if (Array.isArray(part)) words(part, part.under ? indent + 1 : indent);
      else {
        const said = part.words ? withoutBoxGaps(withBoxWords(context, "gap", () => nodesText(part.words!, context))) : "";
        // A to-do's words opening with a space keep it (htmlImportCheckboxes.ts spanBoxParts).
        const text = said && part.spaced ? `${KEPT_SPACES_MARK}&#32;${said}` : said;
        blocks.push({ prefix: `- [${part.checked ? "x" : " "}] `, text: text || KEPT_SPACES_MARK, indent });
        drainInto(blocks, context, indent + 1);
      }
    }
  } else if (lead) {
    const text = context.spacing(holder) === "keep" ? withBoxWords(context, "break", () => nodesText(nodes, context))
      : todoWords(withBoxWords(context, "todo", () => nodes.map((node) => inlineHtmlText(node, context)).join("")));
    blocks.push({ prefix: `- [${lead.checked ? "x" : " "}] `, text: text || KEPT_SPACES_MARK, indent });
  } else {
    withBoxWords(context, mode === "none" ? "none" : "line", () => words(nodes));
  }
  return true;
}

/**
 * A heading's words; a box in a document's heading is left out with the
 * spaces around it, and a script's or a drawing's place too, its words kept
 * (htmlImportLineElements.ts).
 */
function headingText(heading: Element, context: HtmlContext) {
  const words = withLineWords(context, "heading", () => !context.breaks || !holdsCheckbox([heading]) ? paragraphText(heading, context, true)
    : withBoxWords(context, "gap", () => paragraphText(heading, context, true)));
  return withoutBoxGaps(words);
}

/**
 * An export's table of contents, Notion's (nav.table_of_contents) or
 * Leafen's (nav.table-of-contents), is the block, in its color, its headings'
 * links left out as the block draws them, as Notion's API import has it
 * (2026-11-13: it came as a paragraph of each heading's words). Any other
 * document's is the paragraphs of its words, as Notion's HTML import reads it.
 */
function contentsLine(nav: Element, context: HtmlContext) {
  return nav.tagName === "NAV" && !context.breaks && nav.matches(".table_of_contents, .table-of-contents");
}

/**
 * A list item, its words and blocks as openingParts reads them, any other
 * document's words as itemWords reads them (ownItemBlocks). A checkbox,
 * Notion's drawn one too, makes an export's a to-do (a box splits any other
 * document's, boxedItemBlocks); the space after it is not the to-do's; a
 * to-do without words has the spaces mark for them, as a box alone is a
 * to-do of the words "[ ]" (workspace_import_markdown_task_box.go). Its
 * color is its own or its list's; a table in it as itemTable reads it.
 */
function listItemBlocks(item: Element, ordered: boolean, indent: number, blocks: Block[], context: HtmlContext) {
  const boxed = context.breaks ? itemCheckbox(item) : null;
  if (boxed) return boxedItemBlocks(boxed, ordered, indent, blocks, context);
  const all = Array.from(item.childNodes);
  const box = all.find((part): part is Element => part instanceof Element && part.matches("input[type=checkbox], div.checkbox"));
  const { own, under, rest } = openingParts(all.filter((part) => part !== box && !(part === box?.nextSibling && !part.textContent?.trim())), context);
  const color = blockColor(item, context) || blockColor(item.parentElement, context);
  const { words, apart } = ownItemWords(own, context);
  // A box in a formatted part is left out with the spaces around it, the item a to-do when it opens the words.
  const gaps = context.breaks && !box && holdsCheckbox(words);
  const lead = gaps ? leadBox(words) : null;
  const checked = box instanceof HTMLInputElement ? box.checked || box.hasAttribute("checked") : box ? box.classList.contains("checkbox-on") : lead?.checked;
  const prefix = box || lead ? `- [${checked ? "x" : " "}] ` : ordered ? "1. " : "- ";
  const said = gaps ? withoutBoxGaps(withBoxWords(context, "gap", () => nodesText(words, context))) : nodesText(words, context);
  const text = said || ((box || lead) && !color ? KEPT_SPACES_MARK : "");
  const { table, after } = itemTable(rest, !text && !apart.length && !under.length, context);
  const block = { prefix, text: table && !isColumnTable(table) ? tableLines(table, context) : text, indent, color };
  blocks.push(block);
  ownItemBlocks(apart, under, [], indent, blocks, context);
  if (table && isColumnTable(table)) joinColumnTable(block, table, indent, blocks, context);
  htmlBlocks(after, indent + 1, blocks, context);
}

/**
 * A table in a list item or a quote of a document that is no export, as
 * Notion's HTML import reads one (seen 2026-11-13): one opening it is its
 * cells' words (tableLines), and the words of one of a single column right
 * after its words join them (joinColumnTable), a divider for each row inside
 * it. Where Notion runs cells' words together, each cell's words are a line
 * of their own, as workspace_import_markdown_item_tables.go reads Markdown.
 * Gives that table, none for any other, and what follows.
 */
function itemTable(rest: Node[], opening: boolean, context: HtmlContext) {
  const at = rest.findIndex((node) => node.nodeType !== Node.TEXT_NODE || (node.textContent ?? "").trim());
  const table = context.breaks && rest[at] instanceof Element && rest[at].tagName === "TABLE" ? rest[at] : null;
  return table && (opening || isColumnTable(table)) ? { table, after: rest.slice(at + 1) } : { table: null, after: rest };
}

function isColumnTable(table: Element) {
  return Array.from(table.querySelectorAll("tr")).every((row) => row.children.length <= 1);
}

/** A table's cells' words, a line each. */
function tableLines(table: Element, context: HtmlContext) {
  return Array.from(table.querySelectorAll("td, th")).map((cell) => paragraphText(cell, context)).filter(Boolean).join("\n");
}

/** A table of one column's cells' words joined with a block's words, a line each, a divider for each row inside the block. */
function joinColumnTable(block: { text: string }, table: Element, indent: number, blocks: Block[], context: HtmlContext) {
  for (const row of Array.from(table.querySelectorAll("tr"))) {
    const words = row.firstElementChild ? withBoxWords(context, "none", () => paragraphText(row.firstElementChild!, context)) : "";
    if (words) block.text = block.text ? `${block.text}\n${words}` : words;
    blocks.push({ prefix: "---", text: "", indent: indent + 1 });
  }
}

/** A list item's own words, and the words under it (itemWords) in any document but an export. */
function ownItemWords(own: Node[], context: HtmlContext): { words: Node[]; apart: Node[][] } {
  return context.breaks ? itemWords(own) : { words: own, apart: [] };
}

/**
 * What a list item holds after its words: the pictures in them, words of its
 * own apart, a paragraph's next parts, then its blocks.
 */
function ownItemBlocks(apart: Node[][], under: Node[][], rest: Node[], indent: number, blocks: Block[], context: HtmlContext) {
  drainInto(blocks, context, context.breaks ? indent + 1 : indent);
  for (const part of apart) {
    paragraphBlocks([part], indent + 1, blocks, context);
    drainInto(blocks, context, indent + 1);
  }
  paragraphBlocks(under, indent + 1, blocks, context);
  htmlBlocks(rest, indent + 1, blocks, context);
}

/**
 * A document's list item a checkbox splits (itemCheckbox): the words before
 * the box as openingParts reads them, an empty to-do's when there are none,
 * and what follows it under it, its words a paragraph up to each <br> or box.
 */
function boxedItemBlocks({ checked, before, after }: ItemCheckbox, ordered: boolean, indent: number, blocks: Block[], context: HtmlContext) {
  const { own, under, rest } = openingParts(before, context);
  const { words: said, apart } = ownItemWords(own, context);
  const text = nodesText(said, context);
  const todo = !text && !apart.length && !under.length && !rest.length;
  blocks.push({ prefix: todo ? `- [${checked ? "x" : " "}] ` : ordered ? "1. " : "- ", text: todo ? KEPT_SPACES_MARK : text, indent });
  ownItemBlocks(apart, under, rest, indent, blocks, context);
  let words: Node[] = [];
  const flush = () => {
    paragraphBlocks(paragraphParts(words, context), indent + 1, blocks, context);
    drainInto(blocks, context, indent + 1);
    words = [];
  };
  for (const node of after) {
    if (!isBlock(node) && !(node instanceof Element && node.matches("input[type=checkbox i]"))) words.push(node);
    else {
      flush();
      if (isBlock(node)) htmlBlocks([node], indent + 1, blocks, context);
    }
  }
  flush();
}

/** A quote, its words and blocks as openingParts reads them, the pictures in its words under it, a table as itemTable reads it. */
function quoteBlocks(quote: Element, indent: number, blocks: Block[], context: HtmlContext) {
  const { own, under, rest } = openingParts(Array.from(quote.childNodes), context);
  const words = nodesText(own, context);
  const { table, after } = itemTable(rest, !words && !under.length, context);
  const at = blocks.length;
  const block = { prefix: "> ", text: table && !isColumnTable(table) ? tableLines(table, context) : words, indent, color: blockColor(quote, context) };
  blocks.push(block);
  drainInto(blocks, context, context.breaks ? indent + 1 : indent);
  paragraphBlocks(under, indent + 1, blocks, context);
  if (table && isColumnTable(table)) joinColumnTable(block, table, indent, blocks, context);
  htmlBlocks(after, indent + 1, blocks, context);
  if (!block.text && blocks.length === at + 1) blocks.pop();
}

/**
 * A toggle, a toggle heading when its summary is a heading (as Notion's
 * export writes one), with its blocks under it (2026-11-13: its summary and
 * blocks came as lines of the page, a sub-page in it at the page's top level).
 */
function toggleBlocks(details: Element, indent: number, blocks: Block[], context: HtmlContext) {
  const summary = Array.from(details.children).find((part) => part.tagName === "SUMMARY");
  const heading = summary?.querySelector("h1, h2, h3, h4, h5, h6");
  const level = heading ? Math.min(Number(heading.tagName[1]), 4) : "";
  const text = summary ? withBoxWords(context, context.breaks ? "inline" : "none", () => paragraphText(heading ?? summary, context, Boolean(heading))) : "";
  blocks.push({ prefix: `- [](leafen-toggle:${level}) `, text, indent, color: blockColor(details, context) });
  htmlBlocks(Array.from(details.childNodes).filter((part) => part !== summary), indent + 1, blocks, context);
}

/**
 * A callout (htmlCallout) with its words and blocks as openingParts reads
 * them — its words its first line, as Notion's API import has a callout's
 * (2026-11-13: an export's callouts came back as paragraphs). Any other
 * aside is a paragraph of its words (asideLines), its pictures after it.
 */
function asideBlocks(aside: Element, indent: number, blocks: Block[], context: HtmlContext) {
  const callout = htmlCallout(aside, !context.breaks, context.references);
  if (!callout) {
    const lines = asideLines(aside.childNodes, context, []);
    if (lines.length) blocks.push({ prefix: "", text: keptLines(lines), indent });
    drainInto(blocks, context, indent);
    return;
  }
  const { own, under, rest } = openingParts(callout.body, context);
  blocks.push({ prefix: `- ${calloutMark(callout)} `, text: nodesText(own, context), indent });
  drainInto(blocks, context, indent);
  paragraphBlocks(under, indent + 1, blocks, context);
  htmlBlocks(rest, indent + 1, blocks, context);
}

/**
 * An aside's words, as Notion's HTML import reads an aside that is no
 * callout into one paragraph (seen 2026-11-13): its blocks' words a line
 * each — a heading's bold, code as code, a table row's cells run together,
 * a divider none — and a list's items a line each (listLines).
 */
function asideLines(nodes: Iterable<Node>, context: HtmlContext, lines: string[]) {
  let inline: Node[] = [];
  const flush = () => {
    lines.push(...withoutWordMarks(inline.map((node) => inlineHtmlText(node, context)).join("")).split("\n").map(collapse).filter(Boolean));
    inline = [];
  };
  for (const node of nodes) {
    if (!isBlock(node)) {
      if (!(node instanceof Element) || !SKIPPED_TAGS.has(node.tagName)) inline.push(node);
      continue;
    }
    flush();
    const tag = node.tagName;
    if (/^H[1-6]$/.test(tag)) lines.push(...asideLines(node.childNodes, context, []).map((line) => wrapInline(line, "**")));
    else if (tag === "UL" || tag === "OL") listLines(node, 0, context, lines);
    else if (tag === "PRE") {
      const code = (node.textContent ?? "").split("\n").map((line) => line.trim()).filter(Boolean);
      lines.push(...code.map((line) => (line.includes("`") ? escapeImportMarkdown(line) : `\`${line}\``)));
    } else if (tag === "TABLE") {
      for (const row of Array.from(node.querySelectorAll("tr"))) {
        const cells = Array.from(row.children).map((cell) => asideLines(cell.childNodes, context, []).join(""));
        if (cells.join("")) lines.push(cells.join(""));
      }
    } else if (tag !== "HR") asideLines(node.childNodes, context, lines);
  }
  flush();
  return lines;
}

/**
 * A list's items as lines of an aside's words: "• " or the item's number
 * before its words, and a list in an item after a blank line, four spaces
 * further in for each level, "◦ " before its bulleted items' words.
 */
function listLines(list: Element, depth: number, context: HtmlContext, lines: string[]) {
  const nested = (node: Node) => node instanceof Element && (node.tagName === "UL" || node.tagName === "OL");
  const items = Array.from(list.children).filter((item) => item.tagName === "LI");
  for (const [index, item] of items.entries()) {
    const words = asideLines(Array.from(item.childNodes).filter((node) => !nested(node)), context, []);
    if (depth > 0 && index === 0 && lines.length) lines.push("");
    const mark = list.tagName === "OL" ? `${index + 1}. ` : depth > 0 ? "◦ " : "• ";
    lines.push(`${"    ".repeat(depth)}${mark}${words[0] ?? ""}`, ...words.slice(1));
    for (const inner of Array.from(item.childNodes).filter(nested)) listLines(inner as Element, depth + 1, context, lines);
  }
}

/**
 * A figure's caption is its block's when it holds a code block, an image, a
 * file or a block at a web address (2026-11-13: a code block's came as a
 * line after it); else a line.
 */
function figureBlocks(figure: Element, indent: number, blocks: Block[], context: HtmlContext) {
  const caption = Array.from(figure.children).find((part) => part.tagName === "FIGCAPTION");
  const bare = figure.cloneNode(true) as Element;
  bare.querySelector(":scope > figcaption")?.remove();
  const before = blocks.length;
  const web = webBlockLine(bare);
  if (web) blocks.push({ prefix: web, text: "", indent });
  else htmlBlocks([bare], indent, blocks, context);
  const last = blocks.length > before ? blocks[blocks.length - 1]! : undefined;
  const captioned = last && last.indent === indent && ("code" in last
    || "prefix" in last && !last.text && /^(?:!\[|\[(?:\\.|[^\]\\])*\]\(leafen-(?:file|block):)/.test(last.prefix));
  const text = caption ? paragraphText(caption, context) : "";
  if (text) blocks.push({ prefix: captioned ? CAPTION_MARK : "", text, indent });
  drainInto(blocks, context, indent);
}

/**
 * A figure that is a block at a web address, as the line that marks it: a
 * bookmark (Notion's export links its card as one, Leafen's names its figure
 * one); an outside file (Notion's marks its link as a file's, Leafen's names
 * its figure one); and Notion's other blocks that link their address as the
 * figure's source — a PDF, a video, an audio, an embed — the block a pasted
 * address of its kind makes (blockTypeForPastedAssetUrl), else an embed
 * (2026-11-13: each came as a link line, its caption a line after it), and
 * Leafen's link preview, whose figure its class names. A file keeps the name
 * its link shows, when that is not its address.
 */
function webBlockLine(figure: Element) {
  const named = figure.matches(".bookmark, .file") ? figure.querySelector(":scope > a[href]") : null;
  const link = named ?? figure.querySelector(":scope > a.bookmark[href], :scope > div.source > a[href]");
  const address = link?.getAttribute("href")?.trim() ?? "";
  if (!link || !/^https?:\/\/[^\s<>\\]+$/i.test(address)) return null;
  const kind = link.classList.contains("bookmark") || figure.classList.contains("bookmark") ? "bookmark"
    : figure.classList.contains("link-preview") ? "link_preview"
    : link.hasAttribute("data-notion-file") ? "file"
    : blockTypeForPastedAssetUrl(address) ?? (named ? "file" : "embed");
  const words = collapse(link.textContent ?? "");
  const name = kind === "file" && words !== address ? escapeImportMarkdown(words) : "";
  return `[](leafen-block:${kind}) [${name}](<${address}>)`;
}


/**
 * A table keeps the headers its cells mark: a header row when its first
 * row's cells are all headers (or it is the table's head), and a header
 * column when each other row starts with one (as Notion's export writes
 * them, 2026-11-13). One of a single column in a document that is no export
 * is its cells' blocks (columnTableBlocks), and one holding a picture there
 * an inline database (htmlImportImageTables.ts).
 */
function tableBlock(table: Element, indent: number, blocks: Block[], context: HtmlContext) {
  const lines = Array.from(table.querySelectorAll("tr"));
  if (context.breaks && lines.length && lines.every((line) => line.children.length <= 1)) return columnTableBlocks(lines, indent, blocks, context);
  if (holdsImageTable(table, context)) return imageTableBlocks(table, indent, blocks, context);
  const rows = lines
    .map((row) => ({ row, cells: Array.from(row.children).map((cell) => oneLine(paragraphText(cell, context))) }))
    .filter(({ cells }) => cells.some(Boolean));
  if (!rows.length) return;
  const first = rows[0]!.row;
  const row = first.parentElement?.tagName === "THEAD" || Array.from(first.children).every((cell) => cell.tagName === "TH");
  const body = row ? rows.slice(1) : rows;
  const column = body.length > 0 && body.every(({ row: line }) => line.firstElementChild?.tagName === "TH");
  blocks.push({ rows: rows.map(({ cells }) => cells), headers: { row, column }, indent });
}

/**
 * A table of one column, as Notion's HTML import reads one (seen
 * 2026-11-13): each row is its cell's blocks and a divider after them, an
 * empty cell its divider alone (workspace_import_markdown_column_table.go
 * reads a Markdown one so). The cell's words, and a paragraph's in it, are
 * a paragraph for each text and each element among them, inline code a code
 * block, a <br> between them none and a checkbox nothing; its other blocks
 * are as they are.
 */
function columnTableBlocks(rows: Element[], indent: number, blocks: Block[], context: HtmlContext) {
  for (const row of rows) {
    const cell = row.firstElementChild;
    if (cell) withBoxWords(context, "none", () => columnCellBlocks(Array.from(cell.childNodes), indent, blocks, context));
    blocks.push({ prefix: "---", text: "", indent });
  }
}

function columnCellBlocks(nodes: Node[], indent: number, blocks: Block[], context: HtmlContext) {
  for (const node of nodes) {
    if (node instanceof Element && node.tagName === "P") columnCellBlocks(Array.from(node.childNodes), indent, blocks, context);
    else if (isBlock(node)) htmlBlocks([node], indent, blocks, context);
    else if (node instanceof Element && (node.tagName === "CODE" || node.tagName === "TT")) {
      const code = node.textContent ?? "";
      if (code.trim()) blocks.push({ code, language: "", indent });
    } else if (!(node instanceof Element && node.tagName === "BR")) {
      const text = nodesText([node], context);
      if (text) blocks.push({ prefix: "", text, indent });
      drainInto(blocks, context, indent);
    }
  }
}

/**
 * A block holding one link to a file alone is that file, its link's words
 * left out, as a Markdown line holding one is (rewriteMarkdownReferences):
 * an export's file block (2026-11-13: brought back, its name came again as a
 * line above it).
 */
function loneFileLink(block: Element, context: HtmlContext) {
  const links = block.querySelectorAll("a[href]");
  const link = links[0];
  if (links.length !== 1 || !link || block.querySelector("img") || (block.textContent ?? "").trim() !== (link.textContent ?? "").trim()) return false;
  const reference = context.references.get(link.getAttribute("href")?.trim() ?? "");
  if (reference?.kind !== "file") return false;
  context.trailing.push(importFileLine(reference, ""));
  return true;
}

/** The relative links and images an HTML document refers to. */
export function htmlReferenceHrefs(html: string) {
  const document = new DOMParser().parseFromString(html, "text/html");
  const hrefs = new Set<string>();
  for (const element of Array.from(document.querySelectorAll("img[src], a[href]"))) {
    const href = (element.getAttribute("src") ?? element.getAttribute("href") ?? "").trim();
    if (isRelativeImportHref(href)) hrefs.add(href);
  }
  return [...hrefs];
}

export function htmlToImportMarkdown(html: string, references: ImportReferenceMap = NO_REFERENCES) {
  const document = new DOMParser().parseFromString(html, "text/html");
  const blocks: Block[] = [];
  // Notion's export draws its page as an article holding the page's body.
  const breaks = !document.querySelector("article.page > div.page-body");
  // An export draws a linked page's icon before the link's words, Notion's
  // and Leafen's alike (seen 2026-11-14: <a …><img class="icon" …/>title</a>,
  // an emoji's <span class="icon" data-emoji>): the page's, not the line's.
  // The page's own icon and cover are the page's too (pageHeadLine).
  if (!breaks) {
    document.querySelectorAll("div.page-body a > .icon:first-child").forEach((icon) => icon.remove());
    withoutPageHead(document);
  }
  htmlBlocks(document.body.childNodes, 0, blocks, {
    references, trailing: [], spacing: documentSpacing(document), breaks, style: PLAIN_TEXT, box: breaks ? "line" : "none", lines: "lines",
  });
  return blocksToMarkdown(blocks);
}
