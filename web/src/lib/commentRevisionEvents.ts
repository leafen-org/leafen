// Another person's comment on an open page reaches it through the page's
// room: the server tells the room the page's comment revision
// (ygo_room_publication.go), and the comment readers that show the page read
// its comments again when they are behind it.

const COMMENT_REVISION_EVENT = "leafen:comment-revision";

export type CommentRevision = { pageId: string; revision: number };

/** Tells the comment readers a page's comments reached a revision. */
export function announceCommentRevision(pageId: string, revision: number) {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new CustomEvent<CommentRevision>(COMMENT_REVISION_EVENT, { detail: { pageId, revision } }));
}

/** Follows the comment revisions pages' rooms announce. */
export function onCommentRevision(listener: (change: CommentRevision) => void): () => void {
  if (typeof window === "undefined") return () => {};
  const handle = (event: Event) => {
    const detail = (event as CustomEvent<CommentRevision>).detail;
    if (detail && typeof detail.pageId === "string" && Number.isSafeInteger(detail.revision)) listener(detail);
  };
  window.addEventListener(COMMENT_REVISION_EVENT, handle);
  return () => window.removeEventListener(COMMENT_REVISION_EVENT, handle);
}
