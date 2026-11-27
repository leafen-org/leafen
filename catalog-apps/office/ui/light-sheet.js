// The light view of a sheet (light.js showLight): an Excel or CSV file drawn as Notion's preview draws it.
import { script } from "./light.js";

// A sheet reaches as far as Excel's (Notion's preview draws every row, 2026-11-07); its rows and columns are drawn
// as the grid's view comes to them.
const SHEET_ROWS = 1048576;
const SHEET_COLUMNS = 16384;

// A sheet as Notion's preview draws one (seen 2026-11-07 at 999 and 1280px, dark): a 40px formula bar — the
// chosen cell's name in an 80px box, an italic "fx" and the cell's formula or value —, over a grid to the window's
// edges past the used cells (letters and numbers on the page's color, white cells, the file's widths and heights,
// from A1), the chosen cell outlined with its letter and number lit, and a 30px bar under it: ◀ ▶ for the sheet
// before and after, the sheets, and a zoom of 10% steps around 100% on a slider (a CSV file's: its name, − 100% +).
const NUMBERS_PX = 50;
const ZOOM_MIN = 0.1;
const ZOOM_MAX = 4;

// A sheet's sizes as Excel and Notion's preview count them (read 2026-11-12 at 1440 dark, in an embed and full
// screen alike): a column w wide in the file is trunc((256w + trunc(128/d)) / 256 · d) pixels for d the widest
// digit of the file's own face (Calibri 11pt: 7, so 5 is 35px and 20 is 140px; a CSV file reads as Calibri 11pt,
// as Excel opens one), and a row h points tall is round(h · 4/3) pixels. A column or row the file doesn't size is
// its sheet's default width or height, or Excel's 8.43 characters (64px in Calibri 11pt) and 15pt (20px). Notion
// draws that default column at 59px or 67px depending on whether its stand-in font has loaded when it measures;
// Leafen keeps Excel's every time.
const DEFAULT_COLUMN_CHARS = 8.43;
const DEFAULT_ROW_POINTS = 15;
// charsWide is the file width of a column that shows n digits, its cell margins (5px) included.
const charsWide = (n, digit) => Math.trunc((n * digit + 5) / digit * 256) / 256;
const columnPixels = (width, digit) => Math.trunc((256 * width + Math.trunc(128 / digit)) / 256 * digit);
const rowPixels = (points) => Math.round(points * 4 / 3);

// digitWidth is the widest digit of a font from the file's styles, in whole pixels. Calibri's digits are
// 1038/2048 of an em, as Carlito's, the stand-in Notion draws it in; a face the computer lacks falls back to
// them, as Notion's does; another face is measured as this page draws it.
function digitWidth(font) {
  const px = (Number(font?.sz) || 11) * 4 / 3;
  const face = String(font?.name || "Calibri").replace(/["\\]/g, "");
  const calibri = Math.round(px * 1038 / 2048);
  const context = /^calibri( light)?$/i.test(face) ? null : document.createElement("canvas").getContext("2d");
  if (!context) return calibri;
  const width = (fallback) => {
    context.font = `${px}px "${face}", ${fallback}`;
    return context.measureText("0123456789").width;
  };
  if (width("monospace") !== width("serif")) return calibri;
  let widest = 0;
  for (const digit of "0123456789") widest = Math.max(widest, context.measureText(digit).width);
  return Math.round(widest) || calibri;
}

// sheetFormats reads each sheet's own default column width and row height (its sheetFormatPr), which SheetJS
// doesn't keep, from the file's parts (read with bookFiles): the workbook names each sheet's part by relationship.
function sheetFormats(book) {
  const formats = new Map();
  const workbook = String(book.Directory?.workbooks?.[0] || "xl/workbook.xml").replace(/^\//, "");
  const folder = workbook.slice(0, workbook.lastIndexOf("/") + 1);
  // Only a part's head is read: sheetFormatPr comes before the sheet's cells.
  const text = (path, head = Infinity) => {
    const content = book.files?.[path]?.content;
    return content ? new TextDecoder().decode(Uint8Array.from(content.slice(0, head))) : "";
  };
  const value = (tag, name) => tag.match(new RegExp(`\\s${name}="([^"]*)"`))?.[1];
  const words = (raw = "") => raw.replace(/&(lt|gt|quot|apos|amp);/g, (_, entity) => ({ lt: "<", gt: ">", quot: "\"", apos: "'", amp: "&" })[entity]);
  const targets = new Map();
  for (const [tag] of text(`${folder}_rels/${workbook.slice(folder.length)}.rels`).matchAll(/<Relationship\b[^>]*>/g)) {
    targets.set(value(tag, "Id"), value(tag, "Target"));
  }
  for (const [tag] of text(workbook).matchAll(/<sheet\b[^>]*>/g)) {
    const target = targets.get(value(tag, "\\w+:id"));
    if (!target) continue;
    const format = text(target.startsWith("/") ? target.slice(1) : folder + target, 4096).match(/<sheetFormatPr\b[^>]*>/)?.[0] ?? "";
    const number = (name, otherwise) => {
      const said = Number(value(format, name) ?? NaN);
      return Number.isFinite(said) && said >= 0 ? said : otherwise;
    };
    formats.set(words(value(tag, "name")), { columnWidth: number("defaultColWidth", null), rowPoints: number("defaultRowHeight", null) });
  }
  return formats;
}

export async function showXLSX(data, say, root, tabs, csvName = null) {
  await script("xlsx-0.20.3/xlsx.full.min.js");
  const XLSX = globalThis.XLSX;
  const book = readBook(XLSX, data, csvName !== null);
  const names = book ? book.SheetNames.filter((name, index) => !book.Workbook?.Sheets?.[index]?.Hidden) : [];
  const digit = digitWidth(book && csvName === null ? book.Styles?.Fonts?.[0] : null);
  const formats = book && csvName === null ? sheetFormats(book) : new Map();
  const sizes = (name) => ({
    digit,
    columnWidth: formats.get(name)?.columnWidth ?? charsWide(DEFAULT_COLUMN_CHARS, digit),
    rowPoints: formats.get(name)?.rowPoints ?? DEFAULT_ROW_POINTS,
  });
  root.classList.add("xlsx");
  const cellName = Object.assign(document.createElement("input"), { className: "cellName", readOnly: true, tabIndex: -1 });
  const fx = Object.assign(document.createElement("i"), { className: "fx", textContent: "fx" });
  fx.setAttribute("aria-hidden", "true");
  const cellValue = Object.assign(document.createElement("input"), { className: "cellValue", readOnly: true, placeholder: say.pickCell });
  const bar = Object.assign(document.createElement("div"), { className: "formula" });
  bar.append(cellName, fx, cellValue);
  const grid = Object.assign(document.createElement("div"), { className: "grid", tabIndex: 0 });
  root.replaceChildren(bar, grid);

  tabs.hidden = false;
  tabs.removeAttribute("role");
  // ◀ ▶ move the row of sheets along when it is wider than the bar, as Notion's do; they don't change the sheet.
  const step = (text, by) => {
    const button = Object.assign(document.createElement("button"), { type: "button", className: "step", textContent: text });
    button.setAttribute("aria-label", by < 0 ? say.sheetsLeft : say.sheetsRight);
    button.addEventListener("click", () => list.scrollBy({ left: by * list.clientWidth * 0.8, behavior: "smooth" }));
    return button;
  };
  const before = step("◀", -1);
  const after = step("▶", 1);
  const list = Object.assign(document.createElement("div"), { className: "tabs" });
  list.setAttribute("role", "tablist");
  const buttons = names.map((name) => {
    const button = Object.assign(document.createElement("button"), { type: "button", textContent: csvName || name });
    button.setAttribute("role", "tab");
    button.addEventListener("click", () => open(name));
    return button;
  });
  list.append(...buttons);
  const zoomOut = Object.assign(document.createElement("button"), { type: "button", className: "zoomStep", textContent: "−" });
  const zoomIn = Object.assign(document.createElement("button"), { type: "button", className: "zoomStep", textContent: "+" });
  zoomOut.setAttribute("aria-label", say.zoomOut);
  zoomIn.setAttribute("aria-label", say.zoomIn);
  const slider = Object.assign(document.createElement("input"), { type: "range", min: "0", max: "100", step: "any", className: "zoomSlider" });
  slider.setAttribute("aria-label", say.zoom);
  const percent = Object.assign(document.createElement("span"), { className: "zoomPercent" });
  const zoomBox = Object.assign(document.createElement("div"), { className: "sheetZoom" });
  // A CSV file's bar names the file and zooms with − 100% +, with no sheets to move along or slider, as Notion's
  // (seen 2026-11-12 in an embed and its full screen; an Excel file of one sheet keeps its tab and slider).
  tabs.classList.toggle("csv", csvName !== null);
  if (csvName !== null) {
    const label = Object.assign(document.createElement("span"), { className: "sheetName", textContent: csvName });
    zoomBox.append(zoomOut, percent, zoomIn);
    tabs.replaceChildren(label, zoomBox);
  } else {
    zoomBox.append(zoomOut, slider, zoomIn, percent);
    tabs.replaceChildren(before, after, list, zoomBox);
  }

  let view = null;
  let zoom = 1;
  const setZoom = (value) => {
    zoom = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, Math.round(value * 100) / 100));
    slider.value = String(zoom <= 1 ? (zoom - ZOOM_MIN) / (1 - ZOOM_MIN) * 50 : 50 + (zoom - 1) / (ZOOM_MAX - 1) * 50);
    percent.textContent = `${Math.round(zoom * 100)}%`;
    zoomOut.disabled = zoom <= ZOOM_MIN;
    zoomIn.disabled = zoom >= ZOOM_MAX;
    view?.zoom(zoom);
  };
  zoomOut.addEventListener("click", () => setZoom(Math.round(zoom * 10 - 1) / 10));
  zoomIn.addEventListener("click", () => setZoom(Math.round(zoom * 10 + 1) / 10));
  slider.addEventListener("input", () => {
    const at = Number(slider.value);
    setZoom(at <= 50 ? ZOOM_MIN + at / 50 * (1 - ZOOM_MIN) : 1 + (at - 50) / 50 * (ZOOM_MAX - 1));
  });
  const choose = (place) => {
    cellName.value = place ? XLSX.utils.encode_cell(place) : "";
    cellValue.value = place?.formula ?? place?.text ?? "";
    // Notion's bar sets a formula in a fixed-width bold face and a value as the bar's own words.
    cellValue.classList.toggle("formulaText", Boolean(place?.formula));
  };
  const open = (name) => {
    if (!name) return;
    buttons.forEach((button, index) => button.setAttribute("aria-selected", String(names[index] === name)));
    view?.stop();
    view = sheetGrid(XLSX, book.Sheets[name], say, grid, choose, sizes(name));
    view.zoom(zoom);
    choose(null);
    grid.scrollTo(0, 0);
  };
  setZoom(1);
  // A workbook that can't be read keeps the sheet's frame, its bar and zoom with no sheets, round its notice.
  if (!book) showUnreadable(grid, say);
  open(names[0]);
}

// readBook reads an Excel or CSV file with SheetJS, or null when it can't be read as one. An Excel file must be a
// ZIP package, as Notion's preview reads it: SheetJS would draw any other bytes as words in a sheet, where Notion's
// says the file can't be shown (seen 2026-11-15 for a cut-off and a random-byte .xlsx file).
function readBook(XLSX, data, csv) {
  const bytes = new Uint8Array(data);
  if (!csv && !(bytes[0] === 0x50 && bytes[1] === 0x4b)) return null;
  try {
    // Stubs keep a formula cell whose file holds no value, as Notion shows its formula with nothing in the cell;
    // cellStyles keeps the file's column widths and row heights, and bookFiles its parts for sheetFormats.
    return XLSX.read(bytes, { type: "array", dense: true, cellFormula: true, sheetStubs: true, cellStyles: true, bookFiles: !csv, raw: csv });
  } catch {
    return null;
  }
}

// showUnreadable puts the notice of a sheet that can't be drawn in its grid, as Notion's preview draws it on its
// canvas (seen 2026-11-15 in an embed and the preview window, dark; its sizes and places read from its drawing,
// which takes no theme): on #f7f7f8, a red ⚠ 32% down, the words 46% down and the reason under them, each sized and
// placed from the grid's shorter side (light.css .grid.unreadable). The words and reason are Leafen's own.
function showUnreadable(grid, say) {
  const line = (className, text) => Object.assign(document.createElement("p"), { className, textContent: text });
  // In its text form (U+FE0E), so a phone draws it in the notice's red rather than as a yellow emoji.
  const mark = line("mark", "⚠\uFE0E");
  mark.setAttribute("aria-hidden", "true");
  grid.replaceChildren(mark, line("title", say.unreadable), line("why", say.unreadableWhy));
  grid.classList.add("unreadable");
}

// sheetGrid draws one sheet into grid from A1 at its sizes, adds empty rows and columns as the grid's view needs
// them, and keeps the chosen cell; it tells choose the chosen cell's place and what the formula bar says for it.
function sheetGrid(XLSX, ws, say, grid, choose, sizes) {
  if (!ws) {
    showUnreadable(grid, say);
    return { zoom() {}, stop() {} };
  }
  grid.classList.remove("unreadable");
  const range = ws["!ref"] ? XLSX.utils.decode_range(ws["!ref"]) : { e: { r: -1, c: -1 } };
  const allRows = range.e.r + 1;
  const allColumns = range.e.c + 1;
  const data = ws["!data"] ?? [];
  const covered = new Map();
  const spans = new Map();
  for (const merge of ws["!merges"] ?? []) {
    spans.set(`${merge.s.r},${merge.s.c}`, [merge.e.r - merge.s.r + 1, merge.e.c - merge.s.c + 1]);
    for (let r = merge.s.r; r <= merge.e.r; r += 1) for (let c = merge.s.c; c <= merge.e.c; c += 1) {
      if (r !== merge.s.r || c !== merge.s.c) covered.set(`${r},${c}`, `${merge.s.r},${merge.s.c}`);
    }
  }
  const table = document.createElement("table");
  const columns = document.createElement("colgroup");
  const numbersColumn = document.createElement("col");
  numbersColumn.style.width = `${NUMBERS_PX}px`;
  columns.append(numbersColumn);
  const head = table.createTHead().insertRow();
  head.append(document.createElement("th"));
  const body = table.createTBody();
  table.prepend(columns);
  const cells = new Map();
  let rowCount = 0;
  let columnCount = 0;
  // The table as wide as its columns, so each keeps its width whatever is in it.
  let tableWidth = NUMBERS_PX;
  let zoom = 1;

  const columnPx = columnPixels(sizes.columnWidth, sizes.digit);
  const rowPx = rowPixels(sizes.rowPoints);
  const columnWidth = (c) => {
    const column = ws["!cols"]?.[c];
    if (column?.hidden) return 0;
    return column?.width == null ? columnPx : columnPixels(column.width, sizes.digit);
  };
  const rowHeight = (r) => {
    const row = ws["!rows"]?.[r];
    return row?.hidden ? 0 : row?.hpt == null ? rowPx : rowPixels(row.hpt);
  };
  const empty = (r, c) => {
    const value = data[r]?.[c];
    return !covered.has(`${r},${c}`) && !spans.has(`${r},${c}`) && (!value || value.t === "z");
  };
  // A cell's words sit at its foot and are cut at its edges, so a row keeps its height and a column its width; a
  // number too wide shows its end. Words run on over the empty cells after theirs, up to the next cell with
  // something in it, as Notion's (and Excel's) do.
  const cell = (r, c) => {
    const td = document.createElement("td");
    const value = data[r]?.[c];
    const span = spans.get(`${r},${c}`);
    if (span) [td.rowSpan, td.colSpan] = span;
    if (value && value.t !== "z") {
      const words = Object.assign(document.createElement("span"), { textContent: value.w ?? (value.v == null ? "" : String(value.v)) });
      td.dataset.t = value.t;
      td.append(words);
      let tall = 0;
      for (let i = 0; i < (span?.[0] ?? 1); i += 1) tall += rowHeight(r + i);
      words.style.maxHeight = `calc(${tall}px - var(--grid-line))`;
      let room = 0;
      for (let next = c + 1; value.t === "s" && !span && words.textContent && next < SHEET_COLUMNS && empty(r, next) && room < 4096; next += 1) {
        room += columnWidth(next);
      }
      if (room) {
        words.style.maxWidth = `${columnWidth(c) - 3 + room}px`;
        td.classList.add("spill");
      }
    }
    td.dataset.r = String(r);
    td.dataset.c = String(c);
    cells.set(`${r},${c}`, td);
    return td;
  };
  const addColumns = (count) => {
    for (; columnCount < count; columnCount += 1) {
      const c = columnCount;
      const column = document.createElement("col");
      column.style.width = `${columnWidth(c)}px`;
      columns.append(column);
      tableWidth += columnWidth(c);
      table.style.width = `${tableWidth}px`;
      const letter = document.createElement("th");
      letter.textContent = XLSX.utils.encode_col(c);
      head.append(letter);
      [...body.rows].forEach((row, r) => { if (!covered.has(`${r},${c}`)) row.append(cell(r, c)); });
    }
  };
  const addRows = (count) => {
    for (; rowCount < count; rowCount += 1) {
      const r = rowCount;
      const row = body.insertRow();
      row.style.height = `${rowHeight(r)}px`;
      const number = document.createElement("th");
      number.textContent = String(r + 1);
      row.append(number);
      for (let c = 0; c < columnCount; c += 1) if (!covered.has(`${r},${c}`)) row.append(cell(r, c));
    }
  };
  // Two views' worth of rows and columns past what is in view, drawn again as it scrolls toward them.
  const fill = () => {
    const wantColumns = Math.ceil((grid.scrollLeft + grid.clientWidth * 2) / zoom / Math.max(columnPx, 8));
    const wantRows = Math.ceil((grid.scrollTop + grid.clientHeight * 2) / zoom / Math.max(rowPx, 8));
    if (wantColumns > columnCount) addColumns(Math.min(SHEET_COLUMNS, wantColumns + 8));
    if (wantRows > rowCount) addRows(Math.min(SHEET_ROWS, wantRows + 100));
  };
  grid.replaceChildren(table);
  fill();

  let chosen = null;
  const lit = [];
  const choosePlace = (r, c) => {
    const key = covered.get(`${r},${c}`) ?? `${r},${c}`;
    const td = cells.get(key);
    if (!td) return;
    chosen?.classList.remove("chosen");
    lit.splice(0).forEach((th) => th.classList.remove("lit"));
    chosen = td;
    td.classList.add("chosen");
    const [top, left] = key.split(",").map(Number);
    for (let i = 0; i < td.colSpan; i += 1) lit.push(head.cells[left + 1 + i]);
    for (let i = 0; i < td.rowSpan; i += 1) lit.push(body.rows[top + i]?.cells[0]);
    lit.forEach((th) => th?.classList.add("lit"));
    const value = data[top]?.[left];
    choose({ r: top, c: left, formula: value?.f ? `=${value.f}` : null, text: td.textContent });
    td.scrollIntoView({ block: "nearest", inline: "nearest" });
  };
  const onClick = (event) => {
    const td = event.target.closest?.("td");
    if (td && grid.contains(td)) choosePlace(Number(td.dataset.r), Number(td.dataset.c));
  };
  const moves = { ArrowUp: [-1, 0], ArrowDown: [1, 0], ArrowLeft: [0, -1], ArrowRight: [0, 1] };
  // An arrow moves one cell; with ⌘ (Ctrl) it goes to the used cells' edge that way, or the sheet's first row or column.
  const onKey = (event) => {
    const move = moves[event.key];
    if (!move || !chosen) return;
    event.preventDefault();
    const far = event.metaKey || event.ctrlKey;
    const step = (at, by, span, last) => (far ? (by < 0 ? 0 : Math.max(at, last)) : at + (by > 0 ? span : by));
    const r = move[0] ? step(Number(chosen.dataset.r), move[0], chosen.rowSpan, allRows - 1) : Number(chosen.dataset.r);
    const c = move[1] ? step(Number(chosen.dataset.c), move[1], chosen.colSpan, allColumns - 1) : Number(chosen.dataset.c);
    if (r < 0 || c < 0 || r >= SHEET_ROWS || c >= SHEET_COLUMNS) return;
    if (r >= rowCount || c >= columnCount) {
      addRows(Math.min(SHEET_ROWS, Math.max(rowCount, r + 1)));
      addColumns(Math.min(SHEET_COLUMNS, Math.max(columnCount, c + 1)));
    }
    choosePlace(r, c);
  };
  const resize = new ResizeObserver(fill);
  resize.observe(grid);
  grid.addEventListener("scroll", fill);
  grid.addEventListener("click", onClick);
  grid.addEventListener("keydown", onKey);
  return {
    zoom(value) { zoom = value; table.style.zoom = String(value); fill(); },
    stop() {
      resize.disconnect();
      grid.removeEventListener("scroll", fill);
      grid.removeEventListener("click", onClick);
      grid.removeEventListener("keydown", onKey);
    },
  };
}
