import type { Session } from "./types";
import {
  commentOperations,
  type CommentCreateInput,
  type CommentDeleteInput,
  type CommentDeletionResponse,
  type CommentLinkResponse,
  type CommentListResponse,
  type CommentMutationResponse,
  type CommentReactionInput,
  type CommentThreadStateInput,
  type CommentUpdateInput,
  type PageReactionResponse,
  type PropertyDiscussionListResponse,
  type SuggestionCreateInput,
  type SuggestionResolveInput,
} from "./commentWire.generated";

type JsonRequest = <T>(
  path: string,
  init?: RequestInit,
  session?: Session | null,
) => Promise<T>;

function pagePath(path: string, pageId: string) {
  return path.replace("{pageId}", encodeURIComponent(pageId));
}

function commentPath(path: string, pageId: string, commentId: string) {
  return pagePath(path, pageId).replace("{commentId}", encodeURIComponent(commentId));
}

function deletionPath(path: string, pageId: string, jobId: string) {
  return pagePath(path, pageId).replace("{jobId}", encodeURIComponent(jobId));
}

function rowPath(path: string, rowId: string) {
  return path.replace("{rowId}", encodeURIComponent(rowId));
}

function rowCommentPath(path: string, rowId: string, commentId: string) {
  return rowPath(path, rowId).replace("{commentId}", encodeURIComponent(commentId));
}

export function createCommentApi(request: JsonRequest) {
  return {
    /** The open discussions on the values of the rows a page holds, by row and property. */
    listPropertyDiscussions(session: Session, pageId: string, signal?: AbortSignal) {
      return request<PropertyDiscussionListResponse>(
        pagePath(commentOperations.listPropertyDiscussions.path, pageId),
        { method: commentOperations.listPropertyDiscussions.method, signal },
        session,
      );
    },

    /** A database row's own discussion, oldest first (a feed post's comments). */
    listDatabaseRowComments(
      session: Session,
      rowId: string,
      input: { cursor?: string; limit: number },
      signal?: AbortSignal,
    ) {
      const query = new URLSearchParams({ limit: String(input.limit) });
      if (input.cursor) query.set("cursor", input.cursor);
      return request<CommentListResponse>(
        `${rowPath(commentOperations.listDatabaseRowComments.path, rowId)}?${query}`,
        { method: commentOperations.listDatabaseRowComments.method, signal },
        session,
      );
    },

    createDatabaseRowComment(
      session: Session,
      rowId: string,
      input: CommentCreateInput,
      signal?: AbortSignal,
    ) {
      return request<CommentMutationResponse>(
        rowPath(commentOperations.createDatabaseRowComment.path, rowId),
        {
          method: commentOperations.createDatabaseRowComment.method,
          body: JSON.stringify(input),
          signal,
        },
        session,
      );
    },

    /** Edits or resolves a database row's comment. */
    updateDatabaseRowComment(
      session: Session,
      rowId: string,
      commentId: string,
      input: CommentUpdateInput,
      signal?: AbortSignal,
    ) {
      return request<CommentMutationResponse>(
        rowCommentPath(commentOperations.updateDatabaseRowComment.path, rowId, commentId),
        { method: commentOperations.updateDatabaseRowComment.method, body: JSON.stringify(input), signal },
        session,
      );
    },

    /**
     * Starts deleting a database row's comment and its replies; the page
     * holding the row reads and continues the deletion.
     */
    deleteDatabaseRowComment(
      session: Session,
      rowId: string,
      commentId: string,
      input: CommentDeleteInput,
      signal?: AbortSignal,
    ) {
      return request<CommentDeletionResponse>(
        rowCommentPath(commentOperations.deleteDatabaseRowComment.path, rowId, commentId),
        { method: commentOperations.deleteDatabaseRowComment.method, body: JSON.stringify(input), signal },
        session,
      );
    },

    /** Adds or takes back the person's emoji on a database row's comment. */
    setDatabaseRowCommentReaction(
      session: Session,
      rowId: string,
      commentId: string,
      input: CommentReactionInput,
      signal?: AbortSignal,
    ) {
      return request<CommentMutationResponse>(
        rowCommentPath(commentOperations.setDatabaseRowCommentReaction.path, rowId, commentId),
        { method: commentOperations.setDatabaseRowCommentReaction.method, body: JSON.stringify(input), signal },
        session,
      );
    },

    /** Resolves a database row's page's reactions together. */
    resolveDatabaseRowPageReactions(session: Session, rowId: string, signal?: AbortSignal) {
      return request<PageReactionResponse>(
        rowPath(commentOperations.resolveDatabaseRowPageReactions.path, rowId),
        { method: commentOperations.resolveDatabaseRowPageReactions.method, body: "{}", signal },
        session,
      );
    },

    /** Adds or takes back the person's emoji on a database row's page itself. */
    setDatabaseRowPageReaction(
      session: Session,
      rowId: string,
      input: CommentReactionInput,
      signal?: AbortSignal,
    ) {
      return request<PageReactionResponse>(
        rowPath(commentOperations.setDatabaseRowPageReaction.path, rowId),
        { method: commentOperations.setDatabaseRowPageReaction.method, body: JSON.stringify(input), signal },
        session,
      );
    },

    /** Sets the person's own state of a database row's comment thread. */
    setDatabaseRowCommentThreadState(
      session: Session,
      rowId: string,
      commentId: string,
      input: CommentThreadStateInput,
      signal?: AbortSignal,
    ) {
      return request<CommentMutationResponse>(
        rowCommentPath(commentOperations.setDatabaseRowCommentThreadState.path, rowId, commentId),
        { method: commentOperations.setDatabaseRowCommentThreadState.method, body: JSON.stringify(input), signal },
        session,
      );
    },

    /** Suggests an edit to a database page's body, its thread among the row's comments. */
    createDatabaseRowSuggestion(
      session: Session,
      rowId: string,
      input: SuggestionCreateInput,
      signal?: AbortSignal,
    ) {
      return request<CommentMutationResponse>(
        rowPath(commentOperations.createDatabaseRowSuggestion.path, rowId),
        { method: commentOperations.createDatabaseRowSuggestion.method, body: JSON.stringify(input), signal },
        session,
      );
    },

    /** Accepts or rejects a suggestion in a database page's body. */
    resolveDatabaseRowSuggestion(
      session: Session,
      rowId: string,
      commentId: string,
      input: SuggestionResolveInput,
      signal?: AbortSignal,
    ) {
      return request<CommentMutationResponse>(
        rowCommentPath(commentOperations.resolveDatabaseRowSuggestion.path, rowId, commentId),
        { method: commentOperations.resolveDatabaseRowSuggestion.method, body: JSON.stringify(input), signal },
        session,
      );
    },

    listComments(
      session: Session,
      pageId: string,
      input: { cursor?: string; limit: number },
      signal?: AbortSignal,
    ) {
      const query = new URLSearchParams({ limit: String(input.limit) });
      if (input.cursor) query.set("cursor", input.cursor);
      return request<CommentListResponse>(
        `${pagePath(commentOperations.listComments.path, pageId)}?${query}`,
        { method: commentOperations.listComments.method, signal },
        session,
      );
    },

    createComment(
      session: Session,
      pageId: string,
      input: CommentCreateInput,
      signal?: AbortSignal,
    ) {
      return request<CommentMutationResponse>(
        pagePath(commentOperations.createComment.path, pageId),
        {
          method: commentOperations.createComment.method,
          body: JSON.stringify(input),
          signal,
        },
        session,
      );
    },

    updateComment(
      session: Session,
      pageId: string,
      commentId: string,
      input: CommentUpdateInput,
      signal?: AbortSignal,
    ) {
      return request<CommentMutationResponse>(
        commentPath(commentOperations.updateComment.path, pageId, commentId),
        {
          method: commentOperations.updateComment.method,
          body: JSON.stringify(input),
          signal,
        },
        session,
      );
    },

    deleteComment(
      session: Session,
      pageId: string,
      commentId: string,
      input: CommentDeleteInput,
      signal?: AbortSignal,
    ) {
      return request<CommentDeletionResponse>(
        commentPath(commentOperations.deleteComment.path, pageId, commentId),
        {
          method: commentOperations.deleteComment.method,
          body: JSON.stringify(input),
          signal,
        },
        session,
      );
    },

    /** Adds or takes back the person's emoji on a comment. */
    setCommentReaction(
      session: Session,
      pageId: string,
      commentId: string,
      input: CommentReactionInput,
      signal?: AbortSignal,
    ) {
      return request<CommentMutationResponse>(
        commentPath(commentOperations.setCommentReaction.path, pageId, commentId),
        { method: commentOperations.setCommentReaction.method, body: JSON.stringify(input), signal },
        session,
      );
    },

    /** Resolves a page's reactions together, as Notion's "해결" on them. */
    resolvePageReactions(session: Session, pageId: string, signal?: AbortSignal) {
      return request<PageReactionResponse>(
        pagePath(commentOperations.resolvePageReactions.path, pageId),
        { method: commentOperations.resolvePageReactions.method, body: "{}", signal },
        session,
      );
    },

    /** Adds or takes back the person's emoji on a page itself. */
    setPageReaction(
      session: Session,
      pageId: string,
      input: CommentReactionInput,
      signal?: AbortSignal,
    ) {
      return request<PageReactionResponse>(
        pagePath(commentOperations.setPageReaction.path, pageId),
        { method: commentOperations.setPageReaction.method, body: JSON.stringify(input), signal },
        session,
      );
    },

    /**
     * Sets the person's own state of a comment thread, given by its first
     * comment: its replies' notifications turned off, or marked unread.
     */
    setCommentThreadState(
      session: Session,
      pageId: string,
      commentId: string,
      input: CommentThreadStateInput,
      signal?: AbortSignal,
    ) {
      return request<CommentMutationResponse>(
        commentPath(commentOperations.setCommentThreadState.path, pageId, commentId),
        { method: commentOperations.setCommentThreadState.method, body: JSON.stringify(input), signal },
        session,
      );
    },

    /** Suggests an edit to a block's text, starting or growing its suggestion's thread. */
    createSuggestion(
      session: Session,
      pageId: string,
      input: SuggestionCreateInput,
      signal?: AbortSignal,
    ) {
      return request<CommentMutationResponse>(
        pagePath(commentOperations.createSuggestion.path, pageId),
        { method: commentOperations.createSuggestion.method, body: JSON.stringify(input), signal },
        session,
      );
    },

    /** Accepts or rejects a suggestion, settling its text and resolving its thread. */
    resolveSuggestion(
      session: Session,
      pageId: string,
      commentId: string,
      input: SuggestionResolveInput,
      signal?: AbortSignal,
    ) {
      return request<CommentMutationResponse>(
        commentPath(commentOperations.resolveSuggestion.path, pageId, commentId),
        { method: commentOperations.resolveSuggestion.method, body: JSON.stringify(input), signal },
        session,
      );
    },

    readCommentDeletion(
      session: Session,
      pageId: string,
      jobId: string,
      signal?: AbortSignal,
    ) {
      return request<CommentDeletionResponse>(
        deletionPath(commentOperations.readCommentDeletion.path, pageId, jobId),
        { method: commentOperations.readCommentDeletion.method, signal },
        session,
      );
    },

    continueCommentDeletion(
      session: Session,
      pageId: string,
      jobId: string,
      signal?: AbortSignal,
    ) {
      return request<CommentDeletionResponse>(
        deletionPath(commentOperations.continueCommentDeletion.path, pageId, jobId),
        { method: commentOperations.continueCommentDeletion.method, signal },
        session,
      );
    },

    /**
     * The comment an address naming `commentId` opens now: that comment, or
     * for a deleted one its discussion's first comment (a row's page by its id).
     */
    readCommentLink(session: Session, pageId: string, commentId: string, signal?: AbortSignal) {
      return request<CommentLinkResponse>(
        commentPath(commentOperations.readCommentLink.path, pageId, commentId),
        { method: commentOperations.readCommentLink.method, signal },
        session,
      );
    },
  };
}

export type CommentApi = ReturnType<typeof createCommentApi>;
