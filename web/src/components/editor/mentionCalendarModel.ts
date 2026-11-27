import { civilDate, civilDays, localDate } from "@/lib/nativeDate";

/** Native mention shortcuts use civil-day arithmetic; Aria owns the picker. */
export function localIsoDate(offsetDays = 0) {
  return civilDate(new Date()).add({ days: offsetDays }).toString();
}
export function localDateForOffset(offsetDays = 0) {
  return localDate(civilDate(new Date()).add({ days: offsetDays }));
}

export type ReminderMentionChoice = {
  id: "remind-today" | "remind-tomorrow" | "remind-next-week";
  at: Date;
};

/**
 * The @remind choices: later today on the next hour, tomorrow at 9:00 and
 * next Monday at 9:00, all in the writer's local time.
 */
export function reminderMentionChoices(now = new Date()): ReminderMentionChoice[] {
  const choices: ReminderMentionChoice[] = [];
  const nextHour = new Date(now);
  nextHour.setHours(now.getHours() + 1, 0, 0, 0);
  if (nextHour.getDate() === now.getDate()) choices.push({ id: "remind-today", at: nextHour });
  const tomorrow = civilDays(now, 1);
  tomorrow.setHours(9, 0, 0, 0);
  choices.push({ id: "remind-tomorrow", at: tomorrow });
  const monday = civilDays(now, (8 - now.getDay()) % 7 || 7);
  monday.setHours(9, 0, 0, 0);
  choices.push({ id: "remind-next-week", at: monday });
  return choices;
}
