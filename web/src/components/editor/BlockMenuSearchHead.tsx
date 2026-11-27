import { type KeyboardEvent, type ReactNode, type RefObject } from "react";
import { isMenuMoveKey } from "@/lib/menuKeyboard";
import styles from "./editorStyles.generated.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/**
 * The block menu's search (⌘/ and the handle's menu): it keeps the focus
 * while the arrows move the rows' highlight, and Enter runs the best result
 * once something is typed.
 */
export function BlockMenuSearch({ inputRef, query, label, ownerProps, onQuery, onMove, onEnter }: {
  inputRef: RefObject<HTMLInputElement | null>; query: string; label: string;
  ownerProps: { onKeyDown(event: KeyboardEvent<HTMLElement>): boolean; onKeyUp(event: KeyboardEvent<HTMLElement>): void };
  onQuery(value: string): void;
  /** An arrow moved the highlight away from an open submenu's row. */
  onMove(): void;
  /** Enter on what is typed: whether it ran a result. */
  onEnter(): boolean;
}) {
  return <input ref={inputRef} className={styles.blockMenuSearch} value={query} placeholder={label} aria-label={label}
    data-block-menu-search {...ownerProps} onChange={event => onQuery(event.currentTarget.value)}
    onKeyDown={event => {
      if (isMenuMoveKey(event.key)) onMove();
      if (!ownerProps.onKeyDown(event) && fieldEnter(event) && onEnter()) event.preventDefault();
    }} onKeyUp={ownerProps.onKeyUp} />;
}

/**
 * The rows under the search: where the menu runs out of room they scroll on
 * their own, the search staying above them as wide as in a short menu, as
 * Notion's (seen 2026-11-14 and 2026-11-15 at 1440 in dark: a code block's
 * ⌘/ menu 506 high, its search 241 wide 12 under the menu's top, the scroll
 * bar only beside the rows, from 48 under the top). The row the keys light
 * scrolls there as in every menu (the menu's `useKeyWalkScroll`). Without a
 * search (`head` unset) the rows stand in the menu itself.
 */
export function BlockMenuList({ head, children }: { head: boolean; children: ReactNode }) {
  return head ? <div className={styles.blockMenuList} data-block-menu-list>{children}</div> : <>{children}</>;
}
