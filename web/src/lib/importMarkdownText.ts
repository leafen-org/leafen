/**
 * Words that read as written in the block Markdown the workspace importer
 * reads (workspace_import_markdown.go), whichever converter writes them: an
 * HTML, Word or outline document's text, a link's words, a picture's words.
 * Within a line, what Markdown reads as formatting or an equation ("$"), an "&" that would start
 * a character reference, and a "!" ending the words — a link written after
 * it would read as a picture (2026-11-13: "와!" before a link came back the
 * words "와![링크](…)") — are escaped, and so are an address's "http:",
 * "https:" or "mailto:" and a mail address's "@", which the importer would
 * link (2026-11-13: Notion's HTML and Word import keep a bare address as
 * words, where its Markdown import links it); at a line's start, what would
 * open a block of its own — a heading, a list item, a rule, a heading's
 * underline — is escaped too; and a heading's closing "#"s are its words
 * (2026-11-13: a Notion HTML paragraph "# …" or "- …" came back a heading or
 * a list, its "&amp;" as "&", and a heading "… #" without its "#").
 */

const INLINE = /[\\`*_[\]<>~|@$]|&(?=#\d{1,7};|#[xX][\dA-Fa-f]{1,6};|[A-Za-z][A-Za-z\d]{1,31};)/g;
const ADDRESS_SCHEME = /(https?|mailto):/gi;

/** Text escaped so the importer reads it literally within a line. */
export function escapeImportMarkdown(text: string) {
  return text.replace(INLINE, "\\$&").replace(ADDRESS_SCHEME, "$1\\:").replace(/!$/, "\\!");
}

/** A block's escaped words over lines, each line's start escaped where it would open a block. */
export function escapeImportLineStarts(text: string) {
  return text.split("\n")
    .map((line) => line.replace(/^( {0,3})([#=+-])/, "$1\\$2").replace(/^( {0,3}\d+)([.)])/, "$1\\$2"))
    .join("\n");
}

/** A heading's escaped words, a run of "#" ending them kept as words, not read as the heading's closing mark. */
export function escapeImportHeadingEnd(text: string) {
  return text.replace(/(^|[ \t])(#+)([ \t]*)$/, "$1\\$2$3");
}

/** Words written as a paragraph of their own, read as written. */
export function importParagraphText(text: string) {
  return escapeImportLineStarts(escapeImportMarkdown(text));
}
