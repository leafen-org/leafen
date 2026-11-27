import { useLayoutEffect, useRef, type ReactNode, type RefObject } from "react";
import { scrollIntoViewNearest } from "@/lib/scrollIntoViewNearest";
import styles from "./ImportDialog.module.css";

// The room kept between a failure line and the window body's edges.
const ROOM = 12;

/**
 * Brings an element into view in the scrolling body of the window it is in
 * (Notion's, NotionImportPanel; another tool's, ToolMigrationView), clear of
 * the footer the body keeps at its foot as it scrolls.
 */
export function showInNotionWindow(element: HTMLElement | null) {
  if (!element) return;
  let body = element.parentElement;
  while (body && !/(auto|scroll)/u.test(getComputedStyle(body).overflowY)) body = body.parentElement;
  const foot = body?.querySelector<HTMLElement>(":scope > footer")?.offsetHeight ?? 0;
  scrollIntoViewNearest(element, ROOM, ROOM + foot);
}

/**
 * A failure line of the Notion window, drawn beside the press or the field
 * it answers. As it comes, or its words change, it brings itself into view
 * in the window's scrolling body — or `around`, the field it is under with
 * it (2026-11-13: a refused token's line came under the example card, out
 * of view, and "토큰 저장" seemed to do nothing). What to do about it, if
 * the line offers that (`children`, "다시 시도"), follows the words.
 */
export function NotionFailureLine({ text, id, className = styles.serverStatus, around, children }: {
  text: string;
  /** For the field it is under to name it (`aria-describedby`). */
  id?: string;
  /** The window's boxed line by default; under a field, a plain line. */
  className?: string;
  around?: RefObject<HTMLElement | null>;
  children?: ReactNode;
}) {
  const line = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    if (text) showInNotionWindow(around?.current ?? line.current);
  }, [around, text]);
  return text ? <div ref={line} id={id} className={className} role="alert" data-error>{text}{children ? <> {children}</> : null}</div> : null;
}
