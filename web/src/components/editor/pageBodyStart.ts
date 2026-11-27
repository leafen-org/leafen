import { i18next } from "@/i18n";
import { EDITOR_PASTE_BOUNDS } from "./editorClipboardPolicy";
import { parseInternalPastedBlocks, parsePastedHtml, parsePastedMarkdown, type PastedBlock } from "./markdownPaste";
import { admitNativeTiptapClipboard } from "./nativeTiptapPasteBudget";
import { notionCopy, pastesTextOverHtml } from "./pastedHtmlChoice";

// What a page's title doesn't keep of a paste (titlePaste.ts) goes to the start
// of the body beside it, whose editor takes it as a paste of its own: its
// history, its saving, also while offline. A body still opening takes it once
// it can; past the wait the title says so, the text still on the clipboard.

const pasteEvent = "leafen:page-body-start-paste";
const WAIT_MS = 5_000;
const RETRY_MS = 100;

type PasteDetail = { pageId: string; blocks: readonly PastedBlock[]; from: Element; handled: boolean };

/** The window a title or body stands in: a peek, or none (the page itself). */
const windowOf = (element: Element) => element.closest("[data-page-peek],[data-row-peek-panel]");

/**
 * A paste as the body reads one: Leafen's blocks, then HTML its text would
 * lose something of (pastedHtmlChoice.ts), then Markdown. A plain line right
 * after a paragraph or list item is that block's next line, as Notion's title
 * paste reads it (seen 2026-11-04).
 */
function pastedBlocks(data: DataTransfer) {
  admitNativeTiptapClipboard(data);
  const internal = parseInternalPastedBlocks(data, EDITOR_PASTE_BOUNDS);
  if (internal.length) return internal;
  const text = data.getData("text/plain"), html = pastesTextOverHtml(data) ? "" : data.getData("text/html");
  const fromHtml = html ? parsePastedHtml(html, EDITOR_PASTE_BOUNDS, text, notionCopy(data)) : [];
  return fromHtml.length ? fromHtml : parsePastedMarkdown(text, EDITOR_PASTE_BOUNDS, { softLineBreaks: true });
}

/**
 * Hands a title's paste to the body of this page, in the window of the title
 * it came from. What goes wrong is said through notify. The clipboard is read
 * here: it can't be read once the paste's event is over.
 */
export function pasteAtBodyStart(pageId: string, data: DataTransfer, from: Element, notify: (message: string) => void) {
  let blocks: PastedBlock[];
  try { blocks = pastedBlocks(data); }
  catch (error) {
    if (!(error instanceof RangeError)) throw error;
    notify(i18next.t("editor:pasteRefused"));
    return;
  }
  if (!blocks.length) return;
  const started = Date.now();
  const ask = () => {
    const detail: PasteDetail = { pageId, blocks, from, handled: false };
    document.dispatchEvent(new CustomEvent<PasteDetail>(pasteEvent, { detail }));
    if (detail.handled) return;
    if (Date.now() - started >= WAIT_MS) notify(i18next.t("syncStatusBadge:status.retryAfterBodySync"));
    else window.setTimeout(ask, RETRY_MS);
  };
  ask();
}

/** Takes the page's pastes while listening; paste answers whether it could. */
export function listenForBodyStartPaste(pageId: string, root: () => Element | null, paste: (blocks: readonly PastedBlock[]) => boolean) {
  const listener = (event: Event) => {
    const detail = (event as CustomEvent<PasteDetail>).detail;
    const element = root();
    if (detail.handled || detail.pageId !== pageId || !element || windowOf(element) !== windowOf(detail.from)) return;
    detail.handled = paste(detail.blocks);
  };
  document.addEventListener(pasteEvent, listener);
  return () => document.removeEventListener(pasteEvent, listener);
}

const splitEvent = "leafen:page-body-start-split";
/** A title split by Enter: what it was and what it keeps. */
export type TitleSplit = { before: string; after: string };
type SplitDetail = { pageId: string; text: string; title: TitleSplit; from: Element; handled: boolean };

/**
 * A title's Enter in its text: what follows the caret starts a new first line
 * of the body, the caret at its start, and one Undo puts it back in the title,
 * as Notion's (seen 2026-11-06). Answers whether a body took it; none open yet
 * leaves it to the title.
 */
export function splitIntoBodyStart(pageId: string, text: string, title: TitleSplit, from: Element) {
  const detail: SplitDetail = { pageId, text, title, from, handled: false };
  document.dispatchEvent(new CustomEvent<SplitDetail>(splitEvent, { detail }));
  return detail.handled;
}

/** Takes the page's title splits while listening; split answers whether it could. */
export function listenForBodyStartSplit(pageId: string, root: () => Element | null, split: (text: string, title: TitleSplit) => boolean) {
  const listener = (event: Event) => {
    const detail = (event as CustomEvent<SplitDetail>).detail;
    const element = root();
    if (detail.handled || detail.pageId !== pageId || !element || windowOf(element) !== windowOf(detail.from)) return;
    detail.handled = split(detail.text, detail.title);
  };
  document.addEventListener(splitEvent, listener);
  return () => document.removeEventListener(splitEvent, listener);
}
