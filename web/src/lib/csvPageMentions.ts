import { readCSVRecords } from "./csvImportModel";
import { titleFromFileName } from "./pageMarkdownImportModel";

/**
 * An export's CSV writes a page a cell mentions as its name and its file's
 * path in parentheses (Notion's, seen 2026-11-13: "행 둘 행제목쪽 (…/행제목쪽
 * <id>.html) "), the path from the export's top, and names the page of a row
 * whose title mentions one with "@" and the name ("행 둘 @행제목쪽 <id>.html").
 * Brought back, a mention of a page the import brings is that page's
 * mention ([name](leafen-page:<page ID>), workspace_import_csv_mentions.go),
 * and the row's page is found by its title so named.
 */

const MENTION = / \(([^()\s]+?\.(?:md|html?))\)/g;

function fileName(path: string) {
  let name = path.split("/").pop() ?? path;
  try {
    name = decodeURIComponent(name);
  } catch {
    // A malformed escape is read as written.
  }
  return titleFromFileName(name, "", true);
}

/** Each mention in a cell, with the page it names when there is one. */
function cellMentions(cell: string, pageAt?: (path: string) => string | undefined) {
  const found: Array<{ start: number; end: number; name: string; pageId?: string }> = [];
  for (const match of cell.matchAll(MENTION)) {
    const name = fileName(match[1]!);
    const start = match.index! - name.length;
    if (!name || start < 0 || cell.slice(start, match.index) !== name) continue;
    found.push({ start, end: match.index! + match[0].length, name, pageId: pageAt?.(match[1]!) });
  }
  return found;
}

/** A title cell as an export names its row's page: each mention "@" and the name. */
export function csvTitleAsPageName(cell: string) {
  let named = "", at = 0;
  for (const mention of cellMentions(cell)) {
    named += `${cell.slice(at, mention.start)}@${mention.name}`;
    at = mention.end;
  }
  return (named + cell.slice(at)).trim();
}

function quoted(value: string) {
  return `"${value.replace(/"/g, "\"\"")}"`;
}

/** A CSV whose cells mention pages the import brings, written with those mentions; one with none as it is. */
export function withCsvPageMentions(csv: string, pageAt: (path: string) => string | undefined) {
  if (!csv.includes(".md)") && !csv.includes(".html)") && !csv.includes(".htm)")) return csv;
  let changed = false;
  const records = readCSVRecords(csv).map((record) => record.map((cell) => {
    let written = "", at = 0;
    for (const mention of cellMentions(cell, pageAt)) {
      if (!mention.pageId) continue;
      written += `${cell.slice(at, mention.start)}[${mention.name.replace(/[[\]\\]/g, "\\$&")}](leafen-page:${mention.pageId})`;
      at = mention.end;
    }
    if (at === 0) return cell;
    changed = true;
    return written + cell.slice(at);
  }));
  return changed ? records.map((record) => record.map(quoted).join(",")).join("\r\n") : csv;
}
