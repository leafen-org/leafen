import { caretBesideLine } from "./caretBesideLine";

// The caret crosses between a page's title and its body by line, keeping
// where it stands across, as Notion's (seen 2026-11-13): ↓ on the title's
// last line goes to the body's first line under the caret, a title that
// wraps going down its own lines first, and ↑ on the body's first line goes
// to the title's last line above it — at the end of a shorter line, the
// line's end.

type Box = { left: number; top: number; bottom: number };

/** The box of a collapsed range, or of what it stands beside when it has none (an empty line). */
function rangeBox(range: Range): Box | null {
  const rects = range.getClientRects();
  const rect = rects.length ? rects[rects.length - 1] : range.getBoundingClientRect();
  if (rect.height) return rect;
  const host = range.startContainer instanceof Element ? range.startContainer : range.startContainer.parentElement;
  const box = host?.getBoundingClientRect();
  return box?.height ? { left: rect.left || box.left, top: box.top, bottom: box.bottom } : null;
}

/** Where the caret stands in `field`, when it is in it and holds no selection. */
function caretIn(field: HTMLElement): Box | null {
  const selection = field.ownerDocument.getSelection();
  if (!selection?.rangeCount || !selection.isCollapsed || !field.contains(selection.anchorNode)) return null;
  return rangeBox(selection.getRangeAt(0));
}

/**
 * A field's first or last line, from where its text starts or ends: a range
 * at the field's own edge has no box of its own, and the field's whole box
 * stood for that line (a title wrapped onto three lines went to the body
 * from its first).
 */
function lineOf(field: HTMLElement, edge: "first" | "last"): Box | null {
  const walker = field.ownerDocument.createTreeWalker(field, NodeFilter.SHOW_TEXT);
  let text: Text | null = null;
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    if (!(node as Text).length) continue;
    text = node as Text;
    if (edge === "first") break;
  }
  const range = field.ownerDocument.createRange();
  if (text) range.setStart(text, edge === "first" ? 0 : text.length);
  else range.selectNodeContents(field);
  range.collapse(true);
  return rangeBox(range);
}

/** The body's first line that takes text, of the page whose title this is. */
function bodyFirstField(title: HTMLElement, pageId: string): HTMLElement | null {
  const selector = `[data-editor-page="${CSS.escape(pageId)}"]`;
  for (let around = title.parentElement; around; around = around.parentElement) {
    const body = around.querySelector(selector);
    if (!body) continue;
    return [...body.querySelectorAll<HTMLElement>("[data-rt-editable]")].find((field) => (
      field.isContentEditable && field.getClientRects().length > 0)) ?? null;
  }
  return null;
}

/**
 * The field for a new comment between the page's title and its body — a
 * database page's "댓글 추가", always open — when the page shows one.
 */
function titleCommentField(title: HTMLElement, pageId: string): HTMLElement | null {
  const host = `page-comments-inline-${CSS.escape(pageId)}`;
  const selector = `:is([id^="${host}-"], [id="${host}"]) [data-comments-composer] [contenteditable="true"]`;
  for (let around = title.parentElement; around; around = around.parentElement) {
    const field = around.querySelector<HTMLElement>(selector);
    if (field) return field.getClientRects().length > 0 ? field : null;
  }
  return null;
}

/** The caret at the end of what a comment field holds. */
function caretToCommentField(field: HTMLElement): true {
  field.focus({ preventScroll: true });
  const selection = field.ownerDocument.getSelection();
  const range = field.ownerDocument.createRange();
  range.selectNodeContents(field);
  range.collapse(false);
  selection?.removeAllRanges();
  selection?.addRange(range);
  field.scrollIntoView({ block: "nearest" });
  return true;
}

/**
 * ↑ on the body's first line, its page showing a comment field under its
 * title: the caret goes to that field, as Notion's database page's (seen
 * 2026-11-14), and true; false when there is none.
 */
export function caretUpToTitleComment(title: HTMLElement, pageId: string): boolean {
  const field = titleCommentField(title, pageId);
  return field ? caretToCommentField(field) : false;
}

/**
 * ↓ in a page's title: on its last line the caret goes to the body's first
 * line under it — or first to the comment field between them, as Notion's
 * database page's "댓글 추가" (seen 2026-11-14) — and true; elsewhere the
 * title's own lines take it (false). `null` when it is on the last line and
 * the body has no line to take it.
 */
export function caretDownFromTitle(title: HTMLElement, pageId: string): boolean | null {
  const caret = caretIn(title), last = lineOf(title, "last");
  if (!caret || !last) return false;
  if ((caret.top + caret.bottom) / 2 < last.top) return false;
  const comment = titleCommentField(title, pageId);
  if (comment) return caretToCommentField(comment);
  const field = bodyFirstField(title, pageId), first = field && lineOf(field, "first");
  if (!field || !first) return null;
  return caretBesideLine(field, { x: caret.left, y: (first.top + first.bottom) / 2 }) || null;
}

/** Whether the caret stands on the title's last line, where ↓ leaves the title. */
export function caretOnTitleLastLine(title: HTMLElement): boolean {
  const caret = caretIn(title), last = lineOf(title, "last");
  return Boolean(caret && last && (caret.top + caret.bottom) / 2 >= last.top);
}

/** ↑ on the body's first line: the caret goes to the title's last line, above where it stood. */
export function caretUpToTitle(title: HTMLElement, x: number): boolean {
  const last = lineOf(title, "last");
  return last ? caretBesideLine(title, { x, y: (last.top + last.bottom) / 2 }) : false;
}
