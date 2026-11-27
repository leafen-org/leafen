import { isIsoCalendarDay } from "./queryRelativeDate.mjs";
import type { QueryAdapters } from "./queryTypes";

// A datetime string is an *absolute instant* when it carries an explicit UTC
// marker (Z) or a numeric offset — its calendar day depends on the viewer's
// timezone. A bare "YYYY-MM-DD" (or a zone-less "YYYY-MM-DDTHH:MM") is a
// wall-clock value taken verbatim.
function hasExplicitZone(raw: string): boolean {
  return /T.*(Z|[+-]\d{2}:?\d{2})$/.test(raw);
}

// A wall-clock "YYYY-MM-DDTHH:MM" on a named time zone as an instant, or NaN.
function zonedWallInstant(wall: string, zone: string): number {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(wall);
  if (!m) return Number.NaN;
  let format: Intl.DateTimeFormat;
  try {
    format = new Intl.DateTimeFormat("en-US", {
      timeZone: zone, hourCycle: "h23", year: "numeric", month: "2-digit", day: "2-digit",
      hour: "2-digit", minute: "2-digit", second: "2-digit",
    });
  } catch {
    return Number.NaN;
  }
  const wallUtc = Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3]), Number(m[4]), Number(m[5]));
  const offset = (instant: number) => {
    const parts = Object.fromEntries(format.formatToParts(new Date(instant)).map((part) => [part.type, part.value]));
    return Date.UTC(Number(parts.year), Number(parts.month) - 1, Number(parts.day), Number(parts.hour),
      Number(parts.minute), Number(parts.second)) - instant;
  };
  return wallUtc - offset(wallUtc - offset(wallUtc));
}

// The instant of a date object whose time is kept on a time zone of its own
// ({start, timeZone}, as a date property stores one), or NaN.
function ownClockInstant(value: object): number {
  const { start, timeZone } = value as { start?: unknown; timeZone?: unknown };
  const text = typeof start === "string" ? start.trim() : "";
  if (typeof timeZone !== "string" || !timeZone || !/T\d{2}:\d{2}/.test(text) || hasExplicitZone(text)) return Number.NaN;
  return zonedWallInstant(text, timeZone);
}

// Calendar day (YYYY-MM-DD) of an instant, resolved in `timeZone` when given
// (en-CA formats as ISO), else the UTC day.
function dayFromInstant(instantMs: number, timeZone?: string): string {
  if (timeZone) {
    return new Intl.DateTimeFormat("en-CA", {
      timeZone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    }).format(new Date(instantMs));
  }
  return new Date(instantMs).toISOString().slice(0, 10);
}

// Day-granular date comparison key.
//
// When a `timeZone` is supplied (the web app passes the browser zone), absolute
// instants — created_time/last_edited_time, zoned datetimes, epoch numbers —
// resolve to their calendar day in that zone so the key matches the local time
// the cell displays. This is the fix for created_time being stored as "…Z" and
// shown in local time: a KST user's 01:00-Mar-16 row (stored "…T16:00:00Z"
// Mar 15) now keys as 2024-03-16, matching the cell and the filter.
//
// Without a `timeZone` (headless/backend default) the literal leading
// YYYY-MM-DD prefix is kept verbatim, so a day is never shifted off the zone it
// was written in — preserving the established backend semantics.
export function dateKey(value: unknown, timeZone?: string): string {
  if (value == null || value === "") return "";
  if (typeof value === "number") {
    return Number.isFinite(value) ? dayFromInstant(value, timeZone) : "";
  }
  if (value && typeof value === "object" && !Array.isArray(value)) {
    // A time kept on a clock of its own falls on the viewer's day, as its
    // cell shows it and Notion's calendar places it.
    const instant = timeZone ? ownClockInstant(value) : Number.NaN;
    if (Number.isFinite(instant)) return dayFromInstant(instant, timeZone);
    const start = (value as { start?: unknown }).start;
    if (start != null && start !== "") return dateKey(start, timeZone);
    return "";
  }
  const raw = String(value).trim();
  if (!raw) return "";
  if (timeZone && hasExplicitZone(raw)) {
    const t = Date.parse(raw);
    if (Number.isFinite(t)) return dayFromInstant(t, timeZone);
  }
  if (/^\d{4}-\d{2}-\d{2}/.test(raw)) return raw.slice(0, 10);
  const parsed = new Date(raw);
  if (Number.isFinite(parsed.getTime())) return dayFromInstant(parsed.getTime(), timeZone);
  return raw.slice(0, 10);
}

const MINUTE = 60_000;

/**
 * A date filter's value written with a time of day ("2020-01-02T10:00:00+09:00",
 * as the API takes one) as its minute, or NaN for a day alone. Such a filter
 * compares moments to the minute, as Notion's does (seen 2026-11-11), and a
 * time written with no offset is UTC (the server's databaseQueryFilterMinute).
 */
export function filterMinute(value: unknown): number {
  const text = typeof value === "string" ? value.trim() : "";
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/.test(text)) return Number.NaN;
  const instant = hasExplicitZone(text) ? Date.parse(text) : zonedWallInstant(text, "UTC");
  return Number.isFinite(instant) ? Math.floor(instant / MINUTE) * MINUTE : Number.NaN;
}

/**
 * A row's date as the minute it starts: a day alone at its midnight on the
 * reader's clock, a time with no offset on its own time zone, or the
 * reader's when it has none; NaN when it has no date.
 */
export function dateMinute(value: unknown, timeZone = "UTC"): number {
  if (value == null || value === "") return Number.NaN;
  if (typeof value === "number") return Number.isFinite(value) ? Math.floor(value / MINUTE) * MINUTE : Number.NaN;
  if (typeof value === "object" && !Array.isArray(value)) {
    const { start, timeZone: own } = value as { start?: unknown; timeZone?: unknown };
    return dateMinute(start, typeof own === "string" && own ? own : timeZone);
  }
  const text = String(value).trim();
  let instant = Number.NaN;
  if (hasExplicitZone(text)) instant = Date.parse(text);
  else if (/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/.test(text)) instant = zonedWallInstant(text, timeZone);
  else if (/^\d{4}-\d{2}-\d{2}$/.test(text)) instant = zonedWallInstant(`${text}T00:00`, timeZone);
  return Number.isFinite(instant) ? Math.floor(instant / MINUTE) * MINUTE : Number.NaN;
}

/**
 * Whether a date meets a before, after, on-or or equals condition: by the
 * minute when the filter's value has a time of day, else by the day on the
 * reader's clock.
 */
export function dateOrderMatches(operator: string, value: unknown, wanted: unknown, timeZone?: string): boolean {
  const at = filterMinute(wanted);
  let order: number;
  if (Number.isFinite(at)) {
    const minute = dateMinute(value, timeZone);
    if (!Number.isFinite(minute)) return false;
    order = Math.sign(minute - at);
  } else {
    const day = dateKey(value, timeZone), wantedDay = dateKey(wanted, timeZone);
    if (day === "") return false;
    order = day < wantedDay ? -1 : day > wantedDay ? 1 : 0;
  }
  switch (operator) {
    case "equals": return order === 0;
    case "before": return order < 0;
    case "after": return order > 0;
    case "on_or_before": return order <= 0;
    case "on_or_after": return order >= 0;
    default: return true;
  }
}

/**
 * A date value read by its end: the value with its end as its start (keeping
 * its time zone), or the value itself when it has no end.
 */
export function dateEndValue(value: unknown): unknown {
  if (!value || typeof value !== "object" || Array.isArray(value)) return value;
  const end = (value as { end?: unknown }).end;
  return end != null && end !== "" ? { ...(value as object), start: end, end: undefined } : value;
}

// Sub-day-precise sort key for date-like values, so same-day rows order by time
// (the day-granular `dateKey` collapses them). Absolute instants resolve to
// their UTC instant (comparable and environment-independent); zone-less values
// sort by their literal ISO text, so a day-only "2024-03-15" precedes same-day
// datetimes (i.e. sorts at the start of that day).
export function dateSortKey(value: unknown): string {
  if (value == null || value === "") return "";
  if (typeof value === "number") return Number.isFinite(value) ? new Date(value).toISOString() : "";
  if (value && typeof value === "object" && !Array.isArray(value)) {
    // A time kept on a clock of its own sorts by the moment it names.
    const instant = ownClockInstant(value);
    if (Number.isFinite(instant)) return new Date(instant).toISOString();
    const start = (value as { start?: unknown }).start;
    return start != null && start !== "" ? dateSortKey(start) : "";
  }
  const raw = String(value).trim();
  if (!raw) return "";
  if (hasExplicitZone(raw)) {
    const t = Date.parse(raw);
    if (Number.isFinite(t)) return new Date(t).toISOString();
  }
  return raw;
}

export function filterReferenceDay(a: QueryAdapters): string {
  if (typeof a.referenceDay === "string" && isIsoCalendarDay(a.referenceDay)) return a.referenceDay;
  const instant = a.referenceTime?.getTime();
  return dayFromInstant(
    typeof instant === "number" && Number.isFinite(instant) ? instant : Date.now(),
    a.timeZone,
  );
}
