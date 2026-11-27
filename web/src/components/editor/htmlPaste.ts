import { bigFontSize, clipboardHtmlFragment } from "@/lib/clipboardHtml";
import {
  activePastedBlockLabels,
  activePersistentGeneratedLabels,
} from "@/lib/persistentGeneratedLabels";
import { spansToPlainText, type BlockContent, type BlockType, type TextSpan } from "@/lib/types";
import {
  createPastedBlock,
  pastedBlockFromRich,
  plainPastedContent,
} from "./markdownPasteBlocks";
import {
  assertMarkdownPasteChildren,
  assertMarkdownPasteDom,
  assertMarkdownPasteHtml,
  createMarkdownPasteContext,
  type MarkdownPasteBounds,
  type MarkdownPasteContext,
} from "./markdownPasteBudget";
import { pastedCodeLanguage } from "./codeLanguages";
import {
  asideRun,
  calloutContent,
  calloutIcon,
  dropLeadingText,
  equationExpression,
  htmlLines,
  taskMark, withNotionCopyEdges, type NotionCopy,
} from "./notionPasteShapes";
import type { PastedBlock } from "./markdownPasteTypes";
import { parseInlineMarkdownWithin } from "./markdownInlinePaste";
import { safePasteAssetUrl } from "./markdownPasteSyntax";
import { coalesce, htmlToSpans, isInlineRichtextElement } from "./richtext";

/**
 * `outside`: the HTML is another app's, not Leafen's own copy of blocks
 * (`data-leafen-copy`); `apart`: another app's read as blocks, not beside one
 * line of text; `notion`: Notion's own copy (notionCopy) (runBlocks).
 */
type HtmlPasteContext = MarkdownPasteContext & { outside: boolean; apart: boolean; notion: boolean };

const BLOCK_TAGS = new Set([
  "address", "article", "aside", "blockquote", "details", "div", "figure",
  "h1", "h2", "h3", "h4", "h5", "h6", "hr", "img", "li", "ol", "p",
  "pre", "section", "table", "ul",
]);

function isBlockElement(node: Element) {
  return BLOCK_TAGS.has(node.tagName.toLowerCase()) && !(node instanceof HTMLElement && isInlineRichtextElement(node));
}

function elementHasBlockChildren(element: Element) {
  return Array.from(element.children).some(isBlockElement);
}

/**
 * A span, or Google Docs' '<b style="font-weight:normal">', around blocks is
 * read as the blocks it holds, as Notion's paste reads it (seen 2026-11-13:
 * '<span><p>가</p><p>나</p></span>' is two paragraphs, so is Google Docs'
 * copy of two); a bold '<b>' around them is one paragraph of their lines.
 */
function wrapsBlocks(node: Node): node is HTMLElement {
  if (!(node instanceof HTMLElement)) return false;
  const tag = node.localName;
  return (tag === "span" || (tag === "b" || tag === "strong") && node.style.fontWeight === "normal") && elementHasBlockChildren(node);
}

function trimRich(spans: TextSpan[], context: HtmlPasteContext) {
  const next = spans.map((span) => ({ ...span }));
  while (next.length > 0) {
    const text = next[0].text.replace(/^\s+/, "");
    if (text) {
      next[0] = { ...next[0], text };
      break;
    }
    next.shift();
  }
  while (next.length > 0) {
    const last = next[next.length - 1];
    const text = last.text.replace(/\s+$/, "");
    if (text) {
      next[next.length - 1] = { ...last, text };
      break;
    }
    next.pop();
  }
  return coalesce(next, context.bounds.richtext);
}

function richFromHtmlNode(
  node: Node,
  context: HtmlPasteContext,
  removeSelector?: string,
  bigBold = true,
) {
  const clone = node.cloneNode(true);
  if (clone instanceof Element && removeSelector) {
    clone.querySelectorAll(removeSelector).forEach((item) => item.remove());
  }
  // The cloned source can itself carry the selected span's tag/CSS marks.
  // Wrap it so htmlToSpans visits that source, not just its children.
  const container = document.createElement("div");
  container.appendChild(clone);
  return trimRich(htmlToSpans(container, context.bounds.richtext, bigBold), context);
}

/** Whether all of these words are in big type (bigFontSize), by an element at or inside `root`. */
function allBig(root: Node) {
  let words = false;
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  for (let text = walker.nextNode(); text; text = walker.nextNode()) {
    if (!/\S/u.test(text.textContent ?? "")) continue;
    words = true;
    let big = false;
    for (let at = text.parentElement; at && !big; at = at === root ? null : at.parentElement) {
      big = at instanceof HTMLElement && bigFontSize(at.style.fontSize);
    }
    if (!big) return false;
  }
  return words;
}

/**
 * A block of words: a heading 1 where they are all in big type, as Notion's
 * paste makes one of them (seen 2026-11-13: '<p style="font-size:24px">큰
 * 글</p>', a 30px span alone in a paragraph, Google Docs' 26pt title), else a
 * paragraph, its big words bold.
 */
function wordsBlock(words: Node, context: HtmlPasteContext, depth: number, type: BlockType = "paragraph") {
  const big = allBig(words);
  const spans = richFromHtmlNode(words, context, undefined, !big);
  return spans.length ? [blockFromRich(big ? "heading_1" : type, spans, depth, context)] : [];
}

// Each cell keeps its marks and links, as a line does (2026-11-05: a table
// copied from Notion lost its bold, underline, code and links, and the code's
// own asterisks became italics).
function tableFromHtml(table: HTMLTableElement, context: HtmlPasteContext) {
  const rows = Array.from(table.querySelectorAll("tr"))
    .map((row) => Array.from(row.querySelectorAll("th,td")).map((cell) => richFromHtmlNode(cell, context)))
    .filter((row) => row.length > 0);
  if (rows.length === 0) return null;
  const width = Math.max(2, ...rows.map((row) => row.length));
  const normalized = rows.map((row) => Array.from({ length: width }, (_, index) => row[index] ?? []));
  if (normalized.length > context.bounds.maxTableRows) {
    throw new RangeError("Markdown-paste table rows exceed their caller-owned bound.");
  }
  if (normalized.some((row) => row.length > context.bounds.maxTableColumns)) {
    throw new RangeError("Markdown-paste table columns exceed their caller-owned bound.");
  }
  return normalized;
}

function rich(text: string, context: HtmlPasteContext) {
  return parseInlineMarkdownWithin(text, context);
}

function blockFromRich(
  type: BlockType,
  richText: TextSpan[],
  depth: number,
  context: HtmlPasteContext,
  content: BlockContent = {},
) {
  return pastedBlockFromRich(type, richText, content, depth, context);
}

function block(
  type: BlockType,
  text: string,
  depth: number,
  context: HtmlPasteContext,
  content?: BlockContent,
) {
  return createPastedBlock(
    type,
    text,
    content ?? { rich: rich(text, context) },
    depth,
    context,
  );
}

/**
 * The paragraphs a run of words makes, a paragraph's own or words with no
 * block around them (`holder` null), as Notion's paste reads another app's
 * HTML (seen 2026-11-13): a line break of the run's own ('<p><b>가</b><br>나
 * </p>', '<b>가</b><br>나', Google Docs' '<p>…<br>…</p>', Notion's own copy
 * of a ⇧Enter line) starts the next paragraph, two in a row no empty one
 * between them. One deeper in the words ('<p><b>가<br>나</b></p>'), or in a
 * heading, list item, quote or a div of words alone, stays a line break
 * where Notion's drops it ('가나') — a difference kept, as that runs the
 * words together. Leafen's own copy keeps its line breaks, as its own paste
 * of blocks does; so does Notion's own copy of blocks, its ⇧Enter line
 * '<p>가 <code>코드</code><br>\n나</p>' one block of two lines as Notion's
 * paste of it (seen 2026-11-16) — its HTML alone is two paragraphs there.
 * Read as blocks (`apart`), an element of the run all in big type is a block
 * of its own (a heading, wordsBlock), and the run's every element and word
 * then one too (seen 2026-11-13: '<p><span style="font-size:30px">큰</span>
 * 글 <b>둘</b></p>' is a heading '큰', '글' and '둘'; '<p>앞 <span …>큰
 * </span></p>' '앞' and a heading '큰'). Read beside one line of text, the
 * run stays one, its big words bold ('앞 큰' bold).
 */
function runBlocks(nodes: readonly Node[], holder: Element | null, context: HtmlPasteContext, depth: number) {
  const said = (node: Node) => node instanceof Element || node.nodeType === Node.TEXT_NODE && /\S/u.test(node.textContent ?? "");
  // A paragraph in big type of its own is one heading, whatever is in it.
  const apart = context.apart && !(holder instanceof HTMLElement && bigFontSize(holder.style.fontSize))
    && nodes.some((node) => node instanceof Element && allBig(node));
  const pieces: Node[][] = [[]];
  for (const node of nodes) {
    if (context.outside && !context.notion && node instanceof HTMLElement && node.localName === "br") pieces.push([]);
    else {
      if (apart && said(node) && pieces[pieces.length - 1]!.length) pieces.push([]);
      pieces[pieces.length - 1]!.push(node);
    }
  }
  return pieces.flatMap((piece) => {
    const container = holder ? holder.cloneNode(false) : document.createElement("div");
    for (const node of piece) container.appendChild(node.cloneNode(true));
    return wordsBlock(container, context, depth);
  });
}

function parseHtmlChildren(
  parent: ParentNode,
  context: HtmlPasteContext,
  depth: number,
): PastedBlock[] {
  const output: PastedBlock[] = [];
  let inline: Node[] = [];
  const flushInline = () => {
    if (inline.length) output.push(...runBlocks(inline, null, context, depth));
    inline = [];
  };
  const nodes = Array.from(parent.childNodes);
  for (let index = 0; index < nodes.length; index++) {
    const node = nodes[index]!;
    if (node.nodeType === Node.COMMENT_NODE) continue;
    if (node.nodeType === Node.TEXT_NODE || (node instanceof HTMLElement && !isBlockElement(node) && !wrapsBlocks(node))) {
      inline.push(node);
    } else {
      flushInline();
      const aside = asideRun(nodes, index);
      if (aside) {
        output.push(calloutFromAside(aside.inner, context, depth));
        index = aside.end;
        continue;
      }
      output.push(...parseHtmlNode(node, context, depth));
    }
  }
  flushInline();
  return output;
}

function calloutFromAside(inner: readonly Node[], context: HtmlPasteContext, depth: number): PastedBlock {
  const holder = document.createElement("div");
  for (const node of inner) holder.appendChild(node);
  const children = parseHtmlChildren(holder, context, depth + 1);
  const lead = children[0];
  const icon = lead?.type === "paragraph" ? calloutIcon(lead.plainText ?? "") : null;
  if (lead && icon) {
    const rich = dropLeadingText(lead.content?.rich ?? [], icon.length);
    if (rich.length || lead.children?.length) {
      children[0] = { ...lead, content: { ...lead.content, rich }, plainText: spansToPlainText(rich) };
    } else children.shift();
  }
  assertMarkdownPasteChildren(children.length, context);
  const callout = block("callout", "", depth, context, calloutContent(icon));
  return children.length > 0 ? { ...callout, children } : callout;
}

function firstContentNode(element: Element) {
  return Array.from(element.childNodes).find((node) =>
    node.nodeType !== Node.COMMENT_NODE && !(node.nodeType === Node.TEXT_NODE && !node.textContent?.trim()));
}

function parseLeafenTabLabel(
  node: HTMLElement,
  context: HtmlPasteContext,
  depth: number,
): PastedBlock {
  const untitled = activePersistentGeneratedLabels().untitled;
  const titleNode = node.querySelector<HTMLElement>(":scope > [data-leafen-tab-title]");
  const panelNode = node.querySelector<HTMLElement>(":scope > [data-leafen-tab-panel]");
  const richText = titleNode
    ? richFromHtmlNode(titleNode, context, "[data-leafen-tab-icon-text]")
    : richFromHtmlNode(node, context, "[data-leafen-tab-panel],[data-leafen-tab-icon-text]");
  const normalizedRich = richText.length > 0 ? richText : rich(untitled, context);
  const icon = (node.getAttribute("data-leafen-tab-icon") ?? "").trim();
  const content: BlockContent = { rich: normalizedRich, ...(icon ? { icon } : {}) };
  const pasted = blockFromRich("paragraph", normalizedRich, depth, context, content);
  const children = panelNode ? parseHtmlChildren(panelNode, context, depth + 1) : [];
  assertMarkdownPasteChildren(children.length, context);
  pasted.plainText = plainPastedContent(content) || untitled;
  if (children.length > 0) pasted.children = children;
  return pasted;
}

function parseLeafenTab(
  node: HTMLElement,
  context: HtmlPasteContext,
  depth: number,
) {
  if (node.getAttribute("data-leafen-block-type") !== "tab") return null;
  const children = Array.from(node.children)
    .filter((child): child is HTMLElement => child instanceof HTMLElement)
    .filter((child) => child.matches("[data-leafen-tab-label]"))
    .map((child) => parseLeafenTabLabel(child, context, depth + 1));
  assertMarkdownPasteChildren(children.length, context);
  const pasted = block("tab", activePastedBlockLabels().tabs, depth, context, { rich: [] });
  if (children.length > 0) pasted.children = children;
  return pasted;
}

function parseList(
  list: HTMLOListElement | HTMLUListElement,
  context: HtmlPasteContext,
  depth: number,
) {
  const ordered = list.tagName.toLowerCase() === "ol";
  return Array.from(list.children)
    .filter((child): child is HTMLLIElement => child.tagName.toLowerCase() === "li")
    .map((item) => {
      const checkbox = item.querySelector<HTMLInputElement>('input[type="checkbox"]');
      // An item of paragraphs is its first one, the rest inside it, as Notion
      // reads one (seen 2026-11-05: its toggle copied as "<li><p>tog</p><p>in</p>").
      const lead = firstContentNode(item);
      const leadParagraph = lead instanceof HTMLElement && lead.tagName.toLowerCase() === "p" ? lead : null;
      let richText = leadParagraph
        ? richFromHtmlNode(leadParagraph, context, 'input[type="checkbox"]')
        : richFromHtmlNode(item, context, 'ul,ol,details,input[type="checkbox"]');
      const task = !checkbox && !ordered ? taskMark(spansToPlainText(richText)) : null;
      if (task) richText = dropLeadingText(richText, task.length);
      const pasted = blockFromRich(
        checkbox || task ? "to_do" : ordered ? "numbered_list_item" : "bulleted_list_item",
        richText,
        depth,
        context,
        checkbox ? { checked: checkbox.checked } : task ? { checked: task.checked } : {},
      );
      let children: PastedBlock[];
      if (leadParagraph) {
        const rest = document.createElement("div");
        for (const node of item.childNodes) if (node !== leadParagraph) rest.appendChild(node.cloneNode(true));
        children = parseHtmlChildren(rest, context, depth + 1);
      } else {
        children = Array.from(item.children)
          .filter((child) => ["ul", "ol", "details"].includes(child.tagName.toLowerCase()))
          .flatMap((child) => parseHtmlNode(child, context, depth + 1));
      }
      assertMarkdownPasteChildren(children.length, context);
      return children.length > 0 ? { ...pasted, children } : pasted;
    });
}

function parseDetails(
  details: HTMLDetailsElement,
  context: HtmlPasteContext,
  depth: number,
) {
  const summary = Array.from(details.children).find(
    (child) => child.tagName.toLowerCase() === "summary",
  );
  const richText = summary
    ? richFromHtmlNode(summary, context)
    : richFromHtmlNode(details, context, "details,summary");
  const children = Array.from(details.childNodes)
    .filter((child) => child !== summary)
    .flatMap((child) => parseHtmlNode(child, context, depth + 1));
  assertMarkdownPasteChildren(children.length, context);
  const pasted = blockFromRich("toggle", richText, depth, context, { collapsed: !details.open });
  return [{ ...pasted, ...(children.length > 0 ? { children } : {}) }];
}

function parseHtmlNode(
  node: Node,
  context: HtmlPasteContext,
  depth: number,
): PastedBlock[] {
  if (node.nodeType === Node.TEXT_NODE) {
    const text = node.textContent?.replace(/\s+/g, " ").trim() ?? "";
    return text ? [block("paragraph", text, depth, context)] : [];
  }
  if (!(node instanceof HTMLElement)) return [];
  const tag = node.tagName.toLowerCase();
  if (node.matches("[data-leafen-copy]")) return parseHtmlChildren(node, context, depth);
  const leafenTab = parseLeafenTab(node, context, depth);
  if (leafenTab) return [leafenTab];
  if (node.matches("[data-leafen-tab-label]")) {
    return [parseLeafenTabLabel(node, context, depth)];
  }
  if (tag === "ul" || tag === "ol") {
    return parseList(node as HTMLUListElement | HTMLOListElement, context, depth);
  }
  if (tag === "details") return parseDetails(node as HTMLDetailsElement, context, depth);
  if (tag === "hr") return [block("divider", "", depth, context, { rich: [] })];
  if (tag === "table") {
    const table = tableFromHtml(node as HTMLTableElement, context);
    if (!table) return [];
    const parent = block("table", table.flat().map(spansToPlainText).join("\n"), depth, context, {
      tableWidth: table[0]?.length ?? 1,
      hasColumnHeader: !!node.querySelector("tr:first-child th"),
      hasRowHeader: Array.from(node.querySelectorAll("tr")).every((row) => !!row.querySelector(":scope > th:first-child")),
    });
    parent.children = table.map((row) => block("table_row", row.map(spansToPlainText).join("\t"), depth + 1, context, {
      cells: row,
    }));
    assertMarkdownPasteChildren(parent.children.length, context);
    return [parent];
  }
  if (tag === "pre") {
    const code = node.textContent?.replace(/\n$/, "") ?? "";
    const language = pastedCodeLanguage(node.querySelector("code")?.className.match(/language-(\S+)/)?.[1]);
    return [block("code", code, depth, context, {
      rich: code ? [{ text: code }] : [],
      language,
    })];
  }
  if (tag === "figure") {
    const image = node.querySelector("img");
    const url = safePasteAssetUrl(image?.getAttribute("src") ?? "");
    if (url) {
      const captionNode = node.querySelector("figcaption");
      const caption = captionNode
        ? richFromHtmlNode(captionNode, context)
        : rich(image?.getAttribute("alt") ?? "", context);
      return [block("image", plainPastedContent({ rich: caption }), depth, context, { url, caption })];
    }
    return parseHtmlChildren(node, context, depth);
  }
  if (tag === "img") {
    const url = safePasteAssetUrl(node.getAttribute("src") ?? "");
    const caption = rich(node.getAttribute("alt") ?? "", context);
    return url
      ? [block("image", plainPastedContent({ rich: caption }), depth, context, { url, caption })]
      : [];
  }
  if (/^h[1-6]$/.test(tag)) {
    const type: BlockType = tag === "h1"
      ? "heading_1"
      : tag === "h2"
        ? "heading_2"
        : tag === "h3"
          ? "heading_3"
          : "heading_4";
    return wordsBlock(node, context, depth, type);
  }
  if (tag === "blockquote") {
    const richText = richFromHtmlNode(node, context, "blockquote,ul,ol,details,table,pre,figure");
    const pasted = blockFromRich("quote", richText, depth, context);
    const children = Array.from(node.children)
      .filter((child) => ["ul", "ol", "details"].includes(child.tagName.toLowerCase()))
      .flatMap((child) => parseHtmlNode(child, context, depth + 1));
    assertMarkdownPasteChildren(children.length, context);
    return [{ ...pasted, ...(children.length > 0 ? { children } : {}) }];
  }
  if (tag === "p") {
    const expression = equationExpression(htmlLines(node).join("\n"));
    if (expression) return [block("equation", expression, depth, context, { expression })];
    return runBlocks(Array.from(node.childNodes), node, context, depth);
  }
  if (["div", "section", "article", "aside"].includes(tag)) {
    if (elementHasBlockChildren(node)) return parseHtmlChildren(node, context, depth);
    return wordsBlock(node, context, depth);
  }
  if (elementHasBlockChildren(node)) return parseHtmlChildren(node, context, depth);
  return wordsBlock(node, context, depth);
}

/** `notion`: the clipboard is Notion's own copy (notionCopy in pastedHtmlChoice.ts), its blocks' edge spaces kept. */
export function parsePastedHtml(html: string, bounds: MarkdownPasteBounds, inlineText?: string, notion: NotionCopy | boolean = false): PastedBlock[] {
  if (!html.trim() || typeof DOMParser === "undefined") return [];
  const own = createMarkdownPasteContext(bounds);
  assertMarkdownPasteHtml(html, own);
  try {
    const fragment = clipboardHtmlFragment(html, bounds.richtext.maxHtmlCharacters);
    assertMarkdownPasteDom(fragment, own);
    const copy = fragment.querySelector("[data-leafen-copy]");
    const oneLine = inlineText !== undefined && !inlineText.trimEnd().includes("\n");
    const root = copy ?? fragment, context: HtmlPasteContext = { ...own, outside: !copy, apart: !copy && !oneLine, notion: Boolean(notion) };
    const blocks = parseHtmlChildren(root, context, 0).filter((item) => {
      if (["divider", "table", "image", "callout"].includes(item.type)) return true;
      return !!item.plainText || (item.children?.length ?? 0) > 0;
    });
    // Keep copied spaces/soft breaks in a single inline paragraph when its
    // stored text agrees with the clipboard's plain representation. Layout
    // indentation around structured HTML must not become extra blank lines.
    if (inlineText !== undefined && inlineText.length <= bounds.richtext.maxTextCharacters && blocks.length === 1 && blocks[0]!.type === "paragraph" && !blocks[0]!.children?.length) {
      const spans = htmlToSpans(root, context.bounds.richtext);
      if (spansToPlainText(spans) === inlineText.replace(/\r\n/g, "\n")) {
        blocks[0] = { ...blocks[0]!, content: { ...blocks[0]!.content, rich: spans }, plainText: spansToPlainText(spans) };
      }
    }
    return typeof notion === "object" && !copy ? withNotionCopyEdges(blocks, notion) : blocks;
  } catch (error) {
    if (error instanceof RangeError) throw error;
    return [];
  }
}
