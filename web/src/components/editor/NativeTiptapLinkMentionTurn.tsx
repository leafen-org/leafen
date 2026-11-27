import { useContext, useEffect, useRef } from "react";
import type { EditorView } from "@tiptap/pm/view";
import { TextSelection } from "@tiptap/pm/state";
import { LINK_MENTION_TURN_REQUEST, type LinkMentionTurnRequest } from "./linkMention";
import { projectedBlocks } from "./nativeTiptapBinding";
import { NativeTiptapReactContext, useNativeTiptapAdapter } from "./nativeTiptapReactContext";

/**
 * A link mention's "미리보기로 전환" in this editor's text, as Notion's (seen
 * 2026-11-11): the mention leaves its line — the words on either side close
 * up, a line it held alone stays empty — with the caret where it was, and a
 * bookmark of its link comes in right after the line showing the page's
 * preview the mention keeps (one kept before mentions kept it has its page
 * read, as an editor-made bookmark's is); one step ⌘Z takes back. Asked by
 * the mention's card (LinkMentionPreview), it answers only where the text
 * can be changed.
 */
export function NativeTiptapLinkMentionTurn() {
  const { adapter: live, controller } = useContext(NativeTiptapReactContext)!;
  const adapter = useNativeTiptapAdapter(live);
  const latest = useRef(adapter); latest.current = adapter;
  const editor = controller.editor;
  useEffect(() => {
    const dom = editor.view.dom as HTMLElement;
    const ask = (event: Event) => {
      const request = (event as CustomEvent<LinkMentionTurnRequest>).detail;
      const mention = event.target instanceof HTMLElement ? event.target.closest<HTMLAnchorElement>('a[data-mention="external"]') : null;
      if (!request || !mention || !dom.contains(mention) || latest.current.readOnly || !editor.isEditable || !latest.current.isCurrent()) return;
      request.turn = () => {
        const now = latest.current;
        if (now.readOnly || !now.isCurrent() || editor.isDestroyed || !mention.isConnected) return;
        const at = atomAt(editor.view, mention);
        const node = at < 0 ? null : editor.state.doc.nodeAt(at);
        const kept = node ? atomSpan(node.attrs.payload) : {};
        // Its link is the text's link around it, as kept (nativeTiptapInline).
        const link = node ? String(node.marks.find((mark) => mark.type.name === "link")?.attrs.href ?? mention.getAttribute("href") ?? "") : "";
        const block = projectedBlocks(editor.state.doc).find((candidate) =>
          at >= candidate.contentFrom && at < candidate.contentFrom + candidate.node.content.size);
        if (!node || !link || !block) return;
        controller.run(() => {
          const tr = editor.state.tr.delete(at, at + node.nodeSize);
          tr.setSelection(TextSelection.create(tr.doc, at));
          editor.view.dispatch(tr);
          now.insertAfter(block.nativeId, "bookmark", { url: link, ...keptPreview(kept, String(node.attrs.text ?? ""), link) });
        });
        editor.view.focus();
      };
    };
    dom.addEventListener(LINK_MENTION_TURN_REQUEST, ask);
    return () => dom.removeEventListener(LINK_MENTION_TURN_REQUEST, ask);
  }, [editor, controller]);
  return null;
}

/** Where the mention atom drawn by this element sits in the text, or -1. */
function atomAt(view: EditorView, element: HTMLElement) {
  const guess = view.posAtDOM(element, 0);
  for (const at of [guess, guess - 1, guess - 2, guess + 1]) {
    if (at < 0 || at >= view.state.doc.content.size) continue;
    const node = view.state.doc.nodeAt(at);
    const drawn = node?.type.name === "nativeAtom" ? view.nodeDOM(at) : null;
    if (drawn && drawn.contains(element)) return at;
  }
  return -1;
}

/** What a mention atom keeps, from its stored span. */
function atomSpan(payload: unknown): Record<string, unknown> {
  try {
    const value: unknown = typeof payload === "string" ? JSON.parse(payload) : null;
    return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
  } catch {
    return {};
  }
}

/** The bookmark's card from the preview the mention keeps, as read for its link; none where it keeps none. */
function keptPreview(kept: Record<string, unknown>, title: string, link: string) {
  const text = (key: string) => typeof kept[key] === "string" ? kept[key] as string : "";
  if (!text("description") && !text("imageUrl") && !text("provider")) return {};
  return { title, description: text("description"), iconUrl: text("iconUrl"), imageUrl: text("imageUrl"), provider: text("provider"), metadataUrl: link };
}
