import { undoDepth } from "@tiptap/pm/history";
import { createPageTitleEngine, type PageTitleEngine } from "./pageTitleEngine";
import { usePageTitleMentions } from "./usePageTitleMentions";
import { graphWireBounds } from "@/contentGraphWire.generated";
import { focusEditorNow, independentTiptapSelection } from "../editor/nativeTiptapFocus";
import { caretDownFromTitle } from "../editor/titleBodyCaret";
import { CommentIcon, type GlyphFrame, PhotoIcon, SmileyIcon } from "@/icons/leafen";
import { useEffect, useLayoutEffect, useRef, useState, type ComponentProps, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { activeNumberLocale } from "@/lib/i18n";
import { focusNextFrame, isCaretAtEnd, isEditableFullySelected, placeCaret, registerEditable, selectEditableContents } from "@/components/editor/focus";
import { lineArrowStep } from "@/lib/readingDirection";
import { pasteAtBodyStart, splitIntoBodyStart } from "@/components/editor/pageBodyStart";
import { notePageHistoryStep, requestPageUndo } from "@/components/editor/pageUndo";
import { chordLetter, matchesKeyboardShortcut, primaryModifier } from "@/lib/keyboardShortcuts";
import { nextCover } from "@/lib/covers";
import { useEmptyPageTitleCaret } from "@/lib/emptyPageTitleCaret";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { type BodyFocus, consumePageTitleFocus, enterBodyFromTitle, PAGE_TITLE_FOCUS_EVENT, scrollPastPageOptionsOnPhone, typeHeldTypingIntoTitle, waitForHeldComposition, whenPageTitleCanTakeFocus } from "@/lib/pageTitleFocus";
import { useOptionalProductToast } from "@/lib/productToast";
import type { TextSpan } from "@/lib/types";
import type { Page } from "@/types";
import { useSidebarLayout } from "../shell/AppShell";
import type { DatabaseDescriptionParts } from "./DatabaseDescription";
import { DatabasePageNameField, type DatabasePageName } from "./DatabasePageName";
import { DatabaseTitleText } from "../database/DatabaseTitleField";
import { cellSpans } from "../database/richTextCellModel";
import { titleWithClosedMentions } from "@/lib/closedMentions";
import { titlePaste } from "./titlePaste";
import { usePageTitleJoin } from "@/lib/inlineDatabaseRequests";
import type { PageTitleBase } from "@/lib/pageTitleFollow";
import { usePageTitleSave } from "./usePageTitleSave";
import { plainTitleWords, registerPhoneTitleTools } from "../editor/phoneTitleTools";
import { holdsOnlyCopiedDatabases } from "@/components/editor/blockClipboard";
import { pastedFieldSpans } from "@/components/editor/pastedLine";
import { pastesTextOverHtml } from "@/components/editor/pastedHtmlChoice";
import { titleHtmlSpans } from "@/components/editor/pastedHtmlLine";
import { EDITOR_PASTE_BOUNDS } from "@/components/editor/editorClipboardPolicy";
import { PageIconPicker } from "./PageIconPicker";
import { AnchoredIconPicker, usePageIconPickerAnchor } from "./AnchoredEmojiPicker";
import { hasExplicitPageIcon, isPagePictureIcon, PageIconGlyph } from "./PageIcon";
import type { PagePresentationChange } from "./pagePresentation";
import { PageRenamePopover } from "./PageRenamePopover"; import { pageTitleKept } from "@/lib/typedDraftSender";
import { onPageRenameRequest } from "./pageRenameRequest";
import styles from "./PageHeader.module.css";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import { keysFocusHolder } from "@/lib/lastPress";

// The head's options stay up while the keys' focus is in them (data-keys-within).
keysFocusHolder(styles.controls);
const DEFAULT_PAGE_ICONS = ["😀", "✨", "💡", "📌", "📝", "📚", "🚀", "🌿", "☕", "🎯"];

/** A random emoji, as "Add icon" gives a page before one is chosen. */
export function randomPageIcon() {
  return DEFAULT_PAGE_ICONS[Math.floor(Math.random() * DEFAULT_PAGE_ICONS.length)] ?? "😀";
}

// A touch screen's head options draw their marks across their whole 16px box, as Notion's phone and iPad do
// (seen 2026-11-13 at 402 and 834: the face 16×16, the picture 16×12.3, the bubble 16×15.6), where Phosphor's leave a margin round them.
const TOUCH_HEAD_MARKS: Record<"icon" | "cover" | "comment", GlyphFrame> = {
  icon: { drawn: [1.875, 1.875, 16.25, 16.25], notion: [0, 0, 20, 20] },
  cover: { drawn: [1.875, 3.125, 16.25, 13.75], notion: [0, 2.3, 20, 15.4] },
  comment: { drawn: [2.5, 3.16, 15, 14.84], notion: [0, 0.26, 20, 19.48] },
};

function PageHeaderBody({
  page,
  onTitleChange,
  onTitleTyped,
  onPresentationChange,
  onFocusBody,
  readOnly = false,
  staticReadOnlySemantics = false,
  onOpenComments,
  onStartComment,
  backlinks,
  verifyControl,
  verifiedBadge,
  databaseDescription,
  commentsHostId,
  titlePlaceholder,
  compact = false,
  databaseHead = false,
  databaseName,
}: {
  page: Page;
  titlePlaceholder?: string;
  /** Saves a title typed here, with the title it was typed on (pageTitleFollow.ts); answers with the page as saved. */
  onTitleChange: (title: string, base?: PageTitleBase, spans?: TextSpan[]) => unknown;
  /** The title as it is typed, before it is saved: the places naming the page show it at once, as Notion's (seen 2026-11-09). */
  onTitleTyped?: (title: string) => void;
  onPresentationChange: (change: PagePresentationChange) => void;
  onFocusBody: (selection: BodyFocus) => void;
  readOnly?: boolean;
  staticReadOnlySemantics?: boolean;
  onOpenComments?: () => void;
  /** A new comment on the page from a phone's tools in its title, written in a sheet (CommentsPanel); else theirs is `onOpenComments`'. */
  onStartComment?: () => void;
  /** "N개의 백링크", first among the options. */
  backlinks?: ReactNode;
  /** The "Verify" among the header's hover controls. */
  verifyControl?: ReactNode;
  /** A verified page's "Verified" above its title. */
  verifiedBadge?: ReactNode;
  /** A full-page database's description: its control after "Verify" and its field under the title. */
  databaseDescription?: DatabaseDescriptionParts;
  /** The id of the place under the title where the page's discussion shows. */
  commentsHostId?: string;
  /** A side peek's compact head: the options first, a 36px icon, the 32px title. */
  compact?: boolean;
  /** A full-page database's head: its icon small beside its name, as Notion's. */
  databaseHead?: boolean;
  /** A full-page database named by its one source: its title is that name, rich text (DatabasePageName). */
  databaseName?: DatabasePageName;
}) {
  const { t } = useTranslation("pageHeader");
  const { phone, touch } = useSidebarLayout(true);
  const marks = touch ? TOUCH_HEAD_MARKS : undefined;
  // An untitled full-page database is named as its path names it (untitledPageDisplayTitle,
  // Notion's, seen 2026-10-01); on a phone an untitled page's title stands empty (a
  // database's row, drawn by its own head, keeps "새 페이지"; Notion, seen 2026-11-01).
  const untitled = titlePlaceholder ?? (page.kind === "database" ? untitledPageDisplayTitle(page) : phone ? "" : t("untitled"));
  const titleRef = useRef<HTMLSpanElement>(null), namedByDatabase = Boolean(databaseName);
  const coreRef = useRef<PageTitleEngine | null>(null);
  const titleLease = useRef<{ active: boolean; core: PageTitleEngine } | null>(null);
  const titleMentions = usePageTitleMentions(page, readOnly, () => coreRef.current);
  const latest = useRef({ readOnly, staticReadOnlySemantics, untitled, label: t("pageTitle"), onTitleTyped });
  latest.current = { readOnly, staticReadOnlySemantics, untitled, label: t("pageTitle"), onTitleTyped };
  const comment = onStartComment ?? onOpenComments;
  const callbacks = useRef({ keyDown: onTitleKeyDown, paste: onTitlePaste, commit: commitTitle, typed: typedTitle, shown: shownTitle, menus: titleMentions, comment });
  callbacks.current = { keyDown: onTitleKeyDown, paste: onTitlePaste, commit: commitTitle, typed: typedTitle, shown: shownTitle, menus: titleMentions, comment };
  const focusBody = useRef(onFocusBody);
  focusBody.current = onFocusBody;
  const toast = useOptionalProductToast();
  const iconButtonRef = useRef<HTMLButtonElement>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  // The title ⌘⇧R opened the rename popover with, while it is open, and
  // where the top bar's path piece asked it to open.
  const [renaming, setRenaming] = useState<string | null>(null);
  // Its spans as the title shows them then: its mentions, which the popover shows and keeps.
  const renamingSpans = useRef<TextSpan[] | undefined>(undefined);
  const renameFrom = useRef({ page, databaseName });
  renameFrom.current = { page, databaseName };
  function openRename(title: string) {
    const typed = coreRef.current?.value(), { page: now, databaseName: name } = renameFrom.current;
    const shown = typed ? { value: typed.title, spans: typed.spans ? [...typed.spans] : undefined }
      : { value: now.title, spans: cellSpans(name?.spans ?? now.titleSpans, now.title) };
    renamingSpans.current = shown.value === title ? shown.spans : undefined;
    setRenaming(title);
  }
  const [renameAt, setRenameAt] = useState<{ top: number; left: number } | null>(null);
  const hasIcon = hasExplicitPageIcon(page);
  const iconPickerAnchor = usePageIconPickerAnchor(pickerOpen && hasIcon && !readOnly, iconButtonRef);

  // The body's Undo while the title holds the caret a line just brought it.
  const undoJoin = useRef<(() => void) | null>(null);
  function normalizedTitleText() { return coreRef.current?.read() ?? page.title; }
  usePageTitleJoin(page.id, () => {
    const core = coreRef.current;
    return !core || readOnly || page.kind === "database" ? null : {
      title: normalizedTitleText(),
      set(text: string, caret?: number, undo?: () => void) {
        core.replace(text, caret, true);
        titleSaving.save();
        if (caret === undefined) return;
        undoJoin.current = undo ?? null;
        core.editor.view.focus();
      },
      caretAtEnd() { focusEditorNow(core.editor, "end"); },
      caretAtStart() { focusEditorNow(core.editor, "start"); },
      undoTyped() {
        if (!core.editor.can().undo()) return false;
        core.editor.view.focus();
        core.editor.commands.undo();
        titleSaving.save();
        return true;
      },
    };
  });
  // A title is kept as typed, the spaces at its ends too, as Notion keeps them (titleMerge.ts).
  function normalizedTitle() { return normalizedTitleText(); }
  // Each step of the title's history goes in the page's when it is made.
  const titleSteps = useRef({ depth: 0, stepping: false });
  function titleStep(forward: boolean) {
    const core = coreRef.current, steps = titleSteps.current;
    if (!core) return Promise.resolve(false);
    steps.stepping = true;
    core.editor.view.focus();
    const stepped = forward ? core.editor.commands.redo() : core.editor.commands.undo();
    steps.stepping = false;
    steps.depth = undoDepth(core.editor.state);
    titleSaving.save();
    return Promise.resolve(stepped);
  }
  function shownTitle() { const typed = coreRef.current?.read(); if (typed !== undefined) latest.current.onTitleTyped?.(typed); }
  function typedTitle() {
    titleSaving.typed(); shownTitle();
    const core = coreRef.current, steps = titleSteps.current;
    if (!core || steps.stepping) return;
    const depth = undoDepth(core.editor.state);
    if (depth > steps.depth) notePageHistoryStep(page.id, `title:${page.id}:${depth}:${Date.now()}`, titleStep);
    steps.depth = depth;
  }
  useLayoutEffect(() => {
    const element = titleRef.current;
    if (!element) return;
    let held = titleLease.current;
    if (!held) {
      const lease = { active: true };
      const core = createPageTitleEngine(element, { title: page.title, spans: page.titleSpans }, { maxRunes: graphWireBounds.titleRunes,
      attributes: () => ({ class: styles.title, spellcheck: "true", "data-page-title-editor": "true", "data-placeholder": latest.current.untitled,
        ...(latest.current.staticReadOnlySemantics ? {} : { role: "textbox", tabindex: "0", "aria-label": latest.current.label,
          "aria-multiline": "false", "aria-placeholder": latest.current.untitled }) }),
      editable: () => lease.active && !latest.current.readOnly,
      changed: () => callbacks.current.typed(), composed: () => callbacks.current.shown(), blurred: () => callbacks.current.commit(),
      keyDown: event => callbacks.current.keyDown(event), paste: event => callbacks.current.paste(event),
      updated: (remap) => callbacks.current.menus.updated(remap),
      datePressed: mention => callbacks.current.menus.datePressed(mention, titleRef.current ?? mention),
      });
      held = Object.assign(lease, { core }); titleLease.current = held;
    }
    held.active = true; coreRef.current = held.core;
    return () => {
      held.active = false;
      queueMicrotask(() => {
        if (held.active) return;
        held.core.destroy();
        if (coreRef.current === held.core) coreRef.current = null;
        if (titleLease.current === held) titleLease.current = null;
      });
    };
    // Keyed by native page identity, title values project; the plain title's element, gone while a database's name field stands in its place, takes a new editor as it comes back (a refused database start drew no title).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [namedByDatabase]);
  // A phone's editing tools stand under the title's caret too, its "@" the
  // title's and its comment a new one on the page (NativePhoneToolbar).
  useEffect(() => {
    const element = titleRef.current, core = coreRef.current;
    return element && core ? registerPhoneTitleTools(element, { editor: core.editor,
      mention: () => callbacks.current.menus.mention(), comment: () => callbacks.current.comment, words: plainTitleWords(core.editor) }) : undefined;
  }, [namedByDatabase]);
  const nameFieldComing = page.kind === "database" && (Boolean(databaseName) || !databaseDescription);
  useLayoutEffect(() => { coreRef.current?.refresh(); }, [page.title, readOnly, untitled, t, staticReadOnlySemantics]);
  // A database's name field owns its title once it shows (DatabasePageNameField): the page's own title field, made
  // before it came, writes nothing, as it wrote its old words back over a rename (2026-11-10: a member's, mention marks
  // and all, after a rename from the path).
  const titleSaving = usePageTitleSave({ page, readOnly: readOnly || Boolean(databaseName), engine: coreRef, onTitleChange, onTitleTyped });

  // The request waits until the title can take it: a page opening reads as
  // read-only until it has loaded (2026-10-31: a page made from the add
  // window took the request then and never had its caret).
  useLayoutEffect(() => {
    // A database page's title is its name's field (DatabasePageNameField),
    // which takes the request once the name has come; the plain title shown
    // until then doesn't, or what was typed went into it and was lost as
    // the name's field replaced it (2026-11-09: "빠른 이름" kept " 이름").
    // A linked database's page keeps the plain title, which takes it.
    if (readOnly || nameFieldComing) return;
    let stop = () => {};
    // The title's editor may come into its place a few frames after the page
    // opens, and the page behind a window that made it stays inert until the
    // window has closed: it is waited for while the request stands.
    const take = () => {
      stop();
      stop = whenPageTitleCanTakeFocus(page.id, () => titleRef.current, (title) => {
        if (waitForHeldComposition(take) || !consumePageTitleFocus(page.id)) return;
        placeCaret(title, "end");
        scrollPastPageOptionsOnPhone(title);
        // What was typed while the page was being made goes in as typed here.
        typeHeldTypingIntoTitle();
      });
    };
    take();
    // A page that opened before its request was made takes it when it comes.
    const asked = (event: Event) => { if ((event as CustomEvent<string>).detail === page.id) take(); };
    window.addEventListener(PAGE_TITLE_FOCUS_EVENT, asked);
    return () => { stop(); window.removeEventListener(PAGE_TITLE_FOCUS_EVENT, asked); };
  }, [page.id, nameFieldComing, readOnly]);
  // A database page opens with no caret in its title, untitled or not
  // (Notion's, seen 2026-11-07); one just made asks for it (requestPageTitleFocus).
  useEmptyPageTitleCaret(page.id, readOnly || page.kind === "database", titleRef);

  // The top bar's current path piece opens the rename popover under it.
  useEffect(() => readOnly ? undefined : onPageRenameRequest((request) => {
    if (request.pageId !== page.id) return;
    const title = normalizedTitle();
    commitTitle();
    setRenameAt(request.at);
    openRename(title);
  }), [page.id, readOnly]);

  function commitTitle() {
    if (imeComposing(coreRef.current?.editor.view)) return;
    const title = normalizedTitle();
    coreRef.current?.replace(title);
    titleSaving.save();
  }

  function onTitleKeyDown(event: KeyboardEvent) {
    if (imeComposing(event)) return;
    // Escape in a page's title, a full-page database's too, keeps the caret
    // there and takes it to the title's start, choosing nothing: what is
    // typed next goes before the title (Notion's, measured once 2026-11-15
    // in Chrome 1440 with no script before the key: Escape at the end of a
    // page's title, then a letter, which went in before the title; a
    // full-page database's title not measured again).
    if (event.key === "Escape" && !event.defaultPrevented && !(event.metaKey || event.ctrlKey || event.altKey || event.shiftKey)) {
      event.preventDefault();
      if (coreRef.current) focusEditorNow(coreRef.current.editor, "start");
      return;
    }
    if (undoJoin.current && !["Shift", "Meta", "Control", "Alt"].includes(event.key)) {
      const undo = undoJoin.current;
      undoJoin.current = null;
      if (primaryModifier(event) && !event.shiftKey && !event.altKey && chordLetter(event) === "z") {
        event.preventDefault();
        undo();
        return;
      }
    }
    // The page's history holds the title's steps too: there is one ⌘Z, as Notion's (seen 2026-11-05).
    if (primaryModifier(event) && !event.altKey && ["z", "y"].includes(chordLetter(event))
      && requestPageUndo(page.id, event.shiftKey || chordLetter(event) === "y")) {
      event.preventDefault();
      return;
    }
    // ⌘⇧R in the title opens the page's rename popover, as Notion's does
    // there (seen 2026-10-02) — not in the body, where it does nothing.
    if (!readOnly && matchesKeyboardShortcut("renamePage", event)) {
      event.preventDefault();
      const title = normalizedTitle();
      commitTitle();
      openRename(title);
      return;
    }
    if (primaryModifier(event) && !event.shiftKey && !event.altKey && chordLetter(event) === "a") {
      const element = titleRef.current;
      if (!element) return;
      event.preventDefault();
      if (normalizedTitle() === "" || isEditableFullySelected(element)) {
        element.blur();
        onFocusBody("select");
      } else {
        selectEditableContents(element);
      }
    } else if (fieldEnter(event)) {
      event.preventDefault();
      // A full-page database's title has no line to go to.
      if (page.kind !== "database" && !readOnly) enterBodyFromSplitTitle();
      else onFocusBody("start");
    } else if (event.key === "ArrowDown" && !(event.metaKey || event.ctrlKey || event.altKey || event.shiftKey)) {
      // On the title's last line ↓ goes to the body's first line under the
      // caret, as Notion's (titleBodyCaret); a body with no line yet takes it at its start.
      const title = titleRef.current;
      const moved = title ? caretDownFromTitle(title, page.id) : false;
      if (moved === false) return;
      event.preventDefault();
      if (moved === null) onFocusBody("start");
    } else if ((event.key === "ArrowRight" || event.key === "ArrowLeft") && !(event.metaKey || event.ctrlKey || event.altKey || event.shiftKey)) {
      // The arrow on at its end goes to the body: ← where the title reads from the right (lineArrowStep).
      if (!titleRef.current || !isCaretAtEnd(titleRef.current) || lineArrowStep(event.key, titleRef.current, normalizedTitleText()) < 0) return;
      event.preventDefault();
      onFocusBody("start");
    }
  }

  // Enter splits the title at the caret: what follows starts a new first
  // line of the body, as Notion's (seen 2026-11-01: "ab| cd" left
  // "ab" and a first line " cd"; at the end, an empty one). The body takes a
  // split as one step of its own, the caret at the line's start and one Undo
  // putting the words back (Notion's, seen 2026-11-06; pageBodyStart).
  function enterBodyFromSplitTitle() {
    const element = titleRef.current;
    const text = normalizedTitleText();
    const selection = element ? independentTiptapSelection(element) : null;
    const start = selection?.start ?? text.length, end = selection?.end ?? start;
    if (start < text.length && element && splitIntoBodyStart(page.id, text.slice(end), { before: text, after: text.slice(0, start) }, element)) {
      coreRef.current?.replace(text.slice(0, start), start, true);
      titleSaving.save();
      return;
    }
    if (start < text.length) {
      coreRef.current?.replace(text.slice(0, start), start);
      titleSaving.save();
    }
    enterBodyFromTitle(element, (selection) => focusBody.current(selection), text.slice(end), page.id);
  }

  function onTitlePaste(event: ClipboardEvent) {
    const core = coreRef.current, element = titleRef.current;
    if (readOnly || !event.clipboardData || !core || !element) return;
    event.preventDefault();
    // A copied database doesn't go in a title, as Notion's (real ⌘V, seen 2026-11-04).
    if (holdsOnlyCopiedDatabases(event.clipboardData)) return;
    const text = core.read(), selection = independentTiptapSelection(element);
    const around = text.slice(0, selection?.start ?? text.length) + text.slice(selection?.end ?? text.length);
    const paste = titlePaste(event.clipboardData.getData("text/plain"), around, page.kind !== "database");
    // Its Markdown is read, as Notion's title reads a pasted line (beside
    // HTML its text loses nothing of too, pastedHtmlChoice.ts), another app's
    // HTML as a line of the body reads it (titleHtmlSpans), and a plain one
    // ends as typing it would ("a--" is "a—"; seen 2026-11-11).
    if (paste.into === "title") {
      const html = pastesTextOverHtml(event.clipboardData) ? "" : event.clipboardData.getData("text/html");
      const spans = html ? titleHtmlSpans(event.clipboardData, paste.text) : pastedFieldSpans(paste.text, EDITOR_PASTE_BOUNDS);
      core.insert(spans ?? paste.text, !html && !spans);
    } else if (paste.into === "refused") toast?.notify(t("titlePasteTooLong", { max: graphWireBounds.titleRunes.toLocaleString(activeNumberLocale()) }), "error");
    else if (paste.into === "body") pasteAtBodyStart(page.id, event.clipboardData, element, message => toast?.notify(message, "error"));
  }

  function addCover() {
    // Its button goes, and the focus with it: nothing holds it, as in Notion (seen 2026-11-04).
    onPresentationChange({ cover: nextCover(page.cover), coverPosition: 50 });
  }

  function addIcon() {
    if (!hasIcon) onPresentationChange({ icon: randomPageIcon(), iconType: "emoji" });
    setPickerOpen(true);
  }

  function removeIcon() {
    onPresentationChange({ icon: "", iconType: "none" });
    setPickerOpen(false);
  }

  function closePicker() {
    setPickerOpen(false);
  }

  // A picture heads the page in a 140px box, an emoji in 78, as Notion's (measured 2026-11-01).
  const picture = !compact && !databaseHead && isPagePictureIcon(page);
  const iconSize = compact || databaseHead ? 36 : picture ? 140 : 78;

  const icon = hasIcon ? (
    <div className={styles.iconWrap}>
      {readOnly ? (
        <span className={styles.bigIcon} aria-hidden="true" data-icon-picture={picture ? "true" : undefined}>
          <PageIconGlyph page={page} size={iconSize} box={iconSize} fallback="none" />
        </span>
      ) : (
        <button
          ref={iconButtonRef}
          type="button"
          className={styles.bigIcon}
          aria-label={t("changeIcon")}
          aria-haspopup="dialog"
          aria-expanded={pickerOpen}
          onClick={() => setPickerOpen(true)}
          data-icon-picture={picture ? "true" : undefined}
        >
          <PageIconGlyph page={page} size={iconSize} box={iconSize} fallback="none" />
        </button>
      )}
      {!readOnly && pickerOpen && iconPickerAnchor && (
        <AnchoredIconPicker anchor={iconPickerAnchor} onClose={() => closePicker()} picker={{
          returnFocusRef: iconButtonRef,
          returnFocusFromKeysOnly: true,
          pageIcon: true, current: page,
          uploadTarget: { workspaceId: page.workspaceId, pageId: page.id },
          onPick: (emoji) => {
            onPresentationChange({ icon: emoji, iconType: "emoji" });
            closePicker();
          },
          onShuffle: (icon, iconType) => onPresentationChange({ icon, iconType }),
          onPickImage: (url) => {
            onPresentationChange({ icon: url, iconType: "image" });
            closePicker();
          },
          onPickCustomEmoji: (emoji) => {
            onPresentationChange({ icon: emoji.id, iconType: "custom_emoji" });
            closePicker();
          },
          onPickBuiltInIcon: (value) => {
            onPresentationChange({ icon: value, iconType: "icon" });
            closePicker();
          },
          onRemove: removeIcon,
        }} />
      )}
    </div>
  ) : null;

  return (
    <div
      className={styles.header}
      data-has-cover={page.cover ? "true" : "false"}
      data-has-icon={hasIcon && !databaseHead ? "true" : "false"} data-database-head-icon={hasIcon && databaseHead ? "true" : undefined}
      data-database-head={databaseHead ? "true" : undefined}
      data-title-empty={page.title === "" ? "true" : "false"}
      data-compact={compact ? "true" : undefined}
      data-page-header-root
    >
      {!databaseHead && icon}

      {/* The options sit under the icon, over the title, as Notion's (seen
          2026-10-17; a phone's head without a cover leads with them, as
          Notion's phone's — PageHeader.module.css). A page the person can't edit — locked, or theirs only to comment on —
          keeps the comment entry above its title, and a locked one its verification, as Notion's locked page does
          (seen 2026-10-08, 2026-11-14 and 2026-11-16), a locked database its description's showing (useDatabaseDescription); the icon and cover ones go. */}
      {(backlinks || (readOnly ? Boolean(onOpenComments || verifyControl || databaseDescription?.control) : (!hasIcon || !page.cover || onOpenComments || verifyControl || databaseDescription?.control))) && (
        <div
          className={styles.controls}
          role="toolbar"
          aria-label={t("pageOptions")}
          data-page-header-controls
        >
          {/* As Notion's (seen 2026-10-29): backlinks first, for anyone who reads the page. */}
          {backlinks}
          {!hasIcon && !readOnly && (
            <div className={styles.addIconWrap}>
              <button
                ref={iconButtonRef}
                type="button"
                className={styles.ctrlBtn}
                aria-haspopup="dialog"
                aria-expanded={pickerOpen}
                data-page-header-control
                onClick={addIcon}
              >
                <SmileyIcon size="tiny" weight="fill" frame={marks?.icon} aria-hidden="true" />
                <span>{t("addIcon")}</span>
              </button>
              {pickerOpen && (
                <PageIconPicker
                  returnFocusRef={iconButtonRef}
                  returnFocusFromKeysOnly
                  pageIcon current={page}
                  placement="inline"
                  uploadTarget={{ workspaceId: page.workspaceId, pageId: page.id }}
                  onPick={(emoji) => {
                    onPresentationChange({ icon: emoji, iconType: "emoji" });
                    closePicker();
                  }}
                  onShuffle={(icon, iconType) => onPresentationChange({ icon, iconType })}
                  onPickImage={(url) => {
                    onPresentationChange({ icon: url, iconType: "image" });
                    closePicker();
                  }}
                  onPickCustomEmoji={(emoji) => {
                    onPresentationChange({ icon: emoji.id, iconType: "custom_emoji" });
                    closePicker();
                  }}
                  onPickBuiltInIcon={(value) => {
                    onPresentationChange({ icon: value, iconType: "icon" });
                    closePicker();
                  }}
                  onClose={() => closePicker()}
                />
              )}
            </div>
          )}
          {!page.cover && !readOnly && (
            <button
              type="button"
              className={styles.ctrlBtn}
              aria-label={t("addPageCover")}
              data-page-header-control
              onClick={addCover}
            >
              <PhotoIcon size="tiny" weight="fill" frame={marks?.cover} aria-hidden="true" />
              <span>{t("addCover")}</span>
            </button>
          )}
          {/* As Notion's (seen 2026-10-01): verification before the comment, offered on a locked page too where its caller gives it. */}
          {verifyControl}
          {onOpenComments && (
            <button
              type="button"
              className={styles.ctrlBtn}
              aria-label={t("addPageComment")}
              data-page-header-control
              onClick={onOpenComments}
            >
              <CommentIcon size="tiny" weight="fill" frame={marks?.comment} aria-hidden="true" />
              <span>{t("addComment")}</span>
            </button>
          )}
          {databaseDescription?.control}
        </div>
      )}

      {verifiedBadge}
      <div className={styles.titleRow}>
        {databaseHead && icon}
        <h1
          className={styles.titleHeading}
          aria-label={titleWithClosedMentions(page.title, page.titleSpans) || untitled}
          // The title takes its direction from its words, as a text block does.
          dir="auto"
        >
          {databaseName && !readOnly ? (
            <DatabasePageNameField pageId={page.id} title={page.title} name={databaseName} className={styles.title} label={t("pageTitle")}
              untitled={untitled} onFocusBody={onFocusBody} onRename={openRename} />
          ) : databaseName ? (
            // A database's name read only — locked, say — is its words: the plain
            // title's editor took its place only when the page opened read only,
            // so one locked while open showed nothing (2026-11-10). Its own key
            // keeps it apart from the plain title's element, whose editor's
            // words would stay beside its own (the name twice). Without a name
            // it keeps its gray "새 데이터베이스", as Notion's locked one does
            // (seen 2026-11-11).
            cellSpans(databaseName.spans, page.title) || page.title
              ? <span key="database-name" className={styles.title}><DatabaseTitleText title={page.title} spans={databaseName.spans} /></span>
              : <span key="database-name" className={styles.title} data-empty="true" data-placeholder={untitled}><br /></span>
          ) : <span
            key="page-title"
            ref={(element) => {
              titleRef.current = element;
              const release = registerEditable(`title:${page.id}`, element);
              return () => {
                titleRef.current = null;
                release();
              };
            }}
            className={styles.title}

          />}
        </h1>
      </div>
      {titleMentions.element}
      {renaming !== null ? (
        <PageRenamePopover page={page} title={renaming} titleSpans={renamingSpans.current} untitled={untitled}
          onTitleChange={(title, spans) => databaseName && spans ? databaseName.onChange(title, spans) : onTitleChange(title, undefined, spans)}
          onPresentationChange={onPresentationChange} at={renameAt ?? undefined} onTitleShown={onTitleTyped} kept={databaseName ? undefined : pageTitleKept(page.id, () => page)}
          onClose={() => {
            const fromPath = renameAt !== null;
            setRenaming(null); setRenameAt(null);
            const title = () => titleRef.current ?? document.querySelector<HTMLElement>(`[data-database-page-name="${CSS.escape(page.id)}"]`);
            if (!fromPath) focusNextFrame(() => { const shown = title(); return shown ? { field: shown, position: "end" } : null; });
          }} />
      ) : null}
      {databaseDescription?.field}
      <div
        id={commentsHostId ?? `page-comments-inline-${page.id}`}
        className={styles.pageCommentsInlineHost}
        data-page-comments-inline-host
      />
    </div>
  );
}

export function PageHeader(props: ComponentProps<typeof PageHeaderBody>) {
  return <PageHeaderBody key={props.page.id} {...props} />;
}
