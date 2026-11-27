import { csvDelimiter } from "./csvImportModel";
import { readImportText } from "./importText";
import { assertImportReach, ImportOutputBudget } from "./importOutputBudget";
import { readXlsWorkbook } from "./xlsWorkbook";
import { listZipEntries, type ZipFileEntry } from "./zipArchiveReader";

/**
 * Reads the spreadsheets Notion's ZIP import turns into databases, as the
 * CSV the database importer takes: TSV and other delimiter-separated text,
 * Excel workbooks (.xlsx and legacy .xls) and OpenDocument spreadsheets
 * (.ods). Each sheet
 * with content becomes its own database; its first row is the header, as in
 * a CSV. Excel dates are written as ISO dates and formulas as their last
 * computed values. Trailing empty rows and columns are dropped. A sheet is
 * read whole; one whose CSV would be larger than Leafen imports stops with
 * the importer's "too large", as soon as it is.
 */

export class SpreadsheetReadError extends Error {}

const DELIMITERS = ["\t", "|", ";", ",", ":"];

// A value with any delimiter is quoted, so what reads this CSV finds the commas.
function csvCell(value: string) {
  return /[",\t|;\r\n]/.test(value) ? `"${value.replace(/"/g, "\"\"")}"` : value;
}

// Rows and their cells may have gaps (a sheet's unused cells); a gap is empty.
type SheetRows = ReadonlyArray<ReadonlyArray<string | undefined> | undefined>;

function rowHasData(row: ReadonlyArray<string | undefined> | undefined) {
  return Boolean(row?.some((cell) => cell));
}

/** Whether a sheet holds anything. */
function sheetHasData(rows: SheetRows) {
  return rows.some(rowHasData);
}

/**
 * Writes rows as CSV, without trailing empty rows and columns — all of them,
 * or FileImportTooLargeError once the CSV would be larger than Leafen
 * imports, before it is built.
 */
function rowsToCsv(rows: SheetRows, budget = new ImportOutputBudget()) {
  let last = rows.length;
  while (last > 0 && !rowHasData(rows[last - 1])) last -= 1;
  // Every line's break, before the lines are measured.
  budget.add(last);
  let width = 0;
  for (let index = 0; index < last; index += 1) {
    const row = rows[index];
    for (let column = row?.length ?? 0; column > width; column -= 1) {
      if (row![column - 1]) {
        width = column;
        break;
      }
    }
  }
  // And their commas, before a cell is written.
  budget.add(last * Math.max(0, width - 1));
  const lines: string[] = [];
  for (let index = 0; index < last; index += 1) {
    const row = rows[index];
    const line = Array.from({ length: width }, (_, column) => csvCell(row?.[column] ?? "")).join(",");
    budget.add(line.length - (width - 1));
    lines.push(line);
  }
  return lines.join("\n");
}

/** Reads delimiter-separated text (quoted fields allowed); the delimiter is found as a CSV's is when not given. */
function delimitedToRows(text: string, delimiter?: string) {
  const separator = delimiter ?? csvDelimiter(text, DELIMITERS);
  const rows: string[][] = [[]];
  let field = "";
  let quoted = false;
  for (let index = 0; index < text.length; index += 1) {
    const char = text[index]!;
    if (quoted) {
      if (char === "\"" && text[index + 1] === "\"") {
        field += "\"";
        index += 1;
      } else if (char === "\"") quoted = false;
      else field += char;
      continue;
    }
    if (char === "\"" && field === "") quoted = true;
    else if (char === separator) {
      rows.at(-1)!.push(field);
      field = "";
    } else if (char === "\n" || char === "\r") {
      if (char === "\r" && text[index + 1] === "\n") index += 1;
      rows.at(-1)!.push(field);
      field = "";
      rows.push([]);
    } else field += char;
  }
  rows.at(-1)!.push(field);
  return rows;
}

function parseXml(text: string) {
  const document = new DOMParser().parseFromString(text, "application/xml");
  if (document.getElementsByTagName("parsererror").length > 0) throw new SpreadsheetReadError("The spreadsheet is damaged.");
  return document;
}

function elements(root: Document | Element, localName: string) {
  return Array.from(root.getElementsByTagName("*")).filter((node) => node.localName === localName);
}

function childElements(node: Element, localName: string) {
  return Array.from(node.children).filter((child) => child.localName === localName);
}

async function entryText(entries: readonly ZipFileEntry[], name: string) {
  const entry = entries.find((candidate) => candidate.name === name);
  return entry ? new TextDecoder("utf-8").decode(await entry.read()) : "";
}

function columnIndex(reference: string) {
  const letters = /^[A-Z]+/.exec(reference.toUpperCase())?.[0] ?? "";
  let index = 0;
  for (const letter of letters) index = index * 26 + letter.charCodeAt(0) - 64;
  return index - 1;
}

// Built-in Excel number formats 14–22 and 45–47 are dates or times.
const BUILTIN_DATE_FORMATS = new Set([14, 15, 16, 17, 18, 19, 20, 21, 22, 45, 46, 47]);

function excelDate(serial: number) {
  const milliseconds = Math.round((serial - 25569) * 86_400_000);
  const date = new Date(milliseconds);
  if (Number.isNaN(date.getTime())) return String(serial);
  const iso = date.toISOString();
  return serial % 1 === 0 ? iso.slice(0, 10) : iso.slice(0, 16).replace("T", " ");
}

type Workbook = { sheets: Array<{ name: string; path: string }>; entries: ZipFileEntry[] };

async function openXlsx(blob: Blob): Promise<Workbook> {
  const entries = await listZipEntries(blob);
  const workbook = parseXml(await entryText(entries, "xl/workbook.xml"));
  const relationships = parseXml(await entryText(entries, "xl/_rels/workbook.xml.rels") || "<Relationships/>");
  const targets = new Map(elements(relationships, "Relationship")
    .map((relationship) => [relationship.getAttribute("Id") ?? "", relationship.getAttribute("Target") ?? ""]));
  const sheets = elements(workbook, "sheet").map((sheet) => {
    const target = targets.get(sheet.getAttribute("r:id") ?? "") ?? "";
    return { name: sheet.getAttribute("name") ?? "", path: target.startsWith("/") ? target.slice(1) : `xl/${target}` };
  }).filter((sheet) => sheet.path !== "xl/");
  return { sheets, entries };
}

async function xlsxRows(workbook: Workbook, sheetIndex: number) {
  const { entries } = workbook;
  const sheet = workbook.sheets[sheetIndex];
  if (!sheet) throw new SpreadsheetReadError("The sheet is missing.");
  const sharedText = await entryText(entries, "xl/sharedStrings.xml");
  const shared = sharedText ? elements(parseXml(sharedText), "si")
    .map((item) => elements(item, "t").filter((text) => text.parentElement?.localName !== "rPh")
      .map((text) => text.textContent ?? "").join("")) : [];
  const stylesText = await entryText(entries, "xl/styles.xml");
  const dateStyles: boolean[] = [];
  if (stylesText) {
    const styles = parseXml(stylesText);
    const custom = new Map(elements(styles, "numFmt")
      .map((format) => [Number(format.getAttribute("numFmtId")), format.getAttribute("formatCode") ?? ""]));
    const cellXfs = elements(styles, "cellXfs")[0];
    for (const xf of cellXfs ? childElements(cellXfs, "xf") : []) {
      const id = Number(xf.getAttribute("numFmtId") ?? 0);
      const code = (custom.get(id) ?? "").replace(/"[^"]*"|\[[^\]]*\]|\\./g, "");
      dateStyles.push(BUILTIN_DATE_FORMATS.has(id) || /[dy]/i.test(code) || /h.*m|m.*s/i.test(code));
    }
  }
  // Rows and cells go where the sheet says; the unused ones stay gaps.
  const rows: string[][] = [];
  let nextRow = 0;
  for (const row of elements(parseXml(await entryText(entries, sheet.path)), "row")) {
    const rowIndex = Number(row.getAttribute("r") ?? nextRow + 1) - 1;
    if (!Number.isSafeInteger(rowIndex) || rowIndex < 0) continue;
    nextRow = rowIndex + 1;
    let next = 0;
    for (const cell of childElements(row, "c")) {
      const reference = cell.getAttribute("r");
      const column = reference ? columnIndex(reference) : next;
      next = column + 1;
      if (column < 0) continue;
      const type = cell.getAttribute("t") ?? "n";
      const raw = childElements(cell, "v")[0]?.textContent ?? "";
      let value = raw;
      if (type === "s") value = shared[Number(raw)] ?? "";
      else if (type === "inlineStr") value = elements(cell, "t").map((text) => text.textContent ?? "").join("");
      else if (type === "b") value = raw === "1" ? "TRUE" : raw === "0" ? "FALSE" : raw;
      else if (type === "n" && raw && dateStyles[Number(cell.getAttribute("s") ?? -1)]) value = excelDate(Number(raw));
      // Only cells with something in them, so the sheet ends at its last
      // value and a row at its last cell.
      if (!value) continue;
      assertImportReach(rowIndex);
      assertImportReach(column);
      (rows[rowIndex] ??= [])[column] = value;
    }
  }
  return rows;
}

function repeat(node: Element, attribute: string) {
  const count = Number(node.getAttribute(attribute) ?? 1);
  return Number.isSafeInteger(count) && count > 1 ? count : 1;
}

function odsCellText(cell: Element) {
  const type = cell.getAttribute("office:value-type") ?? "";
  if (type === "date") return cell.getAttribute("office:date-value") ?? "";
  if (type === "boolean") return (cell.getAttribute("office:boolean-value") ?? "").toUpperCase();
  if (type === "float" || type === "percentage" || type === "currency") return cell.getAttribute("office:value") ?? "";
  return childElements(cell, "p").map((paragraph) => paragraph.textContent ?? "").join("\n");
}

async function openOds(blob: Blob) {
  const entries = await listZipEntries(blob);
  const content = parseXml(await entryText(entries, "content.xml"));
  return elements(content, "table").filter((table) => table.parentElement?.localName === "spreadsheet");
}

// An ODS sheet repeats a cell or a row by count; its unused area is a long
// run of empty ones. Empty runs are written out only when something follows
// them, and everything written counts against the budget.
function odsRows(table: Element, budget = new ImportOutputBudget()) {
  const rows: string[][] = [];
  let emptyRows = 0;
  for (const row of elements(table, "table-row")) {
    const cells: string[] = [];
    let emptyCells = 0;
    for (const cell of Array.from(row.children)) {
      if (cell.localName !== "table-cell" && cell.localName !== "covered-table-cell") continue;
      const text = cell.localName === "table-cell" ? odsCellText(cell) : "";
      const count = repeat(cell, "table:number-columns-repeated");
      if (!text) {
        emptyCells += count;
        continue;
      }
      budget.add(emptyCells + count * (text.length + 1));
      for (; emptyCells > 0; emptyCells -= 1) cells.push("");
      for (let copy = 0; copy < count; copy += 1) cells.push(text);
    }
    const count = repeat(row, "table:number-rows-repeated");
    if (cells.length === 0) {
      emptyRows += count;
      continue;
    }
    budget.add(emptyRows + count);
    for (; emptyRows > 0; emptyRows -= 1) rows.push([]);
    for (let copy = 0; copy < count; copy += 1) rows.push(cells);
  }
  return rows;
}

export type SpreadsheetKind = "tsv" | "dsv" | "xlsx" | "xls" | "ods";

/** A workbook's sheets that hold data, in order, with their positions. */
export async function spreadsheetSheets(blob: Blob, kind: SpreadsheetKind): Promise<Array<{ name: string; index: number }>> {
  if (kind === "xlsx") {
    const workbook = await openXlsx(blob);
    const sheets = [];
    for (const [index, sheet] of workbook.sheets.entries()) {
      if (/<(?:\w+:)?v>|<(?:\w+:)?is>/.test(await entryText(workbook.entries, sheet.path))) sheets.push({ name: sheet.name, index });
    }
    return sheets;
  }
  if (kind === "xls") {
    return readXlsWorkbook(new Uint8Array(await blob.arrayBuffer()))
      .map((sheet, index) => ({ name: sheet.name, index, empty: !sheetHasData(sheet.rows) }))
      .filter((sheet) => !sheet.empty).map(({ name, index }) => ({ name, index }));
  }
  if (kind === "ods") {
    return (await openOds(blob)).map((table, index) => ({ name: table.getAttribute("table:name") ?? "", index, table }))
      // A sheet too large to import is listed, and says so when imported.
      .filter(({ table }) => { try { return sheetHasData(odsRows(table)); } catch { return true; } }).map(({ name, index }) => ({ name, index }));
  }
  return [{ name: "", index: 0 }];
}

/** Reads one sheet of a spreadsheet as CSV. */
export async function spreadsheetToCsv(blob: Blob, kind: SpreadsheetKind, sheetIndex = 0) {
  let rows: string[][];
  if (kind === "tsv") rows = delimitedToRows(await readImportText(blob), "\t");
  else if (kind === "dsv") rows = delimitedToRows(await readImportText(blob));
  else if (kind === "xlsx") rows = await xlsxRows(await openXlsx(blob), sheetIndex);
  else if (kind === "xls") {
    const sheet = readXlsWorkbook(new Uint8Array(await blob.arrayBuffer()))[sheetIndex];
    if (!sheet) throw new SpreadsheetReadError("The sheet is missing.");
    rows = sheet.rows;
  }
  else {
    const table = (await openOds(blob))[sheetIndex];
    if (!table) throw new SpreadsheetReadError("The sheet is missing.");
    rows = odsRows(table);
  }
  return rowsToCsv(rows);
}
