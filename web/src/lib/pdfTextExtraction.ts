import {
  isPdfStream,
  PdfDocument,
  PdfLexer,
  PdfName,
  PdfOperator,
  PdfReadError,
  PdfString,
  pdfName,
  type PdfDict,
  type PdfValue,
} from "./pdfDocument";
import { ImportOutputBudget } from "./importOutputBudget";
import { loadPdfFont, type PdfFont } from "./pdfFonts";

/**
 * Turns a PDF's text into the block Markdown the workspace importer reads,
 * as Notion's PDF import turns a PDF into a searchable page: text runs are
 * placed by their position into lines, lines into paragraphs, and larger
 * text into headings and bullet characters into list items. Tables, quotes
 * and equations become plain text; images are left out, and a PDF without
 * text (a scan) or one that needs a password to open is refused.
 */

type Matrix = [number, number, number, number, number, number];
type Run = { text: string; x: number; y: number; endX: number; size: number; spaceBefore: boolean };
type Line = { text: string; y: number; size: number; endX: number };

const IDENTITY: Matrix = [1, 0, 0, 1, 0, 0];
// A page is read up to this many drawing operations, and forms inside forms
// this deep: what lies past them — a page denser than any document's, forms
// nested past reason — is left out and counted, never silently.
const MAX_OPERATIONS_PER_PAGE = 2_000_000;
const MAX_FORM_DEPTH = 32;
const BULLET = /^[•·▪‣◦●○■□–−]\s+/u;
const CJK = /[぀-ヿ㐀-鿿가-힯豈-﫿]/u;
const PAGE_NUMBER = /^(?:[-–]\s*)?(?:page\s+)?\d{1,4}(?:\s*(?:\/|of)\s*\d{1,4})?(?:\s*[-–])?$/iu;

function multiply(left: Matrix, right: Matrix): Matrix {
  return [
    left[0] * right[0] + left[1] * right[2], left[0] * right[1] + left[1] * right[3],
    left[2] * right[0] + left[3] * right[2], left[2] * right[1] + left[3] * right[3],
    left[4] * right[0] + left[5] * right[2] + right[4], left[4] * right[1] + left[5] * right[3] + right[5],
  ];
}

function matrixOf(value: PdfValue | undefined): Matrix | null {
  if (!Array.isArray(value) || value.length < 6 || value.slice(0, 6).some((item) => typeof item !== "number")) return null;
  return value.slice(0, 6) as Matrix;
}

class PageReader {
  private readonly fonts = new Map<PdfDict, Map<string, Promise<PdfFont | null>>>();
  private readonly forms = new Set<unknown>();
  private operations = 0;
  runs: Run[] = [];
  omitted = 0;

  constructor(private readonly document: PdfDocument) {}

  private font(resources: PdfDict | undefined, name: string) {
    if (!resources) return Promise.resolve(null);
    let cache = this.fonts.get(resources);
    if (!cache) this.fonts.set(resources, cache = new Map());
    let font = cache.get(name);
    if (!font) {
      const dict = this.document.dict(this.document.dict(resources.get("Font"))?.get(name));
      font = dict ? loadPdfFont(this.document, dict).catch(() => null) : Promise.resolve(null);
      cache.set(name, font);
    }
    return font;
  }

  async read(content: Uint8Array, resources: PdfDict | undefined, baseMatrix: Matrix, depth: number) {
    const lexer = new PdfLexer(content);
    const stack: Array<{ ctm: Matrix; font: PdfFont | null; size: number; tc: number; tw: number; tz: number; tl: number }> = [];
    let ctm = baseMatrix;
    let font: PdfFont | null = null;
    let size = 0;
    let tc = 0;
    let tw = 0;
    let tz = 1;
    let tl = 0;
    let tm: Matrix = IDENTITY;
    let tlm: Matrix = IDENTITY;
    let pendingSpace = false;
    const operands: PdfValue[] = [];
    const moveLine = (tx: number, ty: number) => {
      tlm = multiply([1, 0, 0, 1, tx, ty], tlm);
      tm = tlm;
    };
    const show = (bytes: Uint8Array) => {
      if (!font || size === 0) return;
      const start = multiply(tm, ctm);
      const scale = Math.hypot(start[2], start[3]) * size;
      let text = "";
      for (const glyph of font.decode(bytes)) {
        text += glyph.text;
        const advance = (glyph.width / 1000 * size + tc + (glyph.space ? tw : 0)) * tz;
        tm = multiply([1, 0, 0, 1, advance, 0], tm);
      }
      const end = multiply(tm, ctm);
      if (text) this.runs.push({ text, x: start[4], y: start[5], endX: end[4], size: scale, spaceBefore: pendingSpace });
      pendingSpace = false;
    };
    while (!lexer.atEnd()) {
      // Counted over the whole page, forms drawn inside it too.
      if (this.operations === MAX_OPERATIONS_PER_PAGE) {
        this.omitted += 1;
        break;
      }
      this.operations += 1;
      const token = lexer.read();
      if (token === undefined) break;
      if (!(token instanceof PdfOperator)) {
        operands.push(token);
        continue;
      }
      const number = (index: number) => (typeof operands[index] === "number" ? operands[index] as number : 0);
      switch (token.op) {
        case "q": stack.push({ ctm, font, size, tc, tw, tz, tl }); break;
        case "Q": {
          const saved = stack.pop();
          if (saved) ({ ctm, font, size, tc, tw, tz, tl } = saved);
          break;
        }
        case "cm": ctm = multiply(matrixOf(operands) ?? IDENTITY, ctm); break;
        case "BT": tm = tlm = IDENTITY; break;
        case "Tf":
          font = operands[0] instanceof PdfName ? await this.font(resources, operands[0].name) : null;
          size = number(1);
          break;
        case "Tc": tc = number(0); break;
        case "Tw": tw = number(0); break;
        case "Tz": tz = number(0) / 100; break;
        case "TL": tl = number(0); break;
        case "Td": moveLine(number(0), number(1)); break;
        case "TD": tl = -number(1); moveLine(number(0), number(1)); break;
        case "Tm": tm = tlm = matrixOf(operands) ?? IDENTITY; break;
        case "T*": moveLine(0, -tl); break;
        case "Tj": if (operands[0] instanceof PdfString) show(operands[0].bytes); break;
        case "'": moveLine(0, -tl); if (operands[0] instanceof PdfString) show(operands[0].bytes); break;
        case "\"":
          tw = number(0);
          tc = number(1);
          moveLine(0, -tl);
          if (operands[2] instanceof PdfString) show(operands[2].bytes);
          break;
        case "TJ":
          for (const item of Array.isArray(operands[0]) ? operands[0] : []) {
            if (item instanceof PdfString) show(item.bytes);
            else if (typeof item === "number") {
              tm = multiply([1, 0, 0, 1, -item / 1000 * size * tz, 0], tm);
              if (item < -250) pendingSpace = true;
            }
          }
          break;
        case "Do": {
          const xobject = this.document.resolve(this.document.dict(resources?.get("XObject"))?.get(pdfName(operands[0])));
          if (isPdfStream(xobject) && pdfName(this.document.resolve(xobject.dict.get("Subtype"))) === "Form") {
            // A form drawing itself, directly or through others, is drawn once.
            if (depth >= MAX_FORM_DEPTH || this.forms.has(xobject)) {
              this.omitted += 1;
              break;
            }
            this.forms.add(xobject);
            const matrix = matrixOf(this.document.resolve(xobject.dict.get("Matrix"))) ?? IDENTITY;
            await this.read(await this.document.decode(xobject), this.document.dict(xobject.dict.get("Resources")) ?? resources,
              multiply(matrix, ctm), depth + 1);
            this.forms.delete(xobject);
          }
          break;
        }
        case "ID": {
          // Inline image data runs to the next EI; it holds no text.
          const end = /\sEI(?=[\s]|$)/.exec(new TextDecoder("latin1").decode(content.subarray(lexer.position)));
          lexer.position = end ? lexer.position + end.index + end[0].length : content.length;
          break;
        }
        default: break;
      }
      operands.length = 0;
    }
  }
}

// Every page of the page tree, however deep; a node is visited once, so a
// tree pointing back into itself ends.
function pageList(document: PdfDocument) {
  const pages: Array<{ dict: PdfDict; resources: PdfDict | undefined }> = [];
  const root = document.dict(document.trailer.get("Root"));
  const seen = new Set<PdfDict>();
  const visit = (node: PdfDict | undefined, inherited: PdfDict | undefined) => {
    if (!node || seen.has(node)) return;
    seen.add(node);
    const resources = document.dict(node.get("Resources")) ?? inherited;
    const kids = document.resolve(node.get("Kids"));
    if (Array.isArray(kids)) for (const kid of kids) visit(document.dict(kid), resources);
    else pages.push({ dict: node, resources });
  };
  visit(document.dict(root?.get("Pages")), undefined);
  if (pages.length > 0) return pages;
  return document.objectsOfType("Page").map((value) => {
    const dict = document.dict(value)!;
    return { dict, resources: document.dict(dict.get("Resources")) };
  });
}

async function pageContent(document: PdfDocument, page: PdfDict) {
  const contents = document.resolve(page.get("Contents"));
  const parts = Array.isArray(contents) ? contents.map((part) => document.resolve(part)) : [contents];
  const decoded: Uint8Array[] = [];
  for (const part of parts) if (isPdfStream(part)) decoded.push(await document.decode(part));
  const total = decoded.reduce((sum, part) => sum + part.length + 1, 0);
  const joined = new Uint8Array(total);
  let offset = 0;
  for (const part of decoded) {
    joined.set(part, offset);
    joined[offset + part.length] = 10;
    offset += part.length + 1;
  }
  return joined;
}

function pageLines(runs: Run[]) {
  const lines: Line[] = [];
  for (const run of runs) {
    const line = lines.at(-1);
    const sameLine = line && Math.abs(run.y - line.y) <= 0.4 * Math.max(run.size, line.size)
      && run.x >= line.endX - Math.max(run.size, line.size);
    if (!line || !sameLine) {
      lines.push({ text: run.text, y: run.y, size: run.size, endX: run.endX });
      continue;
    }
    const gap = run.x - line.endX > 0.2 * Math.max(run.size, 1);
    if ((run.spaceBefore || gap) && !/\s$/.test(line.text) && !/^\s/.test(run.text)) line.text += " ";
    line.text += run.text;
    line.endX = run.endX;
    line.size = Math.max(line.size, run.size);
  }
  for (const line of lines) line.text = line.text.replace(/\s+/g, " ").trim();
  const kept = lines.filter((line) => line.text);
  // A lone page number at the top or bottom of a page is not content.
  if (kept.length > 1 && PAGE_NUMBER.test(kept.at(-1)!.text)) kept.pop();
  if (kept.length > 1 && PAGE_NUMBER.test(kept[0]!.text)) kept.shift();
  return kept;
}

function joinLines(left: string, right: string) {
  if (/\p{L}-$/u.test(left) && /^\p{Ll}/u.test(right)) return `${left.slice(0, -1)}${right}`;
  if (CJK.test(left.at(-1) ?? "") || CJK.test(right[0] ?? "")) return `${left}${right}`;
  return `${left} ${right}`;
}

function paragraphsToMarkdown(pages: Line[][]) {
  const sizes = pages.flat().flatMap((line) => Array<number>(Math.min(line.text.length, 200)).fill(line.size))
    .sort((left, right) => left - right);
  const body = sizes[Math.floor(sizes.length / 2)] ?? 12;
  const blocks: string[] = [];
  for (const lines of pages) {
    let current: { lines: string[]; size: number } | null = null;
    let previous: Line | null = null;
    const flush = () => {
      if (!current) return;
      const joined = current.lines.reduce(joinLines).trim();
      const short = current.lines.length <= 2 && joined.length <= 120;
      if (BULLET.test(joined)) blocks.push(`- ${joined.replace(BULLET, "")}`);
      else if (short && current.size >= body * 1.6) blocks.push(`# ${joined}`);
      else if (short && current.size >= body * 1.3) blocks.push(`## ${joined}`);
      else if (short && current.size >= body * 1.15) blocks.push(`### ${joined}`);
      // A paragraph keeps its PDF lines, as Notion's PDF import does (seen
      // 2026-11-05: lines full or short, each its own line in one block, read
      // as Markdown there too: "- " a bullet, "1. " a number, *a* italic).
      else blocks.push(current.lines.join("\n"));
      current = null;
    };
    for (const line of lines) {
      const gap = previous ? previous.y - line.y : 0;
      const size = Math.max(line.size, previous?.size ?? 0);
      // Notion's starts a block only after a gap of about four lines (seen
      // 2026-11-05: three and a half times the type size went on, four and
      // two thirds started a block).
      const breaks = !current || !previous || gap > 4 * size || gap < -0.5 * size
        || Math.abs(line.size - current.size) > 0.15 * current.size || BULLET.test(line.text);
      if (breaks) {
        flush();
        current = { lines: [line.text], size: line.size };
      } else if (current) {
        current.lines.push(line.text);
      }
      previous = line;
    }
    flush();
  }
  return blocks.join("\n\n");
}

/**
 * Reads a PDF's text as block Markdown — every page, or
 * FileImportTooLargeError once its text is larger than Leafen imports — and
 * how many parts were left out (a page past its operations, a form nested
 * too deep or drawing itself).
 */
export async function pdfToImportMarkdown(blob: Blob): Promise<{ markdown: string; omitted: number }> {
  const document = await PdfDocument.open(new Uint8Array(await blob.arrayBuffer()));
  const pages: Line[][] = [];
  const budget = new ImportOutputBudget();
  let omitted = 0;
  for (const [index, page] of pageList(document).entries()) {
    const reader = new PageReader(document);
    await reader.read(await pageContent(document, page.dict), page.resources, IDENTITY, 0);
    const lines = pageLines(reader.runs);
    budget.add(lines.reduce((sum, line) => sum + line.text.length + 2, 0));
    pages.push(lines);
    omitted += reader.omitted;
    if (index % 10 === 9) await new Promise((resolve) => setTimeout(resolve, 0));
  }
  const markdown = paragraphsToMarkdown(pages);
  if (!markdown.trim()) throw new PdfReadError("no_text");
  return { markdown, omitted };
}
