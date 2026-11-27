import { type RefObject, forwardRef, useImperativeHandle, useLayoutEffect, useRef } from "react";
import { flushSync } from "react-dom";
import type { FormulaPiece } from "./formulaEditorModel";
import { createFormulaInputEngine } from "./formulaInputEngine";
import styles from "./FormulaCode.module.css";
import { FORMULA_CODE } from "./formulaTokenTheme";

export type FormulaInputHandle = {
  focus: (at?: number) => void;
  /** Writes `value` as typing, Undone with the name it completes when `completing`. */
  replace: (value: string, at: number, completing?: boolean) => void;
};
type FormulaInputProps = {
  value: string;
  piecesOf(text: string): readonly FormulaPiece[];
  label: string;
  placeholder: string;
  invalid?: boolean;
  /** Where the formula's problems are, as characters of `value`, each underlined; a warning's in its own colour. */
  problems?: readonly { start?: number; end?: number; warning?: boolean }[];
  caretRef?: RefObject<HTMLElement | null>;
  onChange: (value: string, cursor: number) => void;
  onCursor: (cursor: number) => void;
  onKeyDown?: (event: KeyboardEvent) => void;
};

export const FormulaInput = forwardRef<FormulaInputHandle, FormulaInputProps>(function FormulaInput(props, ref) {
  const root = useRef<HTMLDivElement>(null), latest = useRef(props); latest.current = props;
  const lease = useRef<{ active: boolean; core: ReturnType<typeof createFormulaInputEngine> } | null>(null);
  useLayoutEffect(() => {
    if (!root.current) return;
    let held = lease.current;
    if (!held) {
      const core = createFormulaInputEngine(root.current, props.value, {
        pieces: text => latest.current.piecesOf(text),
        attributes: () => ({ class: `${styles.editor} ${FORMULA_CODE}`, role: "textbox", tabindex: "0", "aria-multiline": "true",
          "aria-label": latest.current.label, "aria-invalid": String(Boolean(latest.current.invalid)),
          "data-placeholder": latest.current.placeholder, "data-formula-input": "", spellcheck: "false" }),
        changed: (text, at) => latest.current.onChange(text, at), cursor: at => latest.current.onCursor(at),
        // The dialog's suggestions follow the caret: a key on the heels of End reads them there.
        caretTaken: at => flushSync(() => latest.current.onCursor(at)),
        keyDown: event => latest.current.onKeyDown?.(event),
      });
      held = { active: true, core }; lease.current = held;
    }
    if (props.caretRef) props.caretRef.current = held.core.editor.view.dom;
    held.active = true;
    return () => { held.active = false; queueMicrotask(() => {
      if (!held.active) { held.core.destroy(); if (lease.current === held) lease.current = null; }
    }); };
    // Field identity is its mounted dialog, rather than each typed value.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  useLayoutEffect(() => { lease.current?.core.sync(props.value); },
    [props.value, props.piecesOf, props.label, props.placeholder, props.invalid]);
  useLayoutEffect(() => { lease.current?.core.problems(props.value, props.problems ?? []); }, [props.value, props.problems]);
  useImperativeHandle(ref, () => ({ focus: at => lease.current?.core.focus(at),
    replace: (value, at, completing) => lease.current?.core.replace(value, at, completing) }), []);
  return <div ref={root} />;
});
