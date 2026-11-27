// The PDF app's own words — those pdf.js has none for — in the person's
// language: l10n/words/<Leafen's language code>.json, English for any word a
// language's file doesn't have yet. Korean and English are written with the
// app; the other languages come from Leafen's translations, so until one is
// there its words stay English. The tools' names and "Save" are pdf.js's own
// (its editor buttons'), in every language it has (app.js).
const read = async (code) => {
  try {
    const answer = await fetch(new URL(`./l10n/words/${code}.json`, import.meta.url));
    return answer.ok ? await answer.json() : {};
  } catch {
    return {};
  }
};

const english = read("en");

export async function appWords(locale) {
  const language = String(locale || "en");
  const [base, own] = await Promise.all([english, language === "en" ? {} : read(language)]);
  return { ...base, ...own };
}

// fill puts the values in a word's {{name}} places.
export function fill(word, values) {
  return String(word ?? "").replace(/\{\{(\w+)\}\}/gu, (all, name) => (name in values ? String(values[name]) : all));
}
