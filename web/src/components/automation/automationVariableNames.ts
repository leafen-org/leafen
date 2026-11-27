import type { AutomationAction } from "@/lib/types";

/** Every variable name an automation's actions define, in order. */
export function automationVariableNamesOf(actions: readonly AutomationAction[]) {
  return actions.flatMap((action) => (action.type === "define_variables" ? action.variables.map((variable) => variable.name) : []));
}

/**
 * The first "변수 N" from 1 that no variable of the automation is named — a
 * new "변수 정의" action's first, "다른 변수 정의..." and "복제" alike — as
 * Notion's numbers them across all its actions (seen 2026-11-21 in Chrome at
 * 1440: a second action's first "변수 3" after "변수 1" and "변수 2", the
 * first action's next "변수 5" after a second's "변수 3" and "변수 4", "변수 1"
 * again once the first was renamed "x"; w2249's "변수 4" and "변수 5" too).
 */
export function freshAutomationVariableName(taken: readonly string[], nameOf: (number: number) => string) {
  let number = 1;
  while (taken.includes(nameOf(number))) number += 1;
  return nameOf(number);
}
