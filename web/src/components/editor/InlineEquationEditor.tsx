import { type ChangeEvent, createElement, type KeyboardEvent, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { EnterSign } from "@/components/EnterSign";
import { popoverAnchorHeld } from "@/lib/popoverAnchor";
import { placeAround, popoverBackdropStyle, popoverPlacementStyle, type PopoverRect, usePopoverPlacement } from "@/lib/popoverPlacement";
import { phoneScreen } from "@/lib/screenKind";
import { followVisualViewport } from "@/lib/visualViewportPlace";
import { blockItemText } from "./blockItemLabels";
import { EquationPreview } from "./EquationPreview";
import { inlineEquationError } from "./richtextEquation";
import styles from "./InlineEquationEditor.module.css";
import phoneStyles from "./InlineEquationPhone.module.css";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

// Notion's inline equation field (seen 2026-09-30): a 400px panel just under
// the equation, its TeX selected in a monospace field ("E = mc^2" when
// empty) and a blue "Done ↵". Enter or Done keeps what is typed; Escape or
// a click elsewhere brings back what was there (seen 2026-11-13, in a line,
// a table's cell and over chosen words alike). TeX KaTeX can't read shows
// KaTeX's error under the field and can't be done, as Notion's (seen
// 2026-10-04).
// A math block's is the same panel under the block with a three-line field,
// a sample of a longer formula when empty, where Shift+Enter starts a line
// (seen 2026-10-16). Escape or a click elsewhere closes it keeping what is
// typed, TeX KaTeX can't read too, which the block then shows as its error
// (seen 2026-11-13); Enter and Done still wait for TeX it can read.
// On a phone a piece of a line's field is docked on the keyboard, as the
// Notion Android app's (seen 2026-11-25 on a Galaxy A90, in a page's line and
// a table text cell's box alike): the page shaded, the screen's width on the
// soft ground, its TeX field 10 in at the top with "완료 ↵" at its end, and
// over it, once TeX is typed, the formula drawn in the middle of a white band
// (InlineEquationPhone.module.css).
// In a right-to-left screen the panel reads from the right — "Done" at its
// right, its ↵ turned over — while the TeX reads from its own first letter,
// "x^2" from the left beside the button, as Notion's in Arabic and Hebrew
// (seen 2026-11-19, inline and block alike).

const KATEX_SUPPORTED = "https://katex.org/docs/supported.html";
// A math block's sample, Notion's own (measured 2026-11-19 in Chrome 1440):
// one paragraph whose no-break spaces line up a cases formula, so that a
// field 37 letters wide shows it in three rows — "…cases} x,", "&\quad x \geq 0
// \\ -x,", "&\quad x < 0 \end{cases}" — where four rows had overflowed.
const NBSP = (count: number) => "\u00a0".repeat(count);
const BLOCK_SAMPLE = `|x| = \\begin{cases}${NBSP(13)} ${NBSP(2)}x, &\\quad x \\geq 0 \\\\${NBSP(11)} ${NBSP(1)}-x, &\\quad x < 0${NBSP(13)} \\end{cases}`;

export function InlineEquationEditor({ anchor, initial, block = false, onPreview, onDone, onCancel }: {
  anchor: PopoverRect;
  initial: string;
  block?: boolean;
  onPreview(tex: string): void;
  onDone(tex: string): void;
  onCancel(): void;
}) {
  const [tex, setTex] = useState(initial);
  const fieldRef = useRef<HTMLInputElement & HTMLTextAreaElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const [phone] = useState(() => !block && phoneScreen());
  // Docked, it stands at the foot of what shows over the keyboard.
  useLayoutEffect(() => (phone ? followVisualViewport(panelRef.current) : undefined), [phone]);
  // Under the equation, or over it where only that side has room; a math
  // block's right under its formula, as Notion's (seen 2026-11-01). A piece
  // of a line's stays where it opened, 8px under it, while its TeX is typed,
  // emptied or typed again, and moves only with the page's scrolling
  // (popoverAnchorHeld; Notion's, measured 2026-11-19 in Chrome 1440).
  const held = useMemo(() => (block ? anchor : popoverAnchorHeld(anchor)), [anchor, block]);
  const place = usePopoverPlacement(panelRef, phone ? null : (size, viewport) => placeAround(held, size, viewport, { gap: block ? 0 : 8 }), [held, block], undefined, { kind: "inner" });
  // Its field takes the keys with the formula chosen whole. Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440:
  // an inline equation pressed, "z" typed replaced its "x^2" (Leafen the same).
  useEffect(() => {
    fieldRef.current?.focus();
    fieldRef.current?.select();
  }, []);
  // A browser that sizes no field by what it holds (field-sizing: WebKit) is
  // given a math block's height: three rows empty, its TeX's height typed.
  useLayoutEffect(() => {
    const field = fieldRef.current;
    if (!block || !field || CSS.supports("field-sizing", "content")) return;
    field.style.height = "0px";
    field.style.height = tex ? `${field.scrollHeight}px` : "";
  }, [block, tex]);
  if (typeof document === "undefined") return null;
  const width = 400;
  const error = inlineEquationError(tex, block);
  const done = () => { if (error === null) onDone(tex); };
  // Closed without Done: a block keeps what is typed, a piece of a line what was there.
  const close = () => (block ? onDone(tex) : onCancel());
  const field = createElement(block ? "textarea" : "input", {
    ref: fieldRef, className: block ? `${styles.field} ${styles.blockField}` : styles.field, value: tex,
    placeholder: block ? BLOCK_SAMPLE : "E = mc^2", spellCheck: false, rows: block ? 3 : undefined,
    "aria-label": blockItemText(block ? "equation.blockEditor" : "equation.editor"), "aria-invalid": error !== null,
    onChange: (event: ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => {
      setTex(event.target.value);
      onPreview(event.target.value);
    },
    onKeyDown: (event: KeyboardEvent<HTMLInputElement | HTMLTextAreaElement>) => {
      if (imeComposing(event)) return;
      if (fieldEnter(event) && !(block && event.shiftKey)) {
        event.preventDefault();
        done();
      } else if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        close();
      }
    },
  });
  const doneButton = <span className={styles.doneLine}>
    <button type="button" className={styles.done} disabled={error !== null} onClick={done}>
      {blockItemText("equation.done")} <EnterSign />
    </button>
  </span>;
  const errorLine = error !== null ? (
    <div className={styles.error} role="alert" data-inline-equation-error>
      <span>{error}</span>
      <a href={KATEX_SUPPORTED} target="_blank" rel="noreferrer noopener">{blockItemText("equation.help")}</a>
    </div>
  ) : null;
  if (phone) return createPortal(
    <>
      <button type="button" className={phoneStyles.shade} tabIndex={-1} aria-label={blockItemText("equation.close")} onClick={close} />
      <div ref={panelRef} className={phoneStyles.panel} role="dialog" aria-label={blockItemText("equation.editor")} data-inline-equation-editor="phone">
        {tex.trim() && error === null ? <EquationPreview className={phoneStyles.preview} expression={tex} /> : null}
        <div className={`${styles.panel} ${phoneStyles.fieldBand}`}>{field}{doneButton}{errorLine}</div>
      </div>
    </>,
    document.body,
  );
  return createPortal(
    <>
      <button type="button" className={styles.backdrop} style={popoverBackdropStyle(place)} tabIndex={-1}
        aria-label={blockItemText(block ? "equation.done" : "equation.close")} onClick={close} />
      <div ref={panelRef} className={block ? `${styles.panel} ${styles.blockPanel}` : styles.panel} role="dialog"
        aria-label={blockItemText(block ? "equation.blockEditor" : "equation.editor")} data-inline-equation-editor
        style={{ width, ...popoverPlacementStyle(place) }}>
        {field}
        {doneButton}
        {errorLine}
      </div>
    </>,
    document.body,
  );
}
