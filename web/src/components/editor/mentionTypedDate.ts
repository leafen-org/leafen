import { fromDate } from "@internationalized/date";
import { casual } from "chrono-node/en";
import { daysIntoWeek } from "@/lib/displayPreferences";
import { startsInLetters } from "@/lib/menuSearch";

/**
 * A date typed after "@" (or "@remind "), as Notion takes "a date in any
 * format … or even 'next Wednesday'" and "a time, date, or both" (Notion
 * help, keyboard shortcuts and reminders, read 2026-10-26): "Oct 31",
 * "2026-11-01", "11/1", "next Wednesday", "7pm", "next Monday 9am". It is
 * read in the person's time zone and only when the whole text is the date.
 * English and numeric forms, and the Korean ones Notion's reads (seen
 * 2026-11-06, 2026-11-09 and 2026-11-13): "오늘", "내일", "어제", a weekday
 * ("금요일"), "다음주" (a week from today), and "오전"/"오후" before "4:01" or
 * as a word of their own — not "오전 4시", "내일 오후 3시", "모레", "다음 주",
 * "다음주 월요일", "지난주", "다음달" or "11월 15일". A half of the day as a
 * word of its own, alone or after a day's word, is its hour, "am" and "오전"
 * 9:00, "pm" and "오후" 18:00 ("오늘 오후", "tomorrow pm"; "tpm" and "sam"
 * none). A day or time already gone stays where it is
 * ("4am" late in the day is today's), and a weekday is that day of this week
 * from Monday, the week a date mention's relative name counts in ("monday" on
 * a Friday is four days back, "sunday" two ahead), but today's own weekday,
 * which is next week's ("monday" or "m3" on a Monday, seen 2026-11-09), as
 * Notion's. Short forms too (Notion's, seen 2026-11-09): a day word's first
 * letters and a time's bare digits, alone or together ("w91" Wednesday 9:01,
 * "t1030" today 10:30, "9" today 9:00, "2026" today 20:26, "f5pm"), and the
 * last word begun in Korean, letter by letter, as the first word that fits:
 * "오" 오전 ("@오", "@오늘 오" 9:00; seen 2026-11-13), "ㄷ", "다" and "다음"
 * 다음주, then a weekday from Monday ("ㅁ" and "목" 목요일, "ㄱ" 금요일, "일"
 * 일요일; seen 2026-11-12) — a syllable's last letter begins none ("옿", "당").
 * Letters alone that begin no day's word begin "am" or "pm" ("a" today 9:00,
 * "p" 18:00), else a month's name, its first day this year or, once that
 * month is past, next year ("j" and "jan" January 1, "ma" March, "ju" June,
 * "d" December 1 this year, "nov" November 1 though gone; seen 2026-11-13) —
 * but not what begins "now" ("n", "no"), which is this minute's.
 * English words that count from now ("next week", "in 3 hours", "2 weeks
 * ago", "midnight") or name a part of a day ("morning", "tomorrow evening")
 * are `relative`: their rows say how far off they are (mentionMenuItems).
 * A part of a day is its day alone, the next one once its hour has gone
 * ("morning" or "this morning" in the afternoon is tomorrow, "noon" too; seen
 * 2026-11-13) — midnight keeps its time.
 * This module loads when a menu first needs it.
 */
export type TypedMentionDate = {
  /** "2026-10-31", or "2026-10-31T19:00" with a time. */
  date: string;
  withTime: boolean;
  /** The text names the day with words alone ("today", "monday"), no numbers. */
  named: boolean;
  /** The text counts from now or names a part of a day ("next week", "in 3 hours", "morning"). */
  relative?: boolean;
};

const KOREAN_WORDS: Record<string, string> = { 오늘: "today", 내일: "tomorrow", 어제: "yesterday", 다음주: "next week",
  오전: "am", 오후: "pm",
  월요일: "monday", 화요일: "tuesday", 수요일: "wednesday", 목요일: "thursday", 금요일: "friday", 토요일: "saturday", 일요일: "sunday" };

// The words the last one, while it is typed, may begin, the first that fits.
const KOREAN_BEGUN = ["오전", "오후", "다음주", "월요일", "화요일", "수요일", "목요일", "금요일", "토요일", "일요일"];

function englishDateText(text: string) {
  const words = text.replace(/(오전|오후)\s*(\d{1,2}:\d{2})/gu, (_, half: string, time: string) => `${time}${half === "오전" ? "am" : "pm"}`)
    .split(/\s+/u);
  return words.map((word, index) => {
    const begun = KOREAN_WORDS[word] ? word : index === words.length - 1 && /^\p{Script=Hangul}+$/u.test(word)
      ? KOREAN_BEGUN.find((whole) => startsInLetters(whole, word)) : undefined;
    return begun ? KOREAN_WORDS[begun] : word;
  }).join(" ");
}

// The day words short forms begin, the first that fits taken: "t" is today,
// "tu" Tuesday, "s" Saturday, "y" yesterday ("x9" is none).
const DAY_WORDS = ["today", "tomorrow", "yesterday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"];
const MONTH_WORDS = ["january", "february", "march", "april", "may", "june", "july", "august", "september", "october", "november", "december"];
const SHORT_FORM = /^([a-z]*)\s*(\d{1,4})?\s*(am|pm)?$/u;
// The parts of a day chrono names, but midnight, which keeps its time.
const PART_OF_DAY = /^casualReference\/(?:morning|afternoon|evening|noon|tonight)$/u;
const HALF_ALONE = /^(?:([a-z]+)\s+)?(am|pm)$/u;

// A time's bare digits: a first digit up to 2 starts a two-digit hour, the
// rest are its minutes ("91" 9:01, "910" 9:10, "2026" 20:26, "25" none).
function shortTime(digits: string, half: string | undefined) {
  const hourLength = digits.length >= 2 && digits[0] <= "2" ? 2 : 1;
  const hour = Number(digits.slice(0, hourLength)), minutes = digits.slice(hourLength);
  const minute = minutes ? Number(minutes) : 0;
  if (minutes.length > 2 || minute > 59) return null;
  if (!half) return hour > 23 ? null : { hour, minute };
  return hour < 1 || hour > 12 ? null : { hour: hour % 12 + (half === "pm" ? 12 : 0), minute };
}

// A month's first day, this year's unless the month is past ("nov" in
// mid-November is November 1 this year, "oct" next year's).
function monthBegun(letters: string, today: Date): TypedMentionDate | undefined {
  const month = MONTH_WORDS.findIndex(name => name.startsWith(letters));
  if (month < 0) return undefined;
  const year = today.getFullYear() + (month < today.getMonth() ? 1 : 0);
  return { date: `${year}-${String(month + 1).padStart(2, "0")}-01`, withTime: false, named: false };
}

function readShortForm(typed: string, today: Date): TypedMentionDate | null | undefined {
  const lower = typed.toLowerCase();
  const halfAlone = HALF_ALONE.exec(lower);
  const match = halfAlone ? null : SHORT_FORM.exec(lower);
  if (!halfAlone && (!match || (!match[1] && !match[2]) || (match[3] && !match[2]))) return undefined;
  const [, letters, digits, half] = halfAlone ? [halfAlone[0], halfAlone[1] ?? "", undefined, halfAlone[2]] : match!;
  const word = letters ? DAY_WORDS.find(day => day.startsWith(letters)) : "today";
  if (!word) {
    if (digits || half) return undefined;
    const begunHalf = ["am", "pm"].find(whole => whole.startsWith(letters));
    if (begunHalf) return readShortForm(begunHalf, today);
    return "now".startsWith(letters) ? undefined : monthBegun(letters, today);
  }
  const time = digits ? shortTime(digits, half) : halfAlone ? { hour: half === "am" ? 9 : 18, minute: 0 } : null;
  if (digits && !time) return null;
  const index = DAY_WORDS.indexOf(word);
  let shift = index === 0 ? 0 : index === 1 ? 1 : index === 2 ? -1 : 0;
  if (index > 2) {
    const weekday = (index - 3 + 1) % 7, weekStart = 1;
    shift = -daysIntoWeek(today, weekStart) + (weekday - weekStart + 7) % 7;
    if (shift === 0) shift = 7;
  }
  const day = new Date(today.getFullYear(), today.getMonth(), today.getDate() + shift);
  const pad = (value: number) => String(value).padStart(2, "0");
  const date = `${day.getFullYear()}-${pad(day.getMonth() + 1)}-${pad(day.getDate())}`;
  return { date: time ? `${date}T${pad(time.hour)}:${pad(time.minute)}` : date, withTime: Boolean(time), named: !digits };
}

export function readTypedMentionDate(text: string, timeZone: string, instant = new Date()): TypedMentionDate | null {
  const typed = englishDateText(text.trim());
  if (!typed) return null;
  const local = fromDate(instant, timeZone);
  const short = readShortForm(typed, new Date(local.year, local.month - 1, local.day));
  if (short !== undefined) return short;
  const offset = local.offset / 60_000;
  let [result] = casual.parse(typed, { instant, timezone: offset });
  if (!result || result.index !== 0 || result.text.length !== typed.length) return null;
  // English words alone count from now: Korean ones read as English ("다음주") keep their day's rows.
  const english = !/\p{Script=Hangul}/u.test(text);
  const tags = [...result.start.tags()];
  const partOfDay = english && tags.some(tag => PART_OF_DAY.test(tag));
  if (partOfDay) [result] = casual.parse(typed, { instant, timezone: offset }, { forwardDate: true });
  const { start } = result;
  const month = start.get("month"), dayOfMonth = start.get("day");
  // A month and its day without a year are this year's, next year's once
  // more than 30 days gone: Notion's, seen 2026-11-13 ("oct 14" and "nov 10"
  // this year's; "oct 13", "oct 5", "june 5", "6/5" and "jan 5" next year's),
  // where the parser took the nearer of the two.
  const yearless = start.isCertain("month") && start.isCertain("day") && !start.isCertain("year");
  let shifted: Date | null = null;
  if (yearless && month && dayOfMonth) {
    shifted = new Date(local.year, month - 1, dayOfMonth);
    const gone = Math.round((new Date(local.year, local.month - 1, local.day).getTime() - shifted.getTime()) / 86_400_000);
    if (gone > 30) shifted = new Date(local.year + 1, month - 1, dayOfMonth);
  }
  // A day that year hasn't (February 29) stays the parser's.
  const year = shifted?.getDate() === dayOfMonth ? shifted.getFullYear() : start.get("year") ?? 0;
  // A day before its month's name counts from now ("5 june": its reminder
  // "지금부터 7개월 후", "20 oct" 11개월; seen 2026-11-13).
  const dayFirst = yearless && /^\d{1,2}(?:st|nd|rd|th)?\s+(?:of\s+)?\p{L}/u.test(typed);
  if (year < 100 || year > 9999) return null;
  const pad = (value: number | null) => String(value ?? 0).padStart(2, "0");
  const day = `${year}-${pad(month)}-${pad(dayOfMonth)}`;
  const withTime = !partOfDay && start.isCertain("hour");
  const relative = partOfDay || english && (dayFirst || tags.some(tag => tag === "result/relativeDate" || tag === "casualReference/midnight"));
  return { date: withTime ? `${day}T${pad(start.get("hour"))}:${pad(start.get("minute"))}` : day, withTime,
    named: !/\d/u.test(typed), ...relative ? { relative } : {} };
}
