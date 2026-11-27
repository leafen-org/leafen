import { useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { TextSpan } from "@/lib/types";
import type { Page } from "@/types";
import { hasExplicitPageIcon, PageIconGlyph } from "./PageIcon";
import { PageIconPicker } from "./PageIconPicker";
import type { PagePresentationChange } from "./pagePresentation";
import { useRenameTitleField } from "./RenameTitleField";
import styles from "./PageRenamePopover.module.css";
import { readsRightToLeft } from "@/lib/readingDirection";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

const POPOVER_WIDTH = 380;

// Its left edge for a start edge at `start`, kept inside the window: read
// from the right, its right edge is there and it reaches leftward.
function startingAt(start: number, rtl: boolean) {
  const left = rtl ? start - POPOVER_WIDTH : start;
  return Math.max(12, Math.min(Math.round(left), window.innerWidth - POPOVER_WIDTH - 12));
}

/**
 * Where the popover opens from a sidebar row's "…", as Notion's (seen
 * 2026-10-17 and 2026-10-20): its top left at the button's middle, 6px
 * under it, kept inside the window (its top right, read from the right).
 */
export function renamePopoverAt(trigger: Element | null | undefined): { top: number; left: number } | undefined {
  if (!trigger) return undefined;
  const rect = trigger.getBoundingClientRect();
  const top = Math.round(rect.top + rect.height / 2 + 6);
  return { top: Math.min(top, window.innerHeight - 48), left: startingAt(rect.left + rect.width / 2, readsRightToLeft(trigger)) };
}

/**
 * Where it opens from the menu a right click opened, as Notion's (seen
 * 2026-10-27): its top left 2px right of the pointer and 11px under it,
 * kept inside the window (its top right 2px left of it, read from the right).
 */
export function renamePopoverAtPoint(point: { x: number; y: number }): { top: number; left: number } {
  const rtl = readsRightToLeft(), top = Math.round(point.y + 11);
  return { top: Math.min(top, window.innerHeight - 48), left: startingAt(point.x + (rtl ? -2 : 2), rtl) };
}

/**
 * Where it opens from the top bar's piece of the page, as Notion's (seen
 * 2026-10-27): its top left 4px under the piece's, kept inside the window
 * (its top right under the piece's, read from the right). A row's or card's
 * from ⌘/'s menu opens so under the part of it the menu stood under (`rect`).
 */
export function renamePopoverUnder(piece: Element, rect: { left: number; right: number; bottom: number } = piece.getBoundingClientRect()): { top: number; left: number } {
  const rtl = readsRightToLeft(piece);
  return { top: Math.round(rect.bottom + 4), left: startingAt(rtl ? rect.right : rect.left, rtl) };
}

/**
 * A page's rename popover, as Notion's ⌘⇧R opens it from a page's title
 * (seen 2026-10-02): a 380×36 box at the window's top left with the page's
 * icon, which changes it, and its title, selected, with its mentions
 * (useRenameTitleField), a line more for a title that needs one (seen
 * 2026-11-10). The title changes as it is typed, as Notion's does (seen
 * 2026-10-20: its row and the page's title follow each key); Enter, Escape
 * or a click outside closes it, keeping what was typed.
 */
export function PageRenamePopover({ page, iconFallback, title, titleSpans, untitled, at, onTitleChange, onTitleShown, onPresentationChange, onClose }: {
  /** What it renames: a page, a row or a database, by its icon. */
  page: Pick<Page, "id" | "workspaceId" | "icon" | "iconType"> & { kind?: string; hasContent?: boolean };
  /** The icon shown when it has none of its own, as its sidebar row shows it. */
  iconFallback?: "file" | "database" | ReactNode;
  /** Where it opens instead: where a library's menu was, or by a sidebar row's "…" (`renamePopoverAt`). */
  at?: { top: number; left: number };
  /** The title as the page shows it when the popover opens. */
  title: string;
  /** Its spans as its reader reads them, while they spell it: its mentions and formatting. */
  titleSpans?: readonly TextSpan[];
  untitled: string;
  /** The title typed, with its spans (useRenameTitleField): none for plain words it opened without. */
  onTitleChange: (title: string, spans?: TextSpan[]) => void;
  /** The title as each key leaves it, shown before it is saved (lib/renameShownAtPress.ts). */
  onTitleShown?: (title: string, spans?: TextSpan[]) => void;
  /** Changes the icon from its button; without it the icon is only shown. */
  onPresentationChange?: (change: PagePresentationChange) => void;
  /** Runs once it has closed and kept the title. */
  onClose: () => void;
}) {
  const { t } = useTranslation("pageHeader");
  const fieldRef = useRef<HTMLDivElement>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const rename = useRenameTitleField({ title, spans: titleSpans, placeholder: untitled, className: styles.title, onTitleChange, onTitleShown, onDone: onClose });

  function close() {
    rename.finish();
    onClose();
  }

  function closePicker() {
    setPickerOpen(false);
    focusOpenedField(() => fieldRef.current?.querySelector<HTMLElement>("[data-page-rename-title]"));
  }

  function pick(change: PagePresentationChange) {
    onPresentationChange?.(change);
    closePicker();
  }

  const popover = (
    <>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("renamePage")} onClick={close} />
      <div className={styles.popover} role="dialog" aria-label={t("renamePage")} data-page-rename-popover
        style={at ? { top: at.top, left: at.left, right: "auto" } : undefined}>
        <div className={styles.iconWrap}>
          {onPresentationChange ? (
            <button type="button" className={styles.icon} aria-label={t("changeIcon")} aria-haspopup="dialog"
              aria-expanded={pickerOpen} data-page-rename-icon onClick={() => setPickerOpen((open) => !open)}>
              <PageIconGlyph page={page} size={16} fallback={iconFallback} />
            </button>
          ) : <span className={styles.icon} aria-hidden="true" data-page-rename-icon><PageIconGlyph page={page} size={16} fallback={iconFallback} /></span>}
          {pickerOpen && onPresentationChange ? (
            <PageIconPicker placement="inline" current={page} uploadTarget={{ workspaceId: page.workspaceId, pageId: page.id }}
              onPick={(emoji) => pick({ icon: emoji, iconType: "emoji" })}
              onPickImage={(url) => pick({ icon: url, iconType: "image" })}
              onPickCustomEmoji={(emoji) => pick({ icon: emoji.id, iconType: "custom_emoji" })}
              onPickBuiltInIcon={(value) => pick({ icon: value, iconType: "icon" })}
              onRemove={hasExplicitPageIcon(page) ? () => pick({ icon: "", iconType: "none" }) : undefined}
              onClose={closePicker} />
          ) : null}
        </div>
        <div ref={fieldRef} className={styles.titleWrap}>{rename.field}</div>
      </div>
    </>
  );
  return typeof document === "undefined" ? popover : createPortal(popover, document.body);
}
