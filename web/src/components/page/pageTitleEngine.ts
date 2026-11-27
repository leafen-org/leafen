import { Editor, Extension } from "@tiptap/core";
import { Fragment, Slice, type Node as ProseMirrorNode } from "@tiptap/pm/model";
import { Plugin, TextSelection, type Transaction } from "@tiptap/pm/state";
import { closeHistory } from "@tiptap/pm/history";
import type { TextSpan } from "@/lib/types";
import { pushRoute } from "@/lib/router";
import { takeDomSelection } from "@/lib/tiptapDomSelection";
import { chordLetter, primaryModifier } from "@/lib/keyboardShortcuts";
import { clipboardHtmlForEngine } from "@/lib/clipboardHtml";
import { fieldTakes } from "@/lib/pasteBound";
import { textChanges, titleFromUnits, titleUnits, titleUnitText, type TitleValue } from "@/lib/titleMerge";
import { registerIndependentTiptapFocus } from "../editor/nativeTiptapFocus";
import { nativeInlineFieldExtensions } from "../editor/nativeTiptapSchema";
import { fieldWordsTyped } from "../editor/fieldTypedSpaces";
import { fragmentToNativeRichText, nativeOffsetAt, nativeRichTextToFragment, pmOffsetAt } from "../editor/nativeTiptapInline";
import { EDITOR_PASTE_BOUNDS } from "../editor/editorClipboardPolicy";
import { AndroidEnter } from "../editor/androidEnter";
import { AppleTouchPlainText } from "../editor/appleTouchPlainText";
import { CompositionEndKey } from "../editor/compositionEndKey";
import editorStyles from "../editor/editorStyles.generated.module.css";
import { fieldAtomView } from "../database/richTextCellEngine";
import { NativeFieldInputRules, typedRuleAfter } from "../editor/nativeTiptapInputRules";
import { cellValueFromSpans } from "../database/richTextCellModel";
import type { MenuField } from "../editor/useFieldMenus";
import { TiptapReadOnlyMark } from "@/lib/tiptapReadOnlyMark";
import { compositionEnds, fieldKey, imeComposing } from "@/lib/fieldKeyBoundary";

const projection = "page-title-field-projection";

// What a title's words are: a mention or an equation reads as its words.
const words = (node: ProseMirrorNode) => node.type.name === "nativeAtom" ? String(node.attrs.text) : "";
const oneLine = (text: string) => text.replace(/\s*\n+\s*/gu, " ");

/**
 * A page's title field, one line of words with the page's mentions, inline
 * equations and links, as Notion's title holds them (seen 2026-11-10): its
 * "@" opens the body's mention menu (useFieldMenus), a mention draws as in
 * the body, and formatting written to the title (its API) draws in the
 * title's own look (plainMarks) — ⌘B, ⌘I, ⌘U, ⌘⇧S and ⌘E do nothing, as
 * Notion's. The native owner saves its title (usePageTitleSave); Tiptap owns
 * input. Its words read as plain text where a title is named (`read`), its
 * mentions with them in `value`.
 */
export function createPageTitleEngine(element: HTMLElement, initial: TitleValue, options: {
  maxRunes?: number;
  attributes(): Record<string, string>;
  editable(): boolean;
  /** The words changed: they are saved, and shown where the page is named. */
  changed(): void;
  /**
   * The words changed while an input method composes them: shown where the
   * page is named at once, its last letters too, as Notion's path, tab and
   * table row show a syllable still composing (seen twice 2026-11-21 in
   * macOS Chrome with the 2-set Korean input method); saving waits for the
   * composition to end (`changed`).
   */
  composed?(): void;
  blurred(): void;
  /** The words or the caret moved: the "@" menu reads them, a place moved by a title from elsewhere through `remap`. */
  updated?(remap?: (offset: number) => number): void;
  keyDown?(event: KeyboardEvent): void;
  paste?(event: ClipboardEvent): void;
  /** A date mention pressed: true when its owner opened it. */
  datePressed?(mention: HTMLElement): boolean;
}) {
  const read = (): string => editor.state.doc.textBetween(0, editor.state.doc.content.size, "", words);
  const spans = (): TextSpan[] => fragmentToNativeRichText(editor.state.doc.firstChild!.content);
  // Its words as they are saved, a space typed as U+00A0 a space (fieldTypedSpaces.ts).
  const value = (): TitleValue => {
    const kept = cellValueFromSpans(fragmentToNativeRichText(fieldWordsTyped(editor.state)));
    return kept.spans ? { title: kept.value, spans: kept.spans } : { title: kept.value };
  };
  const fragment = (title: TitleValue) => nativeRichTextToFragment(titleFromUnits(titleUnits(title).map(oneLineUnit)).spans
    ?? [{ text: oneLine(title.title) }], editor.schema);
  let projected = false, disposed = false;
  let initialized: Editor | null = null;
  const publish = () => {
    if (projected || disposed || !options.editable()) return;
    if (imeComposing(initialized?.view)) options.composed?.(); else options.changed();
  };
  // What a composition finished is published before the next one starts (fieldKeyBoundary.ts).
  const composition = compositionEnds(() => disposed || !initialized || initialized.isDestroyed ? null : initialized.view, publish);
  const text = (doc: ProseMirrorNode) => doc.textBetween(0, doc.content.size, "", words);
  const fits = (tr: Transaction) => {
    let lines = false;
    tr.doc.descendants((node) => { if (node.type.name === "hardBreak") lines = true; });
    return !lines && (!options.maxRunes || fieldTakes(text(tr.doc), options.maxRunes, () => text(tr.before)));
  };
  const editor: Editor = new Editor({ element: { mount: element }, injectCSS: false, enableInputRules: ["nativeFieldTypedRules"], enablePasteRules: false,
    editable: options.editable(),
    // Its words take the body's typed shortcuts, as Notion's title (seen
    // 2026-11-11: "a--" "a—", "**b**" bold, "$$x$$" an equation, an address a
    // link; "[l](…)" stays words, as it does typed anywhere).
    extensions: [...nativeInlineFieldExtensions(fieldAtomView), AndroidEnter, AppleTouchPlainText, CompositionEndKey, TiptapReadOnlyMark,
      NativeFieldInputRules.configure({ enabled: () => options.editable() }),
      Extension.create({ name: "pageTitleBounds", addProseMirrorPlugins: () => [new Plugin({
        filterTransaction: tr => !tr.docChanged || Boolean(tr.getMeta(projection) || options.editable()) && tr.doc.childCount === 1 && fits(tr),
      })] })],
    content: { type: "doc", content: [{ type: "nativePropertyText" }] },
    editorProps: {
      attributes: () => {
        const attributes = options.attributes();
        return { ...attributes, class: `${attributes.class ?? ""} ${editorStyles.richMarks} ${editorStyles.plainMarks}`,
          "data-empty": String(!(initialized ? read() : initial.title)) };
      },
      transformPastedHTML: html => clipboardHtmlForEngine(html, EDITOR_PASTE_BOUNDS.richtext.maxHtmlCharacters),
      handleKeyDown: (view, event) => {
        // The input method's key is its own; Enter, Escape and Tab find the words typed published first (fieldKeyBoundary.ts).
        if (imeComposing(view)) return false;
        const meaning = fieldKey(event, { publish });
        if (meaning === "ime") return false;
        // A key on the heels of a click or a caret key (End, Home, an arrow)
        // acts where the caret is now, as the page's keys do: Enter just
        // after End split the title where it was clicked (2026-11-13).
        takeDomSelection(view);
        options.keyDown?.(event);
        if (event.defaultPrevented) return true;
        // Tab moves nothing from a page's title, the caret staying where it was, whether the page is open whole or
        // in a side, centre or mention peek, as Notion's (Chrome 1440, no script before the key, a letter typed
        // after it landing in the title: each peek's in two tabs 2026-11-26, a page's own in two tabs 2026-11-26).
        if (event.key === "Tab" && !(event.shiftKey || event.altKey || event.ctrlKey || event.metaKey)) {
          event.preventDefault();
          return true;
        }
        // A title takes no formatting from keys and no second line.
        const key = chordLetter(event);
        if (primaryModifier(event) && !event.altKey && (["b", "i", "u", "e"].includes(key) && !event.shiftKey || event.shiftKey && key === "s")
          || meaning === "confirm") {
          event.preventDefault();
          return true;
        }
        return false;
      },
      handlePaste: (_view, event) => {
        options.paste?.(event);
        if (!event.defaultPrevented) insert(event.clipboardData?.getData("text/plain") ?? "");
        return true;
      },
      handleDrop: () => true,
      handleClick: (_view, _pos, event) => {
        // A date mention pressed opens its date; a page mention opens its page, as in the body.
        const target = event.target as HTMLElement;
        const date = target.closest<HTMLElement>("[data-mention=\"date\"]");
        if (date && options.datePressed?.(date)) { event.preventDefault(); return true; }
        const anchor = target.closest<HTMLAnchorElement>("a[href]"), href = anchor?.getAttribute("href");
        if (!anchor || !href) return false;
        if (anchor.dataset.mention === "page") { event.preventDefault(); pushRoute(href); return true; }
        if (anchor.dataset.mention === "external" || event.metaKey || event.ctrlKey) {
          event.preventDefault(); window.open(href, "_blank", "noopener,noreferrer"); return true;
        }
        return false;
      },
      handleDOMEvents: { compositionend: () => { composition.ended(); return false; },
      // A menu's virtual focus announcement is not a real blur of this field.
      blur: (view, event) => !event.isTrusted && view.dom.ownerDocument.activeElement === view.dom },
    },
    onUpdate: ({ transaction }) => {
      publish();
      const before = transaction.before.firstChild!.content, after = transaction.doc.firstChild!.content;
      options.updated?.(transaction.getMeta(projection)
        ? (offset) => nativeOffsetAt(after, transaction.mapping.map(1 + pmOffsetAt(before, offset)) - 1) : undefined);
    },
    // A change of words calls it once its words are in (onUpdate), with the caret.
    onSelectionUpdate: ({ transaction }) => { if (!transaction.docChanged) options.updated?.(); },
    onBlur: () => options.blurred(),
  });
  initialized = editor;
  const release = registerIndependentTiptapFocus(element, editor);
  function dispatch(tr: Transaction, remote: boolean) {
    if (remote) tr.setMeta(projection, true).setMeta("addToHistory", false); else closeHistory(tr);
    projected = remote;
    try { editor.view.dispatch(tr); } finally { projected = false; }
  }
  /** Puts a title in the field, the caret at so many characters in when given. */
  function show(title: TitleValue, caret?: number, remote = false) {
    if (editor.isDestroyed) return;
    const field = editor.state.doc.firstChild!;
    const tr = editor.state.tr.replaceWith(1, field.content.size + 1, fragment(title));
    if (caret !== undefined) tr.setSelection(TextSelection.create(tr.doc, 1 + pmOffsetAt(tr.doc.firstChild!.content, Math.max(0, caret))));
    dispatch(tr, remote);
  }
  /**
   * Words written to the field from outside it — the title finished, a line
   * joined to it, a split at the caret: the words it keeps keep their
   * mentions and formatting, new ones come plain (respelledTitle).
   */
  function replace(title: string, caret?: number, remote = false) {
    if (editor.isDestroyed || read() === oneLine(title) && caret === undefined) return;
    show(respelledTitle(value(), oneLine(title)), caret, remote);
  }
  show(initial, undefined, true);
  // A title from elsewhere goes in change by change — a mention whole — so
  // the caret and what is chosen keep their places among the words around them.
  function follow(next: TitleValue) {
    if (editor.isDestroyed) return;
    const before = titleUnits(value()), after = titleUnits(next).map(oneLineUnit);
    if (options.maxRunes) {
      let runes = 0;
      const kept = after.findIndex((unit) => (runes += [...titleUnitText(unit).text].length) > options.maxRunes!);
      if (kept >= 0) after.length = kept;
    }
    const places = [1];
    for (const unit of before) {
      const { text, atom } = titleUnitText(unit);
      places.push(places[places.length - 1] + (atom ? 1 : text.length));
    }
    const tr = editor.state.tr;
    for (const change of textChanges(before, after).reverse()) {
      const inserted = titleFromUnits(change.insert);
      tr.replaceWith(places[change.from], places[change.to], change.insert.length
        ? nativeRichTextToFragment(inserted.spans ?? [{ text: inserted.title }], editor.schema) : Fragment.empty);
    }
    if (tr.docChanged) dispatch(tr, true);
  }
  /**
   * Puts pasted words where the caret is: plain words, or spans read from
   * them (pastedLine.ts). A plain line `typed` ends as typing it would (typedRuleAfter).
   */
  function insert(text: string | readonly TextSpan[], typed = false) {
    if (!editor.isEditable || !options.editable() || !text.length) return;
    const content = typeof text === "string" ? Fragment.from(editor.schema.text(oneLine(text))) : nativeRichTextToFragment(text, editor.schema);
    const tr = closeHistory(editor.state.tr.replaceSelection(new Slice(content, 0, 0)));
    editor.view.dispatch((typed ? typedRuleAfter(tr) : tr).scrollIntoView());
    editor.view.dispatch(closeHistory(editor.state.tr));
  }
  const selection = () => ({ start: nativeOffsetAt(editor.state.doc.firstChild!.content, editor.state.selection.from - 1),
    end: nativeOffsetAt(editor.state.doc.firstChild!.content, editor.state.selection.to - 1) });
  /** The field as its "@" menu writes into it (useFieldMenus). */
  const field: MenuField = {
    editor, read: spans, selection,
    replace(next, start, end = start, remote = false) {
      if (editor.isDestroyed) return;
      show({ title: next.map((span) => span.text).join(""), spans: next }, start, remote);
      const content = editor.state.doc.firstChild!.content;
      if (end !== start) editor.view.dispatch(editor.state.tr.setSelection(TextSelection.create(editor.state.doc,
        1 + pmOffsetAt(content, start), 1 + pmOffsetAt(content, end))));
      if (!remote) editor.view.focus();
    },
  };
  return {
    editor,
    read,
    value,
    field,
    replace,
    follow,
    /** Takes the field's editable state and attributes again. */
    refresh() {
      const editable = options.editable();
      if (editor.isEditable !== editable) editor.setEditable(editable);
      editor.setOptions({ editorProps: editor.options.editorProps });
    },
    insert,
    destroy() { disposed = true; composition.stop(); release(); editor.destroy(); },
  };
}

export type PageTitleEngine = ReturnType<typeof createPageTitleEngine>;

// A unit's words on one line.
function oneLineUnit(unit: string) {
  const { text, atom } = titleUnitText(unit);
  return atom || !text.includes("\n") ? unit : unit.replace(text, " ");
}

/**
 * A title written again as `words`: the characters it keeps keep their
 * formatting, a mention or equation all of whose words stay stays whole, and
 * other words are plain — a mention cut into is its words.
 */
export function respelledTitle(title: TitleValue, words: string): TitleValue {
  const units = titleUnits(title), characters: string[] = [], owner: number[] = [];
  units.forEach((unit, index) => {
    for (const character of titleUnitText(unit).text) { characters.push(character); owner.push(index); }
  });
  const next = Array.from(words), removed = new Uint8Array(characters.length), cut = new Set<number>();
  const typed = new Map<number, string[]>();
  for (const change of textChanges(characters, next)) {
    removed.fill(1, change.from, change.to);
    for (let at = change.from; at < change.to; at += 1) cut.add(owner[at]);
    // Words typed inside a mention cut it too.
    if (change.from > 0 && change.from < characters.length && owner[change.from - 1] === owner[change.from]) cut.add(owner[change.from]);
    if (change.insert.length) typed.set(change.from, change.insert);
  }
  const out: string[] = [];
  for (let place = 0; place <= characters.length; place += 1) {
    out.push(...(typed.get(place) ?? []));
    if (place === characters.length || removed[place]) continue;
    const index = owner[place], unit = units[index], { atom } = titleUnitText(unit);
    if (!atom) out.push(unit);
    else if (cut.has(index)) out.push(characters[place]);
    else if (place === 0 || owner[place - 1] !== index) out.push(unit);
  }
  return titleFromUnits(out);
}
