import { createContext, useContext, useMemo, useState, type ReactNode } from "react";
import { commentChangeRefusal } from "@/components/comments/commentRefusal";
import { commentTextPlaces, hasQuotePlaces, type CommentQuotePlaces } from "@/components/comments/commentQuotePlaces";
import { ReactionPickerLayer } from "@/components/comments/CommentReactions";
import { usePendingCommentMark } from "@/components/comments/pendingCommentMark";
import type { CommentService } from "@/lib/commentService";
import { useOptionalProductToast } from "@/lib/productToast";

// A reaction to selected text, as Notion's toolbar reaction (seen
// 2026-09-30): the emoji picked begins a thread on that text with no comment,
// only the person's reaction; the server marks the text as any thread's —
// words across blocks and a caption's words too (seen 2026-11-09). Words of
// the page's title take a reaction on the page itself, which its discussion
// under the title shows (usePageDiscussionPlace), as Notion's phone tools do
// (seen 2026-11-14 in iPhone Safari: "😀 1", its add and resolve marks, then
// the "댓글 추가" field, left alone; there again when the page is opened).

export type TextReactionRequest = {
  anchor: DOMRect;
  pageId: string;
} & ({
  blockId: string;
  quote: string;
  quoteStart: number;
  quoteEnd: number;
  /** Where the words lie beyond the block's own text: its caption, the blocks they run on into. */
  places?: CommentQuotePlaces;
} | {
  /** Words of the page's title: the reaction is the page's own. */
  page: true;
});

const TextReactionStarter = createContext<((request: TextReactionRequest) => void) | null>(null);

/** Opens the emoji picker for a reaction to selected text; null where there is none. */
export function useTextReactionStarter() {
  return useContext(TextReactionStarter);
}

export function TextReactionProvider({ children, pageId, workspaceId, service }: {
  children: ReactNode;
  pageId: string;
  /** The page's workspace, whose recent and own emoji the picker offers, as a comment's does. */
  workspaceId?: string;
  service: CommentService | null;
}) {
  const toast = useOptionalProductToast();
  const [request, setRequest] = useState<TextReactionRequest | null>(null);
  // The words keep their mark while the emoji is picked, as Notion's (seen 2026-11-09).
  usePendingCommentMark(request && !("page" in request) ? commentTextPlaces(request.blockId, request.quoteStart, request.quoteEnd, request.places) : null);

  async function react(target: TextReactionRequest, emoji: string) {
    if (!service) return;
    if ("page" in target) {
      await service.reactToPage(pageId, emoji, true).catch((error: unknown) => toast?.notify(commentChangeRefusal(error), "error"));
      return;
    }
    try {
      const thread = await service.create(pageId, {
        blockId: target.blockId,
        body: { rich: [], quote: target.quote, quoteStart: target.quoteStart, quoteEnd: target.quoteEnd,
          ...(hasQuotePlaces(target.places) ? target.places : {}) },
      });
      await service.react(pageId, thread, emoji, true).catch(async (error: unknown) => {
        await service.remove(pageId, thread).catch(() => {});
        throw error;
      });
    } catch (error) {
      toast?.notify(commentChangeRefusal(error), "error");
    }
  }

  // The same function while the page and its comments service are: the
  // blocks that ask for it aren't drawn again each time the editor is.
  const start = useMemo(() => service ? (next: TextReactionRequest) => { if (next.pageId === pageId) setRequest(next); } : null, [pageId, service]);
  return (
    <TextReactionStarter.Provider value={start}>
      {children}
      {request ? (
        <ReactionPickerLayer anchor={request.anchor} marker="text-reaction-picker"
          place={workspaceId ? { workspaceId, pageId } : undefined}
          onClose={() => setRequest(null)}
          onPick={(emoji) => {
            setRequest(null);
            void react(request, emoji);
          }} />
      ) : null}
    </TextReactionStarter.Provider>
  );
}
