import { useEffect, useRef, useState, useLayoutEffect } from "react";
import { useCellText } from "./cellText";
import { afterNextPaint } from "@/lib/afterPress";
import type { DbProperty } from "@/lib/types";
import { formatNumberForProperty, numberInputText, parseNumberInput } from "./numberFormat";
import { NumberValueDisplay } from "./NumberValueDisplay";
import styles from "./databaseSheet";
import { readOnlyStop, useCellReadOnly } from "./cellReadOnly";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

/**
 * A number's cell, its editor writing the value as it is typed, as Notion's
 * (seen 2026-11-14: in a view sorted by the number each digit moved the row
 * at once, the editor still open; Escape then chose the moved row's cell).
 * The typed character shows first; the write, which sorts and counts, goes
 * after the next frame, a newer key's taking its place. What doesn't read as
 * a number ("-", "1e") writes nothing until it does.
 */
export function PropertyNumberCell({
  prop,
  value,
  autoFocus,
  onCommit,
}: {
  prop: DbProperty;
  value: unknown;
  autoFocus?: boolean;
  onCommit: (v: number | null) => void;
}) {
  const t = useCellText();
  const readOnly = useCellReadOnly();
  const property = { config: (prop.config ?? {}) as Record<string, unknown> };
  const [text, setText] = useState(() => numberInputText(value, property));
  const [editing, setEditing] = useState(!!autoFocus);
  const inputRef = useRef<HTMLInputElement>(null);
  // The value this editor last wrote or opened with: a key that reads the same writes nothing.
  const written = useRef<number | null | undefined>(parseNumberInput(text, property));
  const pending = useRef<(() => void) | null>(null);
  const composing = useRef(false);
  const display = formatNumberForProperty(value, property);

  // The editor opens over its cell where it is, the table not scrolled to
  // it, as Notion's (seen 2026-11-07: a cell cut by the window's edge), the
  // caret in it (measured once 2026-11-15 in Chrome 1440 with no script
  // before the press: a digit typed then went in; the cut cell not measured
  // again).
  useLayoutEffect(() => {
    if (!editing) return;
    inputRef.current?.focus({ preventScroll: true });
    requestAnimationFrame(() => inputRef.current?.focus({ preventScroll: true }));
  }, [editing]);
  useEffect(() => () => pending.current?.(), []);

  function write(next: string) {
    pending.current?.();
    pending.current = null;
    const number = parseNumberInput(next, property);
    if (number === undefined || number === written.current) return;
    written.current = number;
    onCommit(number);
  }

  function typed(next: string) {
    setText(next);
    if (composing.current) return;
    pending.current?.();
    pending.current = afterNextPaint(() => {
      pending.current = null;
      write(next);
    });
  }

  function close() {
    composing.current = false;
    write(inputRef.current?.value ?? text);
    setEditing(false);
  }

  if (!editing || readOnly) {
    return (
      <button
        type="button"
        className={styles.numberDisplay}
        data-number=""
        aria-label={t("propertyCell:edit", { name: prop.name })}
        {...readOnlyStop(readOnly)}
        onClick={() => {
          if (readOnly) return;
          const opened = numberInputText(value, property);
          written.current = parseNumberInput(opened, property);
          setText(opened);
          setEditing(true);
        }}
      >
        {display ? <NumberValueDisplay value={value} property={property} /> : <span className={styles.cellEmpty}>&nbsp;</span>}
      </button>
    );
  }

  return (
    <input
      ref={inputRef}
      className={styles.cellInput}
      data-number=""
      type="text"
      inputMode="decimal"
      aria-label={t("propertyCell:edit", { name: prop.name })}
      value={text}
      onChange={(e) => typed(e.target.value)}
      onCompositionStart={() => { composing.current = true; }}
      onCompositionEnd={(e) => {
        composing.current = false;
        typed(e.currentTarget.value);
      }}
      onBlur={close}
      onKeyDown={(e) => {
        if (imeComposing(e)) return;
        // Escape keeps what was typed, as Enter does: Notion's number editor
        // keeps it when closed (seen 2026-10-19).
        if (fieldEnter(e) || e.key === "Escape") {
          e.preventDefault();
          (e.target as HTMLInputElement).blur();
        }
      }}
    />
  );
}
