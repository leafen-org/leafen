import { useEffect, useRef } from "react";
import { chordLetter, matchesKeyboardShortcut, primaryModifier } from "@/lib/keyboardShortcuts";
import { lineArrowStep } from "@/lib/readingDirection";
import {
  type BodyFocus, consumePageTitleFocus, PAGE_TITLE_FOCUS_EVENT, scrollPastPageOptionsOnPhone, typeHeldTypingIntoTitle, waitForHeldComposition,
  whenPageTitleCanTakeFocus,
} from "@/lib/pageTitleFocus";
import type { TextSpan } from "@/lib/types";
import { sameTitle } from "@/lib/titleMerge";
import { DatabaseTitleField } from "../database/DatabaseTitleField";
import type { RichTextCellHandle } from "../database/RichTextCellEditor";
import { cellSpans } from "../database/richTextCellModel";
import { notePageHistoryStep, requestPageUndo } from "../editor/pageUndo";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/** A full-page database's name, which its page's title shows, and its writing. */
export type DatabasePageName = {
  spans?: readonly TextSpan[];
  onChange(title: string, spans: TextSpan[]): void;
};

type Named = { title: string; spans: TextSpan[] };
let steps = 0;

/**
 * A full-page database's title: its name as rich text, as Notion's (seen
 * 2026-11-07; DatabaseTitleField), with the keys a page's title has there
 * (PageHeader): Escape takes the caret to its start (seen 2026-11-06), ⌘Z is
 * the page's, a run of typing one step of it (seen 2026-11-05), ⌘⇧R opens
 * the rename popover, ⌘A chooses the name and then the body, and Enter, or ↓
 * at its end, goes to the database under it. A page just made asks for its
 * caret (requestPageTitleFocus).
 */
export function DatabasePageNameField({ pageId, title, name, className, label, untitled, onFocusBody, onRename }: {
  pageId: string;
  title: string;
  name: DatabasePageName;
  className: string;
  label: string;
  untitled: string;
  onFocusBody(focus: BodyFocus): void;
  /** ⌘⇧R: the rename popover, with the name as it stands. */
  onRename(title: string): void;
}) {
  const field = useRef<RichTextCellHandle>(null);
  const latest = useRef({ name, title }); latest.current = { name, title };
  const typed = useRef(title);
  const burst = useRef<{ from: Named; to: Named } | null>(null);
  const shownElement = () => document.querySelector<HTMLElement>(`[data-database-page-name="${CSS.escape(pageId)}"]`);

  useEffect(() => {
    let stop = () => {};
    const take = () => {
      stop();
      stop = whenPageTitleCanTakeFocus(pageId, shownElement, (shown) => {
        if (waitForHeldComposition(take) || !consumePageTitleFocus(pageId)) return;
        field.current?.focusEnd();
        scrollPastPageOptionsOnPhone(shown);
        typeHeldTypingIntoTitle();
      });
    };
    take();
    const asked = (event: Event) => { if ((event as CustomEvent<string>).detail === pageId) take(); };
    window.addEventListener(PAGE_TITLE_FOCUS_EVENT, asked);
    return () => { stop(); window.removeEventListener(PAGE_TITLE_FOCUS_EVENT, asked); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pageId]);

  // What the name was last written as in this run of typing: a key that
  // leaves it as it is writes nothing. A space typed at its ends is kept, as
  // Notion keeps it (seen 2026-11-13: "앱DB 끝 ").
  const written = useRef<Named | null>(null);
  function write(next: Named) {
    typed.current = next.title;
    const { title: shownTitle, name: { spans: shownSpans } } = latest.current;
    const before = written.current ?? { title: shownTitle, spans: cellSpans(shownSpans, shownTitle) ?? [] };
    const after = { title: next.title, spans: next.spans };
    if (sameTitle(before, after)) return;
    written.current = after;
    latest.current.name.onChange(after.title, after.spans);
  }

  function changed(next: Named) {
    if (burst.current) { burst.current.to = next; write(next); return; }
    const { title: before, name: { spans } } = latest.current;
    const step = { from: { title: before, spans: cellSpans(spans, before) ?? [] }, to: next };
    burst.current = step;
    notePageHistoryStep(pageId, `database-name:${pageId}:${++steps}`, async (forward) => {
      const value = forward ? step.to : step.from;
      burst.current = null;
      field.current?.set(value.spans.length ? value.spans : value.title ? [{ text: value.title }] : []);
      write(value);
      return true;
    });
    write(next);
  }

  function keyDown(event: KeyboardEvent) {
    const plain = !(event.metaKey || event.ctrlKey || event.altKey || event.shiftKey);
    const at = field.current?.range();
    if (event.key === "Escape" && plain) {
      field.current?.caretAt(0);
      return true;
    }
    if (primaryModifier(event) && !event.altKey && ["z", "y"].includes(chordLetter(event))) {
      burst.current = null;
      return requestPageUndo(pageId, event.shiftKey || chordLetter(event) === "y") ? true : undefined;
    }
    if (matchesKeyboardShortcut("renamePage", event)) {
      burst.current = null;
      onRename(typed.current);
      return true;
    }
    if (primaryModifier(event) && !event.shiftKey && !event.altKey && chordLetter(event) === "a") {
      if (!at || at.length > 0 && (at.start > 0 || at.end < at.length)) return undefined;
      (event.target as HTMLElement).blur();
      onFocusBody("select");
      return true;
    }
    // A full-page database's title has no line to go to. The arrow on at its
    // end goes to it: ← where the name reads from the right (lineArrowStep).
    const on = (event.key === "ArrowLeft" || event.key === "ArrowRight") && lineArrowStep(event.key, event.target as Element, typed.current) > 0;
    if (fieldEnter(event) || (event.key === "ArrowDown" || on) && plain && at && at.start === at.length && at.end === at.length) {
      onFocusBody("start");
      return true;
    }
    return undefined;
  }

  return (
    <DatabaseTitleField className={className} value={title} spans={name.spans} placeholder={untitled} ariaLabel={label}
      dataAttributes={{ "data-page-title-editor": "true", "data-database-page-name": pageId, dir: "auto" }} names={pageId}
      onTitleChange={(next, spans) => { typed.current = next; changed({ title: next, spans }); }}
      onFinish={() => { burst.current = null; written.current = null; }}
      onKeyDown={keyDown} handleRef={field} />
  );
}
