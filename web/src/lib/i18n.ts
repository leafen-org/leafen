// Single home for UI locale resolution. The native generation currently owns
// its translated labels through the document language instead of i18next, so
// formatting follows that same visible UI-language seam.
function activeLanguage(): string {
  if (typeof document !== "undefined" && document.documentElement.lang) {
    return document.documentElement.lang;
  }
  return typeof navigator !== "undefined" ? navigator.language : "en";
}

export function isKoreanLocale(): boolean {
  return activeLanguage().toLowerCase().startsWith("ko");
}

// BCP-47 locale for DISPLAY-ONLY Intl formatting. It follows the active UI
// language rather than the browser/OS locale, so dates, numbers and currencies
// do not switch language underneath otherwise translated UI.
function activeIntlLocale(): string {
  const normalized = activeLanguage().trim().replaceAll("_", "-");
  const base = normalized.split("-", 1)[0]?.toLowerCase();
  if (base === "ko") return "ko-KR";
  if (base === "en") return "en-US";
  try {
    return Intl.getCanonicalLocales(normalized)[0] ?? "en-US";
  } catch {
    return "en-US";
  }
}

export function activeDateLocale(): string {
  return activeIntlLocale();
}

export function activeNumberLocale(): string {
  return activeIntlLocale();
}

/**
 * The locale for the figures Notion writes in the language's own digits.
 * Notion's Arabic (seen 2026-11-19) writes a menu foot's last edit and its
 * word and character counts ("اليوم الساعة ٩:٤٥ ص", "١٨ من الكلمات"), a
 * calendar month's first day ("١ نوفمبر"), a percent or currency value
 * ("٢٥٪؜", "‏١٬٢٣٤٫٥٠ US$") and the version history's change count in
 * Arabic-Indic digits, and keeps Western digits everywhere else — dates and
 * times in values, mentions, pickers, the timeline and the history list,
 * "… ago" times, other counts and grid numbers — which activeDateLocale
 * writes. Its other languages write the same digits in both, so this is that
 * locale.
 */
export function activeOwnDigitsLocale(): string {
  const locale = activeIntlLocale();
  return locale.split("-", 1)[0] === "ar" ? `${locale}-u-nu-arab` : locale;
}

const ownDigitSets = new Map<string, readonly string[] | null>();

/** `text` with its Western digits written in activeOwnDigitsLocale's digits. */
export function ownDigits(text: string): string {
  const locale = activeOwnDigitsLocale();
  let digits = ownDigitSets.get(locale);
  if (digits === undefined) {
    const format = new Intl.NumberFormat(locale, { useGrouping: false });
    const set = Array.from({ length: 10 }, (_, digit) => format.format(digit));
    digits = set.every((digit, index) => digit === String(index)) ? null : set;
    ownDigitSets.set(locale, digits);
  }
  if (!digits) return text;
  const own = digits;
  return text.replace(/[0-9]/g, (digit) => own[Number(digit)]);
}
