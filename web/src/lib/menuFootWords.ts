import { i18next } from "@/i18n";
import { activeOwnDigitsLocale } from "@/lib/i18n";
import { menuFootTimeLabel } from "@/lib/pageHistoryGroups";

// A menu foot names today's and yesterday's last edit in words of its own,
// as Notion's (dateFormatHelpers.formatMillisToCalendar.todayAt — Arabic
// "اليوم الساعة {time}"), not the version history's (versionHistory2
// .entryTimestamp.todayAt — "اليوم في الساعة {time}"); English and Korean
// write the two alike. A language whose catalog has no foot words yet says
// the history's, as it did before they were its own.
function footWord(foot: "timeToday" | "timeYesterday") {
  const language = i18next.resolvedLanguage ?? i18next.language;
  const own = i18next.getResource(language, "blockHandle", `footer.${foot}`) !== undefined;
  return (time: string) => own ? i18next.t(`blockHandle:footer.${foot}`, { time }) : i18next.t(`updatesPanel:versions.${foot}`, { time });
}

/**
 * When a page, row or block was last edited, as a menu's foot writes it: by
 * the foot's own day words today and yesterday, in the language's own digits
 * (Arabic "اليوم الساعة ٩:٤٥ ص", seen 2026-11-19).
 */
export function menuFootEditedTime(editedAt: number): string {
  return menuFootTimeLabel(editedAt, activeOwnDigitsLocale(), { today: footWord("timeToday"), yesterday: footWord("timeYesterday") });
}
