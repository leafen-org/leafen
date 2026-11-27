import type { ClipboardBounds } from "@/lib/clipboard";
import type { BlockContent, TextSpan } from "@/lib/types";
import {
  consumeRichtextHtml,
  createRichtextBudget,
  visitRichtextDomNode,
  type RichtextBounds,
} from "./richtextBudget";
import type { PastedBlock } from "./markdownPasteTypes";

export type MarkdownPasteBounds = {
  maxBlocks: number;
  maxDepth: number;
  maxChildrenPerBlock: number;
  maxTableRows: number;
  maxTableColumns: number;
  maxStringCharacters: number;
  maxOutputCharacters: number;
  clipboard: ClipboardBounds;
  richtext: RichtextBounds;
};

export type MarkdownPasteBudget = {
  blocks: number;
  inputCharacters: number;
  outputCharacters: number;
  spans: number;
};

export type MarkdownPasteContext = {
  bounds: MarkdownPasteBounds;
  budget: MarkdownPasteBudget;
};

function assertBound(value: number, label: string) {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new RangeError(`${label} requires a caller-owned nonnegative safe-integer bound.`);
  }
}

export function createMarkdownPasteBudget(bounds: MarkdownPasteBounds): MarkdownPasteBudget {
  for (const [label, value] of [
    ["maxBlocks", bounds.maxBlocks],
    ["maxDepth", bounds.maxDepth],
    ["maxChildrenPerBlock", bounds.maxChildrenPerBlock],
    ["maxTableRows", bounds.maxTableRows],
    ["maxTableColumns", bounds.maxTableColumns],
    ["maxStringCharacters", bounds.maxStringCharacters],
    ["maxOutputCharacters", bounds.maxOutputCharacters],
  ] as const) assertBound(value, label);
  return { blocks: 0, inputCharacters: 0, outputCharacters: 0, spans: 0 };
}

export function createMarkdownPasteContext(bounds: MarkdownPasteBounds): MarkdownPasteContext {
  return { bounds, budget: createMarkdownPasteBudget(bounds) };
}

export function consumeMarkdownPasteInput(value: string, context: MarkdownPasteContext) {
  context.budget.inputCharacters += value.length;
  if (
    !Number.isSafeInteger(context.budget.inputCharacters) ||
    context.budget.inputCharacters > context.bounds.maxStringCharacters
  ) {
    throw new RangeError("Markdown-paste input exceeds its caller-owned bound.");
  }
}

function consumeMarkdownPasteOutput(value: string, context: MarkdownPasteContext) {
  context.budget.outputCharacters += value.length;
  if (
    !Number.isSafeInteger(context.budget.outputCharacters) ||
    context.budget.outputCharacters > context.bounds.maxOutputCharacters
  ) {
    throw new RangeError("Markdown-paste output exceeds its caller-owned bound.");
  }
}

export function assertMarkdownPasteDepth(depth: number, context: MarkdownPasteContext) {
  if (!Number.isSafeInteger(depth) || depth < 0 || depth > context.bounds.maxDepth) {
    throw new RangeError("Markdown-paste depth exceeds its caller-owned bound.");
  }
}

export function assertMarkdownPasteChildren(count: number, context: MarkdownPasteContext) {
  if (
    !Number.isSafeInteger(count) ||
    count < 0 ||
    count > context.bounds.maxChildrenPerBlock
  ) {
    throw new RangeError("Markdown-paste children exceed their caller-owned bound.");
  }
}

function admitSpans(spans: readonly TextSpan[] | undefined, context: MarkdownPasteContext) {
  if (!spans) return;
  context.budget.spans += spans.length;
  if (
    !Number.isSafeInteger(context.budget.spans) ||
    context.budget.spans > context.bounds.richtext.maxSpans
  ) {
    throw new RangeError("Markdown-paste spans exceed their caller-owned bound.");
  }
  for (const span of spans) {
    consumeMarkdownPasteOutput(span.text, context);
    for (const value of [
      span.link,
      span.pageId,
      span.blockId,
      span.databaseId,
      span.customEmojiId,
      span.date,
      span.dateEnd,
      span.dateTimeZone,
      span.dateReminder,
    span.dateFormat,
    span.dateTimeFormat,
      span.suggestion,
      span.suggestionId,
      span.suggestionBefore,
      span.userId,
      span.iconUrl,
      span.provider,
      span.description,
      span.imageUrl,
      span.equation,
    ]) {
      if (value) consumeMarkdownPasteOutput(value, context);
    }
  }
}

function admitContent(content: BlockContent | undefined, context: MarkdownPasteContext) {
  if (!content) return;
  for (const value of [
    content.icon,
    content.language,
    content.expression,
    content.url,
    content.fileName,
    content.childPageId,
    content.sourcePageHref,
    content.buttonLabel,
  ]) {
    if (typeof value === "string") consumeMarkdownPasteOutput(value, context);
  }
  admitSpans(content.rich, context);
  admitSpans(content.caption, context);
  if (content.cells) {
    if (content.cells.length > context.bounds.maxTableColumns) {
      throw new RangeError("Markdown-paste table columns exceed their caller-owned bound.");
    }
    for (const cell of content.cells) admitSpans(cell, context);
  }
}

export function admitPastedBlock(
  block: PastedBlock,
  depth: number,
  context: MarkdownPasteContext,
) {
  assertMarkdownPasteDepth(depth, context);
  context.budget.blocks += 1;
  if (
    !Number.isSafeInteger(context.budget.blocks) ||
    context.budget.blocks > context.bounds.maxBlocks
  ) {
    throw new RangeError("Markdown-paste blocks exceed their caller-owned bound.");
  }
  if (block.plainText) consumeMarkdownPasteOutput(block.plainText, context);
  admitContent(block.content, context);
}

export function admitStandaloneSpans(spans: readonly TextSpan[], context: MarkdownPasteContext) {
  admitSpans(spans, context);
}

export function assertMarkdownPasteTable(
  table: readonly (readonly string[])[],
  context: MarkdownPasteContext,
) {
  if (table.length > context.bounds.maxTableRows) {
    throw new RangeError("Markdown-paste table rows exceed their caller-owned bound.");
  }
  for (const row of table) {
    if (row.length > context.bounds.maxTableColumns) {
      throw new RangeError("Markdown-paste table columns exceed their caller-owned bound.");
    }
  }
}

export function assertMarkdownPasteHtml(value: string, context: MarkdownPasteContext) {
  consumeMarkdownPasteInput(value, context);
  const budget = createRichtextBudget(context.bounds.richtext);
  consumeRichtextHtml(value, context.bounds.richtext, budget);
}

export function assertMarkdownPasteDom(root: Node, context: MarkdownPasteContext) {
  const budget = createRichtextBudget(context.bounds.richtext);
  const stack: Array<{ node: Node; depth: number }> = [{ node: root, depth: 0 }];
  while (stack.length) {
    const current = stack.pop();
    if (!current) break;
    visitRichtextDomNode(current.depth, context.bounds.richtext, budget);
    for (let index = current.node.childNodes.length - 1; index >= 0; index--) {
      stack.push({ node: current.node.childNodes[index], depth: current.depth + 1 });
    }
  }
}
