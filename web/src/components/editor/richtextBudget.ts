import type { TextSpan } from "@/lib/types";

export type RichtextBounds = {
  maxSpans: number;
  maxTextCharacters: number;
  maxHtmlCharacters: number;
  maxDomNodes: number;
  maxDomDepth: number;
};

export type RichtextBudget = {
  inputCharacters: number;
  outputCharacters: number;
  htmlCharacters: number;
  domNodes: number;
};

function assertBound(value: number, label: string) {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new RangeError(`${label} requires a caller-owned nonnegative safe-integer bound.`);
  }
}

function validateRichtextBounds(bounds: RichtextBounds) {
  for (const [label, value] of [
    ["maxSpans", bounds.maxSpans],
    ["maxTextCharacters", bounds.maxTextCharacters],
    ["maxHtmlCharacters", bounds.maxHtmlCharacters],
    ["maxDomNodes", bounds.maxDomNodes],
    ["maxDomDepth", bounds.maxDomDepth],
  ] as const) assertBound(value, label);
}

export function createRichtextBudget(bounds: RichtextBounds): RichtextBudget {
  validateRichtextBounds(bounds);
  return {
    inputCharacters: 0,
    outputCharacters: 0,
    htmlCharacters: 0,
    domNodes: 0,
  };
}

function consumeCharacters(
  field: "htmlCharacters" | "inputCharacters" | "outputCharacters",
  value: string,
  limit: number,
  label: string,
  budget: RichtextBudget,
) {
  budget[field] += value.length;
  if (!Number.isSafeInteger(budget[field]) || budget[field] > limit) {
    throw new RangeError(`${label} exceed their caller-owned bound.`);
  }
}

export function consumeRichtextInput(
  value: string,
  bounds: RichtextBounds,
  budget: RichtextBudget,
) {
  consumeCharacters(
    "inputCharacters",
    value,
    bounds.maxTextCharacters,
    "Rich-text input characters",
    budget,
  );
}

export function consumeRichtextOutput(
  value: string,
  bounds: RichtextBounds,
  budget: RichtextBudget,
) {
  consumeCharacters(
    "outputCharacters",
    value,
    bounds.maxTextCharacters,
    "Rich-text output characters",
    budget,
  );
}

export function consumeRichtextHtml(
  value: string,
  bounds: RichtextBounds,
  budget: RichtextBudget,
) {
  consumeCharacters(
    "htmlCharacters",
    value,
    bounds.maxHtmlCharacters,
    "Rich-text HTML characters",
    budget,
  );
}

export function assertRichtextSpanCount(count: number, bounds: RichtextBounds) {
  if (!Number.isSafeInteger(count) || count < 0 || count > bounds.maxSpans) {
    throw new RangeError("Rich-text spans exceed their caller-owned bound.");
  }
}

export function admitRichtextSpans(
  spans: readonly TextSpan[] | undefined,
  bounds: RichtextBounds,
  budget: RichtextBudget,
) {
  const admitted = spans ?? [];
  assertRichtextSpanCount(admitted.length, bounds);
  for (const span of admitted) {
    consumeRichtextInput(span.text, bounds, budget);
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
      span.automation,
      span.automationType,
    ]) {
      if (value) consumeRichtextInput(value, bounds, budget);
    }
  }
  return admitted;
}

export function visitRichtextDomNode(
  depth: number,
  bounds: RichtextBounds,
  budget: RichtextBudget,
) {
  if (!Number.isSafeInteger(depth) || depth < 0 || depth > bounds.maxDomDepth) {
    throw new RangeError("Rich-text DOM depth exceeds its caller-owned bound.");
  }
  budget.domNodes += 1;
  if (!Number.isSafeInteger(budget.domNodes) || budget.domNodes > bounds.maxDomNodes) {
    throw new RangeError("Rich-text DOM nodes exceed their caller-owned bound.");
  }
}
