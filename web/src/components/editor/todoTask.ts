import { useSyncExternalStore } from "react";
import { activeDateLocale } from "@/lib/i18n";
import { safeDateReminderValue } from "@/lib/dateReminders";
import type { MentionItem } from "./blockPickerModel";

/**
 * A to-do's own task data, as Notion's to-do keeps it (seen 2026-11-09): the
 * people it is assigned to, in the order they were added, and its due date
 * with the end, time zone and reminder a date mention has. Both are the
 * block's data, kept in its content apart from its text — taking the "@"
 * mention the menu wrote out of the line leaves them, and a Notion API read
 * shows neither — and a copy or duplicate of the block carries them; Enter
 * at a to-do's end starts the next one without them.
 */
export type TodoDue = { date: string; dateEnd?: string; dateTimeZone?: string; dateReminder?: string };

const MAX_ASSIGNEES = 50;
const DATE_VALUE = /^\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:?\d{2})?)?$/u;

export function todoAssignees(content: Record<string, unknown> | undefined): string[] {
  const value = content?.assignees;
  if (!Array.isArray(value)) return [];
  const ids: string[] = [];
  for (const id of value) {
    if (typeof id === "string" && id.trim() && !ids.includes(id.trim())) ids.push(id.trim());
    if (ids.length === MAX_ASSIGNEES) break;
  }
  return ids;
}

export function todoDue(content: Record<string, unknown> | undefined): TodoDue | null {
  const value = content?.due;
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const due = value as Record<string, unknown>;
  if (typeof due.date !== "string" || !DATE_VALUE.test(due.date)) return null;
  const end = typeof due.dateEnd === "string" && DATE_VALUE.test(due.dateEnd) ? due.dateEnd : undefined;
  const zone = typeof due.dateTimeZone === "string" && due.dateTimeZone ? due.dateTimeZone : undefined;
  const reminder = safeDateReminderValue(typeof due.dateReminder === "string" ? due.dateReminder : undefined, due.date);
  return { date: due.date, ...(end ? { dateEnd: end } : {}), ...(zone ? { dateTimeZone: zone } : {}),
    ...(reminder ? { dateReminder: reminder } : {}) };
}

/** What a to-do's "@" menu says it does: set or change the due date, set or add an assignee. */
export type TodoMenuTask = { due: boolean; assigned: boolean };

export function todoMenuTask(content: Record<string, unknown> | undefined): TodoMenuTask {
  return { due: todoDue(content) !== null, assigned: todoAssignees(content).length > 0 };
}

/** The content change that gives a to-do these assignees; none leaves the key out. */
export function todoAssigneesChange(ids: readonly string[]): Record<string, unknown> {
  return { assignees: ids.length ? ids.slice(0, MAX_ASSIGNEES) : undefined };
}

export function todoDueChange(due: TodoDue | null): Record<string, unknown> {
  return { due: due ?? undefined };
}

/** The due date a picked "@" date row sets: its date, with a reminder's time zone and reminder. */
export function todoDueOfMention(item: MentionItem & { kind: "date" }): TodoDue {
  return { date: item.date, ...(item.dateTimeZone ? { dateTimeZone: item.dateTimeZone } : {}),
    ...(item.dateReminder ? { dateReminder: item.dateReminder } : {}) };
}

function dayOf(value: string, now: Date) {
  const [year, month, day] = value.slice(0, 10).split("-").map(Number);
  const date = new Date(year, month - 1, day);
  const time = /[T ](\d{2}):(\d{2})/u.exec(value);
  const dayText = new Intl.DateTimeFormat(activeDateLocale(), {
    month: "short", day: "numeric", ...(year !== now.getFullYear() ? { year: "numeric" as const } : {}),
  }).format(date);
  if (!time) return dayText;
  const clock = new Intl.DateTimeFormat(activeDateLocale(), { hour: "numeric", minute: "2-digit" })
    .format(new Date(2000, 0, 1, Number(time[1]), Number(time[2])));
  return `${dayText} ${clock}`;
}

/**
 * The due date as the button at the line's end shows it: "11월 9일", its
 * year only when it isn't this year's, its time after it ("11월 10일 오전
 * 9:00"), and its end after an arrow (Notion's, seen 2026-11-09). A date
 * gone by reads the same, not in red.
 */
export function todoDueLabel(due: TodoDue, now = new Date()) {
  const start = dayOf(due.date, now);
  return due.dateEnd ? `${start} → ${dayOf(due.dateEnd, now)}` : start;
}

/**
 * What the open "@" menu of a to-do would set, shown faded at the line's end
 * while a row is highlighted, as Notion's (seen 2026-11-09): the date a date
 * row sets, or the face of the person a person row assigns.
 */
export type TodoMenuPreview = { blockId: string; due?: TodoDue; userId?: string; name?: string };

let preview: TodoMenuPreview | null = null;
const listeners = new Set<() => void>();

export function showTodoMenuPreview(next: TodoMenuPreview | null) {
  if (preview === next || (!preview && !next)) return;
  preview = next;
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

export function useTodoMenuPreview(blockId: string): TodoMenuPreview | null {
  return useSyncExternalStore(subscribe, () => preview?.blockId === blockId ? preview : null, () => null);
}

/** The preview an "@" row makes in a to-do: a date row its date, a person row its face, nothing else. */
export function todoMenuPreviewOf(blockId: string, item: MentionItem | null): TodoMenuPreview | null {
  if (item?.kind === "date") return { blockId, due: todoDueOfMention(item) };
  if (item?.kind === "person") return { blockId, userId: item.userId, name: item.label };
  return null;
}
