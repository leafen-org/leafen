import { useId, useLayoutEffect } from "react";
import { requestCommentPendingWords, type CommentPendingState } from "@/components/editor/nativeTiptapCommentPending";
import { shownBlockElement } from "@/lib/shownBlockCopy";
import { commentCellText } from "./commentBlockElement";
import { commentQuoteCell, type CommentTextPlace } from "./commentQuotePlaces";

/** A block's text, its caption, or a simple table row's cell, as drawn. */
function placeField(place: Pick<CommentTextPlace, "blockId" | "field">) {
  const cell = commentQuoteCell(place.field);
  if (cell !== null) return commentCellText(place.blockId, cell);
  const block = shownBlockElement(place.blockId);
  return block?.querySelector(place.field === "caption" ? "[data-native-caption] [data-rt-editable]" : "[data-rt-editable]") ?? null;
}

/** A place's text from start to end, in its characters (UTF-16, as the server's anchor counts). */
function pendingCommentRange(place: CommentTextPlace): Range | null {
  const text = placeField(place);
  const { start, end } = place;
  if (!text || end <= start) return null;
  const walker = document.createTreeWalker(text, NodeFilter.SHOW_TEXT);
  const range = document.createRange();
  let at = 0, started = false;
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const length = node.nodeValue?.length ?? 0;
    if (!started && start < at + length) {
      range.setStart(node, start - at);
      started = true;
    }
    if (started && end <= at + length) {
      range.setEnd(node, end - at);
      return range;
    }
    at += length;
  }
  return null;
}

/**
 * The box of a quote's words as drawn, in every block they run across: what
 * Notion's reaction picker centres on (seen 2026-11-09), and a new comment's
 * field starts from (anchoredCardPlace.ts).
 */
export function pendingCommentBox(places: readonly CommentTextPlace[]): DOMRect | null {
  let left = Infinity, top = Infinity, right = -Infinity, bottom = -Infinity;
  for (const place of places) {
    const box = pendingCommentRange(place)?.getBoundingClientRect();
    if (!box) continue;
    left = Math.min(left, box.left); top = Math.min(top, box.top);
    right = Math.max(right, box.right); bottom = Math.max(bottom, box.bottom);
  }
  return right >= left ? new DOMRect(left, top, right - left, bottom - top) : null;
}

/**
 * While a comment is being written its text stays marked, as Notion's (seen
 * 2026-11-04) — the chosen words of every block it runs across (seen
 * 2026-11-09) — though focus left it and it has no thread yet; fainter while
 * its draft is put away (`kept`, seen 2026-11-12). The page's editor wraps
 * the words (nativeTiptapCommentPending.ts).
 */
export function usePendingCommentMark(places: readonly CommentTextPlace[] | null, state: CommentPendingState = "writing") {
  const owner = useId();
  const key = places ? JSON.stringify(places) : "";
  useLayoutEffect(() => {
    if (!key) return;
    const ranges = (JSON.parse(key) as CommentTextPlace[]).map(pendingCommentRange).filter((range): range is Range => range !== null);
    if (!ranges.length) return;
    requestCommentPendingWords(owner, ranges, state);
    return () => requestCommentPendingWords(owner, []);
  }, [key, owner, state]);
}
