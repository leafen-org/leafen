import { useCallback, useEffect, useMemo, useRef } from "react";
import { flushSync } from "react-dom";
import type { TextSpan } from "@/lib/types";
import { afterNextPaint } from "@/lib/afterPress";
import { useInnerHtml } from "@/lib/innerHtml";
import { usePeopleDirectoryRevision } from "@/lib/workspacePeopleDirectory";
import { useMentionedPageNames } from "@/lib/pageNames";
import { peopleDirectoryRevision } from "@/lib/peopleDirectory";
import { liveField, sleepUntilWoken } from "@/lib/sleepingFields";
import { showCustomEmojiPictures } from "../editor/InlineCustomEmoji";
import editorStyles from "../editor/editorStyles.generated.module.css";
import { createRichTextCellEngine, type RichTextCellEngine } from "./richTextCellEngine";
import { cellSpansOrText } from "./richTextCellModel";
import { useCellReadOnly } from "./cellReadOnly";

// A table's title or text value is drawn as its words, as its editor draws
// them, and stays asleep until it is edited, as Notion's cells are (seen
// 2026-11-09: no field in a table's rows until a cell is pressed, then one
// with the caret at its end): the pointer coming over it, a press or focus
// reaching it, or the table's keys that edit a cell (tableCellKeys.ts) make
// its editor then. Every cell of a table made its editor as it opened
// (2026-11-09: 2.3 s for 64 rows of a title and a text on a busy machine).
// Its words come from one editor made once, which draws each cell's words
// for it, so they are the editor's own.

const ignore = () => {};
let printer: RichTextCellEngine | null = null;
// Words that name people, pages or databases, and those that don't.
const printed = new Map<string, string>(), printedPlain = new Map<string, string>();
const PRINTED_KEPT = 4096;
let printedRevision = -1;

// Whether these words name a person, a page or a database, as mentions do.
function namesOthers(spans: readonly TextSpan[]) {
  return spans.some((span) => span.mention === "person" || span.mention === "page" || span.mention === "database" || Boolean(span.userId || span.pageId || span.databaseId));
}

// The words as a new editor of a table's cell shows them: from nothing, its
// lines on one line (the cell's "keep"); what it refuses leaves it empty.
// Its links take no keyboard stop: the woken editor's words take none.
function printedCellHtml(spans: TextSpan[], revision: number | null) {
  // A person's name or a page's title changing draws the words naming them
  // again, as their mentions read them now; words naming no one stay as printed.
  if (revision !== null && revision !== printedRevision) { printed.clear(); printedRevision = revision; }
  const cache = revision === null ? printedPlain : printed;
  const key = JSON.stringify(spans), kept = cache.get(key);
  if (kept !== undefined) return kept;
  printer ??= createRichTextCellEngine(document.createElement("div"), [], {
    multiline: () => false, commitOn: () => "input", attributes: () => ({}), changed: ignore, updated: ignore,
    linkRequested: ignore, finished: ignore, pasted: () => null, linesOnOneLine: () => true, pasteRefused: ignore,
    typedRules: () => false,
  });
  printer.replace([], 0, 0, true);
  try { printer.replace(spans, 0, 0, true); } catch (error) { if (!(error instanceof RangeError)) throw error; }
  const words = document.createElement("template");
  words.innerHTML = printer.editor.view.dom.innerHTML;
  for (const link of words.content.querySelectorAll("a")) link.tabIndex = -1;
  // Its workspace emoji show their pictures where it is drawn (RichTextCellShown).
  for (const emoji of words.content.querySelectorAll("[data-custom-emoji-shown]")) emoji.removeAttribute("data-custom-emoji-shown");
  const html = words.innerHTML;
  if (cache.size >= PRINTED_KEPT) cache.delete(cache.keys().next().value!);
  cache.set(key, html);
  return html;
}

/**
 * The words as a field's new editor shows them, for a field drawn before its
 * editor is made (RichTextCellEditor's afterPaint): its lines kept, as a
 * field that doesn't show them on one line keeps them.
 */
export function printedFieldHtml(spans: TextSpan[]) {
  const words = document.createElement("template");
  words.innerHTML = printedCellHtml(spans, namesOthers(spans) ? peopleDirectoryRevision() : null);
  for (const space of words.content.querySelectorAll("[data-cell-line-space]")) space.remove();
  return words.innerHTML;
}

/**
 * The editor a table's shown title or text turns into, made now; the element
 * itself where it is already one (liveField). The caller gives it the caret.
 */
export const liveCellField = liveField;

/** A table's title or text value as its words, until it is edited (above). */
export function RichTextCellShown(props: {
  spans: readonly TextSpan[] | undefined;
  value: unknown;
  ariaLabel: string;
  className: string;
  /** ⇧Enter puts a line in its editor (a text's). */
  shiftBreaks: boolean;
  dataAttributes?: Partial<Record<`data-${string}`, string>>;
  onWake(): void;
}) {
  const shown = useMemo(() => cellSpansOrText(props.spans, props.value), [props.spans, props.value]);
  // Only words naming others are drawn again as names change (2026-11-12: a
  // page put in the trash drew every shown cell of a page's tables again).
  const names = useMemo(() => namesOthers(shown), [shown]);
  // A page it mentions renamed or typed anywhere draws it again too (lib/pageNames.ts).
  const revision = usePeopleDirectoryRevision(names) + useMentionedPageNames(names ? shown : null);
  const html = useInnerHtml(useMemo(() => printedCellHtml(shown, names ? revision : null), [shown, names, revision]));
  const ref = useRef<HTMLDivElement>(null);
  const onWake = useRef(props.onWake); onWake.current = props.onWake;
  // A cell that only shows its value stays asleep: nothing edits it (cellReadOnly.ts).
  const readOnly = useCellReadOnly();
  const asleep = useRef(readOnly); asleep.current = readOnly;
  const hoverWake = useRef<(() => void) | null>(null), pressed = useRef(false);
  const wake = useCallback(() => {
    hoverWake.current?.();
    hoverWake.current = null;
    if (asleep.current) return;
    flushSync(() => onWake.current());
  }, []);
  // Woken by a press or focus reaching it, its editor takes the caret: at the
  // end for a press, where a table's first press into its text leaves it
  // (caretAtEndOfPressedText), at the start for Tab, as a field Tab reaches.
  const wakeToEdit = (pressedIn: boolean) => {
    const field = ref.current;
    if (!field) return;
    const live = liveCellField(field);
    if (live === field) return;
    if (!pressedIn) { live.focus(); return; }
    live.focus({ preventScroll: true });
    const selection = window.getSelection();
    selection?.selectAllChildren(live);
    selection?.collapseToEnd();
  };
  const register = useCallback((element: HTMLDivElement | null) => {
    ref.current = element;
    if (!element) return;
    const stop = sleepUntilWoken(element, wake);
    return () => { stop(); ref.current = null; };
  }, [wake]);
  useEffect(() => () => hoverWake.current?.(), []);
  useEffect(() => { if (ref.current && html.__html.includes("data-custom-emoji-id")) showCustomEmojiPictures(ref.current); }, [html]);
  return (
    <div ref={register} className={`${editorStyles.richMarks} ${props.className}`} role="textbox" tabIndex={readOnly ? -1 : 0}
      aria-label={props.ariaLabel} aria-multiline="false" aria-readonly={readOnly || undefined} spellCheck={false} translate="no"
      data-rich-cell-editor="single" data-rich-cell-shown="" data-empty={shown.some(span => span.text.length) ? "false" : "true"}
      data-placeholder="" data-cell-breaks={props.shiftBreaks ? "true" : undefined} {...props.dataAttributes}
      dangerouslySetInnerHTML={html}
      // The pointer wakes it once the frame after it arrives is painted, so a
      // press then lands in its editor and does what it does there (a mention
      // opens, the caret goes where the table puts it), and a row drawn under
      // a still pointer (a table's "+ 새 페이지") shows before its title's
      // editor is made (made in that frame, it held the row's first paint
      // 30–55 ms at CPU ×2, 2026-11-15). A press before then keeps it asleep
      // for its focus, which wakes it: woken under a press, the press is
      // lost (BlockHandleGate). A tap wakes it the same way: one woken under
      // the finger would be gone.
      onPointerEnter={(event) => {
        if (event.pointerType !== "mouse" || event.buttons !== 0 || hoverWake.current !== null) return;
        hoverWake.current = afterNextPaint(() => { hoverWake.current = null; wake(); });
      }}
      onPointerDownCapture={(event) => {
        hoverWake.current?.();
        hoverWake.current = null;
        pressed.current = event.button === 0;
      }}
      onFocus={(event) => {
        if (event.target !== event.currentTarget) return;
        const atEnd = pressed.current;
        pressed.current = false;
        wakeToEdit(atEnd);
      }}
      // A press that gave it no focus (a browser that focuses nothing pressed) wakes it at its click.
      onClick={() => wakeToEdit(true)} />
  );
}
