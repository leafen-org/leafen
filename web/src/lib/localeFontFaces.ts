/**
 * Declares the Noto faces of Japanese and of Chinese once the page is in that
 * language. typography.css draws CJK text in the page language's own Noto
 * face (html:lang(ja) the Japanese one, html:lang(zh-Hans) Simplified, …) and
 * Korean's for every other language, which typography.generated.css declares.
 * Declared on every page, the other three scripts' 663 @font-face rules held
 * the browser's own heap, which every garbage collection goes through, and
 * every style pass looked through them: on a 60-row table page 5 of its 18 MB
 * and 40–65 ms of the opening's style passes (2026-11-14, CPU ×2).
 * Their sheets come from scripts/vendor-typography-fonts.mjs.
 */
const SHEETS = {
  ja: () => import("@/app/typography.ja.generated.css"),
  "zh-Hans": () => import("@/app/typography.zh-Hans.generated.css"),
  "zh-Hant": () => import("@/app/typography.zh-Hant.generated.css"),
} as const;

const declared = new Set<keyof typeof SHEETS>();

/**
 * The sheet a language tag draws its CJK text from, as typography.css picks
 * it; a Chinese tag naming its script or a region of either takes that one.
 */
function sheetFor(language: string): keyof typeof SHEETS | null {
  const [base, ...subtags] = language.toLowerCase().split("-");
  if (base === "ja") return "ja";
  if (base !== "zh") return null;
  if (subtags.some((part) => part === "hant" || part === "tw" || part === "hk" || part === "mo")) return "zh-Hant";
  if (subtags.some((part) => part === "hans" || part === "cn" || part === "sg")) return "zh-Hans";
  return null;
}

export function declareLocaleFontFaces(language: string): void {
  const sheet = sheetFor(language);
  if (!sheet || declared.has(sheet)) return;
  declared.add(sheet);
  void SHEETS[sheet]().catch(() => declared.delete(sheet));
}
