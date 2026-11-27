// react-i18next runtime setup.
//
// English (src/locales/en) is the SOURCE OF TRUTH; every other language is a
// translation catalog tracked against it (staleness detected by
// scripts/i18n-status.mjs). Namespace files are auto-discovered by each
// src/locales/<lang>.ts wrapper; the wrappers themselves are auto-discovered by
// the Vite glob below. Adding a namespace needs no runtime change. Translation
// target wrappers may exist before a language is released in the selector.
//
// Locale selection: an authenticated account's server preference is mirrored
// into an account-scoped local cache for first paint. Public shares, signed-out
// visitors and anonymous sessions ignore that cache and follow the browser (or
// the language chosen on the first screen before any account here).
// Browser tags are normalized to a released catalog before i18next starts so
// script/region variants remain intact.
import i18next, { type BackendModule, type PostProcessorModule, type ReadCallback } from "i18next";
import { initReactI18next } from "react-i18next";
import { HIDDEN_LANGUAGES, LANGUAGE_OPTIONS } from "./languages";
import { fetchAhead } from "@/lib/startSettled";
import { englishAlsoRead } from "./englishAlsoRead";
import { declareLocaleFontFaces } from "@/lib/localeFontFaces";
import { firstRunLanguage } from "@/lib/firstRunLanguage";

type LanguageCatalogs = Record<string, Record<string, unknown>>;
type LanguageCatalogModule = { catalogs: LanguageCatalogs };

const languageModules = import.meta.glob<LanguageCatalogModule>("../locales/*.ts");

// A line not yet translated into a right-to-left language shows in English.
// It is set apart as its own run, or its full stop jumps to the line's front
// (".Scheduled backups…").
const isolateFallback: PostProcessorModule = {
  type: "postProcessor",
  name: "isolateFallback",
  process(value: string, _key: string | string[], options: { i18nResolved?: { usedLng?: string } }, translator: { language?: string }) {
    const shown = translator?.language ?? "", used = options?.i18nResolved?.usedLng;
    return used && used !== shown && documentDirectionForLanguage(shown) === "rtl" ? `\u2068${value}\u2069` : value;
  },
};
const languageLoaders = new Map<string, () => Promise<LanguageCatalogModule>>();
const languageLoadersByLower = new Map<string, () => Promise<LanguageCatalogModule>>();
for (const [path, loader] of Object.entries(languageModules)) {
  const language = /\/locales\/([^/]+)\.ts$/.exec(path)?.[1];
  if (language) {
    languageLoaders.set(language, loader);
    languageLoadersByLower.set(language.toLowerCase(), loader);
  }
}

export const SUPPORTED_LANGUAGES = LANGUAGE_OPTIONS.map((option) => option.value);
const DEFAULT_NS = "common";

// Prefix for account-scoped language caches. Never read the old unscoped key as
// a preference: doing so would leak one account's UI language into another
// account or into a public share opened in the same browser.
const LANGUAGE_STORAGE_KEY = "leafen:language";

export function languageStorageKey(userId: string): string {
  return `${LANGUAGE_STORAGE_KEY}:${encodeURIComponent(userId.trim())}`;
}

function canonicalLanguageTag(language: string): string {
  const normalized = language.trim().replaceAll("_", "-");
  if (!normalized) return "";
  try {
    return Intl.getCanonicalLocales(normalized)[0] ?? normalized;
  } catch {
    return normalized;
  }
}

function supportedLanguageForCandidate(
  candidate: string,
  supportedLower: ReadonlyMap<string, string>,
): string | undefined {
  const canonical = canonicalLanguageTag(candidate);
  const lower = canonical.toLowerCase();
  if (!lower) return undefined;

  const exact = supportedLower.get(lower);
  if (exact) return exact;

  const [base, ...subtags] = lower.split("-");
  if (base === "zh") {
    const traditional = subtags.includes("hant") || subtags.some((part) =>
      part === "tw" || part === "hk" || part === "mo"
    );
    if (traditional) {
      // Do not silently show Simplified Chinese to Traditional Chinese users.
      // Until zh-Hant is released, continue to the next requested language and
      // ultimately use English.
      return supportedLower.get("zh-hant");
    }
    return supportedLower.get("zh-hans");
  }

  if (base === "pt" && subtags.length === 0) {
    return supportedLower.get("pt-br") ?? supportedLower.get("pt");
  }

  return supportedLower.get(base);
}

export function resolveSupportedLanguage(
  requested: readonly string[],
  supported: readonly string[] = SUPPORTED_LANGUAGES,
): string {
  const supportedLower = new Map(supported.map((language) => [language.toLowerCase(), language]));
  for (const candidate of requested) {
    const resolved = supportedLanguageForCandidate(candidate, supportedLower);
    if (resolved) return resolved;
  }
  return supportedLower.get("en") ?? (supported[0] ?? "en");
}

const catalogPromises = new Map<string, Promise<LanguageCatalogs>>();
function loadLanguageCatalogs(language: string) {
  const resolved = resolveSupportedLanguage([language]);
  const cacheKey = resolved.toLowerCase();
  const cached = catalogPromises.get(cacheKey);
  if (cached) return cached;
  const loader = languageLoadersByLower.get(cacheKey);
  const request: Promise<LanguageCatalogs> = loader
    ? loader().then((module) => module.catalogs)
    : Promise.resolve({} as LanguageCatalogs);
  catalogPromises.set(cacheKey, request);
  return request;
}

// The screens drawn outside the signed-in app follow the browser: a share's
// page always (App.tsx), and a form's link to one signed out. Every other
// address is the app's and speaks the account's language — a form's link
// signed in too, which opens as a page of the app (MemberFormPage) — as the
// app's account step holds it to (applyAccountLanguage), so the two never
// disagree (2026-11-09: a phone in English reloaded a Korean account's form
// link every second, each load starting in English again).
export function shouldUseAccountLanguagePreference(pathname: string, signedIn = false): boolean {
  if (/^\/share\/[^/?#]+\/?$/u.test(pathname)) return false;
  return signedIn || (pathname !== "/form" && !pathname.startsWith("/form/"));
}

const hiddenLanguagesLower = new Set(HIDDEN_LANGUAGES.map((language) => language.toLowerCase()));

// A language hidden until it is translated (i18n/languages.ts; none is now)
// that an account chose before reads in English, the language its catalog
// falls back to; the account keeps it, and it returns once the language is
// offered again.
function hiddenLanguage(pref: string): boolean {
  const lower = canonicalLanguageTag(pref).toLowerCase();
  return hiddenLanguagesLower.has(lower) || hiddenLanguagesLower.has(lower.split("-")[0]);
}

function normalizeLanguagePreference(pref: string): string | undefined {
  if (pref === "system") return "system";
  const supportedLower = new Map(
    SUPPORTED_LANGUAGES.map((language) => [language.toLowerCase(), language]),
  );
  return supportedLanguageForCandidate(pref, supportedLower) ?? (hiddenLanguage(pref) ? "en" : undefined);
}

/** Whether a language preference is one a person can choose now: "system" or an offered language. */
export function isSelectableLanguagePreference(pref: string | null | undefined): boolean {
  return pref === "system" || (typeof pref === "string" && SUPPORTED_LANGUAGES.includes(pref));
}

function storedLanguagePreference(userId = ""): string | undefined {
  if (!userId) return undefined;
  try {
    const stored = localStorage.getItem(languageStorageKey(userId));
    if (!stored) return undefined;
    return normalizeLanguagePreference(stored);
  } catch {
    return undefined;
  }
}

export function browserLanguage(): string {
  const requested: string[] = [];
  if (typeof navigator !== "undefined") {
    if (Array.isArray(navigator.languages)) requested.push(...navigator.languages);
    if (navigator.language) requested.push(navigator.language);
  }
  if (typeof document !== "undefined" && document.documentElement.lang) {
    requested.push(document.documentElement.lang);
  }
  return resolveSupportedLanguage(requested.length > 0 ? requested : ["en"]);
}

/**
 * The language a screen without an account reads in: the one chosen on the
 * first screen before any account here (lib/firstRunLanguage.ts), else this
 * device's.
 */
export function signedOutLanguage(): string {
  const chosen = normalizeLanguagePreference(firstRunLanguage());
  return chosen && chosen !== "system" ? chosen : browserLanguage();
}

const resourceBackend: BackendModule = {
  type: "backend",
  init: () => {},
  read(language: string, namespace: string, callback: ReadCallback) {
    loadLanguageCatalogs(language)
      .then((catalogs) => callback(null, (catalogs[namespace] ?? {}) as Parameters<ReadCallback>[1]))
      .catch((error) => callback(error, false));
  },
};

// i18next starts once; a page that needs it before it knows its account (the
// packaged app's starting screen) starts it early, and a later call moves to
// the language that account asks for.
let initialization: Promise<unknown> | null = null;

export async function initI18n(userIdHint = ""): Promise<unknown> {
  const useAccountPreference = typeof window !== "undefined"
    ? shouldUseAccountLanguagePreference(window.location.pathname, Boolean(userIdHint))
    : false;
  const preference = useAccountPreference
    ? storedLanguagePreference(userIdHint)
    : undefined;
  const language = preference && preference !== "system" ? preference : signedOutLanguage();
  if (initialization) {
    const started = await initialization.then(() => true, () => false);
    if (!started) {
      initialization = null;
      return initI18n(userIdHint);
    }
    if (i18next.language !== language) await switchLanguage(language);
    return i18next;
  }
  initialization = startI18n(language);
  return initialization;
}

// Languages with every key translated (the English + Korean gate of the
// private i18n status check). They draw the first screen without English,
// the fallback for a key a translation lacks, and need no more of it than
// what a screen reads in English by name (englishAlsoRead).
const COMPLETE_LANGUAGES = new Set(["en", "ko"]);

// The English a screen in another complete language reads by name
// (englishAlsoRead.ts) follows alone, over the network once the page's start
// has settled (startSettled.ts) — not English's whole catalog, which a Korean
// page fetched (775 KB of script) and kept (4 MB of its memory) with no line
// to show from it (2026-11-14).
let englishPart: Promise<void> | null = null;
let englishFallback: Promise<void> | null = null;

function loadEnglishAlsoRead() {
  englishPart ??= Promise.all(Object.entries(englishAlsoRead).map(async ([path, load]) => {
    const namespace = /\/([^/]+)\.json$/u.exec(path)?.[1] ?? "";
    i18next.addResourceBundle("en", namespace, await load(), true, true);
  })).then(() => undefined, () => { englishPart = null; });
  return englishPart;
}

async function startI18n(language: string): Promise<unknown> {
  // Both catalogs a screen needs are fetched in parallel and seeded into
  // i18next, so its backend does not request either a second time.
  const complete = COMPLETE_LANGUAGES.has(language.toLowerCase());
  const [sourceCatalogs, activeCatalogs] = await Promise.all([
    complete && language !== "en" ? null : loadLanguageCatalogs("en"),
    loadLanguageCatalogs(language),
  ]);
  // English all here already is not added again as the language changes.
  if (sourceCatalogs) englishFallback = Promise.resolve();
  const namespaces = Object.keys(sourceCatalogs ?? activeCatalogs);
  // Until English arrives its namespaces stand empty, so i18next does not
  // fetch it before the first screen either.
  const english = sourceCatalogs ?? Object.fromEntries(namespaces.map((namespace) => [namespace, {}]));
  const started = await i18next
    .use(resourceBackend)
    .use(isolateFallback)
    .use(initReactI18next)
    .init({
      lng: language,
      fallbackLng: "en",
      supportedLngs: SUPPORTED_LANGUAGES,
      // Detection is normalized before init so full script/region codes use
      // the intended released catalog instead of being reduced to a base code.
      load: "currentOnly",
      nonExplicitSupportedLngs: false,
      ns: namespaces.length > 0 ? namespaces : [DEFAULT_NS],
      defaultNS: DEFAULT_NS,
      // English + the active locale are bundled into the initial resource
      // object above; every other released locale remains lazy-loadable when
      // tests or the live language preference switch calls changeLanguage.
      partialBundledLanguages: true,
      resources: {
        en: english,
        ...(language === "en" ? {} : { [language]: activeCatalogs }),
      },
      interpolation: { escapeValue: false },
      postProcess: ["isolateFallback"],
      postProcessPassResolved: true,
      returnNull: false,
      // An empty line is a translation: words a language says nothing for
      // (English's "" after a link that Korean words around it, a menu
      // item's empty description) stay empty rather than show another
      // language's words or the key, as before i18next 27 made it false.
      returnEmptyString: true,
    });
  if (!sourceCatalogs) fetchAhead(() => void loadEnglishAlsoRead());
  return started;
}

function loadEnglishFallback() {
  englishFallback ??= loadLanguageCatalogs("en").then((catalogs) => {
    for (const [namespace, strings] of Object.entries(catalogs)) {
      i18next.addResourceBundle("en", namespace, strings, true, true);
    }
  }, () => { englishFallback = null; });
  return englishFallback;
}

/**
 * The words a screen shows in `language` before or as the page moves to it:
 * its catalog, and English's whole one where it is English or falls back to
 * it — a page started in another complete language holds only the English
 * the "/" menu reads (englishAlsoRead).
 */
export async function loadLanguageWords(language: string): Promise<void> {
  const lower = language.toLowerCase();
  await Promise.all([
    lower === "en" || !COMPLETE_LANGUAGES.has(lower) ? loadEnglishFallback() : undefined,
    i18next.loadLanguages(language),
  ]);
}

/** Moves the page to `language` once its words are here (loadLanguageWords). */
export async function switchLanguage(language: string): Promise<void> {
  await loadLanguageWords(language);
  await i18next.changeLanguage(language);
}

const RTL_LANGUAGE_BASES = new Set(["ar", "dv", "fa", "he", "ku", "ps", "ur", "yi"]);

export function documentDirectionForLanguage(language: string): "ltr" | "rtl" {
  const base = canonicalLanguageTag(language).toLowerCase().split("-")[0];
  return RTL_LANGUAGE_BASES.has(base) ? "rtl" : "ltr";
}

export function syncDocumentLanguage(language: string): void {
  if (typeof document === "undefined") return;
  const canonical = canonicalLanguageTag(language) || "en";
  document.documentElement.lang = canonical;
  document.documentElement.dir = documentDirectionForLanguage(canonical);
  declareLocaleFontFaces(canonical);
}

// Keep <html lang> and direction in sync with the active language (initial
// resolve + any manual switch) for CSS :lang(), layout and accessibility.
if (typeof document !== "undefined") {
  i18next.on("languageChanged", (lng) => {
    if (!COMPLETE_LANGUAGES.has(lng.toLowerCase())) void loadEnglishFallback();
    try {
      syncDocumentLanguage(lng);
    } catch {
      /* document may be unavailable in exotic contexts */
    }
  });
}

// ── Language preference API (for the in-app language selector) ─────────────
// "system" = follow the browser; otherwise a released language code. System is
// stored explicitly after a user chooses it so it remains distinguishable from
// an account that has never completed language onboarding.
export function currentLanguagePreference(userId = ""): string {
  return storedLanguagePreference(userId) ?? "system";
}

export function hasCachedLanguagePreference(userId = ""): boolean {
  return storedLanguagePreference(userId) !== undefined;
}

export function resolvedLanguageForPreference(pref: string): string {
  const resolved = normalizeLanguagePreference(pref);
  return resolved && resolved !== "system" ? resolved : browserLanguage();
}

export function cacheLanguagePreference(userId: string, pref: string): boolean {
  const resolved = normalizeLanguagePreference(pref);
  if (!userId || !resolved) return false;
  try {
    localStorage.setItem(languageStorageKey(userId), resolved);
    return true;
  } catch {
    return false;
  }
}

/**
 * Once no account is signed in here, the sign-in screen speaks this device's
 * language, as a fresh load does, not the signed-out account's (2026-11-07:
 * the next person on a shared device met the last one's language). A server
 * not set up yet has had nobody sign in on it, so its setup screen does too,
 * whatever account this browser remembers from an earlier server at the same
 * address (2026-11-18); it waits for the words before drawing. A language
 * chosen on the first screen before any account stands for this device's
 * until an account has one (signedOutLanguage).
 */
export async function returnToDeviceLanguage(): Promise<void> {
  const language = signedOutLanguage();
  if (i18next.language !== language) await switchLanguage(language);
}

export function setLanguagePreference(pref: string, userId = ""): void {
  const resolved = normalizeLanguagePreference(pref);
  if (!resolved) return;
  void applyAccountLanguage(userId, resolved);
}

const LANGUAGE_RELOAD_KEY = "leafen:language-reload";

/**
 * Brings the page to an account's language, kept for the loads after. It
 * reloads so EVERY surface renders in it — many option/label arrays bake
 * their strings in at module-evaluation time and would not re-localize on a
 * live changeLanguage — when the next load reads it at this address
 * (shouldUseAccountLanguagePreference) and storage kept it. Otherwise, or
 * when this tab's last reload for it didn't bring it, the page switches in
 * place: applying a language reloads at most once and never loops.
 * Resolves true once the page reads in the language, and false at once
 * when a reload brings it: the screen that waits on it then draws nothing
 * more in the language the page had (2026-11-19: the first run's next
 * step named the tab in Korean, kept through the reload, for half a second
 * after English was chosen).
 */
export function applyAccountLanguage(userId: string, pref: string): Promise<boolean> {
  if (typeof window === "undefined") return Promise.resolve(true);
  const persisted = cacheLanguagePreference(userId, pref);
  const language = resolvedLanguageForPreference(pref);
  if (language === i18next.resolvedLanguage) {
    forgetLanguageReload();
    return Promise.resolve(true);
  }
  if (persisted && shouldUseAccountLanguagePreference(window.location.pathname, true) && claimLanguageReload(language)) {
    window.location.reload();
    return Promise.resolve(false);
  }
  // Its words not here, the page goes on in the language it has.
  return switchLanguage(language).then(() => true, () => true);
}

// A tab notes the language it reloads for; finding the note again, with the
// language still not here, it doesn't reload a second time.
function claimLanguageReload(language: string): boolean {
  try {
    if (sessionStorage.getItem(LANGUAGE_RELOAD_KEY) === language) {
      sessionStorage.removeItem(LANGUAGE_RELOAD_KEY);
      return false;
    }
    sessionStorage.setItem(LANGUAGE_RELOAD_KEY, language);
    return true;
  } catch {
    return false;
  }
}

function forgetLanguageReload(): void {
  try {
    sessionStorage.removeItem(LANGUAGE_RELOAD_KEY);
  } catch {
    // Nothing noted where nothing can be.
  }
}

export { i18next };
