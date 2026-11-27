import { interactiveAutomationFormulaContextNames } from "@/interactiveAutomationWire.generated";

const AUTOMATION_RUN_NAMES: ReadonlySet<string> = new Set(interactiveAutomationFormulaContextNames);

/**
 * Whether an automation mention names a variable an earlier action defines:
 * not one of the names a run gives (who, when, the page and its properties),
 * read from the mention's own formula name, so a message saved before keeps it.
 */
export function automationMentionIsVariable(token: string) {
  const root = /^[A-Za-z_][A-Za-z0-9_]*/u.exec(token)?.[0] ?? "";
  return Boolean(root) && !AUTOMATION_RUN_NAMES.has(root);
}
