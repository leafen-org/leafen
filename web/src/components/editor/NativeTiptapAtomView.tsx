import { popoverAnchorOf } from "@/lib/popoverPlacement";
import { useCallback, useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from "react";
import { createPortal } from "react-dom";
import { NodeViewWrapper, type NodeViewProps } from "@tiptap/react";
import { TextSelection } from "@tiptap/pm/state";
import type { DateMentionChange } from "@/collaboration";
import { changedDateAtomAttrs, fragmentToNativeRichText, readNativeInlineMetadata } from "./nativeTiptapInline";
import { spansToHtml } from "./richtext";
import { EDITOR_MARKDOWN_BOUNDS } from "./editorClipboardPolicy";
import { usePublicRichtextLinks } from "./publicRichtext";
import { nodeViewPlace, useNativeTiptapAdapter, useNativeTiptapNode } from "./nativeTiptapReactContext";
import { withCurrentMentionName, withCurrentPublishedMentionName } from "@/lib/peopleDirectory";
import { usePeopleDirectoryRevision } from "@/lib/workspacePeopleDirectory";
import { useMentionedPageNames } from "@/lib/pageNames";
import { dateMentionTarget, openTextBlockMention } from "./textBlockMentionClicks";
import styles from "./NativeTiptapAtomView.module.css";
import { DateMentionEditor, type DateMentionTarget } from "./DateMentionEditor";
import { MentionCard, type MentionCardTarget } from "./MentionCard";
import { InlineEquationEditor } from "./InlineEquationEditor";
import { HtmlSpanWithCustomEmoji, InlineCustomEmoji } from "./InlineCustomEmoji";
import type { PopoverRect } from "@/lib/popoverPlacement";
import { captureNativeTiptapRange, mapNativeTiptapRange, resolveNativeTiptapRange, type NativeTiptapRangeLease } from "./nativeTiptapRangeLease";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import { giveFocusBack } from "@/lib/focusGiveBack";

/** Noneditable atom visuals. The engine still owns their surrounding DOM. */
export function NativeTiptapAtomView(props: NodeViewProps) {
  const node = useNativeTiptapNode(props), { controller } = node;
  // An inline mention or date draws from the page's adapter as it is.
  const adapter = useNativeTiptapAdapter(node.adapter);
  const links = usePublicRichtextLinks();
  const [date, setDate] = useState<DateMentionTarget | null>(null);
  const [card, setCard] = useState<MentionCardTarget | null>(null);
  const [equation, setEquation] = useState<PopoverRect | null>(null);
  const [preview, setPreview] = useState<string | null>(null);
  const lease = useRef<NativeTiptapRangeLease | null>(null);
  const latest = useRef(adapter); latest.current = adapter;
  useEffect(() => {
    const map = ({ transaction }: { transaction: import("@tiptap/pm/state").Transaction }) => {
      if (!lease.current) return;
      mapNativeTiptapRange(lease.current, transaction);
      if (!resolveNativeTiptapRange(props.editor.state, latest.current.collaborationDocument, lease.current)) {
        lease.current = null; setDate(null); setEquation(null); setPreview(null);
      }
    };
    props.editor.on("transaction", map);
    return () => { props.editor.off("transaction", map); };
  }, [props.editor]);
  useEffect(() => { if (adapter.readOnly) { lease.current = null; setDate(null); setEquation(null); setPreview(null); } }, [adapter.readOnly]);
  const span = fragmentToNativeRichText(props.node.type.schema.nodes.nativeText.create(null, [props.node]).content)[0];
  // A person mention reads with the person's name now, redrawn as names change.
  usePeopleDirectoryRevision();
  // A page's, as it is renamed or typed anywhere (lib/pageNames.ts).
  useMentionedPageNames(span.mention === "page" ? [span] : null);
  const named = links ? withCurrentPublishedMentionName(span) : withCurrentMentionName(span);
  const shown = preview === null ? named : { ...span, text: preview, equation: preview, emptyPiece: preview ? undefined : "equation" as const };
  const closeDate = useCallback(() => { lease.current = null; setDate(null); }, []);
  const wrapper = useRef<HTMLSpanElement>(null);
  // Escape gives the date the focus back, ringed, and Enter there opens its
  // window again; a letter or ⌫ then does nothing, as Notion's (seen
  // 2026-11-14; measured 2026-11-15 in Chrome with no script in the page: a
  // letter typed after Escape went nowhere).
  const escapeDate = useCallback(() => {
    closeDate();
    const chip = wrapper.current?.querySelector<HTMLElement>('[data-mention="date"]');
    if (chip) { chip.tabIndex = -1; chip.focus({ preventScroll: true }); }
  }, [closeDate]);
  // A press beside the window (the page's text, its title) only closes it and
  // gives the date the focus back once the closing is painted, ringless until a
  // key comes, a letter going nowhere, as Notion's (seen 2026-11-26 in Chrome at
  // 1440 in two tabs at different times, judged after the press by the screen and
  // one letter typed: the date took the ring at the letter).
  const pressedAwayFromDate = useCallback(() => {
    closeDate();
    const chip = wrapper.current?.querySelector<HTMLElement>('[data-mention="date"]');
    if (!chip) return;
    chip.tabIndex = -1;
    giveFocusBack({ to: chip, keepScroll: true, ring: () => false, when: () => !document.activeElement || document.activeElement === document.body });
  }, [closeDate]);
  function begin() {
    const position = nodeViewPlace(props)?.pos;
    lease.current = position === undefined ? null : captureNativeTiptapRange(props.editor.state, adapter.collaborationDocument, position, position + 1);
    return Boolean(lease.current);
  }
  function current() {
    if (!lease.current || adapter.readOnly || !adapter.isCurrent() || props.editor.isDestroyed) return null;
    const range = resolveNativeTiptapRange(props.editor.state, adapter.collaborationDocument, lease.current);
    if (!range) return null;
    const position = range.from;
    const node = props.editor.state.doc.nodeAt(position);
    return node?.type.name === "nativeAtom" ? { position, node } : null;
  }
  function changeDate(change: DateMentionChange) {
    const target = current(); if (!target) return;
    controller.run(() => {
      lease.current = null;
      props.editor.view.dispatch(props.editor.state.tr.setNodeMarkup(target.position, undefined,
        changedDateAtomAttrs(target.node.attrs, change), target.node.marks));
      lease.current = captureNativeTiptapRange(props.editor.state, adapter.collaborationDocument, target.position, target.position + 1);
    });
  }
  function remove() {
    const target = current(); if (!target) return;
    const tr = props.editor.state.tr.delete(target.position, target.position + 1);
    tr.setSelection(TextSelection.create(tr.doc, target.position));
    props.editor.view.dispatch(tr); closeDate();
    // Writing goes on where the date stood; just closing it leaves no focus. Notion's,
    // measured 2026-11-15 in Chrome with no script in the page: after "삭제" the next
    // letter typed went where the date was; after Escape it went nowhere.
    if (!props.editor.isDestroyed) props.editor.view.focus();
  }
  function equationDone(tex: string) {
    const target = current(); lease.current = null; setEquation(null); setPreview(null); if (!target) return;
    controller.run(() => {
      const tr = props.editor.state.tr;
      // An empty equation given its TeX is an equation like any other.
      const metadata = readNativeInlineMetadata(target.node.attrs.payload);
      delete metadata.emptyPiece;
      if (tex) tr.setNodeMarkup(target.position, undefined, {
        text: tex, payload: JSON.stringify({ ...metadata, equation: tex }),
      }, target.node.marks);
      else tr.delete(target.position, target.position + 1);
      tr.setSelection(TextSelection.create(tr.doc, target.position + (tex ? 1 : 0)));
      props.editor.view.dispatch(tr); props.editor.view.focus();
    });
  }
  // A workspace emoji shows its picture (InlineCustomEmoji).
  if (span.mention === "custom_emoji" && span.customEmojiId) {
    return <NodeViewWrapper as="span" contentEditable={false} data-native-atom="true">
      <InlineCustomEmoji workspaceId={adapter.workspaceId} emojiId={span.customEmojiId} name={span.text} publicToken={span.customEmojiPublic} />
    </NodeViewWrapper>;
  }
  return <NodeViewWrapper as="span" ref={wrapper} className={styles.atom} contentEditable={false} data-native-atom="true"
    onKeyDown={(event: ReactKeyboardEvent<HTMLSpanElement>) => {
      const chip = event.target instanceof HTMLElement && event.target.matches('[data-mention="date"]') ? event.target : null;
      if (!chip || !fieldEnter(event) || adapter.readOnly) return;
      event.preventDefault();
      const target = dateMentionTarget(chip, chip.parentElement ?? chip);
      if (target && begin()) setDate(target);
    }}>
    <HtmlSpanWithCustomEmoji html={spansToHtml([shown], EDITOR_MARKDOWN_BOUNDS.richtext, links ?? undefined)}
      onClick={event => {
        if ((span.equation !== undefined || span.emptyPiece === "equation") && !adapter.readOnly) {
          event.preventDefault(); if (begin()) setEquation(popoverAnchorOf(event.currentTarget)); return;
        }
        openTextBlockMention(event, { adapter, showsRich: true, closeMenu() {}, openCard: setCard,
          openDate: target => { if (begin()) setDate(target); } });
      }} />
    {date && !adapter.readOnly && createPortal(<DateMentionEditor target={date} onChange={changeDate} onClose={closeDate} onEscape={escapeDate}
      onOutsidePress={pressedAwayFromDate} onRemove={remove} />, document.body)}
    {card && createPortal(<MentionCard adapter={adapter} target={card} onClose={() => setCard(null)} />, document.body)}
    {equation && !adapter.readOnly && <InlineEquationEditor anchor={equation} initial={span.equation ?? ""}
      onPreview={setPreview} onDone={equationDone} onCancel={() => { lease.current = null; setEquation(null); setPreview(null); props.editor.view.focus(); }} />}
  </NodeViewWrapper>;
}
