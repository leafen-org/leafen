import { type PointerEvent as ReactPointerEvent, type RefObject, useLayoutEffect, useRef } from "react";
import { blockItemText } from "./blockItemLabels";
import styles from "./editorStyles.generated.module.css";
import { readsRightToLeft } from "@/lib/readingDirection";

/**
 * A framed block's size: its width as a share of the column, drawn while it
 * is dragged; its block's width in px, kept when the drag ends (Notion's
 * block_width, keptBlockWidth), or `whole` where it stepped to the column's
 * own block, which keeps no width (keptWidthPatch); its box's height in pixels.
 */
export type BlockBoxSize = { width: number; height: number; block?: number; whole?: boolean };

/** The kinds of framed block a drag sizes, each with Notion's least size. */
export type ResizedBlockKind = "image" | "video" | "audio" | "pdf" | "embed";

// Notion's least block width and frame height for each kind, and its most
// height (its resizer's code, read 2026-11-01).
const LEAST_WIDTH: Record<ResizedBlockKind, number> = { image: 36, video: 96, audio: 96, pdf: 96, embed: 246 };
const LEAST_HEIGHT: Record<ResizedBlockKind, number> = { image: 36, video: 96, audio: 24, pdf: 96, embed: 96 };
const MOST_HEIGHT = 930;
// A Notion block stands 8px out from its frame either side; Leafen's column
// is 4px wider than its widest frame (708 to 704, Notion's 720 to 704).
const BLOCK_INSET = 16;
const COLUMN_INSET = 12;
// A page's sides, 96px either side of its column at full width, which an embed stays inside.
const PAGE_SIDES = 192;

/**
 * A drag's block width as Notion's resizer leaves it (its code, read
 * 2026-11-01; measured 2026-11-15 at 1440): the block — the frame and 8px
 * either side — never under its kind's least, in 48px steps (12px under
 * 144px), rounded to the nearest; within a step of the column's own block
 * (720 for its 704 frame) the column's; past that it goes on past the column
 * (a video 848, a PDF 944 wide), an embed too, up to the `widest` block
 * (frameDragShare), which it takes within a step of it or past it (measured
 * 2026-11-16 in Chrome at 1440: an embed pointed at 470px out of its middle,
 * a 956 block, stood 963). Given as a share of the column, to the 0.000001%
 * so the frame is the step's whole pixels, of the figure drawn `inset` px
 * wider than its frame (a picture's 2px each side, a PDF's sides).
 */
export function steppedFrameWidth(dragged: number, column: number, kind: ResizedBlockKind, inset = kind === "image" ? 4 : 0,
  widest = Infinity) {
  const block = Math.max(dragged, LEAST_WIDTH[kind]);
  const step = block < 144 ? 12 : 48;
  const full = column + COLUMN_INSET;
  const stepped = Math.abs(block - full) < step ? full
    : block > full && block > widest - step ? Math.max(full, widest) : Math.round(block / step) * step;
  return Math.round(((stepped - BLOCK_INSET + inset) / Math.max(1, column)) * 1e8) / 1e6;
}

/** An embed's kept share of its column, which a drag takes past the column too (frameDragShare). */
export function storedEmbedShare(value: unknown) {
  return typeof value === "number" ? Math.max(1, Math.min(1000, value)) : undefined;
}

// How much narrower than the page inside its scroll bar (--page-room) a framed block's figure is
// drawn at most: its block grows to that page, an embed's to that less the page's sides
// (frameDragShare's widest), and the figure is its block less 16, a picture's and a PDF's 4 wider
// than its frame (steppedFrameWidth's inset).
const FIGURE_UNDER_PAGE: Record<ResizedBlockKind, number> = { image: 12, video: 16, audio: 16, pdf: 12, embed: PAGE_SIDES + 16 };

/**
 * A framed block's figure at its kept share: past the column it stands out
 * evenly either side, as Notion's, never wider than its kind's widest drag
 * leaves it on the page as it now is — a share kept on a wider page draws
 * narrower on a narrower one (Notion's "max-width", read 2026-11-16 in Chrome:
 * an embed and a picture dragged to their widest at 1440, 963 and 1155, drawn
 * 803 and 995 at 1280, the page 995 inside its scroll bar). Where no page is
 * known (--page-room: inside another block, a touch screen's), its column:
 * a picture kept wider than the column it stands in draws as wide as that
 * column, as Notion's (seen 2026-11-27 at 768: a 288px block in a 258 column, 257.5).
 */
export function frameShareStyle(width: number | undefined, kind: ResizedBlockKind = "image") {
  if (!width) return undefined;
  if (width <= 100) return { width: `${width}%` };
  const under = FIGURE_UNDER_PAGE[kind];
  const drawn = `min(${width}%, calc(var(--page-room, calc(100% + ${under}px)) - ${under}px))`;
  return { width: drawn, marginInline: `calc((100% - ${drawn}) / 2)` };
}

/**
 * Whether a framed block stands inside another block — a column, a toggle, a
 * list, a callout — rather than straight under the page: its row's depth.
 */
export function insideAnotherBlock(element: Element) {
  const depth = element.closest("[data-block-id]")?.getAttribute("data-depth");
  return depth !== null && depth !== undefined && depth !== "0";
}

/**
 * The share a drag of a framed block's side handle gives as the pointer goes
 * from `press` to `x`, as Notion's resizer: the block becomes its frame as the
 * drag starts and the pointer's way out (less in), twice it straight under the
 * page, both sides moving, and once inside another block, its other side held
 * — wherever on the handle the press was — then stepped (steppedFrameWidth).
 * Measured 2026-11-27 in Chrome at 1440 dpr 1 with real drags: under the page
 * a picture pressed 13px in from its frame's edge, 600 → 464 by 70px in and
 * 464 → 560 by 46px out, 560 → 608 by 22px out; in a 337 column a picture
 * 321 → 272 by 50px in and 272 → 224 by 50px more, set right or centred the
 * same, a video and a PDF across the column 321 → 224 by 60px, then
 * 224 → 176 by 40px, 176 → 272 by 90px out pressed 13px in; in a toggle a
 * video and a PDF 666 → 560 by 100px. Every kind sizes so at any alignment,
 * one set to a side keeping its place there as it narrows or grows (measured
 * 2026-11-16 in Chrome at 1440: a picture set left 600 → 416 by 80px and set
 * right 368 → 272 by 50px; an embed set left 240 → 816 by 300px out and
 * 720 → 384 by 150px in, set right 384 → 288 by 40px and 288 → 480 by 100px
 * out, centred 400 → 288 by 50px; a PDF set left 384 → 240 by 60px, centred
 * 720 → 384 by 150px). A block under the page grows at most to the page's
 * width inside its scroll bar, an embed to that less the page's 96px sides,
 * and never under the column (measured 2026-11-16 in Chrome by pointer drags
 * in the page, a centred embed and a picture, the picture set left too, which
 * centred once past the column: at 1440, a 1155 page, 963 and 1155; at 1280,
 * a 995 page, 803 and 995); one inside another block no wider than the column
 * it stands in (seen 2026-11-27: a video in a 337 column dragged 240px out
 * stood 321 across it). Every kind but an embed is taken to go as the
 * picture. The handle stands in its frame's side (its offset parent).
 */
export function frameDragShare(handle: HTMLElement, figure: HTMLElement, kind: ResizedBlockKind, outward: number, press: number) {
  // The box its share is of: a parent that draws no box of its own (display: contents) passes it up.
  let holder = figure.parentElement;
  while (holder && holder.getBoundingClientRect().width === 0) holder = holder.parentElement;
  const shown = figure.getBoundingClientRect();
  const column = Math.max(1, holder?.getBoundingClientRect().width ?? shown.width);
  const frame = (handle.offsetParent ?? figure).getBoundingClientRect();
  // A picture's figure is 4px wider than its frame once sized; another's sides are as drawn.
  const inset = kind === "image" ? 4 : Math.max(0, shown.width - frame.width);
  const nested = insideAnotherBlock(figure);
  const page = (figure.closest(".nscroll") ?? figure.closest("main"))?.clientWidth ?? window.innerWidth;
  // Inside another block, no wider than its column's own block: the widest is that block itself.
  const widest = nested ? column + COLUMN_INSET : page - (kind === "embed" ? PAGE_SIDES : 0);
  const grows = nested ? 1 : 2;
  return {
    /** The share it stands at as the drag starts: its frame's, a picture at its own size too. */
    start: Math.round(((frame.width + inset) / column) * 1e8) / 1e6,
    at: (x: number) => steppedFrameWidth(frame.width + grows * (x - press) * outward, column, kind, inset, widest),
    /** A share's block in px, the step it came from: the frame and 8px either side. */
    blockOf: (value: number) => Math.round((value * column) / 100 - inset + BLOCK_INSET),
    /** Whether a share is the column's own block, the step a drag snaps to within reach of it (steppedFrameWidth). */
    whole: (value: number) => Math.abs((value * column) / 100 - inset + BLOCK_INSET - (column + COLUMN_INSET)) < 0.5,
  };
}

/** A dragged frame's height, between its kind's least and Notion's most. */
function steppedFrameHeight(height: number, kind: ResizedBlockKind) {
  return Math.min(MOST_HEIGHT, Math.max(LEAST_HEIGHT[kind], Math.round(height)));
}

// Notion's corner mark: a rounded "┘", drawn mirrored for the left corner.
const CORNER = "M 14 0C 12.8955 0 12 0.895447 12 2L 12 12L 2 12C 0.895508 12 0 12.8954 0 14C 0 15.1046 0.895508 16 2 16L 14 16C 15.1045 16 16 15.1046 16 14L 16 2C 16 0.895447 15.1045 0 14 0Z";

type Grip = "left" | "right" | "cornerLeft" | "cornerRight" | "bottom";

/**
 * A framed block's size handle, pressed, ends the writing of the page's
 * text: its caption's or a line's caret goes and the text loses the focus,
 * so a key typed after the drag lands nowhere, as Notion's (seen twice on
 * 2026-11-15 in fresh Chrome tabs at 1440 with no script in the page before
 * the action, besides s224 on 2026-11-12: a caption written to, its picture's
 * handle dragged, a typed "z" went nowhere; read after it, the focus on the
 * page's body and no range).
 */
export function letGoOfTextForResize() {
  const held = document.activeElement;
  if (held instanceof HTMLElement && held !== document.body && (held.isContentEditable || held.matches("input, textarea"))) held.blur();
  window.getSelection()?.removeAllRanges();
}

/**
 * Notion's handles in a framed block's lower corners (seen 2026-10-31 on its
 * PDF block): dragging one sizes the block's width — both sides at once under
 * the page, its end side alone inside another block (frameDragShare) — and its
 * box's height by the drag, kept as it ends.
 * With "edges" grips, as its embed block's (seen 2026-10-31), a pill on each
 * side sizes the width alone and one at the foot the height alone; with
 * "sides", as its video block's (seen 2026-11-01), only the side pills.
 * Inside another block the side pills, and an embed's corners, stand at its
 * end side alone (data-grip-start hides the others there), as Notion's (seen
 * 2026-11-27 at 1440 in a column and a toggle: a picture's, a video's and an
 * embed's at their right however aligned; a PDF keeps both corners).
 */
export function BlockCornerHandles({ kind, figureRef, boxRef, grips = "corners", onSize, onEnd }: {
  kind: ResizedBlockKind;
  grips?: "corners" | "edges" | "sides";
  figureRef: RefObject<HTMLElement | null>;
  /** The box whose height the drag sets. */
  boxRef: RefObject<HTMLElement | null>;
  onSize(size: BlockBoxSize): void;
  onEnd(size: BlockBoxSize | null): void;
}) {
  function start(grip: Grip, event: ReactPointerEvent<HTMLButtonElement>) {
    const figure = figureRef.current, box = boxRef.current;
    if (!figure || !box || event.button !== 0) return;
    event.preventDefault();
    event.stopPropagation();
    letGoOfTextForResize();
    // Held by the handle, the drag keeps its moves over a frame's page too.
    event.currentTarget.setPointerCapture?.(event.pointerId);
    const startHeight = box.getBoundingClientRect().height, startY = event.clientY;
    let latest: BlockBoxSize | null = null;
    // "left" and "right" are its start and end grips: turned over where the page reads from the right.
    const outward = (grip === "right" || grip === "cornerRight") !== readsRightToLeft(figure) ? 1 : -1;
    const share = frameDragShare(event.currentTarget, figure, kind, outward, event.clientX);
    function sizeAt(x: number, y: number): BlockBoxSize {
      const width = grip === "bottom" ? share.start : share.at(x);
      return {
        width, block: share.blockOf(width), whole: share.whole(width),
        height: steppedFrameHeight(startHeight + (grip === "left" || grip === "right" ? 0 : y - startY), kind),
      };
    }
    function move(next: PointerEvent) {
      latest = sizeAt(next.clientX, next.clientY);
      onSize(latest);
    }
    // Resizing from the press, at the size it has, so a frame's page under
    // the pointer lets it go even when the first move leaves the handle; a
    // press let go unmoved changes nothing. A picture has no page in it, and
    // one at its own size keeps its place until it moves.
    if (kind !== "image") onSize({ width: share.start, height: startHeight, block: share.blockOf(share.start) });
    function end() {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", end);
      window.removeEventListener("pointercancel", end);
      onEnd(latest);
    }
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", end);
    window.addEventListener("pointercancel", end);
  }
  // No tip on a handle, as Notion's show none (seen 2026-11-06); the name stays for assistive tools,
  // which says the screen's side, not the block's kind — the same handles size a picture, a video,
  // a PDF, an embed and a diagram: the start grip is on the right read from the right.
  const sideName = (side: "left" | "right") => blockItemText((side === "left") !== readsRightToLeft() ? "common.resizeFromLeft" : "common.resizeFromRight");
  const startSide = readsRightToLeft() ? "right" : "left";
  return <>
    {grips !== "corners" ? <>
      {(["left", "right"] as const).map((side) => (
        <button key={side} type="button" className={`${styles.imageResizeHandle} ${side === "left" ? styles.imageResizeLeft : styles.imageResizeRight}`}
          tabIndex={-1} data-edge-handle={side} data-grip-start={side === startSide ? "" : undefined} aria-label={sideName(side)}
          onPointerDown={(event) => start(side, event)} />
      ))}
      {grips === "edges" ? <button type="button" className={styles.bottomHandle} tabIndex={-1} data-edge-handle="bottom"
        aria-label={blockItemText("common.resize")} onPointerDown={(event) => start("bottom", event)} /> : null}
    </> : null}
    {grips !== "sides" ? (["left", "right"] as const).map((side) => (
      <button key={side} type="button" className={styles.cornerHandle} data-side={side} tabIndex={-1}
        data-grip-start={grips === "edges" && side === startSide ? "" : undefined} aria-label={sideName(side)}
        onPointerDown={(event) => start(side === "left" ? "cornerLeft" : "cornerRight", event)}>
        <svg viewBox="0 0 16 16" aria-hidden="true"><path fillRule="evenodd" strokeWidth="1.5" d={CORNER} /></svg>
      </button>
    )) : null}
  </>;
}

/**
 * A narrower framed block's box hugs its frame, 8 around; its handle and its
 * chosen wash go by that box, as Notion's (seen 2026-11-05). A picture at
 * its own size as it came, never sized, keeps the whole row whatever its
 * alignment (Notion's, seen 2026-11-06 and 2026-11-12: a linked 240px or an
 * API's 400px picture washed across the column, set left or right too, the
 * same made narrower or uploaded washed round itself).
 */
export function PictureHandlePlace({ align, hug = true }: { align: unknown; hug?: boolean }) {
  const mark = useRef<HTMLSpanElement>(null);
  useLayoutEffect(() => {
    if (!hug) return undefined;
    let stop = () => {};
    let waiting = 0, tries = 0;
    // The editor puts a block's view in its place after rendering it, so a
    // page opened with the picture in it finds its block a frame or two
    // later (2026-11-12: a reopened page's narrow picture washed the row).
    const attach = () => {
      const frame = mark.current?.parentElement, row = frame?.closest<HTMLElement>("[data-block-id]");
      if (!frame || !row) {
        if (frame && tries++ < 20) waiting = requestAnimationFrame(attach);
        return;
      }
      const place = () => {
        const box = frame.getBoundingClientRect(), around = row.getBoundingClientRect();
        row.style.setProperty("--picture-left", `${Math.max(0, box.left - 8 - around.left)}px`);
        row.style.setProperty("--picture-right", `${Math.max(0, around.right - box.right - 8)}px`);
      };
      place();
      const observer = new ResizeObserver(place);
      observer.observe(frame); observer.observe(row);
      stop = () => { observer.disconnect(); row.style.removeProperty("--picture-left"); row.style.removeProperty("--picture-right"); };
    };
    attach();
    return () => { cancelAnimationFrame(waiting); stop(); };
  }, [align, hug]);
  return <span ref={mark} hidden />;
}
