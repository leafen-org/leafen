import { useState } from "react";
import { useTranslation } from "react-i18next";
import { CalendarDate, getLocalTimeZone, today } from "@internationalized/date";
import { ChevronDown } from "@/icons/leafen";
import { PhoneSheetMenu, PhoneSheetRow } from "@/components/shell/PhoneSheet";
import windowStyles from "@/components/shell/PhoneSheetWindow.module.css";
import { useWeekStartDay } from "@/lib/displayPreferences";
import { NativeDateCalendar } from "@/lib/nativeDateCalendar";
import { phoneScreen } from "@/lib/screenKind";
import calendarStyles from "@/components/database/DateFilterEditor.module.css";
import { expiryAt, expiryChoices } from "./topBarShareModel";
import { choiceLabel, choiceWhen, expiryValue } from "./TopBarShareWebRole";
import css from "./TopBarSharePhone.module.css";
import expiryStyles from "./TopBarShareWebExpiry.module.css";

/**
 * "링크 만료 시점" in a touch screen's "사용 권한 변경" sheet of the web
 * link, as Notion's phone and iPad (seen 2026-11-13): the words and, at the
 * row's end, when the link stops with "⌄" over a line, heading "제거"'s group
 * in a box of its own (seen 2026-11-18 on the A90); a whole date keeps
 * the row on one line, the words ending in "…" as the computer's menu row
 * (Notion's phone row with a date, seen 2026-11-18 on the A90). Its choices (none, an
 * hour, a day or a week from now, each with when, and "날짜 선택 ›") are a
 * phone's bottom list; "날짜 선택" opens a sheet of a month with no head over
 * it, today chosen, and closing that sheet goes back to the list (Notion's
 * iPhone Safari, seen twice 2026-11-16 at 402). On a wider
 * touch screen the list is the iPad's middle window (PhoneSheetWindow), the
 * month opening in it under "날짜 선택" (Notion's iPad Safari, seen twice
 * 2026-11-16 at 834).
 */
export function TopBarShareWebExpiryPhone({ expiresAt, disabled, onExpiry }: {
  expiresAt: string; disabled: boolean; onExpiry(expiresAt: string): void;
}) {
  const { t } = useTranslation("topBar");
  const [open, setOpen] = useState<"list" | "date" | null>(null);
  const wide = !phoneScreen();
  const pick = (next: string) => { setOpen(null); onExpiry(next); };
  const calendar = <ExpiryCalendar expiresAt={expiresAt} onPick={pick} />;
  return <>
    <div className={expiryStyles.phoneSection}>
      <button type="button" className={css.expiryRow} disabled={disabled} aria-haspopup="dialog" aria-expanded={open !== null}
        data-share-web-expiry onClick={() => setOpen("list")}>
        <span className={expiryStyles.phoneLabel}>{t("linkExpiresAt")}</span>
        <span className={`${css.expiryValue} ${expiryStyles.phoneValue}`}>{expiryValue(expiresAt, t)}<ChevronDown size="page" aria-hidden="true" /></span>
      </button>
    </div>
    {open !== null ? <PhoneSheetMenu label={t("linkExpiresAt")}
      className={wide ? `${windowStyles.window} ${expiryStyles.window}` : css.expiryList} onClose={() => setOpen(null)}>
      {expiryChoices.map(choice => <PhoneSheetRow key={choice} label={choiceLabel(choice, t)} description={choiceWhen(choice) || undefined}
        more={false} data={{ "data-share-web-expiry-option": choice }} onPress={() => pick(expiryAt(choice))} />)}
      <PhoneSheetRow label={t("pickExpiryDate")} data={{ "data-share-web-expiry-option": "date" }}
        onPress={() => setOpen(wide && open === "date" ? "list" : "date")} />
      {wide && open === "date" ? <div className={expiryStyles.windowCalendar} data-share-web-expiry-calendar="">{calendar}</div> : null}
    </PhoneSheetMenu> : null}
    {!wide && open === "date" ? <PhoneSheetMenu label={t("pickExpiryDate")} className={expiryStyles.dateSheet} onClose={() => setOpen("list")}>
      <div data-share-web-expiry-calendar="">{calendar}</div>
    </PhoneSheetMenu> : null}
  </>;
}

/**
 * The month to pick the link's last day on, as Notion's touch screens draw it
 * (iPhone and iPad Safari, seen 2026-11-16): the month and "‹ ›" over the
 * weekdays, 44pt days with a 36pt blue box on the day chosen — the link's
 * expiry, else today — the days before today faint and taking no press.
 */
function ExpiryCalendar({ expiresAt, onPick }: { expiresAt: string; onPick(expiresAt: string): void }) {
  const { t } = useTranslation(["topBar", "propertyCell"]);
  const weekStart = useWeekStartDay();
  const now = today(getLocalTimeZone());
  // The link's expiry stands chosen, the calendar at its month (TopBarShareWebRole's); none, and today is.
  const expiry = expiresAt ? new Date(expiresAt) : null;
  const chosen = (expiry && !Number.isNaN(expiry.getTime())
    ? new CalendarDate(expiry.getFullYear(), expiry.getMonth() + 1, expiry.getDate()) : null) ?? now;
  return <NativeDateCalendar value={chosen} focusedValue={chosen} weekStart={weekStart} min={now}
    label={t("topBar:pickExpiryDate")} previousLabel={t("propertyCell:prevMonth")} nextLabel={t("propertyCell:nextMonth")}
    classes={{ root: `${calendarStyles.phoneCalendar} ${expiryStyles.touchCalendar}`,
      head: `${calendarStyles.phoneCalendarHead} ${calendarStyles.filterHead} ${expiryStyles.touchHead}`, title: calendarStyles.phoneMonth,
      shift: calendarStyles.shift, weekdays: `${calendarStyles.phoneWeekdays} ${calendarStyles.filterWeekdays}`,
      grid: `${calendarStyles.phoneDays} ${calendarStyles.filterDays} ${expiryStyles.touchDays}`, day: `${calendarStyles.day} ${calendarStyles.phoneDay}` }}
    marker={date => date.compare(chosen) === 0 ? { "data-mark": "start", "data-single": "true" } : {}}
    onPick={(day: CalendarDate) => onPick(new Date(day.year, day.month - 1, day.day).toISOString())} />;
}
