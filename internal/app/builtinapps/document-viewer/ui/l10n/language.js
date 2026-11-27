// The language pdf.js reads its words in, for Leafen's screen language: its
// own translations (vendor/pdfjs-locale-*, from pdf.js's release, which
// locale.json maps) where it has the language, else its English. Korean is
// "ko-KR", whose words come first from ko.ftl — the PDF app's own names for
// its tools (글, 그리기, 형광펜) — and then from pdf.js's Korean. pdf.js names
// some languages otherwise. The PDF app and Leafen's file preview (which
// serves this folder at pdfjs-words/l10n/) both read their pdf.js words so.
export function pdfjsLanguage(locale) {
  const language = String(locale || "en");
  const named = { ko: "ko-KR", "zh-hans": "zh-CN", "zh-hant": "zh-TW", fil: "tl" };
  return named[language.toLowerCase()] ?? named[language.toLowerCase().split("-", 1)[0]] ?? language;
}
