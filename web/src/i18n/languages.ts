// Display metadata for every language with a catalog. `value` matches the
// folder/wrapper name and `label` is the endonym shown to the user; the codes
// are the account-state allow-list's (contracts/auth-wire.json).
//
// Which of them can be chosen is decided there, in one place. None is hidden
// now: all 58 are offered in every language list (the first run's, the
// account's and Settings'). A language marked "hidden" on its line there
// would stay here and in src/locales, but no list would offer it, browser
// detection would pass over it, the server would refuse it, and an account
// that chose it before would be shown English; dropping the mark and running
// `node scripts/generate-auth-wire.mjs` offers it again.
import { selectableAccountLanguagePreferences } from "@/authWire.generated";

export interface LanguageOption {
  value: string;
  label: string;
}

const CATALOG_LANGUAGE_OPTIONS: LanguageOption[] = [
  { value: "ko", label: "한국어" },
  { value: "en", label: "English" },
  { value: "ja", label: "日本語" },
  { value: "zh-Hans", label: "简体中文" },
  { value: "zh-Hant", label: "繁體中文" },
  { value: "es", label: "Español" },
  { value: "fr", label: "Français" },
  { value: "de", label: "Deutsch" },
  { value: "pt-BR", label: "Português (Brasil)" },
  { value: "pt-PT", label: "Português (Portugal)" },
  { value: "it", label: "Italiano" },
  { value: "nl", label: "Nederlands" },
  { value: "sv", label: "Svenska" },
  { value: "da", label: "Dansk" },
  { value: "nb", label: "Norsk" },
  { value: "fi", label: "Suomi" },
  { value: "pl", label: "Polski" },
  { value: "cs", label: "Čeština" },
  { value: "sk", label: "Slovenčina" },
  { value: "hu", label: "Magyar" },
  { value: "ro", label: "Română" },
  { value: "el", label: "Ελληνικά" },
  { value: "hr", label: "Hrvatski" },
  { value: "sr", label: "Српски" },
  { value: "sl", label: "Slovenščina" },
  { value: "bg", label: "Български" },
  { value: "uk", label: "Українська" },
  { value: "ru", label: "Русский" },
  { value: "ar", label: "العربية" },
  { value: "he", label: "עברית" },
  { value: "fa", label: "فارسی" },
  { value: "ur", label: "اردو" },
  { value: "hi", label: "हिन्दी" },
  { value: "bn", label: "বাংলা" },
  { value: "ta", label: "தமிழ்" },
  { value: "te", label: "తెలుగు" },
  { value: "mr", label: "मराठी" },
  { value: "gu", label: "ગુજરાતી" },
  { value: "kn", label: "ಕನ್ನಡ" },
  { value: "pa", label: "ਪੰਜਾਬੀ" },
  { value: "ml", label: "മലയാളം" },
  { value: "ne", label: "नेपाली" },
  { value: "si", label: "සිංහල" },
  { value: "vi", label: "Tiếng Việt" },
  { value: "th", label: "ไทย" },
  { value: "id", label: "Bahasa Indonesia" },
  { value: "ms", label: "Bahasa Melayu" },
  { value: "fil", label: "Filipino" },
  { value: "my", label: "မြန်မာ" },
  { value: "km", label: "ខ្មែរ" },
  { value: "tr", label: "Türkçe" },
  { value: "az", label: "Azərbaycanca" },
  { value: "kk", label: "Қазақша" },
  { value: "uz", label: "Oʻzbekcha" },
  { value: "sw", label: "Kiswahili" },
  { value: "am", label: "አማርኛ" },
  { value: "af", label: "Afrikaans" },
  { value: "ha", label: "Hausa" },
];

const selectable = new Set<string>(selectableAccountLanguagePreferences);

/** The languages a person can choose, in this list's order. */
export const LANGUAGE_OPTIONS: LanguageOption[] = CATALOG_LANGUAGE_OPTIONS.filter(({ value }) => selectable.has(value));

/** Languages with a catalog that is hidden until it is translated: none now. */
export const HIDDEN_LANGUAGES: readonly string[] = CATALOG_LANGUAGE_OPTIONS
  .filter(({ value }) => !selectable.has(value))
  .map(({ value }) => value);

/**
 * The languages as Notion's list names them (seen 2026-10-30 in its
 * settings, 2026-11-19 on its signed-out login page): each in its own words
 * over its name in the screen's language, in that name's order.
 */
export function languageChoices(screenLanguage: string): (LanguageOption & { name: string })[] {
  let names: Intl.DisplayNames | null = null;
  try { names = new Intl.DisplayNames([screenLanguage], { type: "language" }); } catch { names = null; }
  const collator = new Intl.Collator(screenLanguage);
  return LANGUAGE_OPTIONS
    .map((option) => ({ ...option, name: names?.of(option.value) ?? option.label }))
    .sort((a, b) => collator.compare(a.name, b.name));
}
