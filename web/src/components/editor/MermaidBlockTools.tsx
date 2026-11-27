import { type RefObject, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { ImportIcon, ZoomInIcon } from "@/icons/leafen";
import { triggerBrowserFileDownload } from "@/lib/browserFileDownload";
import { HtmlDiv } from "@/lib/innerHtml";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { phoneScreen } from "@/lib/screenKind";
import { blockItemText } from "./blockItemLabels";
import toolStyles from "./MermaidBlockTools.module.css";

/** What a Mermaid block shows: its code, its diagram, or both as it starts (Notion's, seen 2026-11-05). */
export type MermaidFormat = "code" | "preview" | "split";

export function mermaidFormat(value: unknown): MermaidFormat {
  return value === "code" || value === "preview" ? value : "split";
}

const FORMAT_WORD: Record<MermaidFormat, string> = { code: "code.formatCode", preview: "code.formatPreview", split: "code.formatSplit" };
// The tools' short tips above them, as Notion's (seen 2026-11-06: "표시", "코드", "미리보기", "분할 보기",
// "다이어그램 펼치기", "이미지 다운로드", "복사하기", "코드 줄바꿈", "더 보기"; the language none).
const FORMAT_TIP: Record<MermaidFormat, string> = { code: "code.tips.formatCode", preview: "code.tips.formatPreview", split: "code.tips.formatSplit" };

function FormatIcon({ format }: { format: MermaidFormat }) {
  return <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.25" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    {format === "code" ? <path d="M5.5 4.5 2 8l3.5 3.5M10.5 4.5 14 8l-3.5 3.5M9 3.5 7 12.5" />
      : format === "preview" ? <><rect x="2.5" y="2.75" width="11" height="4.5" rx="1" /><path d="M4.5 10h7M6 13h4" /></>
      : <><rect x="2.5" y="2.75" width="11" height="10.5" rx="1.25" /><path d="M2.5 8h11" /></>}
  </svg>;
}

/** The tool that changes what the block shows, three choices under it. */
export function MermaidFormatTool({ format, buttonClassName, disabled, onChange }: {
  format: MermaidFormat; buttonClassName: string; disabled: boolean; onChange(next: MermaidFormat): void;
}) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLSpanElement>(null);
  useOverlayLayer(open, { kind: "picker", ref: root, restoreFocus: false, onEscape: () => setOpen(false), onOutsidePress: () => setOpen(false) });
  return <span ref={root} className={toolStyles.formatWrap}>
    <button type="button" className={buttonClassName} aria-label={blockItemText("code.previewFormat")} aria-expanded={open}
      data-icon-tip={blockItemText("code.tips.format")} data-tip-side="top" data-mermaid-format-tool disabled={disabled} onClick={() => setOpen(current => !current)}>
      <FormatIcon format={format} />
    </button>
    {open ? <span className={toolStyles.formatMenu} role="group" aria-label={blockItemText("code.previewFormat")}>
      {(["code", "preview", "split"] as const).map(choice => <button key={choice} type="button" aria-label={blockItemText(FORMAT_WORD[choice])}
        aria-pressed={format === choice} data-icon-tip={blockItemText(FORMAT_TIP[choice])} data-tip-side="top" data-mermaid-format={choice}
        onClick={() => { onChange(choice); setOpen(false); }}>
        <FormatIcon format={choice} />
      </button>)}
    </span> : null}
  </span>;
}

export function MermaidExpandTool({ buttonClassName, onExpand }: { buttonClassName: string; onExpand(): void }) {
  return <button type="button" className={buttonClassName} aria-label={blockItemText("code.expandDiagram")}
    data-icon-tip={blockItemText("code.expandDiagram")} data-tip-side="top" data-mermaid-expand onClick={onExpand}><ZoomInIcon size="small" aria-hidden="true" /></button>;
}

/** Draws the diagram on a white or dark card, the block's own color, and saves it as a picture. */
async function saveDiagramJpeg(diagram: SVGSVGElement, background: string) {
  const box = diagram.getBoundingClientRect();
  const scale = Math.min(3, 4096 / Math.max(box.width, box.height, 1));
  const copy = diagram.cloneNode(true) as SVGSVGElement;
  copy.setAttribute("xmlns", "http://www.w3.org/2000/svg");
  copy.setAttribute("width", String(box.width));
  copy.setAttribute("height", String(box.height));
  copy.style.maxWidth = "none";
  const picture = new Image();
  const pad = 24;
  await new Promise<void>((resolve, reject) => {
    picture.onload = () => resolve();
    picture.onerror = () => reject(new Error("The diagram could not be drawn."));
    // As data, not a blob: a blob's foreignObject labels would taint the canvas in Chromium.
    picture.src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(new XMLSerializer().serializeToString(copy))}`;
  });
  const canvas = document.createElement("canvas");
  canvas.width = Math.ceil((box.width + pad * 2) * scale);
  canvas.height = Math.ceil((box.height + pad * 2) * scale);
  const context = canvas.getContext("2d");
  if (!context) throw new Error("The diagram could not be drawn.");
  context.fillStyle = background;
  context.fillRect(0, 0, canvas.width, canvas.height);
  context.drawImage(picture, pad * scale, pad * scale, box.width * scale, box.height * scale);
  const blob = await new Promise<Blob | null>(resolve => canvas.toBlob(resolve, "image/jpeg", 0.95));
  if (!blob) throw new Error("The diagram could not be drawn.");
  triggerBrowserFileDownload(blob, "diagram.jpeg", { revokeAfterMs: 60_000 });
}

export function MermaidDownloadTool({ buttonClassName, previewRef }: { buttonClassName: string; previewRef: RefObject<HTMLElement | null> }) {
  return <button type="button" className={buttonClassName} aria-label={blockItemText("code.downloadDiagram")}
    data-icon-tip={blockItemText("code.tips.download")} data-tip-side="top" data-mermaid-download onClick={() => {
      const diagram = previewRef.current?.querySelector<SVGSVGElement>("svg");
      if (!diagram) return;
      const shown = previewRef.current ? getComputedStyle(previewRef.current.closest("[data-mermaid-format-shown]") ?? previewRef.current).backgroundColor : "";
      void saveDiagramJpeg(diagram, shown && shown !== "rgba(0, 0, 0, 0)" ? shown : "#ffffff").catch(() => {});
    }}><ImportIcon size="small" aria-hidden="true" /></button>;
}

// The room around a diagram in Notion's phone view of it: its picture is
// the diagram with 32px about it, shown as large as the screen lets it with
// no edge (seen 2026-11-17 in iPhone Safari at 402, dark: a 196 × 258
// diagram drawn 260 × 322 and shown 402 × 497.9 in the screen's middle).
const PHONE_DIAGRAM_PAD = 32;

function phoneDiagramBox(diagram: SVGSVGElement | null | undefined) {
  const drawn = diagram?.getBoundingClientRect(), own = diagram?.viewBox?.baseVal;
  const width = (own?.width || drawn?.width || 1) + 2 * PHONE_DIAGRAM_PAD, height = (own?.height || drawn?.height || 1) + 2 * PHONE_DIAGRAM_PAD;
  const scale = Math.min(window.innerWidth / width, window.innerHeight / height);
  return { width: Math.round(width * scale), height: Math.round(height * scale), padding: PHONE_DIAGRAM_PAD * scale };
}

/**
 * The diagram large over the dimmed page, as Notion's (seen 2026-11-05);
 * Escape or a press outside closes it. A phone's is its picture alone the
 * screen's width (phoneDiagramBox) over Notion's darker shade, no window
 * (phoneWindowSheet.ts names it a viewer).
 */
// Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440: its 🔍 pressed, "z" typed went nowhere,
// Notion's focus on <body>, Leafen's on the panel; Escape closes both (next key the same, seen once).
export function MermaidLightbox({ previewRef, onClose }: { previewRef: RefObject<HTMLElement | null>; onClose(): void }) {
  const panel = useRef<HTMLDivElement>(null);
  const [html] = useState(() => previewRef.current?.querySelector("svg")?.outerHTML ?? "");
  const [phone] = useState(() => phoneScreen());
  const [box, setBox] = useState(() => phone ? phoneDiagramBox(previewRef.current?.querySelector("svg")) : null);
  const { zIndex } = useOverlayLayer(true, { kind: "dialog", ref: panel, modalOwned: () => [panel.current], restoreFocus: true, onEscape: onClose, onOutsidePress: onClose });
  useEffect(() => { panel.current?.focus({ preventScroll: true }); }, []);
  useEffect(() => {
    if (!phone) return undefined;
    const follow = () => setBox(phoneDiagramBox(panel.current?.querySelector("svg")));
    window.addEventListener("resize", follow);
    return () => window.removeEventListener("resize", follow);
  }, [phone]);
  return createPortal(<div className={toolStyles.lightbox} style={{ zIndex }} data-phone-view={phone ? "true" : undefined}>
    <div ref={panel} className={toolStyles.lightboxPanel} role="dialog" aria-modal="true" aria-label={blockItemText("code.expandDiagram")} tabIndex={-1} data-mermaid-lightbox
      style={box ?? undefined}>
      <HtmlDiv className={toolStyles.lightboxDiagram} html={html} />
    </div>
  </div>, document.body);
}
