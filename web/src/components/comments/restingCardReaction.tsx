import { useState, type RefObject } from "react";
import { flushSync } from "react-dom";
import type { NativeComment } from "@/lib/commentService";
import { reactionPickerAnchor, useReactionPicker } from "./CommentReactions";
import type { useCommentsPanelController } from "./useCommentsPanelController";

type CommentsController = ReturnType<typeof useCommentsPanelController>;

/**
 * The reaction picker a resting margin card's comment opens — its 😊 or the
 * smiley after its reactions' chips, the thread's first comment's or its last
 * reply's — kept beside the cards rather than in the card, which its press
 * opens. As Notion's (measured 2026-11-26 in
 * Chrome at 1440 in two tabs at different times, judged after the press with
 * nothing run in the page before it, a letter typed next): the press opens the
 * card with the caret in its reply box, the letter going there, and the picker
 * under the button with no focus in it; a first Escape leaves the reply box
 * alone, the picker staying (CommentRichField), a second closes the picker;
 * a press beside it or an emoji picked closes it too — the card staying open
 * and the focus on nothing each time. The card opens first so that its layer
 * lies under the picker's (flushSync), and the picker stands where the button
 * stood (reactionPickerAnchor). Reached by the keys and opened by Enter, the
 * card rests and the picker takes the focus, as before (Notion's not measured).
 */
export function useRestingCardReaction(controller: CommentsController, openCard: RefObject<HTMLElement | null>,
  openPressed: (thread: NativeComment) => void) {
  const [reacting, setReacting] = useState<NativeComment | null>(null);
  const picker = useReactionPicker((emoji) => {
    const comment = reacting && (controller.comments.find((each) => each.id === reacting.id) ?? reacting);
    if (comment) controller.react(comment, emoji, true);
  }, reacting ? { workspaceId: reacting.workspaceId, pageId: reacting.pageId } : undefined);
  return {
    open(comment: NativeComment, thread: NativeComment, button: HTMLElement) {
      if (document.activeElement === button) {
        setReacting(comment);
        picker.open(button);
        return;
      }
      const at = reactionPickerAnchor(button);
      flushSync(() => {
        setReacting(comment);
        openPressed(thread);
      });
      // Closed, the comment's buttons in the open card rest while the pointer stays on it (lib/hoverRest.ts).
      picker.open(button, { at, leavesFocus: true,
        restAround: () => openCard.current?.querySelector(`[data-comment-id="${CSS.escape(comment.id)}"]`) });
    },
    picker: picker.picker,
  };
}
