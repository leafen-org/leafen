import { type RefObject, useLayoutEffect } from "react";
import { HANGUL_EXCESS_EM, OTHER_EXCESS, notionTitleLines } from "./notionTitleLines";

/**
 * A card's title that Leafen's Korean face wraps onto more lines than
 * Notion's face would keeps Notion's lines (font decision B, 2026-10-30: a
 * line Notion doesn't break is a defect), its letters closed up by what
 * Notion's face draws narrower: its Hangul at 15px 500 is 12.975px a
 * syllable, Leafen's Noto Sans KR 13.80, 0.055em less, and its other letters
 * 1.6% narrower than Inter's ("Project Alpha 2026" 134.72 to 136.87, seen
 * 2026-11-15 at 1440), spread over the title's letters. Notion's small
 * gallery card wraps "아주 긴 제목으로 … 씁니다" on four lines up to 166px,
 * three up to 238, two from 240; Leafen's took four up to 176 and three up to
 * 254, and so closed up takes Notion's at every width but 240. A title
 * without Hangul stays as it is.
 *
 * The lines Notion takes are worked out on the title's own words box — its
 * width beside the page icon, inside its padding (DatabaseCardTitleBox) —
 * and a title is closed up only while Leafen's face takes more lines than
 * those, and kept closed up only where that takes lines away without going
 * under them: spread evenly, the closing took a 75-card board's title Notion
 * breaks before its last "86" onto one line fewer (2026-11-15). Measured on
 * a hidden copy at the title's border width with its icon before the first
 * line only, three titles that kept their lines were closed up and three
 * that would lose one were not (2026-11-15).
 *
 * A title is measured once it is laid out, in the ResizeObserver's call that
 * comes after its frame's layout and before that frame is painted: when it is
 * first drawn, and again when its words or its width change. A card the
 * browser skips off the screen (content-visibility: auto) is so measured as
 * it comes near, before it is drawn; measuring every title before the first
 * layout laid out each skipped card on its own (a 96-card gallery's tab press
 * spent 95–127 ms in it at CPU ×2, 2026-11-15). The titles of one call are
 * all read first, those to close up then closed up together and read once
 * more, and their spacing written after; the spacing found for a text on a
 * line in a face is kept.
 */
const HANGUL = /\p{Script=Hangul}/gu;
/** How many found spacings are kept before they are all let go. */
const KEPT = 4000;
let pen: CanvasRenderingContext2D | null = null;
let penFont = "";
const penWidths = new Map<string, number>();

const widths = new WeakMap<Element, number>();
const kept = new Map<string, string>();
let observer: ResizeObserver | null = null;
/** Titles fitted while a face was still loading, fitted again once the faces are in. */
let waiting: Set<HTMLElement> | null = null;

type Fitting = { title: HTMLElement; spacing: string; key?: string; words?: HTMLElement; lineHeight?: number; natural?: number; notion?: number };

function sizes(): ResizeObserver {
  observer ??= new ResizeObserver((entries) => {
    const changed: [HTMLElement, number][] = [];
    for (const entry of entries) {
      // Its border box: a title is fitted again only as that changes.
      const width = entry.borderBoxSize?.[0]?.inlineSize ?? entry.target.getBoundingClientRect().width;
      if (!width || widths.get(entry.target) === width) continue;
      widths.set(entry.target, width);
      changed.push([entry.target as HTMLElement, width]);
    }
    // A title its spacing takes a line from changes height here, before the paint: the browser
    // tells of that size the next frame (an error event on the window, no console line), its width the same.
    if (changed.length) fit(changed);
  });
  return observer;
}

/** The box a title's words are laid out in, beside its icon. */
function wordsBoxOf(title: HTMLElement): HTMLElement | null {
  return title.querySelector<HTMLElement>(":scope > :is([data-search-text], [data-archived-row-title])");
}

/** A piece of text's width in the pen's face, kept per face. */
function measured(text: string): number {
  const key = `${penFont}\n${text}`;
  let width = penWidths.get(key);
  if (width === undefined && pen) {
    if (penWidths.size >= KEPT) penWidths.clear();
    width = pen.measureText(text).width;
    penWidths.set(key, width);
  }
  return width ?? 0;
}

function keep(key: string, spacing: string) {
  // A face still loading measures as another face would.
  if (document.fonts?.status !== "loaded") return;
  if (kept.size >= KEPT) kept.clear();
  kept.set(key, spacing);
}

/**
 * A title laid out in a face still loading takes another face's lines and
 * keeps its width when the face comes, so it is fitted again then, as it is
 * (a fresh page's gallery kept eight titles on two lines Notion draws on one,
 * 2026-11-15).
 */
function refitOnFaces(titles: readonly HTMLElement[]) {
  if (!document.fonts || document.fonts.status === "loaded" || !titles.length) return;
  let pending = waiting;
  if (!pending) {
    const these = new Set<HTMLElement>();
    pending = waiting = these;
    void document.fonts.ready.then(() => {
      if (waiting === these) waiting = null;
      const again: [HTMLElement, number][] = [];
      for (const title of these) {
        const width = widths.get(title);
        if (title.isConnected && width) again.push([title, width]);
      }
      if (again.length) fit(again);
    });
  }
  for (const title of titles) pending.add(title);
}

/** How many lines a words box of so many pixels' line height holds. */
function linesOf(words: HTMLElement, lineHeight: number): number {
  return Math.round(words.getBoundingClientRect().height / lineHeight);
}

function fit(changed: readonly [HTMLElement, number][]) {
  pen ??= document.createElement("canvas").getContext("2d");
  const fittings: Fitting[] = [];
  // Every read first, on the layout just made.
  for (const [title] of changed) {
    const words = wordsBoxOf(title);
    const text = words?.textContent ?? "";
    const hangul = text.match(HANGUL)?.length ?? 0;
    const letters = [...text].length;
    if (!words || !hangul || !letters || !pen) {
      fittings.push({ title, spacing: "" });
      continue;
    }
    const style = getComputedStyle(title), box = getComputedStyle(words);
    const fontSize = parseFloat(style.fontSize) || 15;
    const lineHeight = parseFloat(style.lineHeight) || fontSize * 1.5;
    // The words' line: their box beside the icon, inside its padding.
    const line = words.getBoundingClientRect().width - (parseFloat(box.paddingInlineStart) || 0) - (parseFloat(box.paddingInlineEnd) || 0);
    const font = `${style.fontStyle} ${style.fontWeight} ${style.fontSize} ${style.fontFamily}`;
    const key = [line, font, style.lineHeight, style.fontVariantLigatures, style.whiteSpace, style.overflowWrap, style.wordBreak, text].join("\n");
    const known = kept.get(key);
    if (known !== undefined) {
      fittings.push({ title, spacing: known });
      continue;
    }
    if (penFont !== font) {
      pen.font = font;
      penFont = font;
    }
    const notion = notionTitleLines(text, line, fontSize, measured);
    const others = measured(text.replace(HANGUL, ""));
    const close = (HANGUL_EXCESS_EM * fontSize * hangul + OTHER_EXCESS * others) / letters;
    fittings.push({ title, spacing: `${-close}px`, key, words, lineHeight, notion });
  }
  const measuring = fittings.filter((fitting) => fitting.key !== undefined);
  if (measuring.length) {
    // Its lines with no spacing: as laid out, unless it was closed up before (its width changed), then laid out again once for all.
    for (const { title } of measuring) if (title.style.letterSpacing) title.style.letterSpacing = "";
    for (const fitting of measuring) fitting.natural = linesOf(fitting.words as HTMLElement, fitting.lineHeight as number);
    // Those Leafen's face takes more lines for are closed up together, then laid out once.
    const closing = measuring.filter((fitting) => (fitting.natural as number) > (fitting.notion as number));
    for (const fitting of measuring) if (!closing.includes(fitting)) fitting.spacing = "";
    for (const { title, spacing } of closing) title.style.letterSpacing = spacing;
    for (const fitting of closing) {
      const lines = linesOf(fitting.words as HTMLElement, fitting.lineHeight as number);
      if (lines >= (fitting.natural as number) || lines < (fitting.notion as number)) fitting.spacing = "";
    }
    for (const fitting of measuring) keep(fitting.key as string, fitting.spacing);
  }
  for (const { title, spacing } of fittings) title.style.letterSpacing = spacing;
  refitOnFaces(changed.map(([title]) => title));
}

/** Fits a card's title to Notion's lines while `on`, again as `words` or its width change. */
export function useCardTitleFit(ref: RefObject<HTMLElement | null>, on: boolean, words: string) {
  useLayoutEffect(() => {
    const title = ref.current;
    if (!title) return undefined;
    if (!on) {
      title.style.letterSpacing = "";
      return undefined;
    }
    // Observed anew, it is told of its size after its next layout, before that frame is painted.
    const watched = sizes();
    watched.observe(title);
    return () => {
      watched.unobserve(title);
      widths.delete(title);
    };
  }, [ref, on, words]);
}
