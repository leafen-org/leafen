// A sheet that isn't an Excel workbook (.xlsx) or a CSV file — Excel 97–2003 (.xls), macro-enabled (.xlsm),
// binary (.xlsb), OpenDocument (.ods) or Numbers — drawn as Notion's preview and embed draw one: a plain table,
// not the workbook view of light-sheet.js (seen 2026-11-15 at 1440 dark, twice for .xls and .ods, once for .xlsm
// and .xlsb). The page that frames this one draws the sheets' tabs in its own head (Notion's sit beside the
// file's name): this one tells it their names and draws the one it picks (preview.js).
//
// The table: the sheet's first row is its head, on the bar's ground in the head's grey; under it each row has
// its number from 1 in a 48px column (12px, the faint grey, tabular figures); a cell is 30px high with a 1px line
// under it and at its end, its words 12px in from either side, cut with "…", a number's at its end; the cells keep
// the file's fill; a cell with an error shows "—" at its end in the faint grey (measured twice on 2026-11-15, as the
// words' 13px Arial in a span 12px in on both sides of the cell, cut with "…"). Words are in the size of the file's
// default face (10pt → 13px),
// or, when the file says none (an .ods file), 11px in the head and 16px under it, in a sans face (measured
// 2026-11-15 twice: .xls and .xlsm 13px, .xlsb 16px, .ods 11px and 16px). In an embed, empty lines fill the room
// under the last row. A column is as wide as the file says, in Excel's pixels for that face (columnPixels: .xls
// and .xlsm 100 and 50, .xlsb 114 and 57, measured twice), or else as its longest words (6px a letter, 12px a wide
// one, and its 24px of room), from 64px to 108px (.ods, measured twice); the last column takes the rest.
// Notion's server works the widths out once for each uploaded copy and keeps them: the same .xls (a 10pt default
// face) uploaded ten times in one tab came back five times 100 and 50 and five times 86 and 43, each copy the same
// on every reopen (2026-11-15, two tabs ten minutes apart), so nothing on the screen decides it; Leafen keeps
// Excel's 100 and 50.
import { script } from "./light.js";

const NUMBERS_PX = 48;
const ROW_PX = 31;
const COLUMN_MIN = 64;
const COLUMN_MAX = 108;
// Rows are drawn this many at a time, more as the view comes to them.
const ROWS_AT_ONCE = 200;
const WIDE = /[ᄀ-ᅟ⺀-꓏가-힣豈-﫿︰-﹏＀-｠￠-￦]/u;

// showTable draws the file into root and tells sheets the names of its shown sheets; the function it returns
// draws the sheet at that place in them.
export async function showTable(data, root, sheets) {
  await script("xlsx-0.20.3/xlsx.full.min.js");
  const XLSX = globalThis.XLSX;
  const book = XLSX.read(new Uint8Array(data), { type: "array", dense: true, cellStyles: true });
  const names = book.SheetNames.filter((name, index) => !book.Workbook?.Sheets?.[index]?.Hidden);
  const points = defaultPoints(XLSX, book, data);
  root.classList.add("table");
  if (points) {
    root.style.setProperty("--table-text", `${Math.round(points * 4 / 3)}px`);
    root.style.setProperty("--table-head-text", `${Math.round(points * 4 / 3)}px`);
  }
  // Excel's widest digit for the face, in Calibri's measure (1038/2048 of an em) at its size; 11pt's when unsaid.
  const digit = Math.round((points || 11) * 4 / 3 * 1038 / 2048);
  let stop = () => {};
  const pick = (at) => {
    const name = names[Math.min(Math.max(0, at), names.length - 1)];
    stop();
    stop = drawSheet(XLSX, book.Sheets[name], root, digit);
  };
  sheets(names);
  pick(0);
  return pick;
}

// The size in points of a file's default face: its first font, from an Excel workbook's styles or an Excel
// 97–2003 file's first FONT record (which SheetJS doesn't keep); null when the file says none.
function defaultPoints(XLSX, book, data) {
  const said = Number(book.Styles?.Fonts?.[0]?.sz);
  if (Number.isFinite(said) && said > 0) return said;
  try {
    const bytes = new Uint8Array(data);
    if (bytes[0] !== 0xd0 || bytes[1] !== 0xcf) return null;
    const file = XLSX.CFB.read(bytes, { type: "array" });
    const stream = (XLSX.CFB.find(file, "Workbook") ?? XLSX.CFB.find(file, "Book"))?.content;
    // Records: a 2-byte type and a 2-byte length; a FONT (0x31) starts with its height in twentieths of a point.
    for (let at = 0; stream && at + 6 <= stream.length; at += 4 + (stream[at + 2] | (stream[at + 3] << 8))) {
      if ((stream[at] | (stream[at + 1] << 8)) === 0x31) return (stream[at + 4] | (stream[at + 5] << 8)) / 20 || null;
    }
  } catch {
    // An unreadable record leaves the face unsaid.
  }
  return null;
}

// The words a cell shows: its formatted text, "—" for an error.
function wordsOf(cell) {
  if (!cell || cell.t === "z") return "";
  if (cell.t === "e") return "—";
  return cell.w ?? (cell.v == null ? "" : String(cell.v));
}

// A column w wide in the file is trunc((256w + trunc(128/d)) / 256 · d) pixels for d the face's widest digit, as
// Excel and Notion's preview count it (light-sheet.js's columnPixels).
function columnPx(ws, rows, c, digit) {
  const column = ws["!cols"]?.[c];
  if (column?.hidden) return 0;
  if (column?.width != null) return Math.trunc((256 * column.width + Math.trunc(128 / digit)) / 256 * digit);
  let units = 0;
  for (const row of rows) {
    let length = 0;
    for (const letter of wordsOf(row?.[c])) length += WIDE.test(letter) ? 2 : 1;
    units = Math.max(units, length);
  }
  return Math.min(COLUMN_MAX, Math.max(COLUMN_MIN, units * 6 + 24));
}

function drawSheet(XLSX, ws, root, digit) {
  const range = ws?.["!ref"] ? XLSX.utils.decode_range(ws["!ref"]) : null;
  const data = ws?.["!data"] ?? [];
  const rowCount = range ? range.e.r + 1 : 0;
  const columnCount = range ? range.e.c + 1 : 0;
  const grid = Object.assign(document.createElement("div"), { className: "tableGrid" });
  // The column widths from the first rows the table draws.
  const sample = data.slice(0, ROWS_AT_ONCE);
  const widths = Array.from({ length: columnCount }, (_, c) => columnPx(ws, sample, c, digit));
  grid.style.gridTemplateColumns = `${NUMBERS_PX}px ${widths.map((width) => `${width}px`).join(" ")} minmax(0, 1fr)`;
  const cellOf = (r, c, head) => {
    const box = document.createElement("div");
    box.className = head ? "tableHead" : "tableCell";
    const value = r < rowCount ? data[r]?.[c] : null;
    const words = Object.assign(document.createElement("span"), { textContent: wordsOf(value) });
    if (value?.t === "n" || value?.t === "e") box.dataset.end = "true";
    if (value?.t === "e") box.dataset.error = "true";
    const fill = value?.s?.patternType && value.s.patternType !== "none" ? value.s.fgColor?.rgb : null;
    if (fill && /^[0-9a-f]{6}$/iu.test(fill.slice(-6))) box.style.background = `#${fill.slice(-6)}`;
    box.append(words);
    return box;
  };
  const line = (r) => {
    const number = Object.assign(document.createElement("div"), { className: r === 0 ? "tableCorner" : "tableNumber" });
    number.append(Object.assign(document.createElement("span"), { textContent: r === 0 || r >= rowCount ? "" : String(r) }));
    grid.append(number);
    for (let c = 0; c < columnCount; c += 1) grid.append(cellOf(r, c, r === 0));
    // The rest of the width, lined as the cells.
    grid.append(Object.assign(document.createElement("div"), { className: r === 0 ? "tableHead tableRest" : "tableCell tableRest" }));
  };
  let drawn = 0;
  // In a page's embed (preview.js embed) empty lines follow the last row as many as fit whole in the view, so a
  // short sheet doesn't scroll; the preview window draws none under it (Notion's, 2026-11-15).
  const embedded = document.documentElement.classList.contains("embed");
  // The sheet's rows as far as the view reaches and a batch past it.
  const fill = () => {
    const reach = Math.ceil((root.scrollTop + root.clientHeight) / ROW_PX);
    const want = Math.max(embedded ? Math.floor(root.clientHeight / ROW_PX) : 0, drawn >= reach ? 0 : Math.min(rowCount, reach + ROWS_AT_ONCE));
    for (; drawn < want; drawn += 1) line(drawn);
  };
  root.replaceChildren(grid);
  root.scrollTo(0, 0);
  if (!columnCount) return () => {};
  fill();
  const resize = new ResizeObserver(fill);
  resize.observe(root);
  root.addEventListener("scroll", fill);
  return () => {
    resize.disconnect();
    root.removeEventListener("scroll", fill);
  };
}
