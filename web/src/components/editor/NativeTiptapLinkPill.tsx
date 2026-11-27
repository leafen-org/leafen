import { getMarkRange } from "@tiptap/core";
import { TextSelection, type Transaction } from "@tiptap/pm/state";
import { useContext, useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { Copy, GlobeIcon, Trash } from "@/icons/leafen";
import { PageIcon } from "@/components/page/PageIcon";
import { copyText } from "@/lib/copyText";
import { pageHref } from "@/lib/navigation";
import { usePageNames } from "@/lib/pageNames";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { pageIdFromPageHref } from "@/lib/pageLinks";
import { normalizePastedLink } from "./editorInputModel";
import { linkedPage, openPressedLink, opensLink } from "./linkOpening";
import { linkCopyAddress } from "./linkCopyAddress";
import { NativeTiptapReactContext, useNativeTiptapAdapter } from "./nativeTiptapReactContext";
import { LINK_WINDOW_CHOICE, NATIVE_LINK_EDIT_REQUEST } from "./nativeTiptapInlineUiEvents";
import { nativeProjectionMeta } from "./nativeTiptapBinding";
import { markLinkWindowTarget, withLinkWindowTarget } from "./nativeTiptapLinkTarget";
import { SelectionToolbarLinkPicker } from "./SelectionToolbarLinkPicker";
import { PhoneLinkPreview, useLongPressChoice } from "./PhoneLinkPreview";
import { PhoneLinkSheet, type PhoneLinkEdit as PhoneLinkChange } from "./PhoneLinkSheet";
import { phoneScreen, touchScreen } from "@/lib/screenKind";
import styles from "./editorStyles.generated.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

type Place = { top: number; bottom: number; left: number; right: number };
type Hovered = { href: string; at: Place; from: number; to: number };
/** A link being edited, and the control its window opened from, if any. */
type Editing = Hovered & { opener?: HTMLElement | null };

const SHOW_AFTER_MS = 150, HIDE_AFTER_MS = 150;

/**
 * A link in the text, as Notion's (seen 2026-11-04). A press opens it, an
 * outside address in a new tab; the pointer on it brings its address (a page
 * here's icon and title, for a link to one), a copy button and "편집", 6px
 * under it from its left; "편집" opens its window in the
 * middle 8px under it: its address or a page, and its words. Asked for by the
 * selection panel's link button, the window drops from that button, 4px
 * under it from its left, and the panel stays, as Notion's (seen 2026-11-09).
 */
export function NativeTiptapLinkPill() {
  const pageNames = usePageNames();
  const { adapter: live, controller } = useContext(NativeTiptapReactContext)!;
  const adapter = useNativeTiptapAdapter(live);
  const editor = controller.editor;
  const { t } = useTranslation("selectionToolbar");
  const [hovered, setHovered] = useState<Hovered | null>(null);
  const [editing, setEditing] = useState<Editing | null>(null);
  const timer = useRef(0);
  const pillRef = useRef<HTMLDivElement>(null);
  const clearTimer = () => window.clearTimeout(timer.current);
  // A tip of its own over the page's layers, taking none of their keys or
  // presses: a center peek's window keeps it pressable as one of its own, as
  // any layer opened over it (2026-11-11: drawn apart on the body it was
  // inert there, and "편집" pressed the page mention under it).
  const { zIndex } = useOverlayLayer(Boolean(hovered && !editing), { kind: "tooltip", ref: pillRef });
  // A link's words a finger's long press chose, whose bar shows under them on a touch screen (PhoneLinkPreview).
  const [chosen, setChosen] = useState<Hovered | null>(null);
  useLongPressChoice(editor, () => {
    const type = editor.schema.marks.link, { from, to } = editor.state.selection;
    const range = touchScreen() && type ? getMarkRange(editor.state.doc.resolve(from), type) : undefined;
    const href = editor.state.doc.resolve(from + 1).marks().find((mark) => mark.type === type)?.attrs.href as string | undefined;
    if (!range || !href || range.from > from || range.to < to) { setChosen(null); return; }
    const start = editor.view.coordsAtPos(from), end = editor.view.coordsAtPos(to);
    setChosen({ href, from, to, at: { top: start.top, bottom: Math.max(start.bottom, end.bottom), left: start.left, right: end.right } });
  }, () => setChosen(null));

  useEffect(() => {
    const dom = editor.view.dom as HTMLElement;
    // A text's own links: a mention or a page's card (a child page, a link
    // to a page) opens through its own handler (2026-11-07: a view link's
    // child page, whose card goes nowhere, opened a new tab from here) — an
    // outside page's mention too, though its link wraps it, its card being
    // its own (LinkMentionPreview) — and a linked picture with its label
    // (ImageLinkLabel).
    // Code's words keep a link but draw none, as Notion's (codeHighlightPrismHtml.ts): no pill, no press.
    const inCode = (link: HTMLAnchorElement) => {
      try { return editor.state.doc.resolve(editor.view.posAtDOM(link, 0)).parent.type.spec.code === true; } catch { return false; }
    };
    const linkOf = (target: EventTarget | null) => {
      const link = target instanceof Element
        && !target.closest("[data-mention], [data-page-card], [data-image-link], [data-image-link-label]")
        ? target.closest<HTMLAnchorElement>("a[href]") : null;
      return link && !inCode(link) ? link : null;
    };
    // A press that chose words — dragged across the link, or Shift held —
    // opens nothing: the page's own selection as the click comes holds them.
    // The text's state is read only after the click, so it may still hold
    // what came before the press: the block a page opens on when it starts
    // with one (a link to a page or a database), a block or words chosen
    // before; a link pressed then opens, as Notion's does (seen 2026-11-22:
    // the first press on a page starting with a page link went nowhere).
    const choosing = () => {
      const chosen = document.getSelection();
      return Boolean(chosen && !chosen.isCollapsed && chosen.anchorNode && dom.contains(chosen.anchorNode));
    };
    const rangeOf = (link: HTMLAnchorElement): Hovered | null => {
      const type = editor.schema.marks.link;
      const range = type && getMarkRange(editor.state.doc.resolve(editor.view.posAtDOM(link, 0)), type);
      if (!range) return null;
      const at = link.getClientRects()[0] ?? link.getBoundingClientRect();
      // The address as the link keeps it, not as the anchor opens it (a
      // scheme-less one opens over https but is kept as written).
      const kept = editor.state.doc.resolve(range.from + 1).marks().find((mark) => mark.type === type)?.attrs.href as string | undefined;
      return { href: kept ?? link.getAttribute("href") ?? "", at: { top: at.top, bottom: at.bottom, left: at.left, right: at.right }, from: range.from, to: range.to };
    };
    // A finger brings no pill: a press opens the link and words chosen from
    // its end stay the tools', as Notion's on a phone (seen 2026-11-14 in
    // iPhone Safari); the pointer over it does, words chosen or not — with the
    // selection toolbar shown, the pill over it, as Notion's (seen 2026-11-14
    // and 2026-11-15 in Chrome at 1440: the link's words chosen, the pointer
    // out and back on the link, its card 6px under it, over the toolbar).
    // A pointer held down, choosing words across it, brings none.
    const over = (event: PointerEvent) => {
      const link = event.pointerType === "touch" || event.buttons ? null : linkOf(event.target);
      if (!link) return;
      clearTimer();
      timer.current = window.setTimeout(() => { if (link.isConnected) setHovered(rangeOf(link)); }, SHOW_AFTER_MS);
    };
    const out = (event: PointerEvent) => {
      if (!linkOf(event.target)) return;
      clearTimer();
      timer.current = window.setTimeout(() => setHovered(null), HIDE_AFTER_MS);
    };
    const press = (event: MouseEvent) => {
      // An outside page's mention opens its page in a new tab, as Notion's.
      const link = linkOf(event.target)
        ?? (event.target instanceof Element ? event.target.closest<HTMLAnchorElement>('a[data-mention="external"][href]') : null);
      // The first click alone (opensLink): a database's cell being edited in the page's text opened its link
      // here twice for a double-click, the words the second click chose being the cell's, not the page's (2026-11-18).
      if (!link || !opensLink(event) || choosing()) return;
      event.preventDefault();
      // An anchor (#block) goes to its block, chosen, the anchor in the address, as Notion's link
      // to a block of its page does (Notion's bare anchor does nothing, seen 2026-11-11); this
      // server's address of a page (a copied block link) opens it here, as Notion's own does —
      // in its place too from a side peek, where a mention of it opens in the peek (seen 2026-11-12).
      openPressedLink(link.getAttribute("href") ?? "", event.metaKey || event.ctrlKey, event);
    };
    const edit = (event: Event) => {
      const detail = (event as CustomEvent<{ editor: unknown; opener?: HTMLElement | null }>).detail;
      if (detail?.editor !== editor) return;
      const { from, to, empty } = editor.state.selection;
      if (empty) return;
      const start = editor.view.coordsAtPos(from), end = editor.view.coordsAtPos(to);
      const href = editor.state.doc.resolve(from + 1).marks().find((mark) => mark.type.name === "link")?.attrs.href as string | undefined;
      if (href) setEditing({ href, from, to, at: { top: start.top, bottom: end.bottom, left: start.left, right: end.right }, opener: detail.opener });
    };
    dom.addEventListener("pointerover", over); dom.addEventListener("pointerout", out); dom.addEventListener("click", press);
    window.addEventListener(NATIVE_LINK_EDIT_REQUEST, edit);
    return () => {
      clearTimer();
      dom.removeEventListener("pointerover", over); dom.removeEventListener("pointerout", out); dom.removeEventListener("click", press);
      window.removeEventListener(NATIVE_LINK_EDIT_REQUEST, edit);
    };
  }, [editor]);

  // A link to a page here names the page by its icon, with a page link's
  // arrow, and title, as Notion's names one of its own (seen 2026-11-11, a
  // block's link too); any other, an anchor alone too, shows as written.
  const page = hovered ? linkedPage(hovered.href, adapter.moveContext?.pages) : undefined;
  const pill = hovered && !editing ? createPortal(
    <div ref={pillRef} className={styles.linkPill} contentEditable={false} style={{ left: hovered.at.left, top: hovered.at.bottom + 6, zIndex }}
      onMouseEnter={clearTimer} onMouseLeave={() => { timer.current = window.setTimeout(() => setHovered(null), HIDE_AFTER_MS); }}
      onMouseDown={(event) => event.preventDefault()}>
      {page ? <PageIcon page={page} size={15} className={styles.linkPillPageIcon} />
        : <GlobeIcon size="tiny" className={styles.linkPillGlobe} aria-hidden="true" />}
      <span className={styles.linkPillAddress}>{page ? pageNames.title(page) : hovered.href}</span>
      <button type="button" className={styles.linkPillButton} aria-label={t("linkEditor.copy")} title={t("linkEditor.copy")}
        onClick={() => void copyText(linkCopyAddress(hovered.href))}><Copy size="small" aria-hidden="true" /></button>
      {adapter.readOnly ? null : <button type="button" className={styles.linkPillButton} onClick={() => { setEditing(hovered); setHovered(null); }}>{t("linkEditor.edit")}</button>}
    </div>, document.body) : null;
  const bar = chosen && !editing ? <PhoneLinkPreview href={chosen.href} at={chosen.at} field="page" readOnly={adapter.readOnly}
    onEdit={() => { setEditing(chosen); setChosen(null); }} /> : null;
  // A phone edits a link on its "링크" sheet, as the Notion Android app's (PhoneLinkSheet); other screens in the window.
  const editWindow = !editing ? null : phoneScreen() ? <PhoneLinkEdit key={editing.from} link={editing} onClose={() => setEditing(null)} />
    : <LinkEditWindow key={editing.from} link={editing} onClose={() => setEditing(null)} />;
  return <>{pill}{bar}{editWindow}</>;
}

/**
 * A link's edit on a phone's "링크" sheet (PhoneLinkSheet): as it closes its
 * words typed take the link's words' place, its address the link's, the
 * words chosen with the text's focus given back in the press, the keyboard
 * staying up, as the Notion Android app's (seen 2026-11-25 on a Galaxy A90:
 * "편집", a letter typed, "취소").
 */
function PhoneLinkEdit({ link, onClose }: { link: Editing; onClose: () => void }) {
  const { adapter: live, controller } = useContext(NativeTiptapReactContext)!;
  const adapter = useNativeTiptapAdapter(live);
  const editor = controller.editor;
  const [words] = useState(() => editor.state.doc.textBetween(link.from, link.to, ""));
  const done = ({ href, words: typed }: PhoneLinkChange) => {
    onClose();
    if (editor.isDestroyed || adapter.readOnly) return;
    editor.view.focus();
    const type = editor.schema.marks.link, tr = editor.state.tr;
    if (typed !== words) tr.insertText(typed, link.from, link.to);
    const end = link.from + typed.length;
    tr.removeMark(link.from, end, type);
    if (href) tr.addMark(link.from, end, type.create({ href }));
    editor.view.dispatch(tr.setSelection(TextSelection.create(tr.doc, link.from, end)));
  };
  return <PhoneLinkSheet href={link.href} edit={{ words, onDone: done }} onClose={onClose} onLink={() => onClose()}
    source={adapter.session ? { session: adapter.session, workspaceId: adapter.workspaceId } : undefined} />;
}

function LinkEditWindow({ link, onClose }: { link: Editing; onClose: () => void }) {
  const { adapter: live, controller } = useContext(NativeTiptapReactContext)!;
  const adapter = useNativeTiptapAdapter(live);
  const editor = controller.editor;
  const { t } = useTranslation("selectionToolbar");
  const ref = useRef<HTMLDivElement>(null), addressRef = useRef<HTMLInputElement>(null), titleRef = useRef<HTMLInputElement>(null);
  // A page's address is left out of its field, which shows the page while
  // the focus is elsewhere and is searched again once pressed, as Notion's
  // (seen 2026-11-12); left empty, it keeps the page.
  const pageLink = pageIdFromPageHref(link.href) ? link.href : undefined;
  const shownAddress = pageLink ? "" : link.href;
  const [address, setAddress] = useState(shownAddress);
  const [title, setTitle] = useState(() => editor.state.doc.textBetween(link.from, link.to));
  const saved = useRef(false);
  // Where the link's words are now: a title written while the window stays open moves their end.
  const span = useRef({ from: link.from, to: link.to });
  // The address the link has in the text now, which a title written as it is typed keeps.
  const written = useRef(link.href);
  // An address left as it was stays as written — a scheme-less one isn't
  // given https by opening and closing the window, as Notion's isn't (seen
  // 2026-11-10); one typed is taken as a pasted address is.
  const kept = () => address.trim() === shownAddress.trim() ? link.href
    : normalizePastedLink(address) || (address.trim() ? link.href : "");
  // Writes the title and address to the link, its words chosen; an emptied
  // title leaves the words as they were. A title written as it is typed
  // leaves the caret after the words, as typing there would, so the letters
  // typed together are one ⌘Z, as Notion's ("XY" typed on, one ⌘Z).
  const write = (href: string, done = false, typed = title, typing = false) => {
    const { schema } = editor.state, type = schema.marks.link;
    const tr = editor.state.tr, { from, to } = span.current;
    const shown = editor.state.doc.textBetween(from, to);
    const words = typed.trim() ? typed : shown;
    // Only what changed is written, as typing would write it: the words the
    // title shares with the text at either end stay.
    if (words !== shown) {
      let head = 0, tail = 0;
      while (head < Math.min(words.length, shown.length) && words[head] === shown[head]) head += 1;
      while (tail < Math.min(words.length, shown.length) - head && words[words.length - 1 - tail] === shown[shown.length - 1 - tail]) tail += 1;
      const added = words.slice(head, words.length - tail);
      const marks = editor.state.doc.resolve(from + 1).marks();
      if (added) tr.replaceWith(from + head, to - tail, schema.text(added, marks));
      else tr.delete(from + head, to - tail);
    }
    const end = from + words.length;
    // Letters typed take the words' own marks, link and all; marking them
    // again would end the typing's ⌘Z (nativeTiptapController's captureLocal).
    if (!typing) {
      tr.removeMark(from, end, type);
      if (href) tr.addMark(from, end, type.create({ href }));
    }
    tr.setSelection(TextSelection.create(tr.doc, typing ? end : from, end)).setMeta(LINK_WINDOW_CHOICE, done ? "done" : "quiet");
    withLinkWindowTarget(tr, done ? null : { from, to: end });
    if (!adapter.readOnly) editor.view.dispatch(tr);
    span.current = { from, to: end };
    written.current = href;
  };
  // However it closes the window keeps a typed address, words that are no
  // address leave the link as it was and an emptied address takes it off; the
  // link's words stay chosen, the text keeping the focus. Notion's, measured
  // 2026-11-15 in Chrome (1440, dark, no script in the page; one letter typed
  // after the press): a press outside left the words chosen with the
  // selection toolbar, and the letter took their place.
  const close = (href = kept()) => {
    if (saved.current) return;
    saved.current = true;
    write(href, true);
    onClose();
    editor.view.focus();
  };
  // Keys typed in the text while the window is open close it as it is, its
  // title not written back over what they wrote (2026-11-15: Escape, a
  // letter over the chosen words, then a press outside put the old title
  // back over the letter); Notion's closed on that letter, which stayed
  // (same measure). A change from elsewhere only moves the words.
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  useEffect(() => {
    function observe({ transaction: tr }: { transaction: Transaction }) {
      if (!tr.docChanged || tr.getMeta(LINK_WINDOW_CHOICE) || saved.current) return;
      if (tr.getMeta(nativeProjectionMeta)) {
        span.current = { from: tr.mapping.map(span.current.from, 1), to: tr.mapping.map(span.current.to, -1) };
        return;
      }
      saved.current = true;
      closeRef.current();
    }
    editor.on("transaction", observe);
    return () => { editor.off("transaction", observe); };
  }, [editor]);
  // Escape first leaves a field for the link's words, chosen in the text,
  // the window staying; the next closes it. Notion's, measured as above:
  // after Escape in the title field the field let go, the words were chosen
  // in the text, and the letter typed took their place.
  const escape = () => {
    const field = document.activeElement;
    if (!(field instanceof HTMLInputElement && ref.current?.contains(field))) { close(); return; }
    const { from, to } = span.current, words = TextSelection.create(editor.state.doc, from, to);
    if (!editor.state.selection.eq(words)) editor.view.dispatch(editor.state.tr.setSelection(words).setMeta(LINK_WINDOW_CHOICE, "quiet"));
    editor.view.focus();
  };
  const place = usePopoverPlacement(ref, (size, viewport) => {
    const button = link.opener?.isConnected ? popoverRectOf(link.opener) : null;
    return button ? placeAround(button, size, viewport, { gap: 4 }) : { anchor: link.at, placement: "bottom", gap: 8, options: {} };
  }, [link], undefined, {
    kind: "picker", restoreFocus: false, onEscape: escape, onOutsidePress: () => close(),
  });
  // Its link's words are its target while it is open (nativeTiptapLinkTarget),
  // and are chosen in the text as it opens, so each change it writes is undone
  // and redone with them chosen, wherever the caret was before: Notion's ⌘Z
  // after "링크 제거", or after a title typed, brought the link back with its
  // words chosen, and a letter typed then took their place (measured
  // 2026-11-16 in Chrome, 1440 dark, no script in the page; the caret left at
  // the end of the line above before "편집"). Here it went where the caret
  // had been before the window (2026-11-16: the page's first line).
  useLayoutEffect(() => {
    markLinkWindowTarget(editor.view, { from: link.from, to: link.to });
    const words = link.to <= editor.state.doc.content.size ? TextSelection.create(editor.state.doc, link.from, link.to) : null;
    if (words && !editor.state.selection.eq(words)) {
      editor.view.dispatch(editor.state.tr.setSelection(words).setMeta(LINK_WINDOW_CHOICE, "quiet").setMeta("addToHistory", false));
    }
    return () => markLinkWindowTarget(editor.view, null);
  }, [editor, link.from, link.to]);
  // The title field opens with the caret after its words, none chosen, so
  // what is typed goes on the end: Notion's "편집", measured 2026-11-15 in
  // Chrome (no script in the page; one letter typed after the press, landing
  // at the field's end). It takes the focus as it is drawn, so keys on the
  // heels of ⌘K on a link go there.
  useLayoutEffect(() => {
    const field = titleRef.current;
    field?.focus();
    field?.setSelectionRange(field.value.length, field.value.length);
  }, []);
  return createPortal(
    <div ref={ref} role="dialog" aria-label={t("linkEditor.ariaLabel")} className={styles.linkEditWindow} style={popoverPlacementStyle(place)}
      data-link-edit-window>
      <div className={styles.linkEditLabel}>{t("linkEditor.pageOrUrl")}</div>
      <SelectionToolbarLinkPicker inputRef={addressRef} value={address} onValue={setAddress} popoverRef={ref} placeholder={t("linkEditor.editPlaceholder")} current={shownAddress} pageLink={pageLink}
        source={adapter.session ? { session: adapter.session, workspaceId: adapter.workspaceId } : undefined}
        onPage={(page) => close(pageHref(page.id))} onAddress={() => { if (normalizePastedLink(address)) close(); }} />
      {address.trim() && address !== shownAddress ? null : <>
      <label className={styles.linkEditLabel}>{t("linkEditor.title")}
        {/* The words change in the text as they are typed, the window open,
            as Notion's (seen 2026-11-15 twice in Chrome, 1280 dark: "XY"
            typed on, then emptied and "Z" typed, drawn in the line at once;
            emptied, the line kept its last words). */}
        <input ref={titleRef} className={styles.tbLinkInput} value={title}
          onChange={(event) => { setTitle(event.target.value); write(written.current, false, event.target.value, true); }}
          // Enter writes the title and leaves the window open, "링크 제거" a press away (Notion's, seen 2026-11-07).
          onKeyDown={(event) => { if (fieldEnter(event)) { event.preventDefault(); write(kept()); } }} />
      </label>
      <div className={styles.linkEditDivider} />
      <button type="button" className={styles.linkEditRemove} onClick={() => close("")}><Trash size="regular" aria-hidden="true" />{t("linkEditor.removeLink")}</button>
      </>}
    </div>, document.body);
}
