import { type RefObject, useLayoutEffect } from "react";
import { holdStyleWrite } from "./heldStyleWrites";

/**
 * How far a board's columns run, from its first column's start to the end of
 * what follows its last (its hidden groups and "+ 새 그룹"), kept on the board
 * as --board-span: the blue a chosen inline database draws over a board
 * spans that (EditorBlocks), as Notion's does past the page's text column.
 * Measured after each of the board's renders, which any change of its
 * columns, their sizes or what they hold comes with.
 */
export function useBoardSpan(ref: RefObject<HTMLElement | null>) {
  useLayoutEffect(() => {
    const board = ref.current;
    const first = board?.firstElementChild;
    const last = board?.querySelector(":scope > [data-board-side]") ?? board?.lastElementChild;
    if (!board || !first || !last) return;
    const start = first.getBoundingClientRect();
    const end = last.getBoundingClientRect();
    const span = Math.round((getComputedStyle(board).direction === "rtl" ? start.right - end.left : end.right - start.left) * 10) / 10;
    const value = span > 0 ? `${span}px` : "";
    // Written after the board's other hooks have measured (heldStyleWrites).
    if (board.style.getPropertyValue("--board-span") !== value) {
      holdStyleWrite(() => {
        if (value) board.style.setProperty("--board-span", value);
        else board.style.removeProperty("--board-span");
      });
    }
  });
}
