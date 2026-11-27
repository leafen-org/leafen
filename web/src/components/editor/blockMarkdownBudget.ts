import type { Block, TextSpan } from "@/lib/types";
import type { RichtextBounds } from "./richtext";

export type BlockMarkdownBounds = {
  /** Text for the clipboard, in the shape Notion's copy takes. */
  copied?: boolean;
  maxBlocks: number;
  maxDepth: number;
  maxChildrenPerBlock: number;
  maxTableRows: number;
  maxTableColumns: number;
  maxStringCharacters: number;
  maxOutputCharacters: number;
  richtext: RichtextBounds;
};

export type BlockMarkdownBudget = {
  inputCharacters: number;
  outputCharacters: number;
};

function assertBound(value: number, label: string) {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new RangeError(`${label} requires a caller-owned nonnegative safe-integer bound.`);
  }
}

export function createBlockMarkdownBudget(bounds: BlockMarkdownBounds): BlockMarkdownBudget {
  for (const [label, value] of [
    ["maxBlocks", bounds.maxBlocks],
    ["maxDepth", bounds.maxDepth],
    ["maxChildrenPerBlock", bounds.maxChildrenPerBlock],
    ["maxTableRows", bounds.maxTableRows],
    ["maxTableColumns", bounds.maxTableColumns],
    ["maxStringCharacters", bounds.maxStringCharacters],
    ["maxOutputCharacters", bounds.maxOutputCharacters],
  ] as const) assertBound(value, label);
  return { inputCharacters: 0, outputCharacters: 0 };
}

export function consumeBlockMarkdownInput(
  value: string,
  bounds: BlockMarkdownBounds,
  budget: BlockMarkdownBudget,
) {
  budget.inputCharacters += value.length;
  if (
    !Number.isSafeInteger(budget.inputCharacters)
    || budget.inputCharacters > bounds.maxStringCharacters
  ) {
    throw new RangeError("Block markdown strings exceed their caller-owned bound.");
  }
}

export function consumeBlockMarkdownOutput(
  value: string,
  bounds: BlockMarkdownBounds,
  budget: BlockMarkdownBudget,
) {
  budget.outputCharacters += value.length;
  if (
    !Number.isSafeInteger(budget.outputCharacters)
    || budget.outputCharacters > bounds.maxOutputCharacters
  ) {
    throw new RangeError("Block markdown output exceeds its caller-owned bound.");
  }
  return value;
}

function admitSpans(
  spans: readonly TextSpan[] | undefined,
  bounds: BlockMarkdownBounds,
  budget: BlockMarkdownBudget,
) {
  if (!spans) return;
  if (spans.length > bounds.richtext.maxSpans) {
    throw new RangeError("Block rich-text spans exceed their caller-owned bound.");
  }
  for (const span of spans) {
    consumeBlockMarkdownInput(span.text, bounds, budget);
    for (const value of [
      span.color,
      span.background,
      span.link,
      span.commentId,
      span.mention,
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
      if (value) consumeBlockMarkdownInput(value, bounds, budget);
    }
  }
}

export function admitBlockMarkdownBlock(
  block: Block,
  bounds: BlockMarkdownBounds,
  budget: BlockMarkdownBudget,
) {
  for (const value of [block.id, block.parentId, block.type, block.plainText]) {
    if (value) consumeBlockMarkdownInput(String(value), bounds, budget);
  }
  const content = block.content;
  if (!content) return;
  for (const value of [
    content.color,
    content.icon,
    content.language,
    content.expression,
    content.url,
    content.fileName,
    content.childPageId,
    content.buttonLabel,
  ]) {
    if (typeof value === "string") consumeBlockMarkdownInput(value, bounds, budget);
  }
  admitSpans(content.rich, bounds, budget);
  admitSpans(content.caption, bounds, budget);
  if (content.cells) {
    if (content.cells.length > bounds.maxTableColumns) {
      throw new RangeError("Block table columns exceed their caller-owned bound.");
    }
    for (const cell of content.cells) admitSpans(cell, bounds, budget);
  }
}

export function assertBlockMarkdownDepth(depth: number, bounds: BlockMarkdownBounds) {
  if (!Number.isSafeInteger(depth) || depth < 0 || depth > bounds.maxDepth) {
    throw new RangeError("Block markdown depth exceeds its caller-owned bound.");
  }
}

export function assertBlockMarkdownChildren(count: number, bounds: BlockMarkdownBounds) {
  if (!Number.isSafeInteger(count) || count < 0 || count > bounds.maxChildrenPerBlock) {
    throw new RangeError("Block markdown children exceed their caller-owned bound.");
  }
}
