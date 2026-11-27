// The shapes the date views (timeline, calendar) write their dates in, each
// made once a language: a year of days asked the browser for a new format
// with each of their labels (some 460 as a 46-row timeline with its table
// opened, ~30 ms a drawing at CPU ×2, 2026-11-15), and a calendar's 27 weeks
// of days named each of theirs with every drawing (189 days, ~15 ms a drawing
// at CPU ×2, five drawings as a card is dropped on a day, 2026-11-18). The
// same text as `toLocaleDateString` with them.
const DATE_SHAPES = {
  month: { month: "short" },
  longMonth: { month: "long" },
  date: { month: "short", day: "numeric", year: "numeric" },
  dateTime: { month: "short", day: "numeric", year: "numeric", hour: "numeric", minute: "2-digit" },
  shortDay: { month: "short", day: "numeric" },
  shortDayTime: { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" },
  weekday: { weekday: "short" },
  longWeekday: { weekday: "long" },
  year: { year: "numeric" },
  monthYear: { month: "short", year: "numeric" },
  longMonthYear: { month: "long", year: "numeric" },
  longDate: { month: "long", year: "numeric", day: "numeric" },
  hour: { hour: "numeric" },
  hourMinute: { hour: "numeric", minute: "2-digit" },
} satisfies Record<string, Intl.DateTimeFormatOptions>;
const dateFormats = new Map<string, Intl.DateTimeFormat>();

export type DateShape = keyof typeof DATE_SHAPES;

function formatOf(shape: DateShape, locale?: string) {
  const id = `${locale ?? ""}\0${shape}`;
  let format = dateFormats.get(id);
  if (!format) {
    format = new Intl.DateTimeFormat(locale, DATE_SHAPES[shape]);
    dateFormats.set(id, format);
  }
  return format;
}

/** A date written in one of the date views' shapes for a language (the browser's when none). */
export function dateInShape(date: Date, shape: DateShape, locale?: string) {
  return formatOf(shape, locale).format(date);
}

/** The same, in its parts: its hour, its time of day, the words between them. */
export function dateInShapeParts(date: Date, shape: DateShape, locale?: string) {
  return formatOf(shape, locale).formatToParts(date);
}
