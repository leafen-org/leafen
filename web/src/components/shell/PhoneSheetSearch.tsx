import { useRef, type InputHTMLAttributes } from "react";
import { SearchClearButton } from "@/components/PageActionSearch";
import parts from "./PhoneSheetRowParts.module.css";

/**
 * A phone sheet's search ("속성 검색", "시간대 검색", a condition's or an
 * action's search), its 45px row, and the shared "입력 값 지우기" at its end
 * once it holds words, as Notion's phone sheets (seen 2026-11-17 in Android
 * Chrome on a Galaxy A90 at 411: the filter's and the sort's property
 * searches and "옮기기", a 24px button 12px in from the group's edge; a
 * letter typed after pressing it went into the search).
 */
export function PhoneSheetSearch({ value, onValueChange, ...input }: Omit<InputHTMLAttributes<HTMLInputElement>, "value" | "onChange" | "className"> & {
  value: string;
  onValueChange(value: string): void;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  return (
    <label className={parts.menuSearchRow}>
      <input {...input} ref={inputRef} value={value} onChange={(event) => onValueChange(event.currentTarget.value)} />
      <SearchClearButton value={value} inputRef={inputRef} onClear={() => onValueChange("")} />
    </label>
  );
}
