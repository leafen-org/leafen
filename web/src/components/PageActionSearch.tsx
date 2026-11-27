import type { KeyboardEvent, RefObject } from "react";
import { useTranslation } from "react-i18next";
import { Search, XCircleIcon } from "@/icons/leafen";
import styles from "./PageActionSearch.module.css";

/**
 * A page menu's "작업 검색...": a 16px magnifier, the words, and
 * "입력 값 지우기" at the end while it holds any (SearchClearButton), as
 * Notion's page and open database page menus (seen 2026-11-01, 2026-11-12).
 * `marker` names the field's own data attribute.
 */
export function PageActionSearch({ className, label, value, inputRef, marker, onChange, onKeyDown }: {
  className: string;
  label: string;
  value: string;
  inputRef: RefObject<HTMLInputElement | null>;
  marker: string;
  onChange(value: string): void;
  /** Keys the field leaves to its list, such as its arrows. */
  onKeyDown?(event: KeyboardEvent<HTMLInputElement>): void;
}) {
  return (
    <label className={className}>
      <Search size="small" aria-hidden="true" />
      <input ref={inputRef} value={value} placeholder={label} aria-label={label} {...{ [`data-${marker}`]: "" }} data-native-menu-search
        onChange={(event) => onChange(event.target.value)} onKeyDown={onKeyDown} />
      <SearchClearButton value={value} inputRef={inputRef} onClear={() => onChange("")} />
    </label>
  );
}

/**
 * "입력 값 지우기" at the end of a search or value field while it holds words,
 * Notion's one button wherever a field clears (seen 2026-11-12 and 2026-11-17
 * at 1440, dark: page menu "작업 검색...", settings "검색 설정", the trash,
 * the emoji and icon "필터", a database's search, a files link, a date
 * filter's day): a 24px round box 2px in from the field's end, its ⓧ 20px
 * (a 15.25 circle) in the quietest icon color, shaded under the pointer with no
 * tip; the keyboard's focus (Tab from the field) tips its name, the tip taking
 * the next Escape alone (seen 2026-11-26 in two tabs; iconTips.ts). A field
 * of chosen chips draws it `small`: 16px, its circle 16, at the end of the words (Notion's trash filters, seen 2026-11-17), and on a
 * phone's sheet (`small` and `phone`) 22px, the circle 22 (Notion's phone
 * filter value fields, seen 2026-11-17 in Android Chrome at 411: an option's,
 * a person's and a page's field).
 *
 * Where it sits is the field's: at the end of a field laid out in a row
 * (6px in, the button 4px over it), `over` the end of a field that draws
 * its own box, or by the field's own `className`. Pressed, the words go and,
 * given `inputRef`, the search keeps the focus, as the page menu's (measured
 * once 2026-11-15 in Chrome 1440 with no script before the press: a letter
 * typed then went into the search); without it the focus goes with the
 * button and the keys stay on the page, as the settings search's (Chrome
 * 1440, two tabs 2026-11-17, nothing run in the page before the press: a
 * letter typed then went nowhere).
 */
export function SearchClearButton({ value, inputRef, onClear, className, over = false, small = false, phone = false, label, tabIndex }: {
  /** Shown while this holds anything. */
  value: string | boolean;
  inputRef?: RefObject<HTMLInputElement | null>;
  onClear(): void;
  className?: string;
  over?: boolean;
  small?: boolean;
  /** A chips field's on a phone's sheet, with `small`. */
  phone?: boolean;
  /** Its name where the field names it otherwise (a chips field's "모두 지우기"). */
  label?: string;
  tabIndex?: number;
}) {
  const { t } = useTranslation("blockItem");
  if (!value) return null;
  const place = className ?? (over ? styles.over : styles.atEnd);
  return (
    <button type="button" className={`${styles.clear} ${place}`} aria-label={label ?? t("common.clearInput")} data-search-clear
      data-icon-tip={label ?? t("common.clearInput")} data-tip-keys-only=""
      data-small={small ? "" : undefined} data-phone={small && phone ? "" : undefined} data-focus-leaves={inputRef ? undefined : ""} tabIndex={tabIndex}
      onMouseDown={inputRef ? (event) => event.preventDefault() : undefined}
      onClick={() => { onClear(); inputRef?.current?.focus({ preventScroll: true }); }}>
      <XCircleIcon size={small ? phone ? "wide" : "small" : "regular"} aria-hidden="true" />
    </button>
  );
}
