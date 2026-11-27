// What a screen in another complete language still reads in English: the "/"
// menu finds a block by its English name and group too
// (blockDefinitionSearch.ts), a page's "…" its rows (menuEnglishNames.ts),
// a type search its property types (propertyTypes.ts) and the keyboard
// shortcuts window its rows (KeyboardShortcutsDialog). Each namespace is a small file of its own, so a
// page in Korean loads these alone, not English's whole catalog (i18n/index.ts).
export const englishAlsoRead = import.meta.glob<Record<string, unknown>>([
  "../locales/en/blockHandle.json", "../locales/en/blocks.json", "../locales/en/common.json",
  "../locales/en/keyboardShortcuts.json", "../locales/en/propertyTypes.json", "../locales/en/rowMenu.json", "../locales/en/slashMenu.json", "../locales/en/topBar.json",
], { import: "default" });
