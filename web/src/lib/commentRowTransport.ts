import type { CommentApi } from "@/commentApi";

/**
 * A database row's discussion through the page comment service: the
 * service's key is the row, its reads and changes use the row's routes, and
 * a deletion continues on the page holding the row.
 */
export function databaseRowCommentTransport(transport: CommentApi): CommentApi {
  const deletionPages = new Map<string, string>();
  const deletionPage = (rowId: string, jobId: string) => deletionPages.get(jobId) ?? rowId;
  return {
    ...transport,
    listComments: (session, rowId, input, signal) => transport.listDatabaseRowComments(session, rowId, input, signal),
    createComment: (session, rowId, input, signal) => transport.createDatabaseRowComment(session, rowId, input, signal),
    setCommentThreadState: (session, rowId, commentId, input, signal) => (
      transport.setDatabaseRowCommentThreadState(session, rowId, commentId, input, signal)
    ),
    setCommentReaction: (session, rowId, commentId, input, signal) => (
      transport.setDatabaseRowCommentReaction(session, rowId, commentId, input, signal)
    ),
    setPageReaction: (session, rowId, input, signal) => transport.setDatabaseRowPageReaction(session, rowId, input, signal),
    resolvePageReactions: (session, rowId, signal) => transport.resolveDatabaseRowPageReactions(session, rowId, signal),
    updateComment: (session, rowId, commentId, input, signal) => (
      transport.updateDatabaseRowComment(session, rowId, commentId, input, signal)
    ),
    createSuggestion: (session, rowId, input, signal) => transport.createDatabaseRowSuggestion(session, rowId, input, signal),
    resolveSuggestion: (session, rowId, commentId, input, signal) => (
      transport.resolveDatabaseRowSuggestion(session, rowId, commentId, input, signal)
    ),
    deleteComment: async (session, rowId, commentId, input, signal) => {
      const response = await transport.deleteDatabaseRowComment(session, rowId, commentId, input, signal);
      deletionPages.set(response.deletion.id, response.deletion.pageId);
      return response;
    },
    readCommentDeletion: (session, rowId, jobId, signal) => (
      transport.readCommentDeletion(session, deletionPage(rowId, jobId), jobId, signal)
    ),
    continueCommentDeletion: (session, rowId, jobId, signal) => (
      transport.continueCommentDeletion(session, deletionPage(rowId, jobId), jobId, signal)
    ),
  };
}
