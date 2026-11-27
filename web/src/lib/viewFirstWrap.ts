/**
 * Whether a new view of a layout wraps all its content at first, as Notion's
 * new views do: a table and a board (seen 2026-11-03 — a table made by
 * "/표 보기" and an untouched new inline database; the board alone on
 * 2026-10-24). Views already made keep what they have.
 */
export function viewWrapsAtFirst(type: string) {
  return type === "table" || type === "board";
}
