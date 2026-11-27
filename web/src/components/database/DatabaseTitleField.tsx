import type { Ref } from "react";
import type { TextSpan } from "@/lib/types";
import { useCommentMentionSnapshot } from "@/components/comments/commentMentionContext";
import { RichTextCellEditor, type RichTextCellHandle } from "./RichTextCellEditor";
import { RichTextCellText } from "./RichTextCellText";
import { cellSpans } from "./richTextCellModel";
import editorStyles from "../editor/editorStyles.generated.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/**
 * A database's name read, with its formatting where it is the database's
 * own title (its page, its block); lists, links and the window show its text.
 */
export function DatabaseTitleText({ title, spans, fallback }: { title: string; spans?: readonly TextSpan[]; fallback?: string }) {
  const formatted = title ? cellSpans(spans, title) : undefined;
  return formatted ? <RichTextCellText spans={formatted} className={editorStyles.plainMarks} interactive /> : <>{title || fallback}</>;
}

/**
 * A name's formatting for new words written to it from outside it, as a line
 * joined to it and its Undo: the words it keeps keep their formatting, the
 * new ones come plain.
 */
export function respelledName(spans: readonly TextSpan[] | undefined, from: string, to: string): TextSpan[] {
  let same = 0;
  while (same < from.length && same < to.length && from[same] === to[same]) same += 1;
  const kept: TextSpan[] = [];
  let left = cellSpans(spans, from) ? same : 0;
  for (const span of spans ?? []) {
    if (left <= 0) break;
    const text = span.text.slice(0, left);
    kept.push({ ...span, text });
    left -= text.length;
  }
  const written = kept.reduce((sum, span) => sum + span.text.length, 0);
  return to.length > written ? [...kept, { text: to.slice(written) }] : kept;
}

/**
 * A database's name as rich text, as Notion's (seen 2026-11-07 and
 * 2026-11-09): ⌘B, ⌘I, ⌘U, ⌘⇧S and ⌘E format the words chosen and ⌘K opens
 * the link box under them — the rich text cell's — but no bar shows over
 * chosen words, and the formatting draws in the name's own
 * look, its links underlined (plainMarks). It wraps, as Notion's full-page
 * name does (seen 2026-10-04), unless its owner's style keeps it on one. It
 * stays one line of text: Enter keeps the caret where it is and line breaks
 * pasted in become spaces. Escape leaves the field, choosing nothing (Notion's
 * database page, seen 2026-11-04 and 2026-11-05). Its owner takes keys first
 * (`onKeyDown`, as the rich text cell's). "@" opens the mention menu — dates,
 * people and pages — as a page's name does (Notion's full-page database
 * name, seen 2026-11-10).
 */
export function DatabaseTitleField({ value, spans, onTitleChange, onKeyDown, className, ...rest }: {
  value: string;
  /** The name's formatting, when it has any. */
  spans?: readonly TextSpan[];
  /** The name typed, with its formatting: none is an empty list, the name written plain. */
  onTitleChange(title: string, spans: TextSpan[]): void;
  ariaLabel: string;
  className?: string;
  containerClassName?: string;
  placeholder?: string;
  element?: "div" | "h1";
  dataAttributes?: Partial<Record<`data-${string}` | "dir", string>>;
  /** "input" writes each change; "blur" when typing pauses and on leaving. */
  commitOn?: "input" | "blur";
  autoFocus?: boolean;
  pageId?: string;
  /** The page whose title the name is (a full-page database's): shown where it is named at each key, composing too (RichTextCellEditor). */
  names?: string;
  onKeyDown?(event: KeyboardEvent): boolean | undefined;
  onFocus?(): void;
  onFinish?(): void;
  handleRef?: Ref<RichTextCellHandle>;
}) {
  const mentions = useCommentMentionSnapshot();
  return (
    <RichTextCellEditor {...rest} className={`${editorStyles.plainMarks} ${className ?? ""}`} spans={spans} value={value}
      mentionSnapshot={mentions} holds="title" typedRules phoneName
      onChange={(change) => onTitleChange(change.value, change.spans ?? [])}
      onKeyDown={(event) => {
        const owned = onKeyDown?.(event);
        if (owned !== undefined) return owned;
        // Enter, with ⇧ or ⌘ too, keeps the caret in the name.
        return fieldEnter(event) ? true : undefined;
      }} />
  );
}
