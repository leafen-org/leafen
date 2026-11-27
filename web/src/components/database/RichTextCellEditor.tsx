import { type ReactNode, type Ref, useContext, useEffect, useImperativeHandle, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { TextSpan } from "@/lib/types";
import type { BlockPickerSnapshot, MentionItem } from "../editor/blockPickerModel";
import editorStyles from "../editor/editorStyles.generated.module.css";
import { FieldPagesContext, useMentionSnapshot } from "../editor/mentionSnapshotContext";
import { useProductPages } from "../product/productPagesContext";
import { useFieldMenus } from "../editor/useFieldMenus";
import { cellAnchor } from "../editor/anchoredSubPage";
import { pmOffsetAt } from "../editor/nativeTiptapInline";
import { focusEditorNow } from "../editor/nativeTiptapFocus";
import { TextSelection } from "@tiptap/pm/state";
import { pasteAtBodyStart } from "../editor/pageBodyStart";
import { pastedLines, titleCellPaste, titlePaste } from "../page/titlePaste";
import { i18next } from "@/i18n";
import { graphWireBounds } from "@/contentGraphWire.generated";
import { activeNumberLocale } from "@/lib/i18n";
import { useOptionalProductToast } from "@/lib/productToast";
import { cellRangeFormat, cellSpansOrText, colorCellRange, linkCellRange, toggleCellMark, unlinkCellRange } from "./richTextCellModel";
import { createRichTextCellEngine, fieldElementAttributes, type CellCommentPending, type RichTextCellEngine, type RichTextCellChange } from "./richTextCellEngine";
import type { FieldKept } from "./fieldTypedSave";
import { printedFieldHtml } from "./RichTextCellShown";
import { afterNextPaint } from "@/lib/afterPress";
import { sleepUntilWoken } from "@/lib/sleepingFields";
import { RichTextCellToolbar } from "./RichTextCellToolbar";
import { CellPhoneStrip } from "./CellPhoneStrip";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { registerPhoneTitleTools, type PhoneTitleWords } from "../editor/phoneTitleTools";
import { selectionColorChange } from "../editor/selectionToolbarModel";
import { rememberUsedEditorColor } from "../editor/colorMemory";
import { keepWritingLookWhileCaretStays } from "./richCellCaretKept";
import { showTypedPageTitle } from "@/lib/pageNames";
import { imeComposing } from "@/lib/fieldKeyBoundary";

export type { RichTextCellChange } from "./richTextCellEngine";
type MentionTrigger = { query: string; start: number; end: number; anchor: { left: number; top: number; bottom: number } };

/** What a field's owner does to it from outside: a comment box's @ button, or its emptying once sent. */
export type RichTextCellHandle = {
  focus(): void;
  /** Writes "@" at the caret, opening the @ menu there. */
  mention(): void;
  clear(): void;
  /** Undoes what was typed in the field, the caret going there at once, as a page title's; whether there was any. */
  undoTyped(): boolean;
  /**
   * Takes the caret to the field's end at once — Tiptap's focus comes a frame
   * later, and letters played into the field meanwhile went nowhere
   * (2026-11-09: "빠른 이름" typed while a database was made kept "른 이름").
   */
  focusEnd(): void;
  /** Chooses all its words, as "이름 바꾸기" does a database's name. */
  selectAll(): void;
  /** Puts the caret so many characters in. */
  caretAt(offset: number): void;
  /** Where the field's words are chosen, and how many it holds. */
  range(): { start: number; end: number; length: number };
  /** Shows these words, as its owner's Undo or a joined line writes them; with a caret, it goes there. */
  set(spans: readonly TextSpan[], caret?: number): void;
  /** Publishes the words shown now, a composition's too, before its owner reads them: a send button's press (fieldKeyBoundary.ts). */
  publish(): void;
  /** Whether an input method is composing in it now. */
  composing(): boolean;
};

/** Tiptap owns property input; native property callbacks own its persistence. */
export function RichTextCellEditor(props: {
  spans: readonly TextSpan[] | undefined;
  value: unknown;
  multiline?: boolean;
  /** ⇧Enter puts a line in a field Enter ends. */
  shiftBreaks?: boolean;
  /** A multiline field Enter still ends, ⇧Enter putting its lines. */
  enterEnds?: boolean;
  commitOn?: "input" | "blur";
  autoFocus?: boolean;
  ariaLabel: string;
  className?: string;
  /** The class of the box the field sits in. */
  containerClassName?: string;
  placeholder?: string;
  pageId?: string;
  /** The property whose cell of `pageId` this is: a sub-page made from its "@" hangs on it (anchoredSubPage.ts). */
  propertyId?: string;
  element?: "div" | "h1";
  dataAttributes?: Partial<Record<`data-${string}` | "dir", string>>;
  onChange: (change: RichTextCellChange) => void;
  onFinish?: () => void;
  /** Enter ends the field and goes on from it. */
  onEnter?: () => void;
  /** A key the owner takes first; see the engine's `keyDown`. */
  onKeyDown?: (event: KeyboardEvent) => boolean | undefined;
  /**
   * What the field does with pasted lines, which a one-line field otherwise
   * joins: "keep" holds them, shown on one line until the field is edited (a
   * table's cell), and a page's id makes the field that page's title, with a
   * title's paste (titlePaste.ts).
   */
  pastedLines?: "keep" | { titleOf: string };
  /** An item's title in a cell: a paste takes a title's paste (titleCellPaste); ↑ ↓ ⌘↑ ⌘↓ keep the caret in it (the engine's cellArrow). */
  titleCell?: boolean;
  /** A text cell or a description: a paste takes a text's paste (titleCellPaste with a text's bound); the arrows as titleCell's. */
  textCell?: boolean;
  /**
   * The characters it holds, a title's or a text's, typing past them refused
   * as a page's title refuses it (fieldTakes); titleCell and textCell hold
   * theirs.
   */
  holds?: "title" | "text";
  /**
   * The page whose title it is (a row's, in its table cell or its page's
   * head): what is typed shows wherever the page is named at each key, a
   * syllable still composing too, as Notion's (the table row under a row's
   * side peek, seen 2026-11-21 in macOS Chrome with the Korean input method).
   */
  names?: string;
  /**
   * The words while an input method composes them, at each change (`onChange`
   * waits for the composition to end): a name typed in a rename box shows
   * where its page is named with its last syllable composing too.
   */
  onComposed?: (words: string) => void;
  onFocus?: () => void;
  /** The selection bar's layout; see SelectionToolbarView. */
  toolbarLayout?: "panel" | "row";
  /** What the @ menu offers, where the field isn't in a page body's editing. */
  mentionSnapshot?: BlockPickerSnapshot | null;
  /**
   * A comment box's "@": the menu offers its dates, and a bare "@" shows the
   * row picked in grey after it — its name, or for a reminder what to type —
   * as Notion's comment box does (seen 2026-10-28; its page body shows none).
   */
  commentMention?: boolean;
  /** A database cell: its "@" reminds, and ":", "[[", "+" and "$$" work as in a line, as Notion's (seen 2026-11-05). */
  lineRules?: boolean;
  /**
   * A paste keeps its marks and has its Markdown read, as a comment box's,
   * though typing takes no line's shortcuts: a database's description, as
   * Notion's (seen 2026-11-11).
   */
  pastedMarks?: boolean;
  /** Typing takes a line's shortcuts within words ("--" "—", "**b**"), its menus none: a database's title, as Notion's (seen 2026-11-11). */
  typedRules?: boolean;
  /** A title that takes titles saved elsewhere as they come, merged with the typing here (the engine's `follows`). */
  follows?: boolean;
  /** What a field saved as it is left keeps on this device at each key: a row's page's title (the engine's `kept`). */
  kept?: (change: RichTextCellChange) => FieldKept | null;
  /**
   * The page whose sharing a cell's "@" invite opens, where it isn't the page
   * the cell is on (a row page's own properties); see `invitePage`.
   */
  inviteOn?: string;
  /**
   * A page's title (a row page's): a touch screen's editing tools stand under
   * its caret too, as under a page's (NativePhoneToolbar), its "@" this
   * field's and its comment the one `comment` starts on the page, if any.
   */
  pageTitle?: { comment(): (() => void) | undefined };
  /** A database's name: a touch screen's tools under its caret are its "@" and the keyboard's alone (NativePhoneToolbar). */
  phoneName?: boolean;
  /** A table's text cell: on a phone its caret and chosen words take the strip on the keyboard (CellPhoneStrip), not the bar. */
  phoneStrip?: boolean;
  /** The owner's own @ menu in place of the page's, as an automation message's (AutomationMentionMenu). */
  renderMentionMenu?: (menu: { anchor: MentionTrigger["anchor"]; query: string; pick(item: MentionItem): void; close(): void }) => ReactNode;
  handleRef?: Ref<RichTextCellHandle>;
  /** A database cell's "댓글" in its selection bar (CellComments). */
  onComment?: (at: { start: number; end: number; quote: string; rect: DOMRect }) => void;
  /** Words threads were left on, pressed: every thread on them, innermost last (CellComments). */
  onCommentMark?: (commentIds: readonly string[], rect: DOMRect) => void;
  /** Words a new comment is being written on, or its draft kept for: marked (CellComments). */
  commentPending?: CellCommentPending | null;
  /**
   * Its editor is made once the frame it first shows in is painted, its words
   * drawn meanwhile as its editor draws them (asleep, below): a row page's
   * title and values and a comment box, which a window opens with.
   */
  afterPaint?: boolean;
  /** Its words only shown, as a read-only database's description: its editor isn't editable, nor has a selection bar. */
  readOnly?: boolean;
}) {
  const ref = useRef<HTMLElement>(null), coreRef = useRef<RichTextCellEngine | null>(null);
  const latest = useRef(props); latest.current = props;
  const toast = useOptionalProductToast(), toastRef = useRef(toast); toastRef.current = toast;
  const contextSnapshot = useMentionSnapshot();
  const mentionSnapshot = props.mentionSnapshot === undefined ? contextSnapshot : props.mentionSnapshot;
  const shown = useMemo(() => cellSpansOrText(props.spans, props.value), [props.spans, props.value]);
  const [core, setCore] = useState<RichTextCellEngine | null>(null);
  const [linkRequest, setLinkRequest] = useState(0);
  const fieldPages = useContext(FieldPagesContext);
  // Its link editor finds the workspace's pages, as the page's does: those of
  // the page it is on, or the product's for a comment box.
  const productPages = useProductPages();
  const linkSession = fieldPages?.moveContext.session ?? productPages?.session;
  const linkWorkspace = fieldPages?.workspaceId ?? productPages?.workspaceId;
  const linkPages = useMemo(() => linkSession && linkWorkspace ? { session: linkSession, workspaceId: linkWorkspace } : undefined,
    [linkSession, linkWorkspace]);
  // A cell's "@" ends its people with an invite, as a line's: of the typed
  // name, or "초대" with none typed, which opens the sharing of the page the
  // cell is on — its database's page, or a row page's own (Notion's, seen 2026-11-09).
  // A comment's "@" does so for the page it is on, as Notion's (seen 2026-11-11).
  const invitePage = linkSession ? props.lineRules ? props.inviteOn ?? fieldPages?.pageId
    : props.commentMention ? props.pageId : undefined : undefined;
  const menus = useFieldMenus({ field: () => coreRef.current, core, snapshot: mentionSnapshot, lineRules: props.lineRules,
    commentMention: props.commentMention, pageId: props.pageId, anchor: props.propertyId ? cellAnchor(props.propertyId) : undefined, invitePage, session: linkSession, place: props.commentMention ? "comment" : "cell",
    renderMentionMenu: props.renderMentionMenu });
  const menusRef = useRef(menus); menusRef.current = menus;
  const updated = () => menusRef.current.updated();
  const closeMenu = () => menusRef.current.close();
  // The field's engine is taken down once the code running has ended, React
  // having taken the field off the page by then, as the page title's and the
  // formula's are: taken down while still on it, ProseMirror took
  // contenteditable and its other attributes off a shown field, which made
  // the browser work out styles at once (a side peek's title and comment
  // field as it closed, 13–56 elements 3–9 ms, 2026-11-14, NAS condition,
  // CPU ×2). Drawn again meanwhile (React's second look in development), the
  // field keeps it.
  const lease = useRef<{ active: boolean; core: RichTextCellEngine } | null>(null);
  const release = (kept: { active: boolean; core: RichTextCellEngine }) => () => {
    kept.active = false;
    if (coreRef.current === kept.core) coreRef.current = null;
    queueMicrotask(() => {
      if (kept.active) return;
      kept.core.destroy();
      if (lease.current === kept) lease.current = null;
    });
  };
  // The attributes its owner gives the field, on its editor and while it is asleep.
  const fieldAttributes = () => ({ class: `${editorStyles.richMarks} ${latest.current.className ?? ""}`,
    role: "textbox", tabindex: "0", "aria-label": latest.current.ariaLabel,
    "aria-multiline": String(latest.current.multiline === true),
    // Spelling is marked only while the words are being written: a cell
    // left keeps its field but shows its words plain, as Notion's (seen 2026-11-06).
    spellcheck: String(Boolean(coreRef.current?.editor.isFocused)),
    "data-rich-cell-editor": latest.current.multiline ? "multiline" : "single",
    "data-empty": (coreRef.current?.read().some(span => span.text.length) ?? Boolean(latest.current.value)) ? "false" : "true",
    "data-placeholder": latest.current.placeholder ?? "", ...latest.current.dataAttributes });
  // A field asleep: what draws its words again as they change, and what makes its editor now.
  const asleep = useRef<{ redraw(): void; wake(): void } | null>(null);
  useLayoutEffect(() => {
    const element = ref.current;
    if (!element) return;
    let held = lease.current;
    if (held) { held.active = true; coreRef.current = held.core; return release(held); }
    const make = () => {
      const current = createRichTextCellEngine(element, cellSpansOrText(latest.current.spans, latest.current.value), {
        multiline: () => latest.current.multiline === true,
        shiftBreaks: () => latest.current.shiftBreaks === true,
        enterEnds: () => latest.current.enterEnds === true,
        commitOn: () => latest.current.commitOn ?? "input",
        attributes: fieldAttributes,
        changed: change => latest.current.onChange(change), updated,
        linkRequested: () => setLinkRequest(value => value + 1),
        finished: () => { closeMenu(); latest.current.onFinish?.(); },
        entered: () => latest.current.onEnter?.(),
        keyDown: event => latest.current.onKeyDown?.(event),
        cellArrows: () => latest.current.titleCell === true || latest.current.textCell === true,
        pasted: (data, around) => {
          const text = data.getData("text/plain"), lines = latest.current.pastedLines;
          if (!lines || lines === "keep") {
            const kept = lines ? pastedLines(text) : latest.current.multiline ? text.replace(/\r\n?/gu, "\n") : text.replace(/\s*[\r\n]+\s*/gu, " ");
            if (!latest.current.titleCell && !latest.current.textCell) return kept;
            const fitted = titleCellPaste(kept, around, latest.current.titleCell ? graphWireBounds.titleRunes : graphWireBounds.textCharacters);
            if (fitted === null) toastRef.current?.notify(latest.current.titleCell ? i18next.t("pageHeader:titlePasteTooLong",
              { max: graphWireBounds.titleRunes.toLocaleString(activeNumberLocale()) }) : i18next.t("editor:pasteRefused"), "error");
            return fitted;
          }
          const paste = titlePaste(text, around);
          if (paste.into === "title") return paste.text;
          if (paste.into === "body") pasteAtBodyStart(lines.titleOf, data, ref.current!, message => toastRef.current?.notify(message, "error"));
          return null;
        },
        linesOnOneLine: () => latest.current.pastedLines === "keep",
        pasteRefused: () => toastRef.current?.notify(i18next.t("editor:pasteRefused"), "error"),
        typedRules: () => latest.current.lineRules === true || latest.current.commentMention === true || latest.current.typedRules === true,
        pastedMarks: () => latest.current.pastedMarks === true,
        maxCharacters: () => {
          const holds = latest.current.holds ?? (latest.current.titleCell ? "title" : latest.current.textCell ? "text" : undefined);
          return holds === "title" ? graphWireBounds.titleRunes : holds === "text" ? graphWireBounds.textCharacters : undefined;
        },
        datePressed: mention => menusRef.current.datePressed(mention, mention.closest<HTMLElement>("[data-rich-cell-editor]") ?? mention),
        follows: () => latest.current.follows === true, ...(latest.current.kept ? { kept: (change: RichTextCellChange) => latest.current.kept?.(change) } : {}),
        // A database's text value: a table's text cell and a row page's text (the engine's opensLinks).
        opensLinks: () => latest.current.lineRules === true && latest.current.titleCell !== true && latest.current.holds !== "title",
        commentPending: () => latest.current.commentPending, editable: () => latest.current.readOnly !== true,
        ...(latest.current.names || latest.current.onComposed ? { shown: (words: string, over: string, composing: boolean) => {
          const page = latest.current.names;
          if (page) showTypedPageTitle(page, words, over);
          if (composing) latest.current.onComposed?.(words);
        } } : {}),
      });
      coreRef.current = current; setCore(current); updated();
      current.editor.on("focus", () => { current.editor.view.dom.spellcheck = true; latest.current.onFocus?.(); });
      current.editor.on("blur", () => { current.editor.view.dom.spellcheck = false; });
      held = { active: true, core: current }; lease.current = held;
      return held;
    };
    if (!latest.current.afterPaint) return release(make());
    // Asleep, the field shows its words as its editor draws them, and its
    // editor is made in the task after the frame it first shows in: made in
    // that frame, setting contenteditable made the browser work out the
    // styles of all the window drawn so far at once — a row page's title and
    // a text value 16–24 ms each of a table's "새로 만들기" (2026-11-15, NAS
    // condition, CPU ×2). A press or focus reaching it, or the caret asked
    // there (liveField), makes it at once.
    let added: string[] = [];
    const draw = () => {
      element.classList.remove(...added);
      const spans = cellSpansOrText(latest.current.spans, latest.current.value);
      const attributes = fieldElementAttributes(element, fieldAttributes(), latest.current.multiline === true, latest.current.shiftBreaks === true);
      added = attributes.class.split(" ").filter(name => name && !element.classList.contains(name));
      element.classList.add(...added);
      for (const [name, value] of Object.entries(attributes)) if (name !== "class" && (name !== "style" || value)) element.setAttribute(name, value);
      element.setAttribute("translate", "no");
      element.innerHTML = printedFieldHtml(spans);
    };
    const wake = () => {
      if (asleep.current !== sleeper) return;
      stop();
      element.classList.remove(...added);
      make();
      // Focus that woke it is the editor's from now on.
      if (document.activeElement === element) { element.spellcheck = true; latest.current.onFocus?.(); }
    };
    const sleeper = { redraw: draw, wake };
    const forget = sleepUntilWoken(element, wake);
    const callOff = afterNextPaint(wake);
    element.addEventListener("pointerdown", wake, true);
    element.addEventListener("focus", wake);
    const stop = () => {
      if (asleep.current === sleeper) asleep.current = null;
      forget(); callOff();
      element.removeEventListener("pointerdown", wake, true);
      element.removeEventListener("focus", wake);
    };
    asleep.current = sleeper;
    draw();
    return () => {
      stop();
      if (lease.current?.active) release(lease.current)();
    };
    // The field's native identity is its parent's React key; values project
    // through sync without rebuilding its input DOM or field history.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  useLayoutEffect(() => { asleep.current?.redraw(); coreRef.current?.sync(shown); }, [shown, props.ariaLabel, props.className, props.placeholder, props.multiline, props.dataAttributes, props.readOnly]);
  const pending = props.commentPending;
  const pendingKey = pending ? `${pending.start}:${pending.end}:${pending.kept}:${pending.sent === true}` : "";
  useLayoutEffect(() => { coreRef.current?.redrawCommentPending(); }, [core, pendingKey]);
  const pageTitle = Boolean(props.pageTitle);
  const phoneName = Boolean(props.phoneName) && !pageTitle;
  // A title's chosen words take no bar, ⌘K still opening its link box, as
  // Notion's: a table's title cell (seen 2026-11-15 twice and 2026-11-18), a
  // database's name (seen 2026-11-09) and a row page's title (seen 2026-11-18
  // in Chrome at 1440, two tabs: a word double-clicked in the side peek's and
  // in the full page's, and dragged across in the full page's), as the
  // rename window's title; a text cell's, a value's and a comment's take it.
  // On a touch screen a page title's chosen words take the page's line of
  // tools (PhoneSelectionTools).
  const title = props.titleCell === true || props.holds === "title";
  const { phone } = useSidebarLayout(true);
  const strip = Boolean(props.phoneStrip) && phone;
  useEffect(() => {
    const element = ref.current;
    if (!core || !element || !(pageTitle || phoneName)) return undefined;
    return registerPhoneTitleTools(element, phoneName ? { editor: core.editor, name: true, mention: () => menusRef.current.mention(), comment: () => undefined }
      : { editor: core.editor, mention: () => menusRef.current.mention(), comment: () => latest.current.pageTitle?.comment(), words: cellWords(core) });
  }, [core, pageTitle, phoneName]);
  useEffect(() => core && !core.editor.isDestroyed ? keepWritingLookWhileCaretStays(core.editor.view.dom) : undefined, [core]);
  // Pressing words a thread was left on opens that thread, as Notion's cell does.
  useEffect(() => {
    if (!core || core.editor.isDestroyed) return;
    const dom = core.editor.view.dom;
    const pressed = (event: MouseEvent) => {
      const mark = event.target instanceof Element ? event.target.closest<HTMLElement>("[data-comment-id]") : null;
      const ids = mark?.dataset.commentId?.split(" ").filter(Boolean) ?? [];
      if (!mark || !ids.length || !latest.current.onCommentMark || !core.editor.state.selection.empty) return;
      latest.current.onCommentMark(ids, mark.getBoundingClientRect());
    };
    dom.addEventListener("click", pressed);
    return () => dom.removeEventListener("click", pressed);
  }, [core]);
  // What its owner does to a field asleep makes its editor first.
  const live = () => {
    asleep.current?.wake();
    const current = coreRef.current;
    return current && !current.editor.isDestroyed ? current : null;
  };
  useImperativeHandle(props.handleRef, () => ({
    // The body's ⌘Z with nothing left undoes the title here at once: put off
    // to the next frame, the caret let a quick third ⌘Z go to the body and
    // the title stayed (2026-11-05). A field asleep has nothing typed to undo.
    undoTyped: () => {
      const current = coreRef.current;
      if (!current || current.editor.isDestroyed || !current.editor.can().undo()) return false;
      current.editor.view.focus();
      current.editor.commands.undo();
      return true;
    },
    // The caret goes at once (focusEditorNow): given a frame on, keys typed as a window showed went to the page.
    focus: () => { const current = live(); if (current) focusEditorNow(current.editor); },
    focusEnd: () => { const current = live(); if (current) focusEditorNow(current.editor, "end"); },
    selectAll: () => {
      const current = live();
      if (current) focusEditorNow(current.editor, (doc) => TextSelection.create(doc, 1, 1 + doc.firstChild!.content.size));
    },
    range: () => {
      const current = coreRef.current;
      if (!current || current.editor.isDestroyed) return { start: 0, end: 0, length: 0 };
      return { ...current.selection(), length: current.read().reduce((sum, span) => sum + span.text.length, 0) };
    },
    set: (spans, caret) => {
      const current = live();
      if (!current) return;
      const at = caret ?? spans.reduce((sum, span) => sum + span.text.length, 0);
      current.replace(spans, at, at, true);
      if (caret !== undefined) current.editor.view.focus();
    },
    caretAt: (offset) => {
      const current = live();
      if (current) focusEditorNow(current.editor, (doc) => TextSelection.create(doc, 1 + pmOffsetAt(doc.firstChild!.content, offset)));
    },
    mention: () => { live(); menusRef.current.mention(); },
    clear: () => { live()?.replace([], 0); },
    publish: () => { coreRef.current?.publish(); },
    composing: () => { const current = coreRef.current; return Boolean(current && !current.editor.isDestroyed && imeComposing(current.editor.view)); },
  }), []);
  useLayoutEffect(() => {
    if (!props.autoFocus || !coreRef.current) return;
    focusEditorNow(coreRef.current.editor, "end");
  }, [props.autoFocus, core]);
  return <>
    {props.element === "h1" ? <h1 ref={ref as Ref<HTMLHeadingElement>} /> : <div ref={ref as Ref<HTMLDivElement>} className={props.containerClassName} />}
    {core && !props.readOnly && <RichTextCellToolbar core={core} linkRequest={linkRequest} layout={props.toolbarLayout} onComment={props.onComment}
      linkPages={linkPages} linkOnly={title || strip} marksLinkTarget={!props.titleCell && !pageTitle} />}
    {core && !props.readOnly && strip && <CellPhoneStrip core={core} label={props.ariaLabel} mention={() => menusRef.current.mention()}
      onComment={props.onComment} linkPages={linkPages} />}
    {menus.element}
  </>;
}

/** A field's chosen words, changed in its own text model, as its selection bar changes them (RichTextCellToolbar). */
function cellWords(core: RichTextCellEngine): PhoneTitleWords {
  const edit = (next: (spans: TextSpan[], start: number, end: number) => TextSpan[]) => {
    const spans = core.read(), at = core.selection();
    if (at.start >= at.end) return;
    try { core.replace(next(spans, at.start, at.end), at.start, at.end); } catch { /* A change past the field's bounds is not made. */ }
  };
  return {
    format: () => {
      const at = core.selection();
      return at.start < at.end ? cellRangeFormat(core.read(), at.start, at.end) : null;
    },
    toggle: mark => edit((spans, start, end) => toggleCellMark(spans, start, end, mark)),
    color: token => {
      const change = selectionColorChange(token);
      rememberUsedEditorColor(token);
      edit((spans, start, end) => colorCellRange(spans, start, end, change.kind, change.value));
    },
    link: href => edit((spans, start, end) => href ? linkCellRange(spans, start, end, href) : unlinkCellRange(spans, start, end)),
  };
}
