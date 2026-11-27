import type { CommentBody } from "@/commentWire.generated";
import type { NativeComment } from "./commentService";

/** A change to a comment: its words and files, or its thread resolved or reopened. */
export type CommentPatch = { body?: CommentBody; resolved?: boolean };

/**
 * A comment as shown while its change is on its way: resolved or reopened,
 * or its new words, edited now, with the files it keeps (one added comes with
 * the server's answer, which names it).
 */
export function commentPatched(comment: NativeComment, { body, resolved }: CommentPatch): NativeComment {
  const next = resolved === undefined ? comment : { ...comment, resolved };
  if (body === undefined) return next;
  const kept = comment.attachments?.filter((file) => body.attachmentIds?.includes(file.id));
  return { ...next, body, attachments: kept?.length ? kept : undefined, editedAt: new Date().toISOString() };
}

/** The comment shown with what a refused change touched as the server last said it. */
export function commentRestored(shown: NativeComment, confirmed: NativeComment, { body, resolved }: CommentPatch): NativeComment {
  return {
    ...shown,
    ...(resolved === undefined ? {} : { resolved: confirmed.resolved }),
    ...(body === undefined ? {} : { body: confirmed.body, attachments: confirmed.attachments, editedAt: confirmed.editedAt }),
  };
}

/** How a suggestion is settled. */
export type SuggestionDecision = "accept" | "reject";

/**
 * A suggestion's thread as shown while its settling is on its way: resolved,
 * its suggestion accepted or rejected, as the server's answer will have it
 * (page_suggestions.go finishSuggestionSettle).
 */
export function suggestionSettled(comment: NativeComment, decision: SuggestionDecision): NativeComment {
  const body = comment.body as CommentBody & { suggestion?: Record<string, unknown> };
  if (!body.suggestion) return { ...comment, resolved: true };
  const state = decision === "accept" ? "accepted" : "rejected";
  return { ...comment, resolved: true, body: { ...body, suggestion: { ...body.suggestion, state } } as CommentBody };
}

/** The thread shown as the server last said it, its settling refused. */
export function suggestionRestored(shown: NativeComment, confirmed: NativeComment): NativeComment {
  return { ...shown, resolved: confirmed.resolved, body: confirmed.body };
}

/** The blocks a suggestion suggests away (page_suggestion_removals.go), none for others. */
export function suggestionRemovedBlocks(comment: NativeComment): readonly string[] {
  const removed = (comment.body as { suggestion?: { removed?: unknown } }).suggestion?.removed;
  return Array.isArray(removed) ? removed.filter((id): id is string => typeof id === "string") : [];
}
