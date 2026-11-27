import { readCSVRecords } from "./csvImportModel";

/**
 * Notion's export writes a data source's mention as a link to the source in
 * Notion's app, https://app.notion.com/ds/<space>/<source>?db=<database>
 * (seen 2026-11-22): [name](address) in its Markdown, an <a> in its HTML (in
 * a title and a comment too), "name (address)" in a CSV's text cell. Brought
 * back, the mention of a source whose database the import brings is that
 * database's source's ([name](leafen-data-source:<its page's ID>)), and one
 * of any other source the mention of a source not there
 * ([name](leafen-missing-data-source:<its Notion ID>)), as the server reads
 * them (workspace_import_data_source_references.go).
 */

/** A database the import brings, by its Notion ID: the page it is made on and its name. */
export type ImportedDatabase = (id: string) => { pageId: string; title: string } | undefined;

const SOURCE_ADDRESS = /^https?:\/\/(?:app\.notion\.com|(?:www\.)?notion\.so)\/ds\/[0-9a-f]{32}\/([0-9a-f]{32})\?(?:[^#\s]*&)?db=([0-9a-f]{32})(?:[&#]|$)/i;

function dashed(id: string) {
  const hex = id.toLowerCase();
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

/** The reference a link to a data source in Notion's app is brought back as; undefined for any other address. */
export function dataSourceReference(href: string, database?: ImportedDatabase) {
  const match = SOURCE_ADDRESS.exec(href.trim().replace(/&amp;/g, "&"));
  if (!match) return undefined;
  const imported = database?.(match[2]!);
  return imported ? `leafen-data-source:${imported.pageId}` : `leafen-missing-data-source:${dashed(match[1]!)}`;
}

const MARKDOWN_LINK = /\[((?:\\.|[^\]\\])*)\]\(\s*<?(https?:\/\/[^()\s<>]*\/ds\/[^()\s<>]*)>?\s*\)/g;

/** A document whose links to data sources in Notion's app are their mentions; one with none as it is. */
export function withDataSourceMentions(markdown: string, database?: ImportedDatabase) {
  if (!markdown.includes("/ds/")) return markdown;
  let fenced = false;
  return markdown.split("\n").map((line) => {
    const fence = /^\s*```/.test(line);
    const code = fenced || fence;
    if (fence) fenced = !fenced;
    if (code) return line;
    return line.replace(MARKDOWN_LINK, (whole, text: string, href: string) => {
      const reference = dataSourceReference(href, database);
      return reference ? `[${text}](${reference})` : whole;
    });
  }).join("\n");
}

const NESTED_SOURCE_LINK = /\[([^[\]\n]*)\[((?:\\.|[^\]\\\n])*)\]\(\s*<?https?:\/\/[^()\s<>]*\/ds\/[^()\s<>]*>?\s*\)([^[\]\n]*)\]\(/g;

/**
 * A link to a page whose title mentions a data source: Notion's Markdown
 * export writes the mention's link inside the page link's words ("[title
 * [source](address) more](page.md)", seen 2026-11-22). Its words name the
 * mention "@" and the name, as the page's file does, so the page link reads
 * as one, the page's own sub-page's place among them.
 */
export function withoutNestedDataSourceLinks(markdown: string) {
  return markdown.includes("/ds/") ? markdown.replace(NESTED_SOURCE_LINK, "[$1@$2$3](") : markdown;
}

const CELL_SOURCE = / \((https?:\/\/[^()\s]*\/ds\/[^()\s]*)\)/g;

function quoted(value: string) {
  return `"${value.replace(/"/g, "\"\"")}"`;
}

/**
 * A CSV whose text cells mention data sources of the databases the import
 * brings, written with those mentions: the cell names one by its database's
 * name before its address. A mention of another source, whose name the cell
 * doesn't set apart from the words before it, stays its words and address.
 */
export function withCsvDataSourceMentions(csv: string, database?: ImportedDatabase) {
  if (!csv.includes("/ds/") || !database) return csv;
  let changed = false;
  const records = readCSVRecords(csv).map((record) => record.map((cell) => {
    let written = "", at = 0;
    for (const match of cell.matchAll(CELL_SOURCE)) {
      const found = SOURCE_ADDRESS.exec(match[1]!.replace(/&amp;/g, "&"));
      const imported = found ? database(found[2]!) : undefined;
      const start = imported ? match.index! - imported.title.length : -1;
      if (!imported || start < at || cell.slice(start, match.index) !== imported.title) continue;
      written += `${cell.slice(at, start)}[${imported.title.replace(/[[\]\\]/g, "\\$&")}](leafen-data-source:${imported.pageId})`;
      at = match.index! + match[0].length;
    }
    if (at === 0) return cell;
    changed = true;
    return written + cell.slice(at);
  }));
  return changed ? records.map((record) => record.map(quoted).join(",")).join("\r\n") : csv;
}
