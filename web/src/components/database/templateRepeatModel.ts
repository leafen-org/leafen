import type { DatabaseToolbarTemplateRepeatWire } from "@/databaseToolbarConfigWire.generated";
import { defaultScheduleTriggerDraft, storedTrigger, type ScheduleTriggerDraft } from "./databaseAutomationPanelModel";

/**
 * Whether a template repeats now: it has a repeat with a page still to make,
 * not paused. One that made its last page reads "사용 안 함" again and loses
 * its mark, as Notion's (seen 2026-11-03 once a repeat ending after one page
 * made it).
 */
export function templateRepeats(template: { repeat?: DatabaseToolbarTemplateRepeatWire | null; repeatNextRunAt?: string;
  repeatPausedReason?: string }) {
  return Boolean(template.repeat && template.repeatNextRunAt && !template.repeatPausedReason);
}

/**
 * A template's repeat as the schedule fields edit it; a template without one
 * starts from a daily repeat today at 9:00 where the reader is.
 */
export function templateRepeatDraft(repeat?: DatabaseToolbarTemplateRepeatWire | null): ScheduleTriggerDraft {
  const draft = defaultScheduleTriggerDraft();
  if (!repeat) return draft;
  const weekdays = repeat.weekdays ?? [];
  const monthWeek = repeat.monthWeek ?? 0;
  return {
    ...draft,
    frequency: repeat.frequency,
    interval: String(repeat.interval),
    time: repeat.time,
    timeZone: repeat.timeZone,
    startsOn: repeat.startsOn,
    endsOn: repeat.endsOn ?? "",
    endsAfter: repeat.endsAfter ? String(repeat.endsAfter) : "",
    weekdays: repeat.frequency === "weekly" ? weekdays : [],
    monthlyBy: monthWeek > 0 ? "weekday" : "day",
    monthDay: repeat.monthDay ? String(repeat.monthDay) : "",
    monthWeek: monthWeek > 0 ? String(monthWeek) : "",
    monthWeekday: monthWeek > 0 && weekdays.length === 1 ? String(weekdays[0]) : "",
  };
}

/** The repeat a complete schedule stores, or null while it is incomplete. */
export function storedTemplateRepeat(draft: ScheduleTriggerDraft): DatabaseToolbarTemplateRepeatWire | null {
  const trigger = storedTrigger(draft, []);
  if (!trigger || trigger.type !== "schedule") return null;
  const { type: _type, ...repeat } = trigger;
  return repeat as DatabaseToolbarTemplateRepeatWire;
}
