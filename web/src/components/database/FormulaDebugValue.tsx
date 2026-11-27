import { Fragment } from "react";
import { formulaCharacters } from "./formulaCharacters";
import { FormulaDate, isFormulaReference, type FormulaValue } from "./formulaCore";
import styles from "./FormulaCode.module.css";

// Notion's formula editor in debug mode shows a result as the formula made
// it (seen 2026-09-30): each value in a monospace face followed by a small
// grey note of its type — "num", "text(3)" with its length, "date" — a
// list's items between "[" and "]" separated by commas and followed by
// "list(4)", and true or false as a checkbox without a note; its sizes and
// spaces are in FormulaCode.module.css (seen 2026-11-12). A page's or
// person's note wasn't seen; it names its kind.

export function FormulaDebugValue({ value, text, checkboxLabel }: {
  value: FormulaValue;
  text: (value: FormulaValue) => string;
  checkboxLabel: string;
}) {
  if (value === null) return null;
  if (typeof value === "boolean") {
    // Shown in its colour, as Notion's, and left as it is when clicked.
    return (
      <input className={styles.debugCheck} type="checkbox" checked={value} readOnly tabIndex={-1} aria-label={checkboxLabel}
        onClick={(event) => event.preventDefault()} />
    );
  }
  if (Array.isArray(value)) {
    return (
      <span className={styles.debugValue} data-formula-debug="list">
        <span className={`${styles.debugPunctuation} ${styles.debugOpen}`}>[</span>
        {value.map((item, index) => (
          <Fragment key={index}>
            {index > 0 ? <span className={`${styles.debugPunctuation} ${styles.debugSeparator}`}>,</span> : null}
            <FormulaDebugValue value={item} text={text} checkboxLabel={checkboxLabel} />
          </Fragment>
        ))}
        <span className={`${styles.debugPunctuation} ${styles.debugClose}`}>]</span>
        <span className={styles.debugNote}>list({value.length})</span>
      </span>
    );
  }
  const written = text(value);
  const note = typeof value === "number" ? "num"
    : value instanceof FormulaDate ? "date"
      : isFormulaReference(value) ? value.kind
        : `text(${formulaCharacters(written).length})`;
  return (
    <span className={styles.debugValue}>
      <span>{written}</span>
      <span className={styles.debugNote}>{note}</span>
    </span>
  );
}
