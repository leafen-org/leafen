import { parseStored, personValue, storePersonValue } from "@/lib/browserStorage";
import { validZone } from "./formulaDates";
import { clockAtInstant, localDate } from "@/lib/nativeDate";
import { toCalendarDate } from "@internationalized/date";
import { afterNextPaint, inSlices } from "@/lib/afterPress";
import { timeZoneNamePart, timeZoneOffset, timeZonePlace, timeZoneSearchRank, uniqueTimeZonePlaces, type ZoneCities } from "@/lib/timeZones";
import { TIME_ZONES } from "@/lib/timeZoneList";

export type { ZoneCities } from "@/lib/timeZones";

// The time zones a date's editor offers, as Notion's zone list shows them
// (seen 2026-09-30, 2026-11-14): the reader's own zone, the zones they chose
// lately, then Notion's zones one for each place, each by its place in the
// reader's language and its offset from GMT.

const RECENT_ZONES_KEY = "leafen:v2:date-zones:recent";
// Notion keeps the four chosen last and lists three of them (timeZoneGroups).
const RECENT_ZONES_LIMIT = 4;
const RECENT_ZONES_SHOWN = 3;

export type TimeZoneRow = { zone: string; city: string; offset: string; shortName: string };
export type TimeZoneGroup = { key: "current" | "recent" | "all"; rows: TimeZoneRow[] };

/** A zone's short name as the date editor's zone row shows it: "GMT+9", "EDT", "UTC". */
export function timeZoneShortName(zone: string, at = new Date()) {
  return timeZoneNamePart(zone, "short", at) || zone;
}

/** A zone's offset from GMT, as Notion's zone list writes it (timeZones.ts timeZoneOffset). */
export function timeZoneOffsetLabel(zone: string, at = new Date()) {
  return timeZoneOffset(zone, at);
}

/** An offset a time is written with, named as a zone's: ":00-04:00" is "GMT-4", "+05:30" "GMT+5:30". */
export function offsetShortName(offset: string) {
  const match = /([+-])(\d{2}):?(\d{2})$/u.exec(offset);
  if (!match || (match[2] === "00" && match[3] === "00")) return "UTC";
  return `GMT${match[1]}${Number(match[2])}${match[3] === "00" ? "" : `:${match[3]}`}`;
}

// The zones' cities in each of Leafen's languages, from the Unicode CLDR
// (scripts/generate-zone-city-names.mjs), each loaded on its own.
const zoneCityTables = import.meta.glob<{ cities: Record<string, string> }>(
  ["./zoneCities/*.json", "!./zoneCities/manifest.json"], { import: "default" },
);

/** The zones' cities in a language, as "Asia/Seoul": "서울"; none for a language without a table. */
export async function loadZoneCities(language: string): Promise<ZoneCities> {
  const load = zoneCityTables[`./zoneCities/${language}.json`];
  if (!load) return {};
  try {
    return (await load()).cities;
  } catch {
    return {};
  }
}

/**
 * A zone's city: its place as every zone list names it (timeZones.ts
 * timeZonePlace) — "뉴욕", "Pacific" for US/Pacific.
 */
export function timeZoneCity(zone: string, cities: ZoneCities = {}) {
  return timeZonePlace(zone, cities);
}

/** The zones a zone list offers, in Notion's order (timeZoneList.ts). */
export function allTimeZones(): string[] {
  return [...TIME_ZONES];
}

/**
 * Reads every zone's names for a zone list in slices once the frame after has
 * painted (afterPress.ts), so the list opened next draws from what
 * timeZoneNamePart keeps; returns how to stop. Read as the list opened, its
 * 838 formatters held the date window's "시간대" press 60–75 ms of its
 * 144–192 (2026-11-14, CPU ×2).
 */
export function readTimeZoneNamesAhead(): () => void {
  const controller = new AbortController();
  const cancel = afterNextPaint(() => {
    void inSlices(allTimeZones(), (zone) => {
      timeZoneNamePart(zone, "short");
      timeZoneNamePart(zone, "longOffset");
    }, { signal: controller.signal });
  }, controller.signal);
  return () => {
    controller.abort();
    cancel();
  };
}

export function timeZoneRow(zone: string, at = new Date(), cities: ZoneCities = {}): TimeZoneRow {
  return { zone, city: timeZoneCity(zone, cities), offset: timeZoneOffsetLabel(zone, at), shortName: timeZoneShortName(zone, at) };
}

/**
 * The zone list's groups, as Notion's (its code read 2026-11-14): the
 * reader's zone; of the zones they chose lately, the first three in the
 * list's order, newest first; and every other zone — each zone in one group
 * only. A search keeps the rows that answer it (timeZoneSearchRank: Notion's
 * list search over IANA name, city and offset), best first, else in the
 * list's order; the lately chosen stay newest first.
 */
export function timeZoneGroups(
  rows: readonly TimeZoneRow[], query: string, current: string, recent: readonly string[], cities: ZoneCities = {},
): TimeZoneGroup[] {
  // One zone for each place, as Notion's list (timeZones.ts uniqueTimeZonePlaces).
  const shown = uniqueTimeZonePlaces(rows, (row) => row.city);
  const lately = recent.slice(0, RECENT_ZONES_LIMIT);
  const groups: TimeZoneGroup[] = [{ key: "current", rows: [] }, { key: "recent", rows: [] }, { key: "all", rows: [] }];
  if (current && !shown.some((row) => row.zone === current)) groups[0].rows.push(timeZoneRow(current, new Date(), cities));
  for (const row of shown) {
    const group = row.zone === current ? 0 : lately.includes(row.zone) && groups[1].rows.length < RECENT_ZONES_SHOWN ? 1 : 2;
    groups[group].rows.push(row);
  }
  groups[1].rows.sort((a, b) => lately.indexOf(a.zone) - lately.indexOf(b.zone));
  return groups.map((group) => {
    const ranked = group.rows.map((row) => ({ row, rank: timeZoneSearchRank({ offset: row.offset, place: row.city, zone: row.zone }, query) }))
      .filter(({ rank }) => rank > 0);
    if (group.key !== "recent") ranked.sort((a, b) => b.rank - a.rank);
    return { ...group, rows: ranked.map(({ row }) => row) };
  }).filter((group) => group.rows.length > 0);
}

/** The zones this reader chose lately, newest first; none in a private window. */
export function recentTimeZones(): string[] {
  const stored = parseStored(personValue(RECENT_ZONES_KEY, { previous: [RECENT_ZONES_KEY] }));
  return Array.isArray(stored)
    ? stored.filter((zone): zone is string => typeof zone === "string" && validZone(zone)).slice(0, RECENT_ZONES_LIMIT)
    : [];
}

export function rememberTimeZone(zone: string) {
  const next = [zone, ...recentTimeZones().filter((recent) => recent !== zone)].slice(0, RECENT_ZONES_LIMIT);
  storePersonValue(RECENT_ZONES_KEY, JSON.stringify(next));
}

/** The day and "HH:MM" it is now on a clock written with an offset, as "-04:00". */
export function wallNowAtOffset(offset: string, now = new Date()): { date: Date; time: string } {
  const match = /([+-])(\d{2}):?(\d{2})$/u.exec(offset);
  if (!match) return wallNow("", now);
  const minutes = (match[1] === "-" ? -1 : 1) * (Number(match[2]) * 60 + Number(match[3]));
  const shifted = clockAtInstant(now, "UTC").add({ minutes });
  return { date: localDate(toCalendarDate(shifted)), time: `${String(shifted.hour).padStart(2, "0")}:${String(shifted.minute).padStart(2, "0")}` };
}

/** The day and "HH:MM" it is now on a zone's clock, or on the reader's own. */
export function wallNow(zone = "", now = new Date()): { date: Date; time: string } {
  // The native picker follows the device for a zone-less date; the stored
  // owner zone is explicit. Unknown imported zones keep the existing fallback.
  const moment = clockAtInstant(now, validZone(zone) ? zone : undefined);
  return { date: localDate(toCalendarDate(moment)), time: `${String(moment.hour).padStart(2, "0")}:${String(moment.minute).padStart(2, "0")}` };
}
