import { i18next } from "@/i18n";

/**
 * What a database's sub-items and dependencies screens call its rows, as
 * Notion's {items}: "항목"/"items" in a plain database, "작업"/"tasks" in a
 * task database, "페이지"/"pages" in a wiki (Notion's dbTypes.untyped; seen
 * 2026-11-02, 2026-11-11, and on a wiki's dependencies screen 2026-11-18).
 * Every word of those screens comes from this one table by that kind.
 */
export type RowWord = "items" | "tasks" | "pages";

const SUBITEM_KEYS = {
  items: ["subitems", "subitemsDescription", "parentItemPropertyName", "subitemPropertyName", "subitemPreviewNumbered", "subitemsPropertyHint", "turnOnSubitems", "turnOffSubitems"],
  tasks: ["subtasks", "subtasksDescription", "parentTaskName", "subtaskName", "subtaskPreviewNumbered", "subtasksPropertyHint", "turnOnSubtasks", "turnOffSubtasks"],
  pages: ["subpages", "subpagesDescription", "parentPageName", "subpageName", "subpagePreviewNumbered", "subpagesPropertyHint", "turnOnSubpages", "turnOffSubpages"],
} as const satisfies Record<RowWord, readonly string[]>;

// The dependencies screen's keys, each kind's under its own ending.
const DEPENDENCY_ENDING: Record<RowWord, string> = { items: "", tasks: "Tasks", pages: "Pages" };

export function rowWordOf(taskDatabase: boolean, wiki: boolean): RowWord {
  return taskDatabase ? "tasks" : wiki ? "pages" : "items";
}

export function taskSettingsWords(kind: RowWord) {
  const tr = (key: string, options?: Record<string, unknown>): string => i18next.t(`databaseView:${key}`, options ?? {});
  const [subitems, subitemsDescription, parent, child, childNumbered, subitemsPropertyHint, turnOn, turnOff] = SUBITEM_KEYS[kind];
  const ending = DEPENDENCY_ENDING[kind];
  return {
    subitems: tr(subitems), subitemsDescription: tr(subitemsDescription), parent: tr(parent), child: tr(child),
    childNumbered: (number: number) => tr(childNumbered, { number }),
    subitemsPropertyHint: tr(subitemsPropertyHint), turnOn: tr(turnOn), turnOff: tr(turnOff),
    dependenciesDescription: tr(`dependenciesDescription${ending}`), successorHelp: tr(`dependencySuccessorHelp${ending}`),
    shiftMaintain: tr(`dependencyShiftMaintain${ending}`), avoidWeekendsDescription: tr(`avoidWeekendsDescription${ending}`),
  };
}
