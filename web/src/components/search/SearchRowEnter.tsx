import { ReturnKeyIcon } from "@/icons/leafen";
import styles from "./SearchRowEnter.module.css";

/**
 * The Enter sign "↵" at a search row's end. Notion's ⌘K keeps its room on
 * every row and never shows it on a found or visited page — not on the chosen
 * row, under the pointer or after the arrows (seen 2026-11-19, ko 1440, with
 * and without the preview): the room only ends the title and its place 50px
 * before the row's end. The "새 페이지" row of a search for a new tab shows it
 * while chosen (SearchNewTab). It points back as the screen reads (styles.css,
 * `arrow-elbow-down-left`).
 */
export function SearchRowEnter({ shown = false }: { shown?: boolean }) {
  return (
    <span className={styles.enter} data-search-row-enter={shown ? "shown" : ""} aria-hidden="true">
      <ReturnKeyIcon size="tiny" />
    </span>
  );
}
