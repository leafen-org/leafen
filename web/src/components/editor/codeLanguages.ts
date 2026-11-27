import {
  CODE_LANGUAGE_LABELS, CODE_LANGUAGE_PRISM_NAMES, CODE_LANGUAGE_SHORT_NAMES, PLAIN_TEXT_CODE_LANGUAGE, UNREAD_CODE_LANGUAGE_NAMES,
} from "@/codeLanguages.generated";

// The code block languages, as Notion's code block and public API name them
// (its 88, seen 2026-11-01), and the short names a fence or code element may
// give them, from contracts/code-languages.json, which the server reads too.
// The server accepts these values, and "abc" from Notion's API
// (UNLISTED_CODE_LANGUAGE_LABELS); an empty language reads as plain text.
export { PLAIN_TEXT_CODE_LANGUAGE };

// The names Notion translates (seen 2026-11-01: "Notion 수식", "스몰토크";
// "Plain Text" stays as it is).
const TRANSLATED_CODE_LANGUAGES: Readonly<Record<string, string>> = {
  [PLAIN_TEXT_CODE_LANGUAGE]: "code.plainText", "notion formula": "code.notionFormula", smalltalk: "code.smalltalk",
};

/**
 * The language a pasted code fence or code element names, as Notion reads
 * it (seen 2026-11-01, Markdown and HTML alike): the leading run of
 * letters, digits and "_" — "c++" is C, "sh-session" Bash — by a
 * language's own one-word name or a short name, case as written; anything
 * else, "webassembly" and "assembly" too, is plain text (undefined).
 */
export function pastedCodeLanguage(name: string | undefined): string | undefined {
  const word = name?.trim().match(/^\w+/)?.[0];
  if (!word) return undefined;
  if (Object.hasOwn(CODE_LANGUAGE_SHORT_NAMES, word)) return CODE_LANGUAGE_SHORT_NAMES[word];
  return !UNREAD_CODE_LANGUAGE_NAMES.has(word) && CODE_LANGUAGE_LABELS.some(([value]) => value === word) ? word : undefined;
}

// The language each Prism name an HTML export marks a code block with
// stands for, when it is no language's own name ("bash" is Bash's, which
// Shell's is too).
const PRISM_LANGUAGES = new Map(Object.entries(CODE_LANGUAGE_PRISM_NAMES)
  .filter(([, prism]) => !CODE_LANGUAGE_LABELS.some(([value]) => value === prism))
  .map(([language, prism]) => [prism, language]));

/**
 * The fence an import writes for the language an HTML export marks a code
 * block with, by Prism's name as Notion's export and Leafen's mark it
 * (language-cpp, language-vbnet, seen 2026-11-14): the language's own name,
 * or for one an import reads as plain text by its own name ("vb.net",
 * "webassembly") a short name it reads as that language ("vb", "wasm");
 * any other name as it is.
 */
export function exportedCodeLanguageFence(name: string) {
  const language = PRISM_LANGUAGES.get(name) ?? name;
  if (!UNREAD_CODE_LANGUAGE_NAMES.has(language)) return language;
  return Object.entries(CODE_LANGUAGE_SHORT_NAMES).find(([, value]) => value === language)?.[0] ?? language;
}

// A language Notion's API takes that its menu doesn't list, by the name its
// code block shows (seen 2026-11-10: "abc" as "ABC").
const UNLISTED_CODE_LANGUAGE_LABELS: Readonly<Record<string, string>> = { abc: "ABC" };

/**
 * The select options in Notion's order, by the name shown — "Plain Text"
 * among them and the Korean names after the Latin ones (seen 2026-11-01);
 * a stored value outside the list stays selectable.
 */
export function codeLanguageOptions(text: (key: string) => string, current: string) {
  const options = sortedCodeLanguages(text);
  if (current && !options.some((option) => option.value === current)) {
    return [...options, { value: current, label: UNLISTED_CODE_LANGUAGE_LABELS[current] ?? current }];
  }
  return options;
}

// The sorted list, made again only when the translated names change: every
// code block draws it, and sorting its ~90 names on each draw was most of a
// long page's redraw after a paste (2026-11-14: 84 ms of a 600-block page's
// ⌘V at CPU ×2). Callers read it and never change it.
let sortedList: { names: string; options: { value: string; label: string }[] } | null = null;

function sortedCodeLanguages(text: (key: string) => string) {
  const translated = new Map(Object.entries(TRANSLATED_CODE_LANGUAGES).map(([value, key]) => [value, text(key)]));
  const names = [...translated.values()].join("\n");
  if (sortedList?.names === names) return sortedList.options;
  const collator = new Intl.Collator("en", { sensitivity: "base" });
  const options = [[PLAIN_TEXT_CODE_LANGUAGE, ""] as const, ...CODE_LANGUAGE_LABELS].map(([value, label]) => ({
    value, label: translated.get(value) ?? label,
  }));
  options.sort((a, b) => collator.compare(a.label, b.label));
  sortedList = { names, options };
  return options;
}

/** The name a code block shows for a stored language, plain text for none. */
export function codeLanguageName(text: (key: string) => string, value: string) {
  const stored = value || PLAIN_TEXT_CODE_LANGUAGE;
  return codeLanguageOptions(text, stored).find((option) => option.value === stored)?.label ?? stored;
}
