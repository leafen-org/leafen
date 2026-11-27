import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";
import type { PDFDocumentLoadingTask } from "pdfjs-dist";
import styles from "./editorStyles.generated.module.css";
import { touchScreen } from "@/lib/screenKind";

type PdfJs = typeof import("pdfjs-dist");
let pdfJs: Promise<PdfJs> | null = null;

// pdf.js and its worker load with the first PDF shown.
function loadPdfJs() {
  pdfJs ??= Promise.all([import("pdfjs-dist"), import("pdfjs-dist/build/pdf.worker.min.mjs?url")]).then(([module, worker]) => {
    module.GlobalWorkerOptions.workerSrc = worker.default;
    return module;
  }).catch((error: unknown) => {
    pdfJs = null;
    throw error;
  });
  return pdfJs;
}

// Room Notion leaves beside a page's picture in its frame: 16px each side and a scrollbar's width,
// which a touch screen's scrollbar, lying over the pages, takes none of (Notion's iPad, 2026-11-13: 294 in 326).
const PAGE_INSET = 32 + 15, TOUCH_PAGE_INSET = 32;

// How long a frame's width rests before its pages are drawn again at it.
const REDRAW_REST_MS = 150;

/**
 * The width of the frame a PDF's pages stand in, in whole pixels down, as
 * Notion's (seen 2026-11-27 at 1440 dpr 1 in columns, two tabs: frames 129.5,
 * 193.33, 257.75, 364.33 and 577.5 drew pages 82, 146, 210, 317 and 530 —
 * the frame floored less the room beside the pages): measured before they are
 * first drawn and again once it rests after it changes — the page turned
 * full width, the window or the column narrower, the block dragged wider.
 */
function useFrameWidth(boxRef: RefObject<HTMLDivElement | null>) {
  const [width, setWidth] = useState(0);
  useLayoutEffect(() => {
    const frame = boxRef.current?.parentElement;
    if (!frame) return undefined;
    // Its laid out width, not clientWidth, which rounds a 577.5 frame up to 578, nor a transformed box's.
    const read = () => Math.floor(Number.parseFloat(getComputedStyle(frame).width) || 0);
    setWidth(read());
    let waiting = 0;
    // A drag's width is drawn once it is let go: the pages are scaled meanwhile (.pdfBlock[data-resizing]).
    const measure = () => {
      window.clearTimeout(waiting);
      waiting = window.setTimeout(() => frame.closest("[data-resizing]") ? measure() : setWidth(read()), REDRAW_REST_MS);
    };
    const observer = new ResizeObserver(measure);
    observer.observe(frame);
    return () => { observer.disconnect(); window.clearTimeout(waiting); };
  }, [boxRef]);
  return width;
}

/**
 * A PDF's pages as Notion's block shows them (seen 2026-10-31): pdf.js draws
 * each page white across the box less 16px each side and a scrollbar's
 * width, 8px apart, its words selectable, in a grey box that scrolls. They are
 * drawn again at the frame's width whenever it changes, as Notion's (seen
 * 2026-11-27 at 1440: a linked PDF's pages 657 → 1170 wide as its page turned
 * full width, without opening it again).
 */
export function PdfPages({ url, title, onFailed, boxRef, height }: {
  url: string; title: string; onFailed: () => void;
  boxRef: RefObject<HTMLDivElement | null>;
  /** The box's height its corners set; Notion's 320px without one (2026-11-01). */
  height?: number;
}) {
  const failed = useRef(onFailed);
  failed.current = onFailed;
  const frameWidth = useFrameWidth(boxRef);
  useEffect(() => {
    const box = boxRef.current;
    if (!box || !frameWidth) return;
    let current = true;
    let task: PDFDocumentLoadingTask | null = null;
    void loadPdfJs().then(async (pdfjs) => {
      if (!current) return;
      task = pdfjs.getDocument({ url });
      const loaded = await task.promise;
      if (!current) return;
      // Drawn at the frame's width: the box reaches 16px past it, where the frame hides its scrollbar.
      const width = Math.max(1, frameWidth - (touchScreen() ? TOUCH_PAGE_INSET : PAGE_INSET));
      const ratio = window.devicePixelRatio || 1;
      for (let number = 1; number <= loaded.numPages && current; number += 1) {
        const page = await loaded.getPage(number);
        if (!current) return;
        const scale = width / page.getViewport({ scale: 1 }).width;
        const viewport = page.getViewport({ scale });
        const holder = box.ownerDocument.createElement("div");
        holder.className = styles.pdfPage;
        const canvas = box.ownerDocument.createElement("canvas");
        canvas.width = Math.floor(viewport.width * ratio);
        canvas.height = Math.floor(viewport.height * ratio);
        canvas.style.width = `${viewport.width}px`;
        canvas.style.aspectRatio = `${viewport.width} / ${viewport.height}`;
        const text = box.ownerDocument.createElement("div");
        text.className = styles.pdfTextLayer;
        text.style.setProperty("--total-scale-factor", String(scale));
        text.style.width = `${viewport.width}px`;
        text.style.height = `${viewport.height}px`;
        holder.append(canvas, text);
        box.append(holder);
        await page.render({ canvas, viewport, transform: ratio === 1 ? undefined : [ratio, 0, 0, ratio, 0, 0] }).promise;
        if (!current) return;
        await new pdfjs.TextLayer({ textContentSource: page.streamTextContent(), container: text, viewport }).render();
      }
    }).catch(() => {
      if (current) failed.current();
    });
    return () => {
      current = false;
      void task?.destroy();
      box.replaceChildren();
    };
  }, [url, frameWidth, boxRef]);
  return <div ref={boxRef} className={styles.pdfPages} role="document" aria-label={title} style={height ? { height } : undefined} />;
}
