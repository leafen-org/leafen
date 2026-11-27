import { stepBackNativeEditorCommand } from "@/components/editor/nativeEditorUndoScopes";
import { i18next } from "@/i18n";
import { appToastStack } from "./productToast";
import type { ToastAction } from "./toastStackView";

/**
 * The notice for the dates a change moved with it, which names a task
 * database's rows "작업" and any other database's "항목", with their count,
 * as Notion's (a task database seen 2026-11-07, another 2026-11-08). Rows the
 * writer may not change stayed where they were, which it adds as Notion's
 * does, the others not moved for want of the right (seen 2026-11-08); with none
 * moved it says only that, naming the rows where Notion's shows an unfilled
 * "{items}".
 */
export function dependencyShiftMessage(count: number, tasks: boolean | undefined, denied = 0) {
  if (!count) return i18next.t(tasks ? "databaseView:dependencyShiftsDenied" : "databaseView:dependencyShiftsDeniedItems");
  if (denied) return i18next.t(tasks ? "databaseView:dependencyShiftedDenied" : "databaseView:dependencyShiftedItemsDenied", { count });
  return i18next.t(tasks ? "databaseView:dependencyShifted" : "databaseView:dependencyShiftedItems", { count });
}

// The notices showing, by the page each was said on.
const said = new Map<string, Set<string>>();

/**
 * Says the dates a change moved with it (dependencyShiftMessage) on the page
 * the change was made on — a row's own page or window, or the page of its
 * table — with its step back when the change left a command to take back.
 * The step is ⌘Z of the history holding the command, one step of it that
 * ⌘⇧Z does again (stepBackNativeEditorCommand), and only while what said it
 * is `shown` still: pressed after the row's window has closed, or once a
 * row's window opened over the page, it takes nothing back and the notice
 * goes at the press, as Notion's. Left for another page — a
 * row's whole page for its database, a database for the page above it — the
 * page takes its notice with it (dependencyShiftNoticesLeave), as Notion's
 * (seen 2026-11-18, two tabs: gone as the other page drew, its five seconds
 * not over); a row's window closing leaves its notice its time (two tabs).
 */
export function sayDependencyShift(input: {
  notify(message: string, tone: "default", action?: ToastAction): void;
  pageId: string;
  count: number;
  tasks: boolean | undefined;
  denied?: number;
  step?: { key: string; shown(): boolean; back(): void | Promise<void> };
}) {
  const message = dependencyShiftMessage(input.count, input.tasks, input.denied), step = input.step;
  said.set(input.pageId, (said.get(input.pageId) ?? new Set<string>()).add(message));
  input.notify(message, "default", step && {
    label: i18next.t("toastStack:undo"), icon: "none",
    onClick: () => { if (step.shown()) stepBackNativeEditorCommand(step.key, () => void step.back()); },
  });
}

/** A page left for another takes the notices said on it with it; a row's window closed only lets go of them (sayDependencyShift). */
export function dependencyShiftNoticesLeave(pageId: string | null | undefined, closedWindow = false) {
  if (!pageId) return;
  if (!closedWindow) for (const message of said.get(pageId) ?? []) appToastStack()?.dismissMessage(message);
  said.delete(pageId);
}
