import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { Transaction } from "@tiptap/pm/state";
import type { TextSpan } from "@/lib/types";
import type { RichTextCellEngine } from "./richTextCellEngine";
import { fieldChosenForTyping } from "../editor/nativeTiptapFocus";
import { SelectionToolbarView, type SelectionToolbarPage } from "../editor/SelectionToolbarView";
import { fieldPageAround } from "../editor/selectionToolbarPlace";
import type { SelectionLinkPages } from "../editor/SelectionToolbarLinkPicker";
import { InlineEquationEditor } from "../editor/InlineEquationEditor";
import { selectionColorChange, type SelectionToolbarMark } from "../editor/selectionToolbarModel";
import { rememberUsedEditorColor } from "../editor/colorMemory";
import {
  cellLinkValue, cellRangeFormat, clearCellFormat, colorCellRange, equationCellRange, linkCellRange, toggleCellMark, unlinkCellRange,
} from "./richTextCellModel";
import { copyText } from "@/lib/copyText";
import { textLinkHref } from "../editor/richtextUrlPolicy";
import { markLinkWindowTarget } from "../editor/nativeTiptapLinkTarget";
import { editorHoldsPageFocus } from "../editor/editorHoldsPageFocus";
import { wordsChosenByPerson } from "../editor/inputMethodChoice";

type StoredRange = { start: number; end: number };

/**
 * The text selected in the cell and where it is drawn, or null: the box of
 * its lines, its right edge where the farthest of them ends, as the page's
 * panel measures chosen words (selectionToolbarPlace).
 */
function selectedCellRange(core: RichTextCellEngine, toolbar?: HTMLElement | null): { range: StoredRange; rect: DOMRect } | null {
  const { editor } = core;
  // Words an input method holds chosen (a conversion's clause) take no bar (wordsChosenByPerson).
  if (editor.isDestroyed || (!editorHoldsPageFocus(editor) && !toolbar?.contains(document.activeElement)) || !wordsChosenByPerson(editor.view)) return null;
  const { view } = editor, { from: start, to: end } = editor.state.selection;
  const from = view.coordsAtPos(start), to = view.coordsAtPos(end);
  let left = Math.min(from.left, to.left), top = Math.min(from.top, to.top);
  let right = Math.max(from.right, to.right), bottom = Math.max(from.bottom, to.bottom);
  try {
    const lines = document.createRange(), head = view.domAtPos(start), tail = view.domAtPos(end);
    lines.setStart(head.node, head.offset); lines.setEnd(tail.node, tail.offset);
    for (const line of lines.getClientRects()) {
      if (line.height <= 0) continue;
      left = Math.min(left, line.left); top = Math.min(top, line.top); right = Math.max(right, line.right); bottom = Math.max(bottom, line.bottom);
    }
  } catch { /* Words not drawn yet are placed by their ends. */ }
  return { range: core.selection(), rect: new DOMRect(left, top, right - left, bottom - top) };
}

const LINK_COPY_BOUNDS = 2048;

/**
 * The selection toolbar of a database cell's text, as Notion's (seen
 * 2026-10-30): the marks, the text and background colors, "형식 초기화",
 * the link editor, which ⌘K opens too, an inline equation and a greyed "⋯";
 * a database cell's text also "댓글" under a line (seen 2026-10-31). A cell's
 * text turns into no block.
 */
export function RichTextCellToolbar({ core, linkRequest, layout, onComment, linkPages, linkOnly = false, marksLinkTarget = false }: {
  core: RichTextCellEngine;
  /** Whether the words its link window links are drawn as its target while it is open. */
  marksLinkTarget?: boolean;
  /** No bar over chosen words, only ⌘K's link box: a database's name's (Notion's, seen 2026-11-09). */
  linkOnly?: boolean;
  /** Changes when ⌘K asks for the link editor. */
  linkRequest: number;
  /** The pages its link editor finds and lists as visited, as the page's does. */
  linkPages?: SelectionLinkPages;
  layout?: "panel" | "row";
  /** "댓글": a thread on the selected words, given where they are drawn. */
  onComment?: (at: { start: number; end: number; quote: string; rect: DOMRect }) => void;
}) {
  const editor = core.editor;
  const editorRef = useRef<HTMLElement | null>(editor.view.dom);
  const read = core.read, change = core.replace;
  const [rect, setRect] = useState<DOMRect | null>(null);
  // The page's column and area, for a cell of a database in a page's text (fieldPageAround).
  const [page, setPage] = useState<SelectionToolbarPage | undefined>(undefined);
  const [range, setRange] = useState<StoredRange | null>(null);
  const [colorsOpen, setColorsOpen] = useState(false);
  const [linkOpen, setLinkOpen] = useState(false);
  // ⌘K's link editor sits under the chosen words, not under the toolbar's button.
  const [linkAtText, setLinkAtText] = useState<DOMRect | null>(null);
  const [linkValue, setLinkValue] = useState("");
  const [hadLinkOnOpen, setHadLinkOnOpen] = useState(false);
  const [linkCopied, setLinkCopied] = useState(false);
  const [equation, setEquation] = useState<{ range: StoredRange; anchor: DOMRect; initial: string } | null>(null);
  // The field stays open in the box it is written in while its link or
  // equation box has the focus, as Notion's page value does (seen 2026-11-14:
  // ⌘K over a page's text value; DatabaseCellBasics .rowTextCell).
  const held = linkOpen || equation !== null;
  useLayoutEffect(() => {
    if (!held) return undefined;
    const field = editor.view.dom as HTMLElement;
    field.dataset.fieldHeld = "true";
    return () => { delete field.dataset.fieldHeld; };
  }, [editor, held]);
  // The words the link window links are drawn as its target while it is open,
  // as the page's (nativeTiptapLinkTarget): a text cell's and a comment box's,
  // and no table name cell's, as Notion's (seen 2026-11-15 and 2026-11-16 in
  // Chrome at 1440: a text cell's word on the gray ground, padding 2.5px, a
  // name cell's plain, both times; a comment box's on the gray ground, once).
  useLayoutEffect(() => {
    if (!linkOpen || !marksLinkTarget) return undefined;
    const { from, to } = editor.state.selection;
    markLinkWindowTarget(editor.view, { from, to });
    return () => markLinkWindowTarget(editor.view, null);
  }, [editor, linkOpen, marksLinkTarget]);
  const toolbarRef = useRef<HTMLDivElement>(null);
  const colorMenuRef = useRef<HTMLDivElement>(null);
  const toolbarLinkMenuRef = useRef<HTMLDivElement>(null);
  const turnMenuRef = useRef<HTMLDivElement>(null);
  const linkInputRef = useRef<HTMLInputElement>(null);
  const linkButtonRef = useRef<HTMLButtonElement>(null);
  const colorButtonRef = useRef<HTMLButtonElement>(null);
  const turnButtonRef = useRef<HTMLButtonElement>(null);
  const dismissedRange = useRef<StoredRange | null>(null);
  const spans = range ? read() : null;
  const format = spans && range ? cellRangeFormat(spans, range.start, range.end) : null;

  useEffect(() => {
    function onSelectionChange(event?: { transaction?: Transaction }) {
      // PM's blur transaction runs before DOM focus reaches the toolbar.
      if (event?.transaction?.getMeta("blur")) return;
      // A new row's name chosen to be typed over isn't asking for the bar.
      const selected = fieldChosenForTyping(editor) ? null : selectedCellRange(core, toolbarRef.current);
      if (selected && dismissedRange.current?.start === selected.range.start && dismissedRange.current.end === selected.range.end) return;
      dismissedRange.current = null;
      setRange(selected?.range ?? null);
      setRect(selected?.rect ?? null);
      setPage(selected ? fieldPageAround(editor.view.dom) : undefined);
      if (!selected) { setLinkOpen(false); setLinkAtText(null); setColorsOpen(false); }
    }
    const blur = () => queueMicrotask(() => onSelectionChange());
    editor.on("selectionUpdate", onSelectionChange); editor.on("transaction", onSelectionChange); editor.on("blur", blur);
    onSelectionChange();
    return () => { editor.off("selectionUpdate", onSelectionChange); editor.off("transaction", onSelectionChange); editor.off("blur", blur); };
  }, [core, editor]);

  function openLinkEditor(at: StoredRange | null = range) {
    const current = at ? read() : null;
    const link = current && at ? cellRangeFormat(current, at.start, at.end).link : null;
    if (link === null) return;
    setColorsOpen(false);
    setLinkValue(link);
    setHadLinkOnOpen(link !== "");
    setLinkCopied(false);
    setLinkOpen(true);
  }

  useEffect(() => {
    if (linkRequest === 0) return;
    // ⌘K can come before the selection's change reaches the toolbar, so it
    // reads the selection itself.
    const selected = selectedCellRange(core);
    if (!selected) return;
    dismissedRange.current = null;
    setRange(selected.range);
    setRect(selected.rect);
    setPage(fieldPageAround(editor.view.dom));
    // In the middle under the chosen words, as Notion's in a cell and a comment box (seen 2026-11-09).
    const chosen = document.getSelection();
    setLinkAtText(chosen?.rangeCount && editor.view.dom.contains(chosen.anchorNode) ? chosen.getRangeAt(0).getBoundingClientRect() : selected.rect);
    openLinkEditor(selected.range);
    // Only a new request opens the editor.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [linkRequest]);

  function edit(next: (current: TextSpan[], start: number, end: number) => TextSpan[]) {
    const current = read();
    if (!current || !range) return;
    try {
      change(next(current, range.start, range.end), range.start, range.end);
    } catch {
      // A change past the cell's bounds is not made.
    }
  }

  function closeLinkEditor() {
    setLinkOpen(false);
    setLinkAtText(null);
    setLinkValue("");
    setHadLinkOnOpen(false);
    setLinkCopied(false);
  }

  function applyLinkValue(given?: string) {
    const link = given ?? cellLinkValue(linkValue);
    if (!link && linkValue.trim()) return;
    closeLinkEditor();
    edit((current, start, end) => link ? linkCellRange(current, start, end, link) : unlinkCellRange(current, start, end));
  }

  function openEquation() {
    const current = read();
    if (!current || !range || !rect) return;
    dismissedRange.current = range;
    setRect(null); setLinkOpen(false); setColorsOpen(false);
    setEquation({ range, anchor: rect, initial: current.map((span) => span.text).join("").slice(range.start, range.end) });
  }

  function finishEquation(tex: string) {
    const at = equation?.range, current = read();
    setEquation(null);
    if (!at || !current) return;
    try {
      change(equationCellRange(current, at.start, at.end, tex), at.start + tex.length, at.start + tex.length);
    } catch {
      // A change past the cell's bounds is not made.
    }
  }

  if (equation) {
    return <InlineEquationEditor anchor={equation.anchor} initial={equation.initial} onPreview={() => {}} onDone={finishEquation}
      onCancel={() => { setEquation(null); editor.view.focus(); }} />;
  }
  if (!rect || !format || linkOnly && !linkOpen) return null;
  const view = (
    <SelectionToolbarView
      layout={layout} selectionOwnerRef={editorRef} linkReturnFocusRef={editorRef} linkPages={linkPages} linkOnly={linkOnly}
      // Escape in its link picker gives the cell's text the focus back, the
      // picker staying, and the next closes it, as the page's and Notion's
      // cell's (measured 2026-11-15 in Chrome only, a text cell's word chosen
      // by a double-click and its toolbar's link button clicked, no script in
      // the page: after one Escape the next letter typed took the word's place
      // and the picker and toolbar closed; after two the picker was gone, the
      // toolbar stayed and the next letter took the word's place).
      focusText={() => { if (!editor.isDestroyed) editor.view.focus(); }} dismissToolbar={() => {
        dismissedRange.current = core.selection();
        setRect(null); setLinkOpen(false); setColorsOpen(false);
      }}
      activeColor={format.color} activeBackground={format.background} activeMarks={format.marks} activeType={null} blockActions={false}
      apply={(mark: SelectionToolbarMark) => {
        if (mark === "link") { setLinkAtText(null); openLinkEditor(); }
        else edit((current, start, end) => toggleCellMark(current, start, end, mark));
      }}
      applyColor={(token) => {
        // The menu stays open, as the page's does.
        const change = selectionColorChange(token);
        rememberUsedEditorColor(token);
        edit((current, start, end) => colorCellRange(current, start, end, change.kind, change.value));
      }}
      applyLinkValue={applyLinkValue}
      clearFormatting={() => edit(clearCellFormat)} makeEquation={openEquation} inertMore
      closeColors={() => setColorsOpen(false)}
      closeLinkEditor={closeLinkEditor} closeTurn={() => {}}
      colorButtonRef={colorButtonRef} colorMenuRef={colorMenuRef} colorsOpen={colorsOpen}
      commentOnly={false} commentAction={Boolean(onComment)} commentSelection={() => {
        const current = read();
        if (!onComment || !range || !rect || !current) return;
        dismissedRange.current = range;
        setRect(null); setLinkOpen(false); setColorsOpen(false);
        onComment({ start: range.start, end: range.end, quote: current.map((span) => span.text).join("").slice(range.start, range.end), rect });
      }}
      copyCurrentLink={async () => {
        if (!format.link || format.link.length > LINK_COPY_BOUNDS) return;
        setLinkCopied(await copyText(format.link));
      }}
      currentLinkUrl={format.link} hadLinkOnOpen={hadLinkOnOpen}
      linkButtonRef={linkButtonRef} linkCopied={linkCopied} linkInputRef={linkInputRef}
      linkOpen={linkOpen} linkAtText={linkAtText} linkValue={linkValue}
      openCurrentLink={() => { const href = textLinkHref(format.link); if (href) window.open(href, "_blank", "noopener,noreferrer"); }}
      rect={rect} page={page}
      removeLink={() => {
        closeLinkEditor();
        edit(unlinkCellRange);
      }}
      setColorsOpen={setColorsOpen} setLinkValue={setLinkValue} setTurnOpen={() => {}}
      toolbarLinkMenuRef={toolbarLinkMenuRef} toolbarRef={toolbarRef}
      turnButtonRef={turnButtonRef} turnInto={() => {}} turnMenuRef={turnMenuRef} turnOpen={false}
    />
  );
  return typeof document === "undefined" ? view : createPortal(view, document.body);
}
