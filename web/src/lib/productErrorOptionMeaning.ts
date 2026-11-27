import type { ProductErrorMeaning } from "@/lib/productErrorMeaningCatalog";
import { productErrorText } from "@/lib/productErrorText";

// Why a change of a select's, multi-select's or status's options was refused,
// said by its reason, not as input that didn't fit or a save that failed
// (2026-11-26: such refusals read "변경 사항을 저장하지 못했습니다"): a new
// option name holds no comma, as Notion's; a column has each name once; and
// options pages use are deleted only on purpose (the server's
// database_option_names.go and database_option_removal.go).
export type OptionRefusalClass = "option-name-comma" | "option-name-taken" | "options-in-use";

const CODES: Record<string, OptionRefusalClass> = {
  LEAFEN_DATABASE_OPTION_NAME_COMMA: "option-name-comma",
  LEAFEN_DATABASE_OPTION_NAME_TAKEN: "option-name-taken",
  LEAFEN_DATABASE_OPTIONS_IN_USE: "options-in-use",
};

export function optionRefusalClass(code: string): OptionRefusalClass | null {
  return CODES[code] ?? null;
}

// Each reason's cause, what to do, and when trying again helps.
const MEANINGS: Record<OptionRefusalClass, readonly [string, string, string]> = {
  "option-name-comma": ["optionNameHasComma", "takeCommaOutOrSplitOption", "optionRefusedUntilRenamed"],
  "option-name-taken": ["optionNameTaken", "useAnotherOptionName", "optionRefusedUntilRenamed"],
  "options-in-use": ["optionsInUseLeftOut", "keepOptionsOrDeleteOneByOne", "optionsRefusedUntilKeptOrDeleted"],
};

export function optionRefusalMeaning(
  classification: string,
  operation: string,
  deniedEffect: string,
): ProductErrorMeaning | null {
  const keys = MEANINGS[classification as OptionRefusalClass];
  if (!keys) return null;
  const [cause, nextAction, retry] = keys.map((key) => productErrorText(`meanings.${key}`));
  return {
    cause, effect: deniedEffect, known: true, nextAction, operation, retry,
    retryDisposition: "safe-after-action",
  };
}

/** What an options refusal's code means, as a toast says it (lib/settingsErrors.ts), or null for any other code. */
export function optionRefusalText(code: unknown): string | null {
  const meaning = typeof code === "string" ? optionRefusalMeaning(CODES[code] ?? "", "", "") : null;
  return meaning ? `${meaning.cause} ${meaning.nextAction}` : null;
}
