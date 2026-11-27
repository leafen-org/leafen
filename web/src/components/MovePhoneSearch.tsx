import { useRef } from "react";
import { Search } from "@/icons/leafen";
import { SearchClearButton } from "@/components/PageActionSearch";
import styles from "./MovePhoneSearch.module.css";

/**
 * The search at the top of a phone's "옮기기" sheet, the pages' and a
 * database row's alike (MoveToPhoneSheet, DatabaseRowMovePhoneList), as
 * Notion's phone draws its one "옮기기" (seen 2026-10-31 and 2026-11-13; a
 * database row's "옮기기" opens the same sheet, 2026-11-17 in Android Chrome
 * at 411): a white field across the sheet with its magnifier, and the shared
 * "입력 값 지우기" at its end once it holds words, the search keeping the
 * focus when it is pressed.
 */
export function MovePhoneSearch({ value, label, placeholder, onValueChange }: {
  value: string;
  label: string;
  placeholder: string;
  onValueChange(value: string): void;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  return (
    <label className={styles.search}>
      <Search size="small" aria-hidden="true" />
      <input ref={inputRef} value={value} placeholder={placeholder} aria-label={label}
        onChange={(event) => onValueChange(event.currentTarget.value)} />
      <SearchClearButton value={value} inputRef={inputRef} className={styles.clear} onClear={() => onValueChange("")} />
    </label>
  );
}
