import { drawPrintedEquations } from "./printExportEquations";

/**
 * PDF export prints the server's printable HTML export: the page (and its
 * subpages) as one document, paper size and scale applied, saved as a PDF
 * through the browser's print dialog.
 */

const PRINT_LOAD_WAIT_MS = 20_000;

export const PRINT_PAPER_FORMATS = ["A4", "A3", "Letter", "Legal", "Tabloid"] as const;
export type PrintPaperFormat = (typeof PRINT_PAPER_FORMATS)[number];

export const PRINT_SCALE_BOUNDS = Object.freeze({ min: 10, max: 200, default: 100 });

/** The largest printable document the browser reads into memory. */
const PRINT_DOCUMENT_MAX_BYTES = 96 * 1024 * 1024;

export function clampPrintScale(value: number) {
  if (!Number.isFinite(value)) return PRINT_SCALE_BOUNDS.default;
  return Math.min(PRINT_SCALE_BOUNDS.max, Math.max(PRINT_SCALE_BOUNDS.min, Math.round(value)));
}

export function printableExportHtml(html: string, paper: PrintPaperFormat, scalePercent: number) {
  const size = PRINT_PAPER_FORMATS.includes(paper) ? paper : "A4";
  const zoom = clampPrintScale(scalePercent) / 100;
  const style = `<style>@page{size:${size};margin:16mm}html{zoom:${zoom}}</style>`;
  return html.includes("</head>") ? html.replace("</head>", `${style}</head>`) : `${style}${html}`;
}

export async function readPrintableExport(response: Response) {
  const declared = Number(response.headers.get("content-length"));
  if (Number.isFinite(declared) && declared > PRINT_DOCUMENT_MAX_BYTES) {
    throw new RangeError("The printable export is too large to open.");
  }
  const blob = await response.blob();
  if (blob.size > PRINT_DOCUMENT_MAX_BYTES) throw new RangeError("The printable export is too large to open.");
  return blob.text();
}

/**
 * Opens the printable document in a hidden frame that runs no script and
 * asks the browser to print it; the frame goes when the print ends — its
 * afterprint, which Chrome fires as the dialog closes, print() returning only
 * then (2026-11-18). The frame never takes the focus (printFrameWithoutFocus).
 */
export async function printExportDocument(
  html: string,
  options: { paper: PrintPaperFormat; scale: number; signal?: AbortSignal },
  owner: Document = document,
) {
  options.signal?.throwIfAborted();
  const frame = owner.createElement("iframe");
  frame.setAttribute("sandbox", "allow-same-origin allow-modals");
  frame.setAttribute("aria-hidden", "true");
  frame.tabIndex = -1;
  Object.assign(frame.style, {
    position: "fixed", insetInlineEnd: "0", insetBlockEnd: "0", width: "0", height: "0", border: "0", visibility: "hidden",
  });
  // An image that never arrives doesn't hold printing past the wait.
  const loaded = new Promise<void>((resolve) => { frame.addEventListener("load", () => resolve(), { once: true }); setTimeout(resolve, PRINT_LOAD_WAIT_MS); });
  frame.srcdoc = printableExportHtml(html, options.paper, options.scale);
  owner.body.append(frame);
  let removed = false;
  const remove = () => {
    if (removed) return;
    removed = true;
    frame.remove();
  };
  try {
    await loaded;
    options.signal?.throwIfAborted();
    const view = frame.contentWindow;
    const printed = frame.contentDocument;
    if (!view || !printed) throw new Error("The printable export could not be opened.");
    await drawPrintedEquations(printed);
    options.signal?.throwIfAborted();
    await Promise.all(Array.from(printed.images, (image) => image.decode().catch(() => undefined)));
    options.signal?.throwIfAborted();
    view.addEventListener("afterprint", () => window.setTimeout(remove, 0), { once: true });
    printFrameWithoutFocus(view);
    window.setTimeout(remove, 60_000);
  } catch (error) {
    remove();
    throw error;
  }
}

/** The largest PDF an app may hand Leafen to print. */
export const PRINT_PDF_MAX_BYTES = 96 * 1024 * 1024;
let printingPdf: (() => void) | null = null;

/**
 * Prints a PDF an app made — an office document, say — in a frame of
 * Leafen's page, since the app's own sandboxed frame can't open a print
 * dialog. The frame lives until `until` aborts — the app's frame that asked
 * closed — or the next print takes it away: Chrome tells the page nothing
 * when a PDF's print dialog closes (print() returns at once, no afterprint —
 * 2026-11-18), and while the dialog is open the page under it takes no
 * presses, so the app isn't closed before the print ends.
 */
export async function printPdfDocument(bytes: ArrayBuffer, until: AbortSignal, owner: Document = document) {
  if (bytes.byteLength > PRINT_PDF_MAX_BYTES || new TextDecoder().decode(bytes.slice(0, 5)) !== "%PDF-") {
    throw new RangeError("Not a printable PDF.");
  }
  until.throwIfAborted();
  printingPdf?.();
  const url = URL.createObjectURL(new Blob([bytes], { type: "application/pdf" }));
  const frame = owner.createElement("iframe");
  frame.setAttribute("aria-hidden", "true");
  frame.setAttribute("data-print-pdf", "");
  frame.tabIndex = -1;
  // The browser's PDF viewer draws only in a frame on the page, so it sits
  // off screen, past the side the page never scrolls to.
  Object.assign(frame.style, {
    position: "fixed", insetInlineStart: "-10000px", insetBlockStart: "0", width: "800px", height: "600px", border: "0",
  });
  const remove = () => {
    until.removeEventListener("abort", remove);
    frame.remove();
    URL.revokeObjectURL(url);
    if (printingPdf === remove) printingPdf = null;
  };
  printingPdf = remove;
  until.addEventListener("abort", remove, { once: true });
  // An image that never arrives doesn't hold printing past the wait.
  const loaded = new Promise<void>((resolve) => { frame.addEventListener("load", () => resolve(), { once: true }); setTimeout(resolve, PRINT_LOAD_WAIT_MS); });
  frame.src = url;
  owner.body.append(frame);
  await loaded;
  // The viewer needs a moment after its frame loads before it can print.
  await new Promise((resolve) => setTimeout(resolve, 400));
  // Closed or followed by another print meanwhile: nothing is left to print.
  if (!frame.isConnected || !frame.contentWindow) return;
  printFrameWithoutFocus(frame.contentWindow);
}

/**
 * Prints a hidden frame's own document while the focus stays where the
 * person left it — the app's editor, the export window — so the key after the
 * print dialog closes, printed or cancelled, goes there, as after a print of
 * the page itself. print() on the frame's window prints that frame whichever
 * has the focus; a frame focused to print kept the focus once the dialog
 * closed, and a PDF's frame says nothing then, so nothing could hand it back
 * (Chrome for Testing 147, 2026-11-18: a PDF frame and a page frame focused
 * first — the next key went nowhere; unfocused — the dialog showed the frame's
 * one sheet, not the twelve of the page under it, and the key reached the
 * text box that had it before). Judged after the dialog closed, cancelled
 * and printed to a file, by typing one letter and seeing where it landed,
 * with no script run in the page between the press and the letter.
 */
function printFrameWithoutFocus(view: Window) {
  view.print();
}
