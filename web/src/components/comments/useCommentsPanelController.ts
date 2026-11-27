import type { ProductStatusSink } from "@/lib/productStatus";
import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import i18next from "i18next";
import { api } from "@/api";
import { userErrorToast } from "@/lib/settingsErrors";
import { caretMayComeBack } from "@/components/editor/focus";
import { announceSettledBlockSuggestion } from "@/components/editor/suggestingMode";
import type { NativeComment } from "@/lib/commentService";
import { commentServiceFor, databaseRowCommentServiceFor } from "@/lib/commentService";
import { motionSafeScrollBehavior } from "@/lib/motion";
import type { Block, TextSpan } from "@/lib/types";
import type { Session } from "@/types";
import {
  commentDraftEmpty,
  commentPropertyId,
  isReactionThread,
  editedCommentBody,
  keptCommentBody,
  sentCommentSpans,
  spansToPlainTextSafe,
} from "./commentModel";
import type { CommentFieldHandle } from "./CommentRichField";
import { useCommentAttachmentDraft } from "./commentAttachmentDraft";
import { createCommentDraftStore, useCommentDraftStore, type CommentDraftStore } from "./commentDraftStore";
import { commentChangeRefusal, commentReadGone } from "./commentRefusal";
import {
  discussionPeople,
  discussionShown,
  EVERY_DISCUSSION,
  discussionsInScope,
  type DiscussionFilter,
} from "./discussionListModel";
import { suggestionFacts } from "./suggestionModel";
import { hasQuotePlaces, type CommentQuotePlaces } from "./commentQuotePlaces";
import { commentBlockElement } from "./commentBlockElement";
import { revealTitleThread } from "./titleThreadReveal";
import { latestCalls, repliesByThread } from "./threadCardValues";
import { useWindowCloseReads } from "./useWindowCloseReads";
import { isGuestMFARequired } from "@/lib/guestMFARequired";

export type CommentsPanelControllerInput = {
  activeCommentId?: string;
  /** The comment was opened at its address (a notice, a link). */
  activeQuiet?: boolean;
  /** A block's window opened from its mark or its words, which reads what it showed as it closes. */
  readsShownOnClose?: boolean;
  /** Only these threads of the block: the ones on the text that was pressed. */
  threadIds?: readonly string[];
  /** A simple table's rows, whose discussions show with the table's own (OpenCommentsDetail). */
  tableRowIds?: readonly string[];
  /** The page's discussions as Notion's "All discussions" shows them (discussionsInScope), filtered. */
  all?: boolean;
  blockId?: string | null;
  blocks?: Block[];
  initialQuote?: string;
  initialQuoteEnd?: number;
  initialQuoteStart?: number;
  /** Where the quote lies beyond the block's own text: its caption, the blocks it runs on into. */
  initialQuotePlaces?: CommentQuotePlaces;
  onStatus?: ProductStatusSink;
  organizationId?: string;
  pageId: string;
  /** A database row's own discussion, on the page that holds the row. */
  rowId?: string;
  /**
   * The row's property whose threads these are, a new one quoting its value
   * (`initialQuote`) under its name, as Notion's property "댓글".
   */
  property?: { id: string; name: string };
  session: Session;
  /** The page's workspace, where files attached to comments go. */
  workspaceId?: string;
};

export function useCommentsPanelController(input: CommentsPanelControllerInput) {
  const service = useMemo(
    () => (input.rowId ? databaseRowCommentServiceFor(input.session, api) : commentServiceFor(input.session, api)),
    [input.rowId, input.session],
  );
  // The service keys a row's discussion by the row.
  const target = input.rowId ?? input.pageId;
  const subscribe = useMemo(
    () => (listener: () => void) => service.subscribe(target, listener),
    [target, service],
  );
  const getSnapshot = useMemo(
    () => () => service.getSnapshot(target),
    [target, service],
  );
  const snapshot = useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
  // The words typed in the new comment's field and in each thread's reply
  // box are kept in stores the fields read (commentDraftStore), not in the
  // state of what holds them: a letter typed redraws its field alone, not
  // every thread of the page.
  const draftStore = useCommentDraftStore();
  // The quote is the words as chosen, spaces at either end too, as the range
  // its mark covers and Notion's text-context (seen 2026-11-22: " 마 글 ");
  // words of spaces alone quote nothing.
  const [pendingQuote, setPendingQuote] = useState(input.initialQuote?.trim() ? input.initialQuote : "");
  const [submitting, setSubmitting] = useState(false);
  const [replyingId, setReplyingId] = useState<string | null>(null);
  // Each thread's reply box keeps its own words: typing in another thread's
  // box keeps this one's for it.
  const replyDrafts = useRef(new Map<string, CommentDraftStore>());
  function replyDraftOf(commentId: string): CommentDraftStore {
    let store = replyDrafts.current.get(commentId);
    if (!store) replyDrafts.current.set(commentId, store = createCommentDraftStore());
    return store;
  }
  const [submittingReply, setSubmittingReply] = useState(false);
  const [commentView, setCommentView] = useState<"open" | "resolved">("open");
  const [filter, setFilter] = useState<DiscussionFilter>(EVERY_DISCUSSION);
  const panelRef = useRef<HTMLElement>(null);
  const backdropRef = useRef<HTMLButtonElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const draftRef = useRef<CommentFieldHandle>(null);
  // The box of the reply being written.
  const replyRef = useRef<CommentFieldHandle>(null);
  const targetBlock = input.blockId
    ? input.blocks?.find((block) => block.id === input.blockId)
    : undefined;

  // The files a new comment and the reply being written hold.
  const draftAttachments = useCommentAttachmentDraft({ workspaceId: input.workspaceId, pageId: target, onError: (message: string) => input.onStatus?.(message, "error") });
  const replyAttachments = useCommentAttachmentDraft({ workspaceId: input.workspaceId, pageId: target, onError: (message: string) => input.onStatus?.(message, "error") });

  useEffect(() => {
    void service.load(target).catch(() => {});
  }, [target, service]);

  useEffect(() => {
    if (!snapshot.error || !input.onStatus) return;
    // A guest asked for 2-step verification sees the page's screen for it, not this refusal,
    // and so does one whose page is no longer theirs (the card saying so).
    if (isGuestMFARequired(snapshot.error) || commentReadGone(snapshot.error)) return;
    const text = userErrorToast(snapshot.error, i18next.t("commentsPanel:errors.loadFailed"));
    if (text) input.onStatus(text, "error");
  }, [input.onStatus, snapshot.error]);

  // A linked comment opens in the view that has it, once: resolved there
  // afterwards, it leaves the open threads as any thread does.
  const viewFollowed = useRef<string | null>(null);
  useEffect(() => {
    if (!input.activeCommentId || viewFollowed.current === input.activeCommentId) return;
    const target = snapshot.comments.find((comment) => comment.id === input.activeCommentId);
    if (!target) return;
    const root = target.parentId
      ? snapshot.comments.find((comment) => comment.id === target.parentId) ?? target
      : target;
    viewFollowed.current = input.activeCommentId;
    setCommentView(root.resolved ? "resolved" : "open");
  }, [input.activeCommentId, snapshot.comments]);

  const topLevel = snapshot.comments.filter((comment) => !comment.parentId);
  // A thread's replies stay the same list while they are the same, for its card (threadCardValues.ts).
  const keptReplies = useRef<ReadonlyMap<string, readonly NativeComment[]>>(new Map());
  const repliesByParent = useMemo(() => keptReplies.current = repliesByThread(snapshot.comments, keptReplies.current), [snapshot.comments]);
  const ordered = discussionsInScope(topLevel, { all: input.all, propertyId: input.property?.id, blockId: input.blockId,
    pressed: Boolean(input.threadIds), rows: input.tableRowIds });
  const open = ordered.filter((comment) => !comment.resolved);
  const resolved = ordered.filter((comment) => comment.resolved);
  const inView = input.all
    ? ordered.filter((comment) => discussionShown(comment, repliesByParent.get(comment.id) ?? [], filter))
    : commentView === "open" ? open : resolved;
  const visibleThreads = input.threadIds ? inView.filter((comment) => input.threadIds?.includes(comment.id)) : inView;
  // A block's window keeps showing what was new while open and reads it as it closes (useWindowCloseReads.ts).
  useWindowCloseReads({ service, target, blockId: input.blockId, addressed: input.activeQuiet ? input.activeCommentId : undefined,
    readsShown: Boolean(input.readsShownOnClose), shown: visibleThreads, comments: snapshot.comments });

  function report(error: unknown) {
    input.onStatus?.(commentChangeRefusal(error), "error");
  }

  // A thread on a page's text or a database value's words keeps its mark
  // there, which the server made as no edit (comment_text_anchors.go,
  // comment_cell_anchors.go): the editor and the cells show the marks of the
  // open threads alone (CommentAnchorMarks, CellCommentMarks), so resolving,
  // reopening or deleting one writes nothing, as Notion's lists no edit for
  // it and keeps its last edited time (seen 2026-11-11).

  // submit says whether the comment was made; with a thread, it is that thread's reply.
  // Enter pressed again while a comment goes sends the next as its own, as
  // Notion's (seen by the strike 2026-11-15 under the NAS condition: "c3",
  // "4" and "c5" each went alone where Leafen kept "c3c4c5" in its field):
  // each shows at once and they reach the server in the order written
  // (commentService create). A refused one's words come back ahead of any
  // typed since, and only the files it held leave the field.
  const inFlight = useRef({ comments: 0, replies: 0 });
  const replyingNow = useRef(replyingId);
  replyingNow.current = replyingId;
  const wordsBack = (sent: TextSpan[], now: TextSpan[]) => commentDraftEmpty(now) ? sent : [...sent, { text: "\n" }, ...now];
  // The field starts a new thread wherever it stands — under a page's title,
  // in a feed post's discussion, in a block's or a value's card; a thread is
  // answered from its own reply box (submitReply), as Notion's (seen
  // 2026-11-25: under a title, and a feed post's discussion in two tabs).
  async function submit(): Promise<boolean> {
    const draft = draftStore.get();
    const rich = sentCommentSpans(draft);
    const attachmentIds = draftAttachments.fileIds;
    if ((commentDraftEmpty(rich) && attachmentIds.length === 0) || draftAttachments.uploading) return false;
    inFlight.current.comments += 1;
    setSubmitting(true);
    // The box takes the caret back once the comment is answered, unless the person went on elsewhere meanwhile.
    const caretBack = caretMayComeBack();
    const quote = pendingQuote || undefined;
    // The field empties as the comment goes, which shows at once
    // (commentService create); a refused one's words come back to it.
    const sentDraft = draft;
    const sentFiles = draftAttachments.items.map((item) => item.key);
    draftStore.set([]);
    draftRef.current?.clear();
    setPendingQuote("");
    try {
      await service.create(target, {
        blockId: input.blockId,
        body: {
          rich,
          ...(attachmentIds.length > 0 ? { attachmentIds } : {}),
          ...(input.property ? { propertyId: input.property.id, propertyName: input.property.name } : {}),
          ...(quote ? { quote } : {}),
          ...(typeof input.initialQuoteStart === "number" ? { quoteStart: input.initialQuoteStart } : {}),
          ...(typeof input.initialQuoteEnd === "number" ? { quoteEnd: input.initialQuoteEnd } : {}),
          ...(quote && hasQuotePlaces(input.initialQuotePlaces) ? input.initialQuotePlaces : {}),
        },
      });
      sentFiles.forEach(draftAttachments.remove);
      return true;
    } catch (error) {
      // A field in focus keeps what comes from outside until it is left
      // (richTextCellEngine sync), so the words go back into it directly.
      const back = wordsBack(sentDraft, draftStore.get());
      draftStore.set(back);
      draftRef.current?.set(back);
      if (quote) setPendingQuote(quote);
      report(error);
      return false;
    } finally {
      inFlight.current.comments -= 1;
      if (inFlight.current.comments === 0) setSubmitting(false);
      window.requestAnimationFrame(() => { if (caretBack()) draftRef.current?.focus(); });
    }
  }

  async function submitReply(parent: NativeComment) {
    const replyDraft = replyDraftOf(parent.id);
    const rich = sentCommentSpans(replyDraft.get());
    const attachmentIds = replyAttachments.fileIds;
    if ((commentDraftEmpty(rich) && attachmentIds.length === 0) || replyAttachments.uploading) return;
    inFlight.current.replies += 1;
    setSubmittingReply(true);
    // As a comment: the reply shows at once and the field empties; a refused
    // one's words come back. The box stays the thread's while a next reply is
    // written in it.
    const sentReply = replyDraft.get();
    const sentFiles = replyAttachments.items.map((item) => item.key);
    replyDraft.set([]);
    replyRef.current?.clear();
    try {
      await service.create(target, {
        blockId: parent.blockId,
        parentId: parent.id,
        body: { rich, ...(attachmentIds.length > 0 ? { attachmentIds } : {}) },
      });
      setReplyingId((current) => current === parent.id && commentDraftEmpty(replyDraft.get()) ? null : current);
      sentFiles.forEach(replyAttachments.remove);
    } catch (error) {
      // Into its thread's box, straight into its field while it is the one being written.
      const back = wordsBack(sentReply, replyDraft.get());
      replyDraft.set(back);
      if (replyingNow.current === parent.id) replyRef.current?.set(back);
      report(error);
    } finally {
      inFlight.current.replies -= 1;
      if (inFlight.current.replies === 0) setSubmittingReply(false);
    }
  }

  function toggleResolved(comment: NativeComment) {
    void service.update(target, comment, { resolved: !comment.resolved }).catch(report);
  }

  function settle(comment: NativeComment, decision: "accept" | "reject") {
    // A suggestion that made blocks, or suggests blocks away, keeps or removes them as it settles.
    const facts = suggestionFacts(comment), madeBlocks = (facts?.blocks ?? 0) > 0 || (facts?.removes ?? 0) > 0;
    void service.settle(target, comment, decision).then(() => {
      if (madeBlocks) announceSettledBlockSuggestion(target);
    }, report);
  }

  // An edit that changed its files holds the ones kept and those added.
  function edit(comment: NativeComment, spans: readonly TextSpan[], files?: { kept: readonly string[]; added: readonly string[] }) {
    const { attachmentIds, ...body } = editedCommentBody(comment, spans);
    const ids = files ? [...files.kept, ...files.added] : attachmentIds ?? [];
    void service.update(target, comment, { body: ids.length > 0 ? { ...body, attachmentIds: ids } : body }).catch(report);
  }

  // Deleting a file from a comment keeps the rest of the comment, as Notion's chip menu does.
  function removeAttachment(comment: NativeComment, fileId: string) {
    const { attachmentIds = [], ...rest } = keptCommentBody(comment.body);
    const ids = attachmentIds.filter((id) => id !== fileId);
    void service.update(target, comment, { body: ids.length > 0 ? { ...rest, attachmentIds: ids } : rest }).catch(report);
  }

  // Taking back the last reaction of a thread begun by a reaction to text,
  // with no replies, removes the thread, as Notion's does.
  function react(comment: NativeComment, emoji: string, reacted: boolean) {
    void service.react(target, comment, emoji, reacted).then((next) => {
      if (reacted || !isReactionThread(next) || next.reactions.length > 0) return;
      if ((repliesByParent.get(next.id)?.length ?? 0) > 0) return;
      remove(next);
    }, report);
  }

  /** Adds or takes back the person's emoji on the page itself. */
  function resolvePageReactions() {
    void service.resolvePageReactions(target).catch(report);
  }

  function reactToPage(emoji: string, reacted: boolean) {
    void service.reactToPage(target, emoji, reacted).catch(report);
  }

  /** Sets the person's own state of the thread `root` begins; says whether it took. */
  function setThreadState(root: NativeComment, change: { repliesMuted?: boolean; unread?: boolean }) {
    return service.setThreadState(target, root, change).then(() => true, (error: unknown) => {
      report(error);
      return false;
    });
  }

  function remove(comment: NativeComment) {
    void service.remove(target, comment).catch(report);
  }

  function reveal(comment: NativeComment) {
    // A page's own thread shows its place under the page's title (titleThreadReveal).
    if (!comment.blockId && !commentPropertyId(comment.body)) {
      revealTitleThread(comment.id);
      return;
    }
    if (comment.blockId) {
      const url = new URL(window.location.href);
      url.hash = `comment-${encodeURIComponent(comment.id)}`;
      window.history.replaceState(window.history.state, "", `${url.pathname}${url.search}${url.hash}`);
    }
    const inline = document.querySelector<HTMLElement>(`[data-rt-editable] [data-comment-id~="${CSS.escape(comment.id)}"]`);
    const block = comment.blockId ? commentBlockElement(comment.blockId) : null;
    const propertyId = commentPropertyId(comment.body);
    const property = propertyId ? document.querySelector<HTMLElement>(`[data-row-property-id="${CSS.escape(propertyId)}"]`) : null;
    const target = inline ?? block ?? property;
    if (!target) return;
    target.scrollIntoView({ behavior: motionSafeScrollBehavior(), block: "center", inline: "nearest" });
    target.dataset.commentFlash = "true";
    window.setTimeout(() => delete target.dataset.commentFlash, 1400);
  }

  // The reply box typed in is the thread's that holds the reply's files.
  function startReply(comment: NativeComment) {
    if (replyingId === comment.id) return;
    // Files picked for one reply don't follow the box to another thread.
    replyAttachments.clear();
    setReplyingId(comment.id);
  }

  // What a thread's card calls: one object for the controller's life, each
  // call reaching this draw's functions, so a card left undrawn while
  // another thread changed still acts on the page's comments as they are
  // now (threadCardValues.ts).
  const threadCalls = { edit, react, remove, removeAttachment, reveal, setThreadState, settle, startReply, submitReply, toggleResolved };
  const latestThreadCalls = useRef(threadCalls);
  latestThreadCalls.current = threadCalls;
  const threadActions = useMemo(() => latestCalls(latestThreadCalls), []);

  return {
    ...snapshot,
    backdropRef,
    blockId: input.blockId,
    commentView,
    draftStore,
    draftAttachments,
    draftRef,
    edit,
    filter,
    listRef,
    openCount: open.length,
    ordered,
    panelRef,
    pendingQuote,
    people: discussionPeople(snapshot.comments),
    react,
    reactToPage,
    resolvePageReactions,
    setThreadState,
    remove,
    removeAttachment,
    repliesByParent,
    replyingId,
    replyAttachments,
    replyRef,
    reveal,
    resolvedCount: resolved.length,
    setCommentView,
    setFilter,
    setDraft: draftStore.set,
    settle,
    replyDraftOf,
    startReply,
    submit,
    submitReply,
    submitting,
    submittingReply,
    threadActions,
    targetPreview: targetBlock
      ? spansToPlainTextSafe(targetBlock.content?.rich) || targetBlock.plainText || targetBlock.type
      : "",
    toggleResolved,
    visibleThreads,
  };
}
