import { useEffect, useLayoutEffect, useRef, useState, type ComponentType } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { TextSelection } from "@tiptap/pm/state";
import type { TextSpan } from "@/lib/types";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { placePhoneTools } from "@/lib/phoneTools";
import { softKeyboardShown } from "@/lib/softKeyboard";
import { bringLineOver } from "@/lib/phoneLineInSight";
import { AtIcon, BoldIcon, CodeIcon, CommentIcon, DotsHorizontal, InlineEquationIcon, ItalicIcon, KeyboardIcon, LinkIcon, LinkPlusIcon,
  StrikethroughIcon, Trash, UnderlineIcon, UndoIcon, type LeafenIconProps } from "@/icons/leafen";
import { wordsChosenByPerson } from "../editor/inputMethodChoice";
import { focusEditorNow } from "../editor/nativeTiptapFocus";
import { pmOffsetAt } from "../editor/nativeTiptapInline";
import { InlineEquationEditor } from "../editor/InlineEquationEditor";
import { PhoneLinkSheet, type PhoneLinkEdit } from "../editor/PhoneLinkSheet";
import { PhoneLinkPreview, useLongPressChoice } from "../editor/PhoneLinkPreview";
import type { SelectionLinkPages } from "../editor/SelectionToolbarLinkPicker";
import type { RichTextCellEngine } from "./richTextCellEngine";
import { cellRangeFormat, equationCellRange, linkCellRange, replaceCellRange, toggleCellMark, unlinkCellRange, type CellMark } from "./richTextCellModel";
import styles from "../editor/NativePhoneToolbar.module.css";

// The strip's height, its 1px line on top within (NativePhoneToolbar.module.css).
const STRIP_HEIGHT = 44;
const ENGINE_MARKS: Record<CellMark, string> = { bold: "bold", italic: "italic", underline: "underline", strikethrough: "strike", code: "code" };

type Tool = { key: string; icon: ComponentType<LeafenIconProps>; name: string; disabled?: boolean; pressed?: boolean; run?(): void };
/** The link sheet or the equation's window open for the words from `start` to `end`. */
type Opened = { kind: "link" | "equation"; start: number; end: number; href: string; words: string; anchor: DOMRect };
type Change = { spans: TextSpan[]; start: number; end: number };
type VirtualKeyboard = { show(): void; hide(): void };
const keyboardApi = () => (navigator as Navigator & { virtualKeyboard?: VirtualKeyboard }).virtualKeyboard;

/**
 * A table text cell's tools on a phone, as the Notion Android app's (measured
 * 2026-11-25 on a Galaxy A90 at 411, two openings, light): while the keyboard
 * is up for the cell's box, the line's strip docked on it (the page's strip's
 * look, NativePhoneToolbar.module.css) holds ↶ · B · U · I · S · @ · 링크 ·
 * 코드 · √x and the line's 링크, 삭제 and ⋯ dimmed, with 댓글 after 링크
 * while words are chosen; the keyboard's cell at the end puts the keyboard
 * away, the box staying open, and the strip goes with it, the page's foot
 * coming back; a tap on the box's words brings them back. A mark pressed at
 * the caret holds for what is typed next, its tile staying while the caret
 * goes on typing there and none drawn for the marks the words before it
 * carry on (seen 2026-11-25: B held after a letter typed, and none at a
 * caret after bold words, a letter typed there going in bold); on chosen
 * words it marks them, held while they all hold it; 링크 is held on
 * linked words. 링크 opens the link sheet (PhoneLinkSheet), its field taking
 * the keyboard, its link going in as its words at a caret and linked words'
 * link edited; √x the equation's field docked on the keyboard
 * (InlineEquationEditor) of the words, or empty at a caret; a link's words a
 * long press chose show its bar (PhoneLinkPreview), whose "편집" opens that
 * edit. The app's title and number cells take no strip, nor does
 * Leafen's (RichTextCellEditor's `phoneStrip`). Undo is the cell's own.
 */
export function CellPhoneStrip({ core, label, mention, onComment, linkPages }: {
  core: RichTextCellEngine;
  /** The strip's name: the cell's own. */
  label: string;
  /** Writes "@" at the caret, opening the cell's "@" menu there. */
  mention(): void;
  /** "댓글" on the chosen words (CellComments). */
  onComment?: (at: { start: number; end: number; quote: string; rect: DOMRect }) => void;
  linkPages?: SelectionLinkPages;
}) {
  const { phone } = useSidebarLayout(true);
  const { t } = useTranslation(["editor", "selectionToolbar", "blockHandle"]);
  const editor = core.editor;
  const strip = useRef<HTMLDivElement>(null);
  const [top, setTop] = useState<number | null>(null);
  const [, redraw] = useState(0);
  const [opened, setOpened] = useState<Opened | null>(null);
  // A link's words a finger's long press chose show the link's bar under them (PhoneLinkPreview), as the app's (seen
  // 2026-11-25 on a Galaxy A90: "celllink" chosen in a text cell's box, the bar under it kept 12 inside the window).
  const [chosenLink, setChosenLink] = useState<Omit<Opened, "kind"> | null>(null);
  useLongPressChoice(editor, () => {
    const { start, end } = core.selection(), spans = core.read(), href = cellRangeFormat(spans, start, end).link;
    setChosenLink(href ? { start, end, href, words: spans.map(span => span.text).join("").slice(start, end), anchor: wordsPlace(editor) } : null);
  }, () => setChosenLink(null));
  // The keyboard put away by its cell, the box kept open with the focus.
  const away = useRef(false);
  // The marks pressed at the caret since it came there, drawn held.
  const held = useRef(new Set<CellMark>());
  // The box stays open while its link sheet or equation's window has the focus, as its desktop bar's (RichTextCellToolbar).
  useLayoutEffect(() => {
    if (!opened) return undefined;
    const field = editor.view.dom as HTMLElement;
    field.dataset.fieldHeld = "true";
    return () => { delete field.dataset.fieldHeld; };
  }, [editor, opened]);
  // The strip stands at the foot of what shows while the keyboard is up for
  // the cell's caret, drawn again as the cell changes. A table cell's box
  // stands 12 over it by itself, the page left where it rests, as the app's
  // (phoneCellBoxInSight.ts); a field in no such box has its caret's line
  // brought up to stand 10 over it as the strip comes, again once the
  // keyboard has settled, and as the words change, as the page's strip does
  // its line (NativePhoneToolbar).
  useEffect(() => {
    if (!phone) { setTop(null); return undefined; }
    const field = editor.view.dom as HTMLElement;
    let frame = 0, settled = 0, was = false, owed = false;
    const update = (reveal = false) => {
      const viewport = window.visualViewport;
      const shown = !editor.isDestroyed && editor.view.hasFocus() && editor.state.selection instanceof TextSelection && softKeyboardShown();
      const spot = (viewport?.height ?? window.innerHeight) - STRIP_HEIGHT, at = shown ? (viewport?.offsetTop ?? 0) + spot : null;
      setTop(at);
      // Its place told at once, before it is drawn: the cell's box stands over it first (phoneCellBoxInSight.ts).
      placePhoneTools(strip, at === null ? null : { top: at, bottom: at + STRIP_HEIGHT });
      if (shown) {
        redraw(count => count + 1);
        const page = field.closest("[data-cell-editor]") ? null : field.closest<HTMLElement>("[data-page-scroll]");
        const shift = document.documentElement.getBoundingClientRect().top + (viewport?.pageTop ?? window.scrollY);
        if (page && (reveal || owed || !was)) bringLineOver(page, editor.view.coordsAtPos(editor.state.selection.head).bottom - shift, spot);
        if (!was) { window.clearTimeout(settled); settled = window.setTimeout(() => update(true), 250); }
      }
      owed = false; was = shown;
    };
    const schedule = () => { cancelAnimationFrame(frame); frame = requestAnimationFrame(() => update()); };
    // The keyboard put away comes back at a tap in the cell, as the app's (seen 2026-11-25: a tap on the box's words).
    const back = () => {
      if (!away.current) return;
      away.current = false;
      field.removeAttribute("data-keyboard-away");
      keyboardApi()?.show();
      requestAnimationFrame(() => { if (!away.current) field.removeAttribute("virtualkeyboardpolicy"); });
    };
    // A caret moved elsewhere, not by typing, lets its pressed marks go.
    const changed = ({ transaction }: { transaction: { docChanged: boolean; selectionSet: boolean } }) => {
      if (transaction.selectionSet && !transaction.docChanged) held.current.clear();
      owed ||= transaction.docChanged;
      schedule();
    };
    const left = () => {
      held.current.clear();
      if (away.current) { away.current = false; field.removeAttribute("virtualkeyboardpolicy"); field.removeAttribute("data-keyboard-away"); }
      schedule();
    };
    editor.on("transaction", changed); editor.on("focus", schedule); editor.on("blur", left);
    field.addEventListener("click", back);
    window.addEventListener("resize", schedule);
    window.visualViewport?.addEventListener("resize", schedule); window.visualViewport?.addEventListener("scroll", schedule);
    update();
    return () => {
      cancelAnimationFrame(frame); window.clearTimeout(settled);
      editor.off("transaction", changed); editor.off("focus", schedule); editor.off("blur", left);
      field.removeEventListener("click", back);
      window.removeEventListener("resize", schedule);
      window.visualViewport?.removeEventListener("resize", schedule); window.visualViewport?.removeEventListener("scroll", schedule);
    };
  }, [phone, editor]);
  // Where the strip stands while drawn, for the cell's "@" list to keep clear of it (phoneTools.ts).
  useLayoutEffect(() => {
    placePhoneTools(strip, strip.current && top !== null ? { top, bottom: top + strip.current.offsetHeight } : null);
  });
  useEffect(() => () => placePhoneTools(strip, null), []);

  // The sheet or window closes, and the field takes the focus back in the
  // press that closed it, the keyboard up with it as the app's after the link
  // sheet's "취소" (seen 2026-11-25) — the keyboard never going down between
  // the window's field and the cell, which would end the cell's editing
  // (keyboardDownEndsEditing.ts); its words are chosen again, changed as
  // `change` says, once the press is through.
  function close(change?: (spans: TextSpan[], start: number, end: number) => Change | null) {
    const was = opened;
    setOpened(null);
    if (!was) return;
    if (!editor.isDestroyed) editor.view.focus();
    requestAnimationFrame(() => {
      if (editor.isDestroyed) return;
      const next = change?.(core.read(), was.start, was.end) ?? null;
      try { if (next) core.replace(next.spans, next.start, next.end); } catch { /* A change past the cell's bounds is not made. */ }
      const { start, end } = next ?? was;
      focusEditorNow(editor, doc => TextSelection.create(doc, 1 + pmOffsetAt(doc.firstChild!.content, start), 1 + pmOffsetAt(doc.firstChild!.content, end)));
    });
  }
  // Linked words' edit: the words typed go in with the first one's look, then the address (none takes the link off).
  const edit = opened?.kind === "link" && opened.href && opened.words ? { words: opened.words,
    onDone: ({ href, words }: PhoneLinkEdit) => close((spans, start, end) => {
      const typed = words === opened.words ? spans : replaceCellRange(spans, start, end, [{ ...lookAt(spans, start), text: words }]);
      const stop = start + words.length;
      return { spans: href ? linkCellRange(typed, start, stop, href) : unlinkCellRange(typed, start, stop), start, end: stop };
    }) } : undefined;
  const sheet = opened?.kind === "link" ? <PhoneLinkSheet href={opened.href} source={linkPages} edit={edit} onClose={() => close()}
    onLink={(href, named) => close((spans, start, end) => {
      if (start < end) return { spans: href ? linkCellRange(spans, start, end, href) : unlinkCellRange(spans, start, end), start, end };
      if (!href) return null;
      const words = named || href;
      return { spans: replaceCellRange(spans, start, end, [{ text: words, link: href }]), start: start + words.length, end: start + words.length };
    })} />
    : opened ? <InlineEquationEditor anchor={opened.anchor} initial={opened.words} onPreview={() => {}} onCancel={() => close()}
      onDone={tex => close((spans, start, end) => ({ spans: equationCellRange(spans, start, end, tex), start: start + tex.length, end: start + tex.length }))} />
    : null;
  if (!phone || top === null || opened || editor.isDestroyed) return sheet;

  const at = core.selection(), spans = core.read();
  const chosen = at.start < at.end && wordsChosenByPerson(editor.view);
  const format = chosen ? cellRangeFormat(spans, at.start, at.end) : null;
  const words = spans.map(span => span.text).join("").slice(at.start, at.end);
  const place = () => wordsPlace(editor);
  const open = (kind: Opened["kind"]) => setOpened({ kind, start: at.start, end: at.end, href: format?.link ?? "", words, anchor: place() });
  // Chosen words take the mark; a caret holds it for what is typed next, and lets it go pressed again.
  const mark = (key: CellMark): Tool => ({ key, name: t(`selectionToolbar:marks.${key}`),
    icon: { bold: BoldIcon, italic: ItalicIcon, underline: UnderlineIcon, strikethrough: StrikethroughIcon, code: CodeIcon }[key],
    pressed: format ? format.marks[key] : held.current.has(key),
    run: () => {
      const now = core.selection();
      if (now.start < now.end) { try { core.replace(toggleCellMark(core.read(), now.start, now.end, key), now.start, now.end); } catch { /* past its bounds */ } }
      else if (held.current.delete(key)) editor.commands.unsetMark(ENGINE_MARKS[key]);
      else { held.current.add(key); editor.commands.setMark(ENGINE_MARKS[key]); }
    } });
  const tools: Tool[] = [
    { key: "undo", icon: UndoIcon, name: t("phoneToolbar.undo"), disabled: !editor.can().undo(), run: () => editor.commands.undo() },
    mark("bold"), mark("underline"), mark("italic"), mark("strikethrough"),
    { key: "mention", icon: AtIcon, name: t("phoneToolbar.mention"), run: mention },
    { key: "link", icon: LinkPlusIcon, name: t("selectionToolbar:marks.link"), pressed: Boolean(format?.link), run: () => open("link") },
    ...(chosen && onComment ? [{ key: "comment", icon: CommentIcon, name: t("phoneToolbar.comment"),
      run: () => onComment({ start: at.start, end: at.end, quote: words, rect: place() }) }] : []),
    mark("code"),
    { key: "equation", icon: InlineEquationIcon, name: t("selectionToolbar:marks.equation"), run: () => open("equation") },
    // The line's own tools, which a cell's words have no use for, as the app's draws them dimmed.
    { key: "copyLink", icon: LinkIcon, name: t("blockHandle:menu.copyLinkToBlock"), disabled: true },
    { key: "delete", icon: Trash, name: t("phoneToolbar.delete"), disabled: true },
    { key: "more", icon: DotsHorizontal, name: t("phoneToolbar.more"), disabled: true },
  ];
  // A press keeps the cell's focus (the strip's mousedown) and acts on the next frame, as the page's strip.
  const press = (tool: Tool) => {
    if (!editor.view.hasFocus()) editor.view.focus();
    requestAnimationFrame(() => { if (!editor.isDestroyed) tool.run?.(); });
  };
  // A browser that can put the keyboard away keeping the focus (Android Chrome) keeps the box open, as the app's; elsewhere the cell is left.
  const keyboardAway = () => {
    const api = keyboardApi(), field = editor.view.dom as HTMLElement;
    if (!api) { field.blur(); return; }
    away.current = true;
    // The page's foot comes back meanwhile, as the app's (PhonePageFoot).
    field.setAttribute("data-keyboard-away", "");
    field.setAttribute("virtualkeyboardpolicy", "manual");
    api.hide();
  };
  return <>{createPortal(<div ref={strip} className={styles.toolbar} style={{ top }} data-phone-strip="" data-phone-tools-field="cell"
    role="toolbar" aria-label={label} onPointerDown={event => event.preventDefault()} onMouseDown={event => event.preventDefault()}
    onKeyDown={event => {
      event.stopPropagation();
      if (event.key === "Escape") { event.preventDefault(); editor.view.dom.blur(); }
    }}>
    <div className={styles.tools}>
      {tools.map(tool => <button type="button" key={tool.key} data-phone-tool={tool.key} disabled={tool.disabled} aria-label={tool.name}
        title={tool.name} aria-pressed={tool.pressed} onClick={() => press(tool)}><tool.icon size="large" /></button>)}
    </div>
    <button type="button" className={styles.keyboard} data-phone-tool="keyboard" aria-label={t("phoneToolbar.keyboard")}
      onClick={keyboardAway}><KeyboardIcon size="large" /></button>
  </div>, document.body)}{sheet}
  {chosenLink ? <PhoneLinkPreview href={chosenLink.href} at={chosenLink.anchor} field="cell"
    onEdit={() => { setChosenLink(null); setOpened({ kind: "link", ...chosenLink }); }} /> : null}</>;
}

/** The look of the words a range starts in: their marks, colours and link. */
function lookAt(spans: readonly TextSpan[], start: number): Omit<TextSpan, "text"> {
  let at = 0;
  for (const span of spans) {
    if (start < at + span.text.length) {
      const { bold, italic, underline, strikethrough, code, color, background, link } = span;
      return Object.fromEntries(Object.entries({ bold, italic, underline, strikethrough, code, color, background, link }).filter(([, value]) => value !== undefined));
    }
    at += span.text.length;
  }
  return {};
}

/** Where a cell's chosen words or its caret are drawn. */
function wordsPlace(editor: RichTextCellEngine["editor"]) {
  const range = window.getSelection()?.rangeCount ? window.getSelection()!.getRangeAt(0) : null;
  if (range && editor.view.dom.contains(range.startContainer)) return range.getBoundingClientRect();
  const caret = editor.view.coordsAtPos(editor.state.selection.head);
  return new DOMRect(caret.left, caret.top, caret.right - caret.left, caret.bottom - caret.top);
}
