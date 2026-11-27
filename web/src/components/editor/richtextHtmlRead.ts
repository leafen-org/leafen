import type { TextSpan } from "@/lib/types";
import { safeDateReminderValue } from "@/lib/dateReminders";
import { bigFontSize } from "@/lib/clipboardHtml";
import { MENTION_SERVER_ATTRIBUTE, isServerMention, otherMentionServer } from "@/lib/clipboardServer";
import { safeDateMentionFormat, safeDateMentionTimeFormat } from "./dateMentions";
import { safeInlineEquationValue } from "./richtextEquationPolicy";
import {
  safeDateMentionTimeZoneValue,
  safeDateMentionValueValue,
  safeExternalMentionUrlValue,
  safeLinkMentionHref,
  safeMentionIdValue,
} from "./richtextMentionPolicy";
import { mentionWords, otherServerMentionSpan } from "./richtextMentionServer";
import { styleColorTokens } from "./richtextStyleColors";
import { safeUrl } from "./richtextUrlPolicy";
import { linkMentionPreviewOf } from "./linkMentionHtml";
import {
  assertRichtextSpanCount,
  consumeRichtextInput,
  consumeRichtextOutput,
  createRichtextBudget,
  visitRichtextDomNode,
  type RichtextBounds,
  type RichtextBudget,
} from "./richtextBudget";
import { coalesceWithin, safeColorTokenValue, templateMentionValue } from "./richtextSpanValues";
import { dataSourceMentionAddress } from "@/lib/dataSourceMentionNames";

// Reading pasted HTML into spans: each element's marks, and Leafen's own
// mention marks as mentions (htmlToSpans).

const BLOCK_TAGS = new Set([
  "div", "p", "li", "blockquote", "h1", "h2", "h3", "h4", "h5", "h6",
]);

// `big`: within words in big type (bigFontSize).
type PendingNode = { node: Node; marks: Partial<TextSpan>; depth: number; root?: boolean; big?: boolean };

function scheduleChildren(
  node: Node,
  marks: Partial<TextSpan>,
  depth: number,
  stack: PendingNode[],
  bounds: RichtextBounds,
  budget: RichtextBudget,
  big = false,
) {
  const children = node.childNodes;
  if (budget.domNodes + stack.length + children.length > bounds.maxDomNodes) {
    throw new RangeError("Rich-text DOM nodes exceed their caller-owned bound.");
  }
  for (let index = children.length - 1; index >= 0; index -= 1) {
    stack.push({ node: children[index], marks, depth, big });
  }
}

/** HTML copy sources can use an inline div for code rather than a code tag. */
export function isInlineRichtextElement(element: HTMLElement) {
  return (element.style.display ?? "").startsWith("inline") || element.classList?.contains("notion-inline-code-container") === true;
}

/**
 * Notion's copy of words draws its inline code in a monospace type on its
 * own tokens: plain code in a notion-inline-code-container, linked code as
 * the link token itself, '<a class="notion-link-token …" style="font-family:
 * SFMono-Regular, …, monospace">', with no code element or container around
 * it (seen 2026-11-16). Both stay code, as Notion's paste of its own copy
 * keeps them; another app's monospace type is read as words.
 */
function notionCodeToken(element: HTMLElement, bounds: RichtextBounds, budget: RichtextBudget) {
  const family = element.style.fontFamily;
  if (!family || !Array.from(element.classList ?? []).some(name => name.startsWith("notion-"))) return false;
  consumeRichtextInput(family, bounds, budget);
  return /\bmonospace\b/iu.test(family);
}

function elementMarks(element: HTMLElement, inherited: Partial<TextSpan>, bounds: RichtextBounds, budget: RichtextBudget) {
  const marks = { ...inherited };
  const tag = element.tagName.toLowerCase();
  if (tag === "strong" || tag === "b") marks.bold = true;
  else if (tag === "em" || tag === "i") marks.italic = true;
  else if (tag === "u") marks.underline = true;
  else if (tag === "s" || tag === "strike" || tag === "del") marks.strikethrough = true;
  else if (tag === "code") marks.code = true;
  else if (tag === "a") {
    const rawHref = element.getAttribute("href") ?? undefined;
    if (rawHref) consumeRichtextInput(rawHref, bounds, budget);
    const href = safeUrl(rawHref);
    if (href) marks.link = href;
  }
  const { fontWeight, fontStyle, textDecoration, textDecorationLine } = element.style;
  for (const value of [fontWeight, fontStyle, textDecoration, textDecorationLine]) {
    if (value) consumeRichtextInput(value, bounds, budget);
  }
  if (fontWeight === "bold" || fontWeight === "bolder" || Number(fontWeight) >= 600) marks.bold = true;
  else if (fontWeight === "normal" || (fontWeight && Number(fontWeight) > 0 && Number(fontWeight) < 600)) delete marks.bold;
  if (fontStyle === "italic" || fontStyle?.startsWith("oblique")) marks.italic = true;
  else if (fontStyle === "normal") delete marks.italic;
  const decorations = (textDecorationLine || textDecoration || "").split(/\s+/);
  if (decorations.includes("underline")) marks.underline = true;
  if (decorations.includes("line-through") || element.classList?.contains("notion-inline-code-strikethrough")) marks.strikethrough = true;
  if (element.classList?.contains("notion-inline-code-container") || notionCodeToken(element, bounds, budget)) marks.code = true;
  const rawDataColor = element.dataset?.color;
  const rawBackground = element.style.backgroundColor;
  const rawColor = element.style.color;
  for (const value of [rawDataColor, rawBackground, rawColor]) {
    if (value) consumeRichtextInput(value, bounds, budget);
  }
  // A text color and a background each set their own kind; a background in
  // a data-color is the background.
  const dataColor = safeColorTokenValue(rawDataColor);
  const styled = styleColorTokens(element);
  if (dataColor?.endsWith("_background")) styled.background = dataColor;
  else if (dataColor) styled.color = dataColor;
  if (styled.color) marks.color = styled.color;
  if (styled.background) marks.background = styled.background;
  // A comment's or suggested edit's mark is never read: a paste brings words
  // with no thread on them, wherever they were copied (withoutThreadMarks).
  return marks;
}

// A mention is atomic, but its serializer draws marks inside it. Read the
// marks common to its label with the same DOM/input budget as ordinary text.
function mentionContent(
  node: Node,
  depth: number,
  bounds: RichtextBounds,
  budget: RichtextBudget,
  inherited: Partial<TextSpan>,
) {
  const stack: Array<{ node: Node; depth: number; marks: Partial<TextSpan> }> = [];
  const parts: string[] = [];
  let common: Partial<TextSpan> | null = null;
  for (let index = node.childNodes.length - 1; index >= 0; index -= 1) {
    stack.push({ node: node.childNodes[index], depth, marks: inherited });
  }
  while (stack.length) {
    const current = stack.pop();
    if (!current) break;
    visitRichtextDomNode(current.depth, bounds, budget);
    if (current.node.nodeType === 3) {
      const text = current.node.textContent ?? "";
      consumeRichtextInput(text, bounds, budget);
      parts.push(text);
      if (text) {
        if (!common) common = { ...current.marks };
        else for (const key of Object.keys(common) as (keyof TextSpan)[]) {
          if (common[key] !== current.marks[key]) delete common[key];
        }
      }
      continue;
    }
    const marks = current.node.nodeType === 1
      ? elementMarks(current.node as HTMLElement, current.marks, bounds, budget) : current.marks;
    const children = current.node.childNodes;
    if (budget.domNodes + stack.length + children.length > bounds.maxDomNodes) {
      throw new RangeError("Rich-text DOM nodes exceed their caller-owned bound.");
    }
    for (let index = children.length - 1; index >= 0; index -= 1) {
      stack.push({ node: children[index], depth: current.depth + 1, marks });
    }
  }
  return { ...(common ?? inherited), text: parts.join("") };
}

/**
 * Spans from pasted HTML. A link stays a link, one to a page here too, as
 * Notion keeps a link to its own page pasted as HTML, marked as its mention
 * or not (seen 2026-11-11); only Leafen's own mention marks (data-mention)
 * read as mentions, and of a page, database, person or emoji only when they
 * name this server (otherMentionServer). Words in big type (bigFontSize)
 * are bold, an inner normal weight too, as Notion's paste reads them (seen
 * 2026-11-13: '<span style="font-size:30px">큰</span> 글' is '큰' bold);
 * `bigBold` false reads them as they are, for a heading they make.
 */
export function htmlToSpans(root: Node, bounds: RichtextBounds, bigBold = true) {
  const budget = createRichtextBudget(bounds);
  const output: TextSpan[] = [];
  const stack: PendingNode[] = [{ node: root, marks: {}, depth: 0, root: true }];
  // Where a line break's span ends: a newline right after it is the HTML's
  // own layout, not a second break, as a browser draws it (Notion's copy of
  // a ⇧Enter line is '가<br>\n나', seen 2026-11-16).
  let breakEnd = -1;
  const push = (span: TextSpan) => {
    assertRichtextSpanCount(output.length + 1, bounds);
    consumeRichtextOutput(span.text, bounds, budget);
    output.push(span);
  };

  while (stack.length) {
    const current = stack.pop();
    if (!current) break;
    visitRichtextDomNode(current.depth, bounds, budget);
    if (current.root) {
      scheduleChildren(current.node, current.marks, 1, stack, bounds, budget);
      continue;
    }
    if (current.node.nodeType === 3) {
      const raw = current.node.textContent ?? "";
      consumeRichtextInput(raw, bounds, budget);
      const text = (breakEnd === output.length ? raw.replace(/^\r?\n/u, "") : raw).replace(/\u200B/g, "");
      breakEnd = -1;
      if (text) push({ ...current.marks, text });
      continue;
    }
    if (current.node.nodeType !== 1) continue;
    const element = current.node as HTMLElement;
    const tag = element.tagName.toLowerCase();
    if (tag === "br") {
      push({ ...current.marks, text: "\n" });
      breakEnd = output.length;
      continue;
    }
    if (BLOCK_TAGS.has(tag) && !isInlineRichtextElement(element) && output.length > 0) push({ text: "\n" });
    const size = element.style?.fontSize ?? "";
    if (size) consumeRichtextInput(size, bounds, budget);
    const big = bigBold && (current.big || bigFontSize(size));
    const marks: Partial<TextSpan> = big ? { ...current.marks, bold: true } : { ...current.marks };
    const rawEquation = element.dataset?.equation;
    if (rawEquation) consumeRichtextInput(rawEquation, bounds, budget);
    // An inline equation's TeX is its text, whatever is drawn inside it.
    const equation = safeInlineEquationValue(rawEquation);
    if (equation) {
      push({ ...marks, text: equation, equation });
      continue;
    }
    const mention = element.dataset?.mention;
    if (mention) consumeRichtextInput(mention, bounds, budget);
    // One copied from another server's page names what is there: its link
    // there or its words (otherServerMentionSpan).
    const server = isServerMention(mention) ? mentionServerOf(element, bounds, budget) : null;
    if (server !== null) {
      push(otherServerMentionSpan(element, mentionContent(element, current.depth + 1, bounds, budget, marks), server));
      continue;
    }
    if (mention === "page") {
      const rawId = element.dataset.pageId;
      const rawBlockId = element.dataset.mentionBlockId;
      if (rawId) consumeRichtextInput(rawId, bounds, budget);
      if (rawBlockId) consumeRichtextInput(rawBlockId, bounds, budget);
      const pageId = safeMentionIdValue(rawId);
      const blockId = safeMentionIdValue(rawBlockId);
      if (pageId) {
        push({ ...mentionContent(element, current.depth + 1, bounds, budget, marks), mention, pageId, ...(blockId ? { blockId } : {}) });
        continue;
      }
    } else if (mention === "database") {
      // A data source's names it by its address (dataSourceMentionNames.ts).
      const source = element.dataset.mentionDataSource;
      const rawId = source ? dataSourceMentionAddress(source) : element.dataset.databaseId;
      if (rawId) consumeRichtextInput(rawId, bounds, budget);
      const databaseId = source ? (safeMentionIdValue(source) && rawId) : safeMentionIdValue(rawId);
      if (databaseId) {
        push({
          ...mentionContent(element, current.depth + 1, bounds, budget, marks),
          mention,
          databaseId,
        });
        continue;
      }
    } else if (mention === "custom_emoji") {
      const rawId = element.dataset.customEmojiId;
      if (rawId) consumeRichtextInput(rawId, bounds, budget);
      const customEmojiId = safeMentionIdValue(rawId);
      if (customEmojiId) {
        push({
          ...mentionWords(element, mentionContent(element, current.depth + 1, bounds, budget, marks)),
          mention,
          customEmojiId,
        });
        continue;
      }
    } else if (mention === "date") {
      const rawDate = element.dataset.date;
      const rawDateEnd = element.dataset.dateEnd;
      const rawDateTimeZone = element.dataset.dateTimeZone;
      const rawDateReminder = element.dataset.dateReminder;
      const dateFormat = safeDateMentionFormat(element.dataset.dateFormat);
      const dateTimeFormat = safeDateMentionTimeFormat(element.dataset.dateTimeFormat);
      if (rawDate) consumeRichtextInput(rawDate, bounds, budget);
      if (rawDateEnd) consumeRichtextInput(rawDateEnd, bounds, budget);
      if (rawDateTimeZone) consumeRichtextInput(rawDateTimeZone, bounds, budget);
      if (rawDateReminder) consumeRichtextInput(rawDateReminder, bounds, budget);
      const date = safeDateMentionValueValue(rawDate);
      if (date) {
        const dateEnd = safeDateMentionValueValue(rawDateEnd);
        const dateTimeZone = safeDateMentionTimeZoneValue(rawDateTimeZone);
        const dateReminder = safeDateReminderValue(rawDateReminder, date);
        const storedText = element.dataset.text;
        if (storedText) consumeRichtextInput(storedText, bounds, budget);
        push({
          ...mentionContent(element, current.depth + 1, bounds, budget, marks),
          ...(storedText !== undefined ? { text: storedText } : {}),
          mention,
          date,
          ...(dateEnd ? { dateEnd } : {}),
          ...(dateTimeZone ? { dateTimeZone } : {}),
          ...(dateReminder ? { dateReminder } : {}),
          ...(dateFormat ? { dateFormat } : {}),
          ...(dateTimeFormat ? { dateTimeFormat } : {}),
        });
        continue;
      }
    } else if (mention === "template") {
      const template = templateMentionValue(element.dataset.template);
      if (template) {
        push({ ...mentionContent(element, current.depth + 1, bounds, budget, marks), mention, template });
        continue;
      }
    } else if (mention === "automation") {
      const automation = element.dataset.automation;
      if (automation) {
        consumeRichtextInput(automation, bounds, budget);
        push({ ...mentionContent(element, current.depth + 1, bounds, budget, marks), mention, automation,
          ...(element.dataset.automationType ? { automationType: element.dataset.automationType } : {}) });
        continue;
      }
    } else if (mention === "person") {
      const rawId = element.dataset.userId;
      if (rawId) consumeRichtextInput(rawId, bounds, budget);
      const userId = safeMentionIdValue(rawId);
      if (userId) {
        push({ ...mentionContent(element, current.depth + 1, bounds, budget, marks), mention, userId });
        continue;
      }
    } else if (mention === "external") {
      const rawHref = element.getAttribute("href") ?? undefined;
      if (rawHref) consumeRichtextInput(rawHref, bounds, budget);
      for (const kept of [element.dataset.iconUrl, element.dataset.provider, element.dataset.description, element.dataset.imageUrl, element.dataset.title]) {
        if (kept) consumeRichtextInput(kept, bounds, budget);
      }
      const link = safeLinkMentionHref(rawHref);
      if (link) {
        // Its words are its title, kept whole on the link (its line may show
        // it cut), not the site's name drawn before it.
        const label = element.querySelector<HTMLElement>('[data-mention-label="external"]') ?? element;
        const content = mentionContent(label, current.depth + 1, bounds, budget, marks);
        push({
          ...content,
          ...(element.dataset.title ? { text: element.dataset.title } : {}),
          mention,
          link,
          ...linkMentionPreviewOf(element, safeExternalMentionUrlValue),
        });
        continue;
      }
    }
    // Use the element's own marks as they are: an inner font-weight or font-style
    // of normal removes the outer bold or italic, so merging would keep it.
    const inner = elementMarks(element, marks, bounds, budget);
    scheduleChildren(element, big ? { ...inner, bold: true } : inner, current.depth + 1, stack, bounds, budget, big);
  }
  return coalesceWithin(output, bounds, budget);
}

/** The other server a pasted mention names, by its mark or its link (otherMentionServer); null for this one. */
function mentionServerOf(element: HTMLElement, bounds: RichtextBounds, budget: RichtextBudget) {
  const named = element.getAttribute(MENTION_SERVER_ATTRIBUTE);
  const href = element.getAttribute("href");
  for (const value of [named, href]) if (value) consumeRichtextInput(value, bounds, budget);
  return otherMentionServer(named, href);
}
