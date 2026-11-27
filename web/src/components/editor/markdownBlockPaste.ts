import type { BlockContent, BlockType } from "@/lib/types";
import { pastedCodeLanguage } from "./codeLanguages";
import { closeUpWordSpaces, createPastedBlock, underlinedPieces } from "./markdownPasteBlocks";
import {
  assertMarkdownPasteChildren,
  consumeMarkdownPasteInput,
  createMarkdownPasteContext,
  type MarkdownPasteBounds,
  type MarkdownPasteContext,
} from "./markdownPasteBudget";
import type { PastedBlock, PastedEntry } from "./markdownPasteTypes";
import {
  addPiece,
  createItemWords,
  headItems,
  joinedParagraphs,
  joinItemWords,
  jointAfter,
  LIST_BLOCKS,
  placedIn,
  underline,
} from "./markdownItemWords";
import { parseInlineMarkdownWithin, spansFromMarkdownTokens } from "./markdownInlinePaste";
import { markdownBlockTokens } from "./markdownLexer";
import { readQuoteLines } from "./markdownQuotePaste";
import {
  isMarkdownTableRow,
  isMarkdownTableSeparator,
  markdownTableWithin,
} from "./markdownTablePaste";
import { safePasteAssetUrl } from "./markdownPasteSyntax";
import {
  codeFenceStart,
  columnsOf,
  dropColumns,
  heldBy,
  type Holder,
  isCodeFenceEnd,
  itemColumn,
  lineDepth,
  parseMarkdownImageLine,
  stripDepthIndent,
} from "./markdownPasteLines";
import { calloutContent, calloutIcon, isAsideClose, isAsideOpen } from "./notionPasteShapes";

function nestEntries(entries: PastedEntry[], context: MarkdownPasteContext) {
  const roots: PastedBlock[] = [];
  const stack: PastedEntry[] = [];
  for (const entry of entries) {
    while (stack.length > 0 && entry.depth <= stack[stack.length - 1].depth) stack.pop();
    const parent = stack[stack.length - 1]?.block;
    if (parent) {
      const nextCount = (parent.children?.length ?? 0) + 1;
      assertMarkdownPasteChildren(nextCount, context);
      parent.children = [...(parent.children ?? []), entry.block];
    } else roots.push(entry.block);
    stack.push(entry);
  }
  return roots;
}

function block(
  type: BlockType,
  text: string,
  content: BlockContent | undefined,
  depth: number,
  context: MarkdownPasteContext,
) {
  return createPastedBlock(
    type,
    text,
    content ?? { rich: parseInlineMarkdownWithin(text, context) },
    depth,
    context,
  );
}

// A line of standard Markdown, as Tiptap Markdown's lexer reads it: a
// heading, a list item or to-do, a divider or a paragraph. Anything else (a
// link definition, HTML) stays the text it was written as. `words` is its
// text's Markdown, which lines after it may go on. A list item whose words
// are a list item of their own ("- * a", "1. - a", "- -") is an empty one
// holding that one, `inner`, as Notion's paste reads it; a to-do's keeps
// them as its words ("- [ ] * a" is "a", seen 2026-11-13).
type LineRead = { read: PastedBlock; words: string; inner?: LineRead & { line: string } };

// A list item line's words, its mark and the spaces after it off.
const itemWordsOf = (line: string) => line.replace(/^(?:[-+*]|\d{1,9}[.)])[ \t]+/u, "");

/**
 * A list item line's to-do mark, as Notion's paste reads one (seen
 * 2026-11-13): the item's words, under any list mark, starting with "[ ]" or
 * "[x]" — never "[X]" ("- [X] a" is a bulleted "[X] a"). Spaces after it
 * end the mark; anything else, or nothing, leaves it in the words ("- [ ]"
 * is a to-do "[ ]", "1. [x]a" a done one "[x]a", "- [ ]⇥a" a to-do "[ ]⇥a").
 */
function toDoMark(line: string) {
  const words = itemWordsOf(line);
  const mark = /^\[([ x])\]( +(?=\S))?/u.exec(words);
  return mark ? { checked: mark[1] === "x", words: mark[2] ? words.slice(mark[0].length) : words } : null;
}

function standardLineBlock(line: string, depth: number, context: MarkdownPasteContext): LineRead {
  const token = markdownBlockTokens(line).find((candidate) => candidate.type !== "space");
  const spans = (tokens: unknown) => spansFromMarkdownTokens(Array.isArray(tokens) ? tokens : [], context);
  if (token?.type === "heading") {
    const level = Math.min(Math.max(token.depth ?? 1, 1), 4);
    return { read: block(`heading_${level}` as BlockType, token.text ?? "", { rich: spans(token.tokens) }, depth, context), words: "" };
  }
  if (token?.type === "list" && token.items?.[0]) {
    const item = token.items[0];
    const toDo = toDoMark(line);
    if (toDo) {
      // The lexer's own read where its words are these ("[ ] a", "[ ] * a").
      const rich = toDo.words === item.text ? spans(item.tokens) : parseInlineMarkdownWithin(toDo.words, context);
      return { read: block("to_do", toDo.words, { rich, checked: toDo.checked }, depth, context), words: toDo.words };
    }
    const type = token.ordered ? "numbered_list_item" : "bulleted_list_item";
    if (item.task) {
      // "[X] ", a to-do mark to the lexer, is words to Notion's paste.
      const words = itemWordsOf(line);
      return { read: block(type, words, undefined, depth, context), words };
    }
    // The lexer turns a tab in an item's first line into spaces up to the
    // next tab stop; Notion's paste keeps it ("- a⇥b" is "a⇥b", seen
    // 2026-11-14), so such words are read from the line.
    const own = itemWordsOf(line).trimEnd();
    const tabbed = own.includes("\t") && item.tokens?.[0]?.type !== "code";
    const words = tabbed ? own : item.text ?? "";
    if (item.tokens?.[0]?.type === "list") {
      return { read: block(type, "", { rich: [] }, depth, context), words: "", inner: { ...standardLineBlock(words, depth + 1, context), line: words } };
    }
    return { read: block(type, words, tabbed ? undefined : { rich: spans(item.tokens) }, depth, context), words };
  }
  if (token?.type === "hr") return { read: block("divider", "", { rich: [] }, depth, context), words: "" };
  if (token?.type === "paragraph") return { read: block("paragraph", line, { rich: spans(token.tokens) }, depth, context), words: line.trim() };
  return { read: block("paragraph", line, { rich: [{ text: line }] }, depth, context), words: line.trim() };
}

// Whether a line is more of the quote before it: a ">" line, or one that
// is only words — no block of its own, an equation's "$$" or a picture.
function quoteGoesOn(line: string) {
  if (line.startsWith(">")) return true;
  if (!line || line.startsWith("$$") || parseMarkdownImageLine(line) || isAsideOpen(line) || isAsideClose(line)) return false;
  return markdownBlockTokens(line).find((token) => token.type !== "space")?.type === "paragraph";
}

// Whether an "<aside>" line has its closing one further on.
function asideCloses(lines: readonly string[], start: number) {
  let open = 0;
  for (let index = start; index < lines.length; index++) {
    const line = lines[index] ?? "";
    if (isAsideOpen(line)) open++;
    else if (isAsideClose(line) && --open === 0) return true;
  }
  return false;
}

// An equation with nothing in it ("$$ $$", "$$$$", or "$$" lines around
// none): Notion's paste drops it, the lines around it staying apart (seen
// 2026-11-11); here its lines stay the words they were, a line of their own
// between those, as a pasted line of it does.
function emptyEquationWords(lines: readonly string[], depth: number, context: MarkdownPasteContext) {
  const words = lines.map(line => line.trim()).join("\n");
  return block("paragraph", words, { rich: [{ text: words }] }, depth, context);
}

// An equation whose lines run on from an opening line to one ending in
// "$$": its TeX, its ends' spaces and line breaks off, and the line after
// it; null when no line closes it or a line can't be one of it (words gives
// null). Notion's paste reads "$$ m", "n $$" as "m⏎n", and a list item's or
// a quote's "$$" lines as its words: "1. $$", "   w", "   $$" is an item "w"
// and "> $$", "> q", "> $$" a quote "q" (seen 2026-11-13).
function equationLines(lines: readonly string[], start: number, first: string, words: (line: string) => string | null) {
  const tex = [first];
  for (let index = start + 1; index < lines.length; index++) {
    const line = words(lines[index] ?? "");
    if (line === null) return null;
    if (line.trimEnd().endsWith("$$")) {
      tex.push(line.trimEnd().slice(0, -2));
      return { expression: tex.join("\n").trim(), next: index + 1 };
    }
    tex.push(line);
  }
  return null;
}

const SOFT_LINE_BLOCKS: ReadonlySet<BlockType> = new Set(["paragraph", "bulleted_list_item", "numbered_list_item", "to_do"]);

// The Markdown of a block's words over its lines, read again whole once
// they're all in, so a mark or an equation may run over them ("**a⏎b**",
// "$a⏎b$", "`a⏎b`" as "a b"), as Notion's paste reads one (seen 2026-11-13).
type SoftLines = { words: Map<PastedBlock, string>; ran: Set<PastedBlock> };

function continueBlock(lines: SoftLines, pasted: PastedBlock, line: string) {
  lines.words.set(pasted, `${lines.words.get(pasted) ?? pasted.plainText ?? ""}\n${line.trim()}`);
  lines.ran.add(pasted);
}

function depthOf(entries: readonly PastedEntry[], pasted: PastedBlock) {
  for (let at = entries.length - 1; at >= 0; at--) if (entries[at].block === pasted) return entries[at].depth;
  return -1;
}

function readSoftLines(lines: SoftLines, context: MarkdownPasteContext) {
  for (const pasted of lines.ran) {
    const rich = parseInlineMarkdownWithin(lines.words.get(pasted) ?? "", context);
    pasted.content = { ...pasted.content, rich };
    pasted.plainText = rich.map((span) => span.text).join("");
  }
}

/**
 * A list mark, "*", "-" or "•", with the spaces after it, starting a piece
 * of a heading's, list item's or to-do's words is no word of its own:
 * Notion's paste drops one from the start of each piece, whatever its marks
 * ("# **a" is "*a", "# * a" and "- -a" are "a", "- *a**" is "a" in italics,
 * "# **a**-b" is "a" in bold and "b"); a page line whose words start with
 * one, a mark of their own or not, is a bulleted item ("*a" is "a", "--a" is
 * "-a", "**a*" is "a" in italics, "**-a**" is "a" in bold) holding no line
 * after it, as the paragraph it was ("*a⏎  - b" are two items side by
 * side), unless the line goes on the block before it ("x⏎*a" stays
 * "x⏎*a"). A quote keeps it, "+" and "1." stay words, and a line's first
 * mark opening words in italics or bold reads as that ("*a*b", "**a** b").
 * Seen 2026-11-13.
 */
const LIST_MARK = /^[*\-•][^\S\n]*/u;

const MARKLESS_BLOCKS: ReadonlySet<BlockType> = new Set(["heading_1", "heading_2", "heading_3", "heading_4", ...LIST_BLOCKS]);

function startsWithListMark(pasted: PastedBlock) {
  const first = pasted.content?.rich?.[0];
  return !!first && !first.equation && !first.mention && LIST_MARK.test(first.text);
}

function dropListMarks(pasted: PastedBlock) {
  const rich = (pasted.content?.rich ?? [])
    .map((span) => span.equation || span.mention ? span : { ...span, text: span.text.replace(LIST_MARK, "") })
    .filter((span) => span.text);
  pasted.content = { ...pasted.content, rich };
  pasted.plainText = rich.map((span) => span.text).join("");
}

/**
 * Reads pasted Markdown, a block for each line Notion's paste makes one of
 * (seen 2026-11-11): a heading, a list item or to-do, a quote, a divider, a
 * code fence, an equation, a table, a callout's "<aside>" lines and a line
 * of nothing but a picture. Any other line is words, however it looks — a
 * link, "[[name]]", "[Button: …]", "▶ …" — read as standard Markdown. With
 * softLineBreaks such a line right after a paragraph, list item or to-do is
 * that block's next line instead, and "===" or "---" under a paragraph makes
 * it a heading, as standard Markdown has it and Notion's paste (seen
 * 2026-11-04 and 2026-11-11); under a list item's or to-do's words it
 * underlines them (ItemWords). A quote's lines are Markdown of their own,
 * the blocks after its words in it (markdownQuotePaste.ts).
 */
export function parsePastedMarkdown(text: string, bounds: MarkdownPasteBounds, options: { softLineBreaks?: boolean } = {}): PastedBlock[] {
  const context = createMarkdownPasteContext(bounds);
  consumeMarkdownPasteInput(text, context);
  return readMarkdown(text, context, { ...options, base: 0 });
}

// The blocks of Markdown at `base`, the depth of the block holding them.
function readMarkdown(text: string, context: MarkdownPasteContext, options: { softLineBreaks?: boolean; base: number }): PastedBlock[] {
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  const entries: PastedEntry[] = [];
  let index = 0;
  let softLineOf: PastedBlock | null = null;
  const softLines: SoftLines = { words: new Map(), ran: new Set() };
  // Paragraphs a line starts (not one going on the block before it), which
  // its list mark makes a bulleted item (LIST_MARK).
  const pageLines = new Set<PastedBlock>();
  // Callouts written as "<aside>" lines, the lines up to "</aside>" inside them.
  const asides: { callout: PastedBlock; first: boolean }[] = [];
  const holders: Holder[] = [];
  // Where the line being read goes; a block it starts closes the list items it
  // doesn't stand in, and a list item holds the lines after it that reach its words.
  let place = heldBy(holders, 0, options.base);
  const itemWords = createItemWords();
  // Whether a blank line stands right before the line being read.
  let parted = false;
  const push = (depth: number, pasted: PastedBlock) => {
    holders.length = place.kept;
    const holder = holders[place.kept - 1];
    if (holder) holder.bare = false;
    if (holder?.item) placedIn(itemWords, holder.item, pasted, parted);
    if (LIST_BLOCKS.has(pasted.type)) {
      holders.push({ depth, column: itemColumn(lines[index] ?? ""), item: pasted });
      if (parted) itemWords.parted.add(pasted);
    }
    entries.push({ depth, block: pasted });
  };

  while (index < lines.length) {
    const continues: PastedBlock | null = softLineOf;
    softLineOf = null;
    const line = lines[index] ?? "";
    const indent = lineDepth(line);
    const contentLine = stripDepthIndent(line, indent);
    const trimmed = contentLine.trimEnd();
    const stripped = trimmed.trim();
    if (!stripped) {
      // An item a blank line follows before anything went in it holds nothing more.
      while (holders.length > 0 && holders[holders.length - 1].bare) holders.pop();
      index++;
      continue;
    }
    parted = index > 0 && !(lines[index - 1] ?? "").trim();
    place = heldBy(holders, columnsOf(line), options.base);
    const depth = place.depth;
    // The item the line stands right in, and whether the block before it is
    // an item with no words yet (ItemWords).
    const held = holders[place.kept - 1];
    const wordless = !!continues && itemWords.wordless.has(continues);

    // Four columns further in than where it goes, a line goes on the words
    // before it, or after a blank line or another block is code, its lines
    // running on to one that stands less far in (seen 2026-11-13: "p⏎⏎    q"
    // and "- a⏎⏎      c" make code, "p⏎    q" the paragraph "p⏎q").
    if (columnsOf(line) - place.column >= 4) {
      if (continues && !wordless) {
        continueBlock(softLines, continues, stripped);
        softLineOf = continues;
        index++;
        continue;
      }
      const code: string[] = [];
      let end = index;
      for (; end < lines.length && (!(lines[end] ?? "").trim() || columnsOf(lines[end] ?? "") >= place.column + 4); end++) {
        code.push(dropColumns(lines[end] ?? "", place.column + 4));
      }
      while (code.length > 0 && !code[code.length - 1].trim()) code.pop();
      const value = code.join("\n");
      index = end;
      if (continues && held?.item === continues) {
        // An item with no words takes the code as its words, in code ("-⏎      b").
        continues.content = { ...continues.content, rich: value ? [{ text: value, code: true }] : [] };
        continues.plainText = value;
        itemWords.wordless.delete(continues);
        held.bare = false;
        continue;
      }
      push(depth, block("code", value, { rich: [{ text: value }], language: pastedCodeLanguage("") }, depth, context));
      continue;
    }

    if (isAsideOpen(stripped) && asideCloses(lines, index)) {
      const callout = block("callout", "", calloutContent(null), depth, context);
      push(depth, callout);
      holders.push({ depth, column: columnsOf(line), aside: true });
      asides.push({ callout, first: true });
      index++;
      continue;
    }
    if (isAsideClose(stripped) && asides.length > 0) {
      asides.pop();
      while (holders.length > 0 && !holders[holders.length - 1].aside) holders.pop();
      holders.pop();
      index++;
      continue;
    }
    const aside = asides.at(-1);
    if (aside?.first) {
      // Its first line starts with its icon, as Notion writes one.
      aside.first = false;
      const icon = calloutIcon(stripped);
      aside.callout.content = calloutContent(icon);
      if (icon) {
        lines[index] = line.slice(0, line.length - contentLine.trimStart().length) + stripped.slice(icon.length);
        continue;
      }
    }

    const codeFence = codeFenceStart(stripped);
    if (codeFence) {
      const code: string[] = [];
      index++;
      while (
        index < lines.length &&
        !isCodeFenceEnd(stripDepthIndent(lines[index] ?? "", indent), codeFence.fenceLength)
      ) {
        code.push(stripDepthIndent(lines[index] ?? "", indent));
        index++;
      }
      if (index < lines.length) index++;
      const value = code.join("\n");
      push(depth, block("code", value, {
        rich: value ? [{ text: value }] : [],
        language: codeFence.language,
      }, depth, context));
      continue;
    }

    if (stripped === "$$") {
      // It closes at a line ending in "$$", what stands before that its TeX's
      // last line (seen 2026-11-13: "## $$h$$" closed one as "## $$h").
      const equation: string[] = [];
      index++;
      while (index < lines.length && !(lines[index] ?? "").trimEnd().endsWith("$$")) {
        equation.push(stripDepthIndent(lines[index] ?? "", indent));
        index++;
      }
      const closed = index < lines.length;
      if (closed) {
        const last = stripDepthIndent(lines[index] ?? "", indent).trimEnd().slice(0, -2);
        if (last.trim()) equation.push(last);
        index++;
      }
      const expression = equation.join("\n").trim();
      if (expression) push(depth, block("equation", expression, { expression }, depth, context));
      else push(depth, emptyEquationWords(["$$", ...equation, ...(closed ? ["$$"] : [])], depth, context));
      continue;
    }

    // "$$" with its TeX's first line after it, closed on a later line.
    const runOn = stripped.startsWith("$$") && !stripped.slice(2).includes("$$")
      ? equationLines(lines, index, stripped.slice(2), (next) => stripDepthIndent(next, indent))
      : null;
    if (runOn) {
      const from = index;
      index = runOn.next;
      push(depth, runOn.expression
        ? block("equation", runOn.expression, { expression: runOn.expression }, depth, context)
        : emptyEquationWords(lines.slice(from, index), depth, context));
      continue;
    }

    if (isMarkdownTableRow(trimmed) && isMarkdownTableSeparator(lines[index + 1] ?? "")) {
      let consumed = 2;
      while (index + consumed < lines.length && isMarkdownTableRow(lines[index + consumed] ?? "")) {
        consumed++;
      }
      const table = markdownTableWithin(
        lines.slice(index, index + consumed).map((row) => stripDepthIndent(row, indent)).join("\n"),
        context,
      );
      if (table) {
        index += consumed;
        const parent = block("table", table.plain.flat().join("\n"), {
          tableWidth: table.plain[0]?.length ?? 1,
          hasColumnHeader: true,
          hasRowHeader: false,
        }, depth, context);
        parent.children = table.cells.map((row, rowIndex) => block("table_row", table.plain[rowIndex].join("\t"), {
          cells: row,
        }, depth + 1, context));
        assertMarkdownPasteChildren(parent.children.length, context);
        push(depth, parent);
        continue;
      }
    }

    const markdownImage = parseMarkdownImageLine(stripped);
    const imageUrl = markdownImage ? safePasteAssetUrl(markdownImage.href) : "";

    if (markdownImage && imageUrl) {
      const caption = markdownImage.label;
      push(depth, block("image", caption, { url: imageUrl, caption: parseInlineMarkdownWithin(caption, context) }, depth, context));
    } else if (stripped.startsWith(">")) {
      const value = stripped.replace(/^>\s*/, "");
      const quoted = value === "$$" ? equationLines(lines, index, "", (next) => {
        const words = stripDepthIndent(next, indent).trim();
        return words.startsWith(">") ? words.replace(/^>\s?/, "") : null;
      }) : null;
      if (quoted) {
        push(depth, block("quote", quoted.expression, { rich: quoted.expression ? [{ text: quoted.expression }] : [] }, depth, context));
        index = quoted.next;
        continue;
      }
      // Its lines: the ">" ones and the lines of words between them, which go
      // on its words (seen 2026-11-13: "> q⏎*⏎  b" is "q" and an item "b").
      let end = index + 1;
      while (options.softLineBreaks && end < lines.length && quoteGoesOn(stripDepthIndent(lines[end] ?? "", indent).trim())) end++;
      const read = readQuoteLines(lines.slice(index, end).map((next) => stripDepthIndent(next, indent)).join("\n"), context);
      const quote = block("quote", value, read ? { rich: read.rich } : undefined, depth, context);
      if (read?.rest.trim()) {
        const held = readMarkdown(read.rest, context, { ...options, base: depth + 1 });
        assertMarkdownPasteChildren(held.length, context);
        quote.children = held;
      }
      push(depth, quote);
      index += read?.lines ?? 1;
      continue;
    } else if (/^\$\$(.*)\$\$$/.test(stripped) && !stripped.slice(2, -2).includes("$$")) {
      // A line of one "$$…$$" is an equation; "$$x$$ $$y$$" is two inline
      // ones, as Notion's paste reads them (seen 2026-11-13).
      const expression = stripped.replace(/^\$\$/, "").replace(/\$\$$/, "").trim();
      push(depth, expression ? block("equation", expression, { expression }, depth, context) : emptyEquationWords([stripped], depth, context));
    } else if (options.softLineBreaks && continues && LIST_BLOCKS.has(continues.type) && !wordless && held?.item === continues && /^(?:=+|-+)$/.test(stripped)) {
      // Under an item's words, in it, it underlines them (ItemWords).
      underline(itemWords, continues, stripped);
    } else if (options.softLineBreaks && continues?.type === "paragraph" && /^(?:=+|-+)$/.test(stripped)) {
      // "===" or "---" under a paragraph makes it a heading, as standard
      // Markdown and Notion's paste read one (seen 2026-11-11).
      continues.type = stripped.startsWith("=") ? "heading_1" : "heading_2";
    } else {
      const { read, words, inner } = standardLineBlock(stripped, depth, context);
      const itemEquation = LIST_BLOCKS.has(read.type) && read.plainText?.trim() === "$$"
        ? equationLines(lines, index, "", (next) => stripDepthIndent(next, indent + 1))
        : null;
      if (itemEquation) {
        read.content = { ...read.content, rich: itemEquation.expression ? [{ text: itemEquation.expression }] : [] };
        read.plainText = itemEquation.expression;
        push(depth, read);
        index = itemEquation.next;
        continue;
      }
      // A line goes on the words before it, an item with no words taking it
      // only when it reaches its words; a mark line with no words ("*", "1.")
      // right under words it stands with is a line of them (ItemWords).
      const markLine = LIST_BLOCKS.has(read.type) && !words && !inner;
      const breaksIn = markLine && stripped !== "-" && !!continues && !wordless && (continues.type === "paragraph"
        ? depthOf(entries, continues) === depth
        : LIST_BLOCKS.has(continues.type) && held?.item === continues);
      if (options.softLineBreaks && continues && ((read.type === "paragraph" && (!wordless || held?.item === continues)) || breaksIn)) {
        if (wordless) {
          softLines.words.set(continues, words);
          softLines.ran.add(continues);
          itemWords.wordless.delete(continues);
          if (held) held.bare = false;
        } else continueBlock(softLines, continues, breaksIn ? stripped : words);
        softLineOf = continues;
      } else {
        // A paragraph a line starts right in an item after a block of it may
        // be more of the item's words.
        const piece = options.softLineBreaks && read.type === "paragraph" && !parted ? held?.item : undefined;
        const joint = piece ? jointAfter(itemWords, piece) : "";
        push(depth, read);
        if (piece) addPiece(itemWords, piece, read, joint);
        if (markLine) {
          itemWords.wordless.add(read);
          holders[holders.length - 1].bare = true;
        }
        if (read.type === "paragraph") pageLines.add(read);
        softLines.words.set(read, words);
        // An item an item's words make goes in it, holding the lines that
        // reach its own words ("- * a⏎  b" is "a⏎b" in the empty item).
        let last = read, column = itemColumn(line), nestedDepth = depth;
        for (let nested = inner; nested; nested = nested.inner) {
          column += itemColumn(nested.line);
          const bare = !nested.words && !nested.inner;
          holders.push({ depth: ++nestedDepth, column, item: nested.read, bare });
          if (bare) itemWords.wordless.add(nested.read);
          entries.push({ depth: nestedDepth, block: nested.read });
          softLines.words.set(nested.read, nested.words);
          last = nested.read;
        }
        if (options.softLineBreaks && SOFT_LINE_BLOCKS.has(last.type)) softLineOf = last;
      }
    }
    index++;
  }
  readSoftLines(softLines, context);
  const joined = options.softLineBreaks ? joinedParagraphs(itemWords, entries) : new Set<PastedBlock>();
  // Read whole, as a paragraph that runs over lines is ("**a⏎b**" is "a⏎b" in bold).
  for (const pasted of pageLines) if (!joined.has(pasted) && pasted.type === "paragraph" && startsWithListMark(pasted)) pasted.type = "bulleted_list_item";
  for (const { block: pasted } of entries) if (MARKLESS_BLOCKS.has(pasted.type)) dropListMarks(pasted);
  joinItemWords(itemWords, joined);
  headItems(itemWords);
  // An item's underlined words after its first go in it (underlinedPieces).
  const read = entries.filter((entry) => !joined.has(entry.block)).flatMap((entry) => {
    const pieces = underlinedPieces(entry.block, entry.depth, context);
    closeUpWordSpaces(entry.block);
    return [entry, ...pieces.map((block) => ({ depth: entry.depth + 1, block }))];
  });
  return nestEntries(read, context);
}
