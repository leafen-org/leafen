// The light way to read a Word, Excel or PowerPoint file, inside the Office
// app when ONLYOFFICE isn't for the device or didn't open (document apps
// decision of 2026-10-10, 11): docx-preview, SheetJS and pptx-renderer
// (ui/vendor, each beside its license) draw it to read, loaded only when a
// file of their kind opens.
import { showXLSX } from "./light-sheet.js";

const vendor = new URL("./vendor/", import.meta.url);

export const lightWords = {
  en: {
    unreadable: "This sheet can't be shown here.", unreadableWhy: "The file is damaged or isn't an Excel workbook.", noSlides: "This presentation has no slides to show.",
    pickCell: "Select a cell to see its value", sheetsLeft: "Scroll sheets left", sheetsRight: "Scroll sheets right",
    zoom: "Zoom", zoomIn: "Zoom in", zoomOut: "Zoom out",
    slide: (at, of) => `Slide ${at} of ${of}`,
  },
  ko: {
    unreadable: "이 시트는 여기서 보여 줄 수 없습니다.", unreadableWhy: "파일이 손상되었거나 Excel 통합 문서가 아닙니다.", noSlides: "이 프레젠테이션에는 보여 줄 슬라이드가 없습니다.",
    pickCell: "셀을 선택하여 값을 확인하세요", sheetsLeft: "시트 목록을 왼쪽으로", sheetsRight: "시트 목록을 오른쪽으로",
    zoom: "확대/축소", zoomIn: "확대", zoomOut: "축소",
    slide: (at, of) => `슬라이드 ${at} / ${of}`,
  },
};

// showLight draws the file into root; place says where it is, as a slide count.
export async function showLight(ext, data, say, { root, tabs, place, name }) {
  root.hidden = false;
  if (ext === "docx") return showDOCX(data, root);
  // A CSV file is a sheet named for its file whose cells keep their words as written, as Notion's
  // (2026-11-12: "1" stands left, its tab "〈name〉.csv").
  if (ext === "xlsx" || ext === "csv") return showXLSX(data, say, root, tabs, ext === "csv" ? name || "" : null);
  return showPPTX(data, say, root, place);
}

// script loads an engine that sets a global, as a classic script.
export function script(path) {
  return new Promise((resolve, reject) => {
    const element = document.createElement("script");
    element.src = new URL(path, vendor).href;
    element.onload = resolve;
    element.onerror = () => reject(new Error(`couldn't load ${path}`));
    document.head.append(element);
  });
}

async function showDOCX(data, root) {
  await script("jszip-3.10.1/jszip.min.js");
  await script("docx-preview-0.4.1/docx-preview.min.js");
  root.classList.add("docx");
  await globalThis.docx.renderAsync(data, root, undefined, {
    className: "docx", inWrapper: true, ignoreLastRenderedPageBreak: true, useBase64URL: true,
    renderChanges: false, renderComments: false,
  });
  await placeTablesAsWord(data, root);
  await listsAsWord(data, root);
  spaceLinesAsWord(root);
  const fit = () => fitPages(root, [...root.querySelectorAll("section.docx")]);
  fit();
  addEventListener("resize", fit);
}

// placeTablesAsWord sets each table where the Word a document was laid out
// for (its compatibility mode) puts it. Word 2010 and older line the first
// cell's text up with the margin, so the table's border stands out past the
// margin by that cell's left margin; Word 2013 and later put the border on
// the margin. docx-preview always does the latter, and Notion's preview the
// former for a mode-14 file (its table 8px left of the text, 2026-11-06).
async function placeTablesAsWord(data, root) {
  const zip = await globalThis.JSZip.loadAsync(data);
  const settings = await zip.file("word/settings.xml")?.async("string");
  const xml = settings ? new DOMParser().parseFromString(settings, "application/xml") : null;
  const mode = [...(xml?.getElementsByTagName("w:compatSetting") ?? [])]
    .find((setting) => setting.getAttribute("w:name") === "compatibilityMode")?.getAttribute("w:val");
  // A file without the setting was laid out before Word 2010 wrote it.
  if (Number(mode) >= 15) return;
  const tables = [...root.querySelectorAll("section.docx table")].filter((table) => !table.parentElement?.closest("table"));
  const shifts = tables.map((table) => {
    const cell = table.querySelector("td");
    return cell ? parseFloat(getComputedStyle(cell).paddingLeft) || 0 : 0;
  });
  // The whole table moves, as wide as before: a negative margin would widen an
  // auto-width table by as much.
  tables.forEach((table, index) => {
    if (shifts[index]) table.style.translate = `${-shifts[index]}px 0`;
  });
}

// A symbol face's private letters (U+F0xx, its own byte) that lists use as
// bullets, as the letters they draw: no browser face has them at that place,
// so docx-preview drew a box where Notion's preview draws "•" (2026-11-15).
const SYMBOL_LETTERS = {
  symbol: { "\uf0b7": "\u2022" },
  wingdings: { "\uf0a7": "\u25aa", "\uf0a8": "\u25fb", "\uf06e": "\u25a0", "\uf076": "\u2756", "\uf0d8": "\u27a2", "\uf0fc": "\u2713" },
};

// listsAsWord draws a list as Word does where docx-preview doesn't: a bullet
// written in a symbol face as the letter it stands for, and a style's
// contextual spacing — no space between two paragraphs of that same style,
// as Word's "List Bullet" — so the items of a list stand close together, as
// Notion's preview draws them (2026-11-15: 19px apart where docx-preview
// left each one's 10pt after).
async function listsAsWord(data, root) {
  for (const sheet of [...root.querySelectorAll("style")].map((element) => element.sheet).filter(Boolean)) {
    for (const rule of sheet.cssRules) {
      if (!rule.style?.content || !rule.selectorText?.endsWith(":before")) continue;
      const letters = SYMBOL_LETTERS[(rule.style.fontFamily ?? "").replace(/["']/g, "").trim().toLowerCase()];
      if (letters && /[\uf020-\uf0ff]/u.test(rule.style.content)) {
        rule.style.content = rule.style.content.replace(/[\uf020-\uf0ff]/gu, (letter) => letters[letter] ?? letter);
      }
      // A number or bullet followed by a tab, Word's usual, reaches the
      // paragraph's indent before its words start: the mark takes its
      // paragraph's hanging width (--word-hang, below), as Notion's preview
      // starts the words a quarter inch in (2026-11-15).
      if (/\t|\\9/u.test(rule.style.content)) {
        rule.style.display = "inline-block";
        rule.style.minWidth = "var(--word-hang, 0px)";
        // Its own line takes none of the paragraph's hanging indent, which it inherits.
        rule.style.textIndent = "0";
      }
    }
  }
  for (const paragraph of root.querySelectorAll("section.docx p[class*='-num-']")) {
    const hang = -parseFloat(getComputedStyle(paragraph).textIndent);
    if (hang > 0) paragraph.style.setProperty("--word-hang", `${hang}px`);
  }
  const zip = await globalThis.JSZip.loadAsync(data);
  const styles = await zip.file("word/styles.xml")?.async("string");
  if (!styles) return;
  const xml = new DOMParser().parseFromString(styles, "application/xml");
  const own = new Map([...xml.getElementsByTagName("w:style")].map((style) => {
    const spacing = [...style.getElementsByTagName("w:contextualSpacing")][0];
    const value = spacing?.getAttribute("w:val");
    return [style.getAttribute("w:styleId") ?? "", {
      contextual: spacing ? !["0", "false", "off"].includes(value ?? "") : undefined,
      basedOn: [...style.getElementsByTagName("w:basedOn")][0]?.getAttribute("w:val") ?? "",
    }];
  }));
  // A style takes it from the one it is based on unless it says.
  const contextual = (id, seen = new Set()) => {
    const style = own.get(id);
    if (!style || seen.has(id)) return false;
    seen.add(id);
    return style.contextual ?? contextual(style.basedOn, seen);
  };
  // docx-preview names a paragraph's style as a class: docx_ and its ID, lowered (processStyleName).
  const classOf = (id) => `docx_${id.replace(/[ .]+/g, "-").replace(/[&]+/g, "and").toLowerCase()}`;
  const closed = [...own.keys()].filter((id) => contextual(id)).map(classOf);
  for (const name of closed) {
    for (const paragraph of root.querySelectorAll(`section.docx p.${CSS.escape(name)}`)) {
      const next = paragraph.nextElementSibling;
      if (next?.tagName !== "P" || !next.classList.contains(name)) continue;
      paragraph.style.marginBottom = "0";
      next.style.marginTop = "0";
    }
  }
}

// spaceLinesAsWord gives "auto" line spacing Word's height: Word multiplies the
// spacing (line/240) by the line's own font's line height (ascent, descent and
// gap; Cambria's is about 1.17em) at its largest text, where docx-preview
// multiplies the paragraph's font size, so single-spaced table rows drew 20px
// against Notion's 24 (2026-11-06). Exact and at-least spacing stay as written.
const PLAIN_NUMBER = /^\d*\.?\d+$/u;
function spaceLinesAsWord(root) {
  // A multiple docx-preview wrote becomes that multiple of --word-line, so a
  // paragraph spaced "auto" is the one whose height follows it.
  const multiply = (style) => {
    if (PLAIN_NUMBER.test(style.lineHeight)) style.lineHeight = `calc(${style.lineHeight} * var(--word-line, 1))`;
  };
  for (const sheet of [...root.querySelectorAll("style")].map((element) => element.sheet).filter(Boolean)) {
    for (const rule of sheet.cssRules) if (rule.style) multiply(rule.style);
  }
  for (const element of root.querySelectorAll("section.docx [style*='line-height']")) multiply(element.style);
  const paragraphs = [...root.querySelectorAll("section.docx p")];
  const heightAt = (value) => {
    paragraphs.forEach((paragraph) => paragraph.style.setProperty("--word-line", value));
    return paragraphs.map((paragraph) => parseFloat(getComputedStyle(paragraph).lineHeight));
  };
  const doubled = heightAt("2"), single = heightAt("1");
  paragraphs.forEach((paragraph) => paragraph.style.removeProperty("--word-line"));
  const spaced = paragraphs.map((paragraph, index) => {
    if (!(doubled[index] > single[index])) return null;
    const multiple = single[index] / parseFloat(getComputedStyle(paragraph).fontSize);
    // The largest text in it, where its runs carry their own sizes.
    const runs = [...paragraph.querySelectorAll("span")].filter((span) => span.textContent.trim());
    const largest = runs.reduce((top, run) => (parseFloat(getComputedStyle(run).fontSize) > parseFloat(getComputedStyle(top).fontSize) ? run : top),
      runs[0] ?? paragraph);
    return { paragraph, multiple, style: getComputedStyle(largest) };
  }).filter(Boolean);
  // Each one's text on one line at that font with the browser's normal line height, measured all at once.
  const probes = spaced.map(({ paragraph, style }) => {
    const probe = document.createElement("div");
    probe.textContent = (paragraph.textContent || "").slice(0, 40) || "Hg";
    Object.assign(probe.style, { position: "absolute", visibility: "hidden", whiteSpace: "nowrap", lineHeight: "normal",
      fontFamily: style.fontFamily, fontSize: "100px", fontWeight: style.fontWeight, fontStyle: style.fontStyle });
    return probe;
  });
  root.append(...probes);
  const natural = probes.map((probe) => probe.getBoundingClientRect().height / 100);
  probes.forEach((probe) => probe.remove());
  spaced.forEach(({ paragraph, multiple, style }, index) => {
    if (natural[index] > 0) paragraph.style.lineHeight = `${(multiple * natural[index] * parseFloat(style.fontSize)).toFixed(2)}px`;
  });
}

// fitPages draws the pages as wide as the frame, as Notion's preview does
// (2026-11-06). A wider frame zooms them; a narrower one, as on a phone,
// scales each page inside a box of the scaled size instead: WebKit drops table
// borders thinner than a pixel once zoomed down (iOS Safari, 2026-11-05).
function fitPages(root, pages) {
  if (!pages.length) return;
  const boxOf = (page) => (page.parentElement?.classList.contains("fit") ? page.parentElement : null);
  for (const page of pages) {
    page.style.transform = "";
    page.style.zoom = "";
  }
  const style = getComputedStyle(root);
  const room = root.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight);
  const widest = Math.max(...pages.map((page) => page.offsetWidth));
  const scale = room / widest;
  for (const page of pages) {
    let box = boxOf(page);
    if (scale >= 1) {
      box?.replaceWith(page);
      page.style.zoom = String(scale);
      continue;
    }
    if (!box) {
      box = document.createElement("div");
      box.className = "fit";
      page.replaceWith(box);
      box.append(page);
    }
    page.style.transform = `scale(${scale})`;
    box.style.width = `${page.offsetWidth * scale}px`;
    box.style.height = `${page.offsetHeight * scale}px`;
  }
}


export function note(text) {
  const element = document.createElement("p");
  element.className = "note";
  element.textContent = text;
  return element;
}

async function showPPTX(data, say, root, place) {
  const { PptxViewer, RECOMMENDED_ZIP_LIMITS } = await import(new URL("pptx-renderer-1.3.0/aiden0z-pptx-renderer.browser.es.js", vendor).href);
  root.classList.add("pptx");
  let count = 0;
  // pptx-renderer fits slides to its container's whole width, so it gets one
  // inside the padding.
  const stage = document.createElement("div");
  root.append(stage);
  const viewer = await PptxViewer.open(data, stage, {
    zipLimits: RECOMMENDED_ZIP_LIMITS, lazyMedia: true, lazySlides: true, pdfjs: false, fitMode: "contain",
    listOptions: { windowed: true, initialSlides: 4, batchSize: 4 },
    onSlideChange: (index) => { if (count) place(say.slide(index + 1, count)); },
  });
  count = viewer.slideCount;
  if (!count) {
    root.replaceChildren(note(say.noSlides));
    return;
  }
  place(say.slide(viewer.currentSlideIndex + 1, count));
}
