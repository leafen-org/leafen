import { useCallback, useState } from "react";
import styles from "./DocumentSheetTabs.module.css";

/**
 * The sheets of a sheet the document preview draws as a plain table (an .xls,
 * .xlsm, .xlsb, .ods or Numbers file; documentKinds' "sheet"), which the frame
 * names (preview.js) and the head that shows their tabs picks.
 */
export function useDocumentSheets() {
  const [names, setNames] = useState<string[]>([]);
  const [at, setAt] = useState(0);
  const heard = useCallback((message: { type: string; names?: unknown }) => {
    if (message.type !== "leafen:document-preview:sheets" || !Array.isArray(message.names)) return;
    setNames(message.names.map(String));
    setAt(0);
  }, []);
  return { names, at, pick: setAt, heard };
}

/**
 * Its tabs, as Notion's beside an embed's name and at the start of its
 * preview's head (measured 2026-11-15 at 1440 dark, twice): 28px pills 6px
 * apart in a row with 6px round it, 14px medium words 10px in, the chosen one
 * on the hover wash in the head's ink, the others in its grey; none for a
 * file of one sheet.
 */
export function DocumentSheetTabs({ names, at, onPick, label }: {
  names: string[]; at: number; onPick: (at: number) => void; label: string;
}) {
  if (names.length < 2) return null;
  return <div className={styles.tabs} role="tablist" aria-label={label} data-document-sheet-tabs>
    {names.map((name, index) => <button key={`${index}:${name}`} type="button" role="tab" className={styles.tab}
      aria-selected={index === at} tabIndex={index === at ? 0 : -1} onClick={() => onPick(index)}
      onKeyDown={(event) => {
        const step = event.key === "ArrowRight" ? 1 : event.key === "ArrowLeft" ? -1 : 0;
        if (!step) return;
        event.preventDefault();
        const next = (index + step + names.length) % names.length;
        onPick(next);
        (event.currentTarget.parentElement?.children[next] as HTMLElement | undefined)?.focus();
      }}>{name}</button>)}
  </div>;
}
