import { blockAnchor } from "@/lib/blockAddress";
import { Extension, Mark, Node, mergeAttributes, type Extensions, type NodeViewRenderer } from "@tiptap/core";
import { Plugin } from "@tiptap/pm/state";
import StarterKit from "@tiptap/starter-kit";
import Code from "@tiptap/extension-code";
import UniqueID from "@tiptap/extension-unique-id";
import { newId } from "@/lib/ids";
import { absolutePageUrl } from "@/lib/navigation";
import { clipboardServer, otherClipboardServer, withoutThreadMarks } from "@/lib/clipboardServer";
import { bigFontSize } from "@/lib/clipboardHtml";
import { dateMentionCopyText, dateMentionDisplayText } from "./dateMentions";
import { DOMSerializer, Fragment, type DOMOutputSpec, type Schema } from "@tiptap/pm/model";
import { nativeProjectionMeta } from "./nativeTiptapBinding";
import { NativeTiptapLineKeys } from "./nativeTiptapLineKeys";
import { NativeTiptapClipboardSelection } from "./nativeTiptapClipboardSelection";
import { NativeTiptapChosenAtoms } from "./nativeTiptapChosenAtoms";
import { FieldTypedSpaces } from "./fieldTypedSpaces";
import { NativeTiptapCaptionBound } from "./nativeTiptapCaptionBound";
import { textLinkHref } from "./richtextUrlPolicy";
import { readNativeInlineMetadata } from "./nativeTiptapInline";
import { otherServerContent, otherServerLine, pastedContent, type OtherServerCopy } from "./otherServerBlocks";
import { suggestionSplitsPlugin } from "./nativeTiptapSuggestionSplits";
import { underlineByItsKey } from "./underlineKey";

const NativeDocument = Node.create({ name: "doc", topNode: true, content: "nativeBlock*" });

function copiedProps(element: HTMLElement): Record<string, unknown> {
  try {
    const value: unknown = JSON.parse(element.getAttribute("data-props") ?? "{}");
    return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
  } catch { return {}; }
}

/** The other server a copied block was made on, by the one its copy names (clipboardServer.ts); null for this one. */
function copiedOn(element: HTMLElement): OtherServerCopy | null {
  const named = element.getAttribute("data-native-server");
  const server = named === null ? null : otherClipboardServer(named);
  return server === null ? null : { server: server ?? null, pageId: null };
}

/** The line a block copied on another server is here when it names something there (otherServerLine). */
function otherServerBlockLine(element: HTMLElement) {
  const from = copiedOn(element);
  const words = element.querySelector(":scope > [data-native-text]")?.textContent?.trim() ?? "";
  return from && otherServerLine(element.getAttribute("data-kind") ?? "", copiedProps(element), words,
    element.getAttribute("data-source-id") || element.getAttribute("data-id"), from);
}

const NativeBlock = Node.create({
  name: "nativeBlock", group: "block", content: "(nativeText | nativeCode | nativeWidget | nativeTableCells) nativeCaption? nativeChildren?",
  defining: true,
  addAttributes: () => ({
    id: { default: null },
    kind: { default: "paragraph", parseHTML: element => otherServerBlockLine(element) ? "paragraph" : element.getAttribute("data-kind"), rendered: false },
    sourceId: { default: null, parseHTML: element => element.getAttribute("data-source-id"), rendered: false },
    childrenSourceId: { default: null, rendered: false },
    projectionStatus: { default: null, rendered: false },
    props: { default: {}, rendered: false, parseHTML: element => {
      const props = copiedProps(element), from = copiedOn(element);
      if (!from) return pastedContent(props);
      return otherServerBlockLine(element) ? (typeof props.color === "string" ? { color: props.color } : {}) : otherServerContent(props, from.server);
    } },
  }),
  // A block copied on another server naming something there is a line of
  // its name, linked to it there (otherServerBlocks.ts).
  parseHTML: () => [{ tag: "div[data-native-block]", priority: 60, getAttrs: element => otherServerBlockLine(element) ? null : false,
    getContent: (element, schema) => otherServerLineContent(element as HTMLElement, schema) },
  { tag: "div[data-native-block]" }],
  // A copy names its server, whose pages, files and people its blocks name.
  renderHTML: ({ node }) => ["div", {
    "data-native-block": "true", "data-block-id": node.attrs.id,
    "data-kind": node.attrs.kind,
    "data-id": node.attrs.id, "data-source-id": node.attrs.sourceId,
    "data-props": JSON.stringify(node.attrs.props), "data-native-server": clipboardServer(),
  }, 0],
});

function otherServerLineContent(element: HTMLElement, schema: Schema) {
  const line = otherServerBlockLine(element);
  const words = line?.title ? schema.text(line.title, line.href ? [schema.marks.link.create({ href: line.href })] : []) : null;
  return Fragment.from(schema.nodes.nativeText.create(null, words));
}

const NativeChildren = Node.create({
  name: "nativeChildren", content: "nativeBlock+",
  parseHTML: () => [{ tag: "div[data-native-children]" }],
  renderHTML: () => ["div", { "data-native-children": "true" }, 0],
});

const NativeText = Node.create({
  name: "nativeText", content: "inline*", defining: true,
  parseHTML: () => [
    { tag: "div[data-native-text]" }, { tag: "p" },
    { tag: "h1" }, { tag: "h2" }, { tag: "h3" }, { tag: "h4" },
  ],
  renderHTML: ({ HTMLAttributes }) => ["div", mergeAttributes(HTMLAttributes, { "data-native-text": "true" }), 0],
});

const NativeCode = Node.create({
  // Words in code take what the toolbar gives, inline code too: a desktop's
  // selection panel offers none there (seen 2026-11-05), a phone's tools do
  // and draw it, as Notion's iPhone (seen 2026-11-14).
  name: "nativeCode", content: "text*", code: true, defining: true,
  marks: "annotation bold italic underline strike code link nativeColor nativeBackground",
  parseHTML: () => [{ tag: "pre", preserveWhitespace: "full" }],
  renderHTML: () => ["pre", { "data-native-code": "true" }, ["code", {}, 0]],
});

const NativeWidget = Node.create({
  name: "nativeWidget", atom: true,
  addAttributes: () => ({ blockId: { default: null, rendered: false } }),
  parseHTML: () => [{ tag: "div[data-native-widget]" }],
  renderHTML: () => ["div", { "data-native-widget": "true" }],
});

const NativeCaption = Node.create({
  name: "nativeCaption", content: "inline*", defining: true,
  parseHTML: () => [{ tag: "figcaption[data-native-caption]" }],
  renderHTML: () => ["figcaption", { "data-native-caption": "true" }, 0],
});

const NativeTableCells = Node.create({
  name: "nativeTableCells", content: "nativeTableCell+", isolating: true,
  parseHTML: () => [{ tag: "div[data-native-table-cells]" }],
  renderHTML: () => ["div", { "data-native-table-cells": "true" }, 0],
});

const NativeTableCell = Node.create({
  name: "nativeTableCell", content: "inline*", isolating: true,
  parseHTML: () => [{ tag: "div[data-native-table-cell]" }],
  renderHTML: () => ["div", { "data-native-table-cell": "true" }, 0],
});

const NativeAtom = Node.create({
  name: "nativeAtom", inline: true, group: "inline", atom: true,
  addAttributes: () => ({
    text: { default: "", rendered: false, parseHTML: element => element.getAttribute("data-text") ?? element.textContent ?? "" },
    payload: { default: "{}", rendered: false, parseHTML: element => element.getAttribute("data-payload") ?? "{}" },
  }),
  // One copied on another server is its words here, a page's or block's
  // mention its link to it there (clipboardServer.ts).
  parseHTML: () => [{ tag: "span[data-native-atom]", getAttrs: element => otherServerAtom(element) ? false : null },
    // The link a page mention takes to other apps is none here: its atom comes back alone.
    { tag: "a[data-native-mention-link]", skip: true, priority: 60,
      getAttrs: element => otherServerAtom(element.querySelector("span[data-native-atom]")) ? false : null }],
  renderHTML: ({ node }) => nativeAtomDom(node.attrs, nativeAtomCopyText(node.attrs)),
  renderText: ({ node }) => nativeAtomCopyText(node.attrs),
});

// An atom's markup with the words it gives other apps: a page here reads it
// back from its attributes (parseHTML above), whatever the words.
function nativeAtomDom(attrs: { text?: unknown; payload?: unknown }, words: string): DOMOutputSpec {
  const atom: DOMOutputSpec = ["span", { "data-native-atom": "true", "data-text": String(attrs.text ?? ""), "data-payload": String(attrs.payload ?? "{}"),
    ...atomServer(attrs.payload) }, words];
  // A page mention copied to another app links to its page's whole address, as Notion's (seen 2026-11-06), at its block for a block's.
  const page = mentionedPage(attrs.payload);
  return page ? ["a", { href: absolutePageUrl(page.pageId) + blockAnchor(page.pageId, page.blockId),
    "data-native-mention-link": "true" }, atom] : atom;
}

function atomPayload(payload: unknown): Record<string, unknown> {
  try {
    const value: unknown = typeof payload === "string" ? JSON.parse(payload) : null;
    return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
  } catch { return {}; }
}

function mentionedPage(payload: unknown) {
  const { mention, pageId, blockId } = atomPayload(payload);
  return mention === "page" && typeof pageId === "string" && pageId
    ? { pageId, blockId: typeof blockId === "string" && blockId ? blockId : null } : null;
}

// The mentions that name what is on their server: a copy of one says which.
const SERVER_MENTIONS = new Set(["page", "database", "person", "custom_emoji"]);

function atomServer(payload: unknown): Record<string, string> {
  return SERVER_MENTIONS.has(String(atomPayload(payload).mention)) ? { "data-native-server": clipboardServer() } : {};
}

/** Whether a copied atom names what is on another server than this one. */
function otherServerAtom(element: Element | null) {
  return element !== null && otherClipboardServer(element.getAttribute("data-native-server") ?? undefined) !== null;
}

/**
 * What an atom gives a copy for another app, wherever it is copied from: a
 * date mention its date with no "@" and no relative day, as a block's copy
 * gives it (dateMentionCopyText; Notion's ⌘C of words with "@오늘" in them,
 * seen 2026-11-06), anything else its text.
 */
export function nativeAtomCopyText(attrs: { text?: unknown; payload?: unknown }) {
  const value = atomPayload(attrs.payload);
  const text = (key: string) => typeof value[key] === "string" ? value[key] as string : undefined;
  const date = value.mention === "date" ? text("date") : undefined;
  return date ? dateMentionCopyText(date, text("dateEnd") ?? "", text("dateFormat"), text("dateTimeFormat")) : String(attrs.text ?? "");
}

/**
 * What an atom gives other apps when words are copied from within one line,
 * as Notion's (seen 2026-11-13): what its chip shows — a date mention's
 * words as they read today ("@내일", "@2026년 12월 24일"), a person's
 * "@이름", a page's or database's title after "@" in text and linked
 * without it in markup; an inline equation its TeX between "$"s in text and
 * the TeX alone in markup (seen 2026-11-14 and 2026-11-15: "줄 $x_1^2$ 끝",
 * its HTML a span of "x_1^2"); anything else its copy text. A copy of whole
 * lines or blocks keeps nativeAtomCopyText.
 */
export function nativeAtomShownText(attrs: { text?: unknown; payload?: unknown }, markup = false, now = new Date()) {
  const value = atomPayload(attrs.payload);
  const text = (key: string) => typeof value[key] === "string" ? value[key] as string : undefined;
  const words = String(attrs.text ?? "");
  const at = (shown: string) => shown.startsWith("@") ? shown : `@${shown}`;
  const date = value.mention === "date" ? text("date") : undefined;
  if (date) return at(dateMentionDisplayText(date, "", now, text("dateEnd") ?? "", text("dateFormat"), text("dateTimeFormat")));
  if (value.mention === "person") return at(words);
  if (value.mention === "page" || value.mention === "database") return markup ? words : at(words);
  const equation = text("equation");
  if (equation && !markup) return `$${equation}$`;
  return nativeAtomCopyText(attrs);
}

/** Text of words copied from within one line, each atom as nativeAtomShownText gives it; a hard break a new line. */
export function shownWordsText(content: Fragment) {
  return content.textBetween(0, content.size, "", node =>
    node.type.name === "nativeAtom" ? nativeAtomShownText(node.attrs) : node.type.name === "hardBreak" ? "\n" : "");
}

/** Markup of words copied from within one line, each atom as nativeAtomShownText gives it. */
export function shownWordsSerializer(schema: Schema) {
  const base = DOMSerializer.fromSchema(schema);
  return new DOMSerializer({ ...base.nodes, nativeAtom: node => nativeAtomDom(node.attrs, nativeAtomShownText(node.attrs, true)) }, base.marks);
}

const NativeAnnotation = Mark.create({
  name: "nativeAnnotation", group: "annotation", inclusive: false,
  addAttributes: () => ({ stringValue: { default: "{}" } }),
  addProseMirrorPlugins: () => [suggestionSplitsPlugin()],
  parseHTML: () => [{ tag: "span[data-native-annotation]", getAttrs: element => annotationAttrs(element) }],
  renderHTML: ({ mark }) => {
    let value: Record<string, unknown> = {};
    try { const parsed = JSON.parse(mark.attrs.stringValue); if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) value = parsed; } catch { /* invalid optional metadata */ }
    const id = (name: string) => typeof value[name] === "string" && /^[A-Za-z0-9_-]+$/.test(value[name] as string) ? value[name] as string : null;
    return ["span", { "data-native-annotation": mark.attrs.stringValue,
      // The threads marked on this text, separated by spaces.
      "data-comment-id": typeof value.commentId === "string" && /^[A-Za-z0-9_-]+( [A-Za-z0-9_-]+)*$/.test(value.commentId) ? value.commentId : null,
      "data-suggestion-id": id("suggestionId"),
      "data-suggestion": ["insert", "delete", "format"].includes(String(value.suggestion)) ? value.suggestion : null,
      "data-suggestion-before": typeof value.suggestionBefore === "string" ? value.suggestionBefore : null }, 0];
  },
});

/**
 * An annotation read from markup: a paste's or a drop's, copied anywhere,
 * keeps no comment or suggested-edit thread (withoutThreadMarks) but what
 * else it holds, or is no mark; the editor's own markup, read back after the
 * browser changed it in place, keeps it whole.
 */
function annotationAttrs(element: HTMLElement) {
  const stringValue = element.getAttribute("data-native-annotation") ?? "{}";
  // A paste's markup is read in a document of its own (prosemirror-view's readHTML).
  if (typeof document !== "undefined" && element.ownerDocument === document && element.isConnected) return { stringValue };
  const [kept] = withoutThreadMarks([readNativeInlineMetadata(stringValue)]);
  return Object.keys(kept).length ? { stringValue: JSON.stringify(kept) } : false;
}

const NativeColor = Mark.create({
  name: "nativeColor",
  addAttributes: () => ({ stringValue: { default: "default" } }),
  parseHTML: () => [{ tag: "span[data-native-color]", getAttrs: element => ({ stringValue: element.getAttribute("data-native-color") }) }],
  renderHTML: ({ mark }) => ["span", { "data-native-color": mark.attrs.stringValue, "data-color": mark.attrs.stringValue }, 0],
});

// A span's background, beside its text color, as Notion keeps both.
const NativeBackground = Mark.create({
  name: "nativeBackground",
  addAttributes: () => ({ stringValue: { default: "default_background" } }),
  parseHTML: () => [{ tag: "span[data-native-background]", getAttrs: element => ({ stringValue: element.getAttribute("data-native-background") }) }],
  renderHTML: ({ mark }) => ["span", { "data-native-background": mark.attrs.stringValue, "data-color": mark.attrs.stringValue }, 0],
});

// A link opens where its address leads (textLinkHref), a page's text's as
// its editor says (Editor.tsx).
function nativeInlineExtensions(history: boolean, linkHref: (href: string) => string = textLinkHref): Extensions {
  return [
    StarterKit.extend({ addExtensions() {
      return (this.parent?.() ?? []).map(extension => extension.name === "link" ? (extension as Mark).extend({
        renderHTML({ HTMLAttributes }) {
          return ["a", mergeAttributes(this.options.HTMLAttributes, HTMLAttributes,
            { href: linkHref(String(HTMLAttributes.href ?? "")) }), 0];
        },
      // Words pasted in big type are bold, as a page's paste reads them (richtextHtmlRead.ts).
      }) : extension.name === "bold" ? (extension as Mark).extend({
        parseHTML() {
          return [...this.parent?.() ?? [], { style: "font-size", getAttrs: (value: string) => bigFontSize(value) && null }];
        },
      // An underline or a strikethrough is drawn inside the words' color and
      // inline code, so its line takes their color, as Notion's one span with
      // both does (seen 2026-11-15: red code struck through in red, red words
      // underlined in red); marks after the others nest innermost. Underline's key is ⌘U alone (underlineKey.ts).
      }) : extension.name === "underline" ? underlineByItsKey((extension as Mark).extend({ priority: 90 }))
        : extension.name === "strike" ? (extension as Mark).extend({ priority: 90 }) : extension);
    } }).configure({
      document: false, paragraph: false, heading: false, codeBlock: false,
      blockquote: false, bulletList: false, listItem: false, listKeymap: false,
      orderedList: false, horizontalRule: false, trailingNode: false,
      undoRedo: history ? {} : false, code: false,
      // Notion has no caret between blocks; one there took typing as a new block part.
      gapcursor: false,
      // The body's own paste links chosen words, the caret after them (nativeTiptapClipboard).
      link: { openOnClick: false, autolink: false, linkOnPaste: false },
    }),
    NativeAtom, NativeAnnotation, NativeColor, NativeBackground, NativeTiptapLineKeys, NativeTiptapClipboardSelection,
    Code.extend({ excludes: "", addKeyboardShortcuts() {
      return { ...this.parent?.(), "Mod-e": () => this.editor.commands.toggleCode() };
    } }),
  ];
}

/** Native property fields share inline representation; their existing property
 * owner persists values and this sole view owns the field's input/history. */
// Words copied from a field (a cell's text, a title) give other apps its
// mentions as their chips show them, as words within a page's line do.
const NativeFieldShownWords = Extension.create({
  name: "nativeFieldShownWords",
  addProseMirrorPlugins() {
    return [new Plugin({ props: {
      clipboardTextSerializer: slice => shownWordsText(slice.content),
      clipboardSerializer: shownWordsSerializer(this.editor.schema),
    } })];
  },
});

export function nativeInlineFieldExtensions(atomView?: NodeViewRenderer): Extensions {
  // A field's mentions a selection passes over show its highlight, as the
  // page's do and Notion's fields' do (2026-11-10: a rename box's ⌘A).
  return [NativeTiptapChosenAtoms, NativeFieldShownWords, FieldTypedSpaces, Node.create({ name: "doc", topNode: true, content: "nativePropertyText" }),
    Node.create({ name: "nativePropertyText", content: "inline*", group: "block", defining: true,
      parseHTML: () => [{ tag: "span[data-native-property-text]" }, { tag: "p" }],
      renderHTML: () => ["span", { "data-native-property-text": "true", style: "display:block;white-space:inherit" }, 0],
    }),
    ...nativeInlineExtensions(true).map(extension => atomView && extension.name === "nativeAtom"
      ? NativeAtom.extend({ addNodeView: () => atomView }) : extension)];
}

/** Native blocks are the tree; their editable text DOM belongs to Tiptap. */
export function nativeTiptapExtensions(nodeViews: Readonly<Record<string, NodeViewRenderer>> = {}, linkHref?: (href: string) => string): Extensions {
  const extensions: Extensions = [
    ...nativeInlineExtensions(false, linkHref),
    NativeDocument, NativeBlock, NativeChildren, NativeText, NativeCode, NativeWidget,
    NativeCaption, NativeTiptapCaptionBound, NativeTableCells, NativeTableCell,
    UniqueID.configure({
      types: ["nativeBlock"], generateID: () => newId(),
      filterTransaction: transaction => !transaction.getMeta(nativeProjectionMeta),
    }),
  ];
  return extensions.map(extension => nodeViews[extension.name] && extension.type === "node"
    ? (extension as Node).extend({ addNodeView: () => nodeViews[extension.name] }) : extension);
}
