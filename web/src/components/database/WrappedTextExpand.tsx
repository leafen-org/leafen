import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ExpandRowIcon } from "../icons";
import styles from "./databaseSheet";

/**
 * A wrapped value in a column past the reach a table opened with sets no
 * row's height, as Notion's (seen 2026-11-03, 2026-11-06, 2026-11-14 for
 * files and choices; useWrappedTextCut): it wraps in the height the row's
 * other values give it and the rest is cut. While cut its cell shows "확장"
 * at its end — pointed at or not (seen 2026-11-06) — which grows the row to
 * the whole value until the table is opened again; a text written shows
 * whole in its box. Its column reached (`cut` no longer), the row grows to
 * the value and "확장" stays as it was while cut, until pressed (Notion's,
 * seen 2026-11-24 in two tabs).
 */
export function WrappedTextExpand({ cut: columnCut, onExpand }: { cut: boolean; onExpand(): void }) {
  const { t } = useTranslation("tableView");
  const ref = useRef<HTMLButtonElement>(null);
  const [cut, setCut] = useState(false);
  // Measured only while its column is cut: reached, it keeps what it was.
  const measuring = useRef(columnCut);
  useLayoutEffect(() => { measuring.current = columnCut; }, [columnCut]);

  useEffect(() => {
    const cell = ref.current?.closest<HTMLElement>("[data-table-cell]");
    if (!cell) return undefined;
    // Its text, shown or woken into its editor (RichTextCellShown), which takes
    // its place; any other value is its cell's editor box, held to the cell.
    const valueOf = () => cell.querySelector<HTMLElement>("[data-cell-editor] [data-rich-cell-editor]")
      ?? cell.querySelector<HTMLElement>("[data-cell-editor]");
    let watched: HTMLElement | null = null;
    const measure = () => {
      if (!measuring.current) return;
      // The value's own box is held to the cell's height and cuts the rest.
      const value = valueOf();
      if (value && value !== watched) {
        if (watched) observer.unobserve(watched);
        observer.observe(watched = value);
      }
      setCut(Boolean(value && value.scrollHeight > cell.clientHeight + 1));
    };
    // The cell's height or the value's length changing cuts it or not.
    const observer = new ResizeObserver(measure);
    observer.observe(cell);
    measure();
    cell.addEventListener("pointerenter", measure);
    return () => { observer.disconnect(); cell.removeEventListener("pointerenter", measure); };
  }, []);

  // Its cell says it shows, for its value's room and tools (DatabaseTableRows).
  useLayoutEffect(() => {
    const cell = ref.current?.closest<HTMLElement>("[data-table-cell]");
    if (!cell || !cut) return undefined;
    cell.dataset.cutShown = "true";
    return () => { delete cell.dataset.cutShown; };
  }, [cut]);

  return (
    <button ref={ref} type="button" className={styles.wrappedTextExpand} hidden={!cut} aria-label={t("expandRow")} data-wrapped-text-expand
      onClick={(event) => {
        event.stopPropagation();
        onExpand();
      }}>
      <ExpandRowIcon size="small" aria-hidden="true" />
    </button>
  );
}
