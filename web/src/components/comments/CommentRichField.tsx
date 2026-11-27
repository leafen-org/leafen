import { useMemo, type Ref } from "react";
import type { TextSpan } from "@/lib/types";
import { RichTextCellEditor, type RichTextCellHandle } from "@/components/database/RichTextCellEditor";
import { commentBoxSends, useDisplayPreferences } from "@/lib/displayPreferences";
import { layerOpenOver, routeOverlayKeyDown } from "@/lib/overlayLayer";
import { useCommentMentionSnapshot } from "./commentMentionContext";
import styles from "../CommentsPanel.module.css";

export type CommentFieldHandle = RichTextCellHandle;

/**
 * A comment's text box, as Notion's (seen 2026-10-15): ⌘B, ⌘I, ⌘U, ⌘⇧S and
 * ⌘E mark the text, a selection shows the formatting bar, ⌘K links it, and @
 * mentions a date, a person or a page. Enter sends, or Cmd/Ctrl+Enter when
 * the account makes Enter break the line; Shift+Enter breaks it. An edit
 * saves the same way (Notion, seen 2026-10-29).
 */
export function CommentRichField({
  ariaLabel,
  autoFocus,
  className,
  containerClassName,
  dataAttributes,
  handleRef,
  pageId,
  placeholder,
  spans,
  onChange,
  onEscape,
  onFocus,
  onSubmit,
}: {
  ariaLabel: string;
  autoFocus?: boolean;
  className?: string;
  containerClassName?: string;
  dataAttributes?: Record<`data-${string}`, string>;
  handleRef?: Ref<CommentFieldHandle>;
  /** The page the comment is on, which the @ menu leaves out of its pages. */
  pageId?: string;
  placeholder?: string;
  spans: readonly TextSpan[];
  onChange: (spans: TextSpan[]) => void;
  /** Escape, once the @ menu is closed: true when the box took it, false to
   * leave it to what the box sits in. Without it the key passes on. */
  onEscape?: () => boolean;
  onFocus?: () => void;
  onSubmit: () => void;
}) {
  const { enterAddsNewLine } = useDisplayPreferences();
  const mentionSnapshot = useCommentMentionSnapshot();
  // Marked as a comment's box, whose keys put the page's bar away (typingFade.ts).
  const fieldAttributes = useMemo(() => ({ ...dataAttributes, "data-comment-field": "" }), [dataAttributes]);
  return (
    <RichTextCellEditor
      spans={spans}
      value={spans.map((span) => span.text).join("")}
      multiline
      commitOn="input"
      autoFocus={autoFocus}
      // A box not opened to write in has its editor made after the frame it shows in (RichTextCellEditor).
      afterPaint={!autoFocus}
      ariaLabel={ariaLabel}
      className={`${styles.commentField} ${className ?? ""}`}
      containerClassName={`${styles.commentFieldBox} ${containerClassName ?? ""}`}
      placeholder={placeholder}
      pageId={pageId}
      dataAttributes={fieldAttributes}
      mentionSnapshot={mentionSnapshot}
      commentMention
      toolbarLayout="row"
      handleRef={handleRef}
      onFocus={onFocus}
      onChange={(change) => onChange(change.spans ?? (change.value ? [{ text: change.value }] : []))}
      onKeyDown={(event) => {
        if (commentBoxSends(event, enterAddsNewLine)) {
          event.stopPropagation();
          onSubmit();
          return true;
        }
        if (event.key === "Escape") {
          if (onEscape?.()) {
            event.stopPropagation();
            return true;
          }
          // A picker opened over what the box sits in, the box keeping the
          // caret (a resting margin card's reaction, restingCardReaction.tsx):
          // Escape leaves the box alone and the picker stays, as Notion's
          // (seen 2026-11-26 in two tabs; its second Escape closes the picker).
          if (event.target instanceof Element && layerOpenOver(event.target)) {
            if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
            event.stopPropagation();
            return true;
          }
          // Nothing of the box's own took it: Escape closes what the box sits
          // in — a popover — before the field's editor can cancel the key, as
          // Notion's do (2026-10-16: a property's discussion stayed open); a
          // margin card, which Escape leaves open, and a box in no layer leave
          // it to the page.
          routeOverlayKeyDown(event);
          return event.defaultPrevented;
        }
        return undefined;
      }}
    />
  );
}
