import { useLayoutEffect, useMemo, useRef, useState, type ReactElement } from "react";
import { useTranslation } from "react-i18next";
import type { TextSpan } from "@/lib/types";
import { useTypedDraftSave } from "@/lib/useTypedDraftSave";
import { RichTextCellEditor, type RichTextCellHandle } from "@/components/database/RichTextCellEditor";
import { cellSpans } from "@/components/database/richTextCellModel";
import { useCommentMentionSnapshot } from "@/components/comments/commentMentionContext";
import { titleOnOneLine } from "./titlePaste";
import { fieldKey } from "@/lib/fieldKeyBoundary";

type Draft = { title: string; spans?: TextSpan[] };

function draftKey(draft: Draft) {
  return JSON.stringify([draft.title, draft.spans ?? null]);
}

// A title with line breaks shows on one line, its spans too while they
// still spell it; it keeps its breaks until it is changed.
function oneLine(title: string, spans: readonly TextSpan[] | undefined): Draft {
  const line = titleOnOneLine(title);
  const lined = cellSpans(spans?.map((span) => ({ ...span, text: titleOnOneLine(span.text) })), line);
  return lined ? { title: line, spans: lined } : { title: line };
}

/**
 * A rename box's title, as Notion's rename box (seen 2026-11-10): its words
 * with their mentions — a page's with its icon, "@〈사람〉" and "@2026년 11월
 * 10일" lighter — a mention closed to its reader as a page's own title says
 * it, the box growing a line for a title that needs one; all chosen as it
 * opens, so typing replaces them, unless it opens with its caret at the end. What is typed keeps the mentions it still
 * holds, written with the title (`onTitleChange`'s spans: an empty list for
 * plain words, none for a title that opened with none and stays plain, which
 * its owner writes as words alone), and the title follows the typing, as
 * Notion's does (its row and the page's title follow each key): each key's
 * words are shown at once (`onTitleShown`, lib/renameShownAtPress.ts) and
 * saved once the typing stops (`onTitleChange`). Enter or Escape ends it
 * (`onDone`, told which), keeping what was typed; `finish` keeps it where
 * the box closes otherwise.
 */
export function useRenameTitleField({ title, spans, placeholder, className, caret = "all", onTitleChange, onTitleShown, onDone }: {
  title: string;
  /** The title's spans as its reader reads them, while they spell it. */
  spans?: readonly TextSpan[];
  placeholder: string;
  className?: string;
  /**
   * Where the box opens: its words all chosen, or the caret after them, as
   * Notion's library opens a row's name (seen 2026-11-11).
   */
  caret?: "all" | "end";
  onTitleChange: (title: string, spans?: TextSpan[]) => void;
  /** The words as each key leaves them, to show wherever the title is drawn before they are saved. */
  onTitleShown?: (title: string, spans?: TextSpan[]) => void;
  onDone: (key: "Enter" | "Escape") => void;
}): { field: ReactElement; finish: () => void } {
  const { t } = useTranslation("pageHeader");
  // "@" opens the mention menu, as Notion's rename box (2026-11-10), with
  // what the page's comments mention: dates, people and pages.
  const mentions = useCommentMentionSnapshot();
  const handle = useRef<RichTextCellHandle>(null);
  // What the box opened with, which needs no saving.
  const [shown] = useState(() => oneLine(title, spans));
  const [draft, setDraft] = useState(shown);
  const written = useRef(draft);
  written.current = draft;
  // What was last written: closing writes only what is newer, as a second
  // write of the same title went with the page as it was before the first
  // (2026-11-10: a database's page, renamed after a pause, doubled the words
  // typed when its first write moved the page on).
  const lastSaved = useRef(draftKey(shown));
  const save = (next: Draft) => {
    lastSaved.current = draftKey(next);
    // Written as typed, the spaces at its ends too, as Notion keeps them (titleMerge.ts).
    onTitleChange(next.title, next.spans ?? (shown.spans ? [] : undefined));
  };
  const saveRef = useRef(save);
  saveRef.current = save;
  const showRef = useRef(onTitleShown);
  showRef.current = onTitleShown;
  const typed = useTypedDraftSave({
    active: true, draft: draftKey(draft), initial: draftKey(shown),
    save: () => saveRef.current(written.current), holding: () => handle.current?.composing() === true,
  });

  // The box has the caret as it is drawn, in the same step: a frame on, an
  // Enter or a letter typed as it showed went to the page's title or text,
  // the box staying open (2026-11-23, ⌘⇧R then Enter at once).
  useLayoutEffect(() => {
    if (caret === "end") handle.current?.focusEnd();
    else handle.current?.selectAll();
    // Only as the box opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function finish() {
    typed.settle();
    if (draftKey(written.current) !== lastSaved.current) save(written.current);
  }

  const doneRef = useRef((key: "Enter" | "Escape") => { finish(); onDone(key); });
  doneRef.current = (key) => { finish(); onDone(key); };
  const field = useMemo(() => (
    <RichTextCellEditor handleRef={handle} spans={shown.spans} value={shown.title} placeholder={placeholder}
      ariaLabel={t("pageTitle")} className={className} mentionSnapshot={mentions} holds="title"
      dataAttributes={{ "data-page-rename-title": "", dir: "auto" }}
      onChange={(change) => {
        const next = { title: change.value, ...(change.spans ? { spans: change.spans } : {}) };
        setDraft(next);
        showRef.current?.(next.title, next.spans ?? (shown.spans ? [] : undefined));
      }}
      // A syllable still composing shows where the page is named too, as the title's own field does (pageTitleEngine).
      onComposed={(words) => showRef.current?.(words, shown.spans ? [] : undefined)}
      onKeyDown={(event) => {
        const key = fieldKey(event);
        if (key !== "confirm" && key !== "escape") return undefined;
        event.preventDefault();
        event.stopPropagation();
        doneRef.current(key === "confirm" ? "Enter" : "Escape");
        return true;
      }} />
    // The field keeps its own words once open; its owner's changes don't reach it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  ), [placeholder, className, t]);
  return { field, finish };
}
