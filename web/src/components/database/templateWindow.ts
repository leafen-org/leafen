import { createContext, useContext } from "react";

/**
 * A database template's window as the views in its body see it: a row one of
 * them opens beside or over the page — one "+ 새 페이지" or "새로 만들기" just
 * made, or one already there — takes the window's place, as Notion's (seen
 * 2026-11-20 at 1440 in tabs at different times: a filtered table's "+ 새
 * 페이지" twice, a table's "새로 만들기" and a row's "열기", a board's and a
 * gallery's: the template's window closed and the row opened over the
 * database's page as its view opens rows, `?p=〈row〉&pm=s` beside it, a
 * gallery's `pm=c` in the middle; back opened the template again). The row's
 * address takes the template's in one step (useDatabaseViewController), and
 * the window leaves as its ✕ does, what was written in it kept
 * (DatabaseTemplateEditor). None outside a template's window.
 */
export type TemplateWindow = {
  /** Closes the template's window, what it holds sent first. */
  leave(): void;
};

export const TemplateWindowContext = createContext<TemplateWindow | null>(null);

export function useTemplateWindow() {
  return useContext(TemplateWindowContext);
}
