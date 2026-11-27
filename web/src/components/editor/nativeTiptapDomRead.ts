import { Extension, type NodeViewRenderer } from "@tiptap/core";
import type { Node } from "@tiptap/pm/model";
import { Plugin, TextSelection, type Transaction } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";
import { takeDomSelection } from "@/lib/tiptapDomSelection";
import { imeComposing } from "@/lib/fieldKeyBoundary";
import { textLongPress } from "./textLongPress";

type DomObserver = { handleDOMChange: (...change: unknown[]) => void; flush: () => void; pendingRecords: () => MutationRecord[] };

/** How many widgets lie wholly between two places of a document. */
function widgetsBetween(doc: Node, from: number, to: number) {
  let count = 0;
  if (to > from) doc.nodesBetween(from, to, (node, pos) => {
    if (node.type.name === "nativeWidget") {
      if (pos >= from && pos + node.nodeSize <= to) count += 1;
      return false;
    }
    return !node.isTextblock;
  });
  return count;
}

/** Whether a change leaves fewer widgets where it replaced than there were. */
export function takesWidgetOut(tr: Transaction) {
  return tr.steps.some((step, index) => {
    const before = tr.docs[index], after = tr.mapping.slice(index);
    let lost = false;
    step.getMap().forEach((from, to) => {
      const had = lost ? 0 : widgetsBetween(before, from, to);
      if (had && widgetsBetween(tr.doc, after.map(from, -1), after.map(to, 1)) < had) lost = true;
    });
    return lost;
  });
}

/**
 * What the engine reads back from the page's elements never takes a widget
 * block — an inline database, a picture, an embed, a page link, a column —
 * out of the page; only a person's command does (a key, cut, a drop, the
 * block menu), each a change of its own. The browser's own edits (a phone's
 * text over chosen blocks, a native cut or drag out, a script's
 * execCommand) and an extension's take a widget's element out of the page,
 * and the engine read that as the block gone: an inline database left the
 * page for good (2026-11-13). The change read so is refused whole and the
 * engine draws the page back as it was, as Notion's puts the element back
 * (seen 2026-11-13: a database's element taken out came back, the same
 * element; text typed over chosen lines with a database among them did
 * nothing). A phone's delete and Enter come as input before they are read
 * and go as keys there (nativeTiptapMergeKeys); text put in over lines goes
 * as typed, in place of what is read (typesOverLines).
 */
function domReadKeepsWidgets() {
  let reading = 0;
  // Text the browser put in over lines, to go in as typed in place of what is read (typedInputOverLines).
  let typedOver: { text: string; doc: Node } | null = null;
  return new Plugin({
    view(view: EditorView) {
      const observer = (view as unknown as { domObserver?: DomObserver }).domObserver;
      const read = observer?.handleDOMChange, flush = observer?.flush;
      if (!observer || !read || !flush) return {};
      // A drawing is put back, and the selection where the engine has it, before anything is read,
      // on a page the reader can't change too.
      observer.flush = () => {
        const records = observer.pendingRecords();
        records.forEach(putsDrawingBack);
        keepsSelection(view, records.flatMap(contentMovedIn));
        flush.call(observer);
      };
      observer.handleDOMChange = (...change) => {
        const typed = typedOver;
        typedOver = null;
        if (typed && typed.doc === view.state.doc && typesOverLines(view)) {
          typeOver(view, typed.text);
          return;
        }
        reading += 1;
        try { read.apply(observer, change); } finally { reading -= 1; }
      };
      return { destroy: () => { observer.handleDOMChange = read; observer.flush = flush; } };
    },
    filterTransaction: (tr) => !reading || !tr.docChanged || Boolean(tr.getMeta("appendedTransaction")) || !takesWidgetOut(tr),
    props: { handleDOMEvents: {
      beforeinput: (view, event) => {
        const text = typedText(view, event);
        // A caret's typing is the browser's: only text over what is chosen is looked at.
        if (text === null || view.state.selection.empty && view.dom.ownerDocument.getSelection()?.isCollapsed !== false) return false;
        takeDomSelection(view);
        if (!typesOverLines(view)) return false;
        event.preventDefault();
        typeOver(view, text);
        return true;
      },
      // A script's execCommand says nothing before it edits the page's
      // elements: its input comes once it has, and what it did is read after.
      input: (view, event) => {
        const text = typedText(view, event);
        if (text !== null && typesOverLines(view)) {
          typedOver = { text, doc: view.state.doc };
          setTimeout(() => { typedOver = null; });
        }
        return false;
      },
    } },
  });
}

/** What a text input puts in — typing, a suggestion taken, dictation — when it is one line's text; null otherwise. */
function typedText(view: EditorView, event: Event) {
  const input = event as InputEvent;
  if (input.inputType !== "insertText" && input.inputType !== "insertReplacementText") return null;
  const text = input.data ?? input.dataTransfer?.getData("text/plain") ?? "";
  return !text || /[\r\n]/u.test(text) || imeComposing(view) ? null : text;
}

/**
 * Text that comes as input rather than as a key — a phone's keyboard and its
 * suggestions, dictation, a script's execCommand, an extension — over more
 * than one line, over a chosen block or over chosen code goes in as a typed
 * letter does (the engine's keypress): the lines join around it, the caret
 * after it, one ⌘Z taking it back. Left to the browser, it emptied the last
 * line's element rather than take it out, and the engine read an empty line
 * after the joined one (2026-11-13); Notion's joins them (seen 2026-11-13:
 * '뒤 글' to '끝 글' chosen across, 'y' made '뒤y 글' with no line after it,
 * one ⌘Z bringing both back). In code, Chrome took the colored words'
 * element out with the choice and put the letter beside the field, where
 * nothing reads it (2026-11-06). Over a widget block — an inline database, a
 * picture — nothing changes, as Notion's (seen 2026-11-13). Within one line's
 * text the browser's own stays.
 */
function typesOverLines(view: EditorView) {
  const { selection } = view.state, { $from, $to } = selection;
  return !selection.empty && !(selection instanceof TextSelection && $from.sameParent($to) && $from.parent.inlineContent
    && $from.parent.type.name !== "nativeCode");
}

function typeOver(view: EditorView, text: string) {
  const { $from, $to } = view.state.selection;
  const typed = () => view.state.tr.insertText(text).scrollIntoView();
  if (takesWidgetOut(typed())) return;
  if (!view.someProp("handleTextInput", handle => handle(view, $from.pos, $to.pos, text, typed))) view.dispatch(typed());
}

type KeptView = { dom: globalThis.Node; contentDOM?: HTMLElement | null };
// Each node view by its box, the element the engine places for it.
const keptViews = new WeakMap<globalThis.Node, KeptView>();

/**
 * A node view's own drawing — what its component draws in the view's box: a
 * database, a picture, a block's frame around its line — taken out of the
 * page by anything but the engine or the component is put back where it
 * was, as Notion's puts a block's element back (seen 2026-11-13). The engine
 * doesn't read what is drawn there as the page, so the block stayed but
 * showed nothing, nor what the engine draws inside it (2026-11-13: an inline
 * database's block frame, [data-block-id], taken out by a script, the
 * database gone until the page was opened again), and the component's next
 * unmount, leaving the page, stopped the app at '문제가 발생했습니다'
 * (2026-11-13: a database's drawing taken out). What is put back is what
 * left the box empty — no component of the page's draws nothing there — or
 * what held the engine's own part of the view: the component moves that part
 * into what it draws anew in the same commit, never leaving it out.
 */
/*
 * A view the engine has just made takes its caret from the engine within
 * the press that made it, though its component draws its frame only in the
 * microtasks after: the engine's part waits alone in the view's box until
 * then, the caret in it, and the component moves it into the frame with the
 * caret kept (keepsSelection). Text put in in the same moment — a script's
 * Enter and words, an extension, a phone keyboard's — goes into the new
 * line: with the caret left to the frame's first drawing, it went into the
 * line before (2026-11-16).
 */
export function keepsOwnDrawing(renderer: NodeViewRenderer): NodeViewRenderer {
  return (props) => {
    const view = renderer(props);
    keptViews.set(view.dom, view);
    return view;
  };
}

/** The node view a place in the page is in, and its box. */
function keptViewOf(place: globalThis.Node) {
  let box: globalThis.Node | null = place;
  while (box && !keptViews.has(box)) box = box.parentNode;
  return { box, view: box ? keptViews.get(box) : undefined };
}

function putsDrawingBack(record: MutationRecord) {
  const { target } = record;
  if (record.type !== "childList" || !record.removedNodes.length || !target.isConnected) return;
  // The view whose drawing it was in, and not the engine's own part of it, which the engine reads.
  const { box, view } = keptViewOf(target), content = view?.contentDOM;
  if (!box || !view || content?.contains(target)) return;
  const gone = [...record.removedNodes].filter(node => node instanceof Element && !node.isConnected);
  const emptied = target === box && box instanceof Element && !box.firstElementChild;
  const before = record.nextSibling?.parentNode === target ? record.nextSibling : null;
  gone.filter(node => emptied || content && node.contains(content)).forEach(node => target.insertBefore(node, before));
}

/** A view's own part of the page its component took into what it draws (a frame drawn anew, a view drawn late). */
function contentMovedIn(record: MutationRecord) {
  if (record.type !== "childList" || !record.addedNodes.length) return [];
  const content = keptViewOf(record.target).view?.contentDOM;
  return content && [...record.addedNodes].includes(content) ? [content] : [];
}

/**
 * A node view's component draws the engine's part of it into a frame of its
 * own once the engine has put it in the page: a block drawn anew in another
 * frame (a list line turned to a paragraph, a synced block), a block an Undo
 * brought back, drawn a moment after the engine chose in it. The browser's
 * selection falls out of what is moved unless it lies wholly inside, and the
 * engine then read it back elsewhere: a list line's ⌫ left the caret at its
 * end and the next ⌫ took a letter (2026-11-04); an Undo of text typed over
 * two lines chose only within the first (2026-11-13). The engine still has
 * the right place, so the browser's selection goes back to it before
 * anything reads it.
 */
function keepsSelection(view: EditorView, moved: readonly HTMLElement[]) {
  const { selection } = view.state, chosen = view.dom.ownerDocument.getSelection();
  if (!moved.length || !(selection instanceof TextSelection) || !chosen || imeComposing(view) || !view.hasFocus()) return;
  const anchor = view.domAtPos(selection.anchor), head = view.domAtPos(selection.head);
  if (!moved.some(content => content.contains(anchor.node) || content.contains(head.node))) return;
  if (chosen.anchorNode === anchor.node && chosen.anchorOffset === anchor.offset
    && chosen.focusNode === head.node && chosen.focusOffset === head.offset) return;
  chosen.setBaseAndExtent(anchor.node, anchor.offset, head.node, head.offset);
}

// prosemirror-view's look back at a focus: this long after the focus event it
// puts its own selection back if the browser's is not the one it last heard.
const engineFocusLookBack = 20;

/**
 * What a press chose stays chosen through the engine's look back at a focus.
 * Typed keys hold that timer back (input goes first), and the engine hears of
 * a press's selection only at its selectionchange, which came after it: a
 * press made meanwhile lost its choice — words double-pressed at once after
 * typing into a new page that its start panel's Escape had focused went back
 * to the caret at the line's end, and no toolbar came (2026-11-21, gate lap 6:
 * 3 of 20 new pages, the double press some 15 ms after the last key). So the
 * selection of a press made after a focus, before that look back has run, is
 * taken in as the press is released, and the engine then finds it its own.
 * A press that gives the focus, and any press after the look back, is read as
 * before.
 */
function pressSelectionKept() {
  // Focuses whose look back may not have run yet.
  let lookingBack = 0;
  return new Plugin({ props: { handleDOMEvents: {
    mousedown: (view) => {
      if (lookingBack) view.dom.ownerDocument.addEventListener("mouseup", () => {
        if (!view.isDestroyed && view.hasFocus()) takeDomSelection(view);
      }, { capture: true, once: true });
      return false;
    },
    focus: () => {
      lookingBack += 1;
      // Set just before the engine's own look back, this is due first; the
      // one it sets is due after the engine's, and ends the watch.
      setTimeout(() => setTimeout(() => { lookingBack -= 1; }), engineFocusLookBack);
      return false;
    },
  } } });
}

export const NativeTiptapDomRead = Extension.create({
  name: "nativeDomRead",
  addProseMirrorPlugins: () => [domReadKeepsWidgets(), pressSelectionKept(), textLongPress()],
});
