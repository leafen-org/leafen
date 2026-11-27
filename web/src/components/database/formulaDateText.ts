import { clockOffset, ownZone, parseFormulaDate, wallFromUtc } from "./formulaDates";
import { formatFormulaDate, wallMilliseconds, type FormulaMoment } from "./formulaFormats";
import type { FormulaViewer } from "./formulaLocales";
import { FormulaDate, FormulaRichText, type FormulaValue } from "./formulaSyntax";
import { isFormulaReference, richPlain, toText } from "./formulaValues";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";

// A formula writes a date as text as Notion does, in the viewer's language
// (formulaLocales.ts): a day in its LL layout, "August 30, 2023", and a time
// in its LLL layout, "August 30, 2023 2:05 PM", on the clock the time keeps,
// that clock's name following in brackets, "(EDT)" or "(GMT+9)", when the
// viewer's zone is at another offset. A time zone goes by the short name Intl
// gives it in the language at that offset, else by the offset. A range joins
// its ends with " → ", the end's time alone (LT) when it falls on the same
// day. Joined to other text, a date is a mention showing "@" and this text
// (formulaRichText.ts). The server writes dates the same way in English
// (database_formula_date_text.go).

type DateEnd = FormulaMoment & { offset: number; viewerOffset: number; zone: string };

// dateEnd reads one end of a date as the viewer sees it: a day, or a time on
// the clock it keeps.
function dateEnd(text: string, zone: string, timeZone: string): DateEnd | null {
  const moment = parseFormulaDate(new FormulaDate(text, zone), timeZone);
  if (!moment) return null;
  const viewerOffset = wallMilliseconds(moment.wall) - moment.instant;
  if (!moment.hasTime || !moment.own) return { ...moment, offset: viewerOffset, viewerOffset, zone: "" };
  const offset = clockOffset(moment.own, moment.instant);
  return { ...moment, wall: wallFromUtc(moment.instant + offset), offset, viewerOffset, zone: ownZone(moment) };
}

const zoneNameFormats = new Map<string, Intl.DateTimeFormat>();

// intlZoneName is a time zone's short name as Intl writes it in a language at
// an instant, for a language without a zones table: "GMT-4", "UTC−4".
function intlZoneName(language: string, zone: string, instant: number) {
  const key = `${language}\u0000${zone}`;
  let format = zoneNameFormats.get(key);
  if (!format) {
    try {
      format = new Intl.DateTimeFormat(language, { timeZone: zone, timeZoneName: "short" });
    } catch {
      return undefined;
    }
    zoneNameFormats.set(key, format);
  }
  return format.formatToParts(instant).find((part) => part.type === "timeZoneName")?.value;
}

// zoneLabel names the clock a time keeps: a named time zone by its short
// name in the viewer's language at that offset, else the offset.
function zoneLabel(end: DateEnd, viewer: FormulaViewer) {
  const { zones, intl } = viewer.locale;
  return zones[end.zone]?.[Math.round(end.offset / 60_000)]
    ?? (intl && end.zone ? intlZoneName(intl, end.zone, end.instant) : undefined)
    ?? offsetLabel(end.offset);
}

function offsetLabel(offset: number) {
  if (offset === 0) return "UTC";
  const minutes = Math.abs(offset) / 60_000;
  const label = `GMT${offset < 0 ? "-" : "+"}${Math.floor(minutes / 60)}`;
  return minutes % 60 ? `${label}:${String(minutes % 60).padStart(2, "0")}` : label;
}

function dateEndText(end: DateEnd, viewer: FormulaViewer) {
  const { layouts } = viewer.locale;
  if (!end.hasTime) return formatFormulaDate(end, layouts.LL, viewer.locale);
  const text = formatFormulaDate(end, layouts.LLL, viewer.locale);
  return end.offset === end.viewerOffset ? text : `${text} (${zoneLabel(end, viewer)})`;
}

// formulaDateText writes a date as Notion writes it as text.
export function formulaDateText(value: FormulaDate, viewer: FormulaViewer) {
  const split = value.text.indexOf("/");
  const start = dateEnd(split < 0 ? value.text : value.text.slice(0, split), value.zone, viewer.timeZone);
  // A date it can't read — Notion's invalid date, a day past 9999 — is no
  // text, a range's end with its arrow alone, its spaces kept as Notion's
  // editor keeps them (seen 2026-11-15: format(dateRange(now(),
  // fromTimestamp(3e14))) is "2026년 11월 15일 오후 2:16 → ", the other way
  // " → 2026년 11월 15일 오후 2:17"; its API trims only a text result's ends, as the server's API).
  const text = start ? dateEndText(start, viewer) : "";
  if (split < 0) return text;
  const end = dateEnd(value.text.slice(split + 1), value.zone, viewer.timeZone);
  if (!start || !end) return `${text} → ${end ? dateEndText(end, viewer) : ""}`;
  const sameDay = start.wall.year === end.wall.year && start.wall.month === end.wall.month && start.wall.day === end.wall.day;
  if (start.hasTime && end.hasTime && sameDay) return `${text} → ${formatFormulaDate(end, viewer.locale.layouts.LT, viewer.locale)}`;
  return `${text} → ${dateEndText(end, viewer)}`;
}

// formatText is a value as format() writes it: a date as its text, rich
// text as its plain text, a list's items, flattened, between commas.
export function formatText(value: FormulaValue | undefined, viewer: FormulaViewer): string {
  if (value instanceof FormulaDate) return formulaDateText(value, viewer);
  if (value instanceof FormulaRichText) return richPlain(value);
  if (Array.isArray(value)) return value.map((item) => formatText(item, viewer)).join(",");
  // An untitled page writes the viewer's word for one, as Notion's format()
  // ("새 페이지" on its Korean screen, "New page" through its API, seen 2026-11-12).
  if (isFormulaReference(value) && value.kind === "page" && !value.name) return untitledPageDisplayTitle("page");
  return toText(value);
}
