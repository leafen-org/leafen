import { useContext, useEffect, useRef } from "react";
import { commentWordsBlockId } from "@/components/comments/commentBlockElement";
import { openComments } from "@/components/comments/commentPanelEvents";
import { useOpenCommentThreads } from "./blockCommentCounts";
import { NativeTiptapReactContext, useNativeTiptapAdapter } from "./nativeTiptapReactContext";

/**
 * Pressing text a discussion was left on opens that discussion's card, as
 * Notion's (seen 2026-11-04): only the threads on that text, and the caret
 * stays in the text.
 */
export function NativeTiptapCommentMarkPress() {
  const { adapter: live, controller } = useContext(NativeTiptapReactContext)!;
  const adapter = useNativeTiptapAdapter(live);
  const editor = controller.editor;
  const open = useOpenCommentThreads();
  const latest = useRef({ open, pageId: adapter.pageId });
  latest.current = { open, pageId: adapter.pageId };
  useEffect(() => {
    if (!editor) return;
    const dom = editor.view.dom;
    const pressed = (event: MouseEvent) => {
      if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || !editor.state.selection.empty) return;
      const target = event.target instanceof Element ? event.target : null;
      const mark = target && !target.closest("a,[data-native-atom]") ? target.closest<HTMLElement>("[data-comment-id]") : null;
      const threadIds = mark?.dataset.commentId?.split(" ").filter((id) => latest.current.open.has(id));
      // A table cell's words are its row's.
      const blockId = mark ? commentWordsBlockId(mark) : null;
      if (threadIds?.length && blockId) openComments({ pageId: latest.current.pageId, blockId, threadIds });
    };
    dom.addEventListener("click", pressed);
    return () => dom.removeEventListener("click", pressed);
  }, [editor]);
  return null;
}
