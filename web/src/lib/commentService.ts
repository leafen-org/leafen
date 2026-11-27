import type { CommentApi } from "@/commentApi";
import {
  type CommentBody,
  commentErrorCodes,
  type CommentActorProfile,
  type CommentAttachment,
  type CommentDeletionRecord,
  type CommentReaction,
  type CommentListResponse,
  type CommentRecord,
  type SuggestionCreateInput,
} from "@/commentWire.generated";
import { newId } from "@/lib/ids";
import type { Comment } from "@/lib/types";
import type { Session } from "@/types";
import { onCommentRevision } from "./commentRevisionEvents";
import { onCommentRefresh } from "./commentRefreshEvents";
import { afterOwnWrites, sendInOrder } from "./ownWritesInFlight";
import { onNoticeArrival } from "./noticeArrivals";
import { keptComments, keptProfiles } from "./commentKept";
import { readThread, withThreadUnread } from "./commentThreadUnread";
import { announceOwnSuggestion, type PressMarked } from "./ownSuggestionEvents";
import { readWholeCommentPage } from "./commentPageReads";
import { commentsAfterDelete } from "./commentThreadHandover";
import * as reactions from "./commentReactions";
import {
  commentPatched, commentRestored, suggestionRemovedBlocks, suggestionRestored, suggestionSettled, type CommentPatch, type SuggestionDecision,
} from "./commentPatches";
import { showSuggestionSettling } from "./suggestionSettling";
import { databaseRowCommentTransport } from "./commentRowTransport";

export type NativeComment = Comment & Pick<CommentRecord, "originalGone"> & {
  /** The workspace files the comment holds, as they are now. */
  attachments?: readonly CommentAttachment[];
  /** Each emoji with the people who used it, in the order first used. */
  reactions: readonly CommentReaction[];
  /** On a thread's first comment: the person turned off its replies' notifications. */
  repliesMuted?: boolean;
  revision: number;
  /** On a thread's first comment: the person marked it unread. */
  markedUnread?: boolean;
  /** Someone else's comment the person was told of and hasn't read yet (commentThreadUnread.ts). */
  new?: boolean;
  /** On a thread's first comment: marked unread, or holding a new comment. */
  unread?: boolean;
  workspaceId: string;
};

export type CommentPageSnapshot = {
  actorProfiles: Readonly<Record<string, CommentActorProfile>>;
  comments: readonly NativeComment[];
  error: unknown;
  hasMore: boolean;
  loading: boolean;
  /** The page's own reactions, shown over its comments (a feed post's). */
  pageReactions: readonly CommentReaction[];
  pendingDeletion: CommentDeletionRecord | null;
  sourceRevision: number | null;
};

type PageOwner = {
  controller: AbortController | null;
  flight: Promise<void> | null;
  generation: number;
  listeners: Set<() => void>;
  stopFollowing: (() => void) | null;
  refreshQueued: boolean;
  /** The revision the queued read must reach; Infinity reads whatever the last one reached. */
  refreshTo: number;
  snapshot: CommentPageSnapshot;
};

const EMPTY_PROFILES: Readonly<Record<string, CommentActorProfile>> = Object.freeze({});
const EMPTY_SNAPSHOT: CommentPageSnapshot = Object.freeze({
  actorProfiles: EMPTY_PROFILES,
  comments: Object.freeze([]),
  error: null,
  hasMore: false,
  loading: false,
  pageReactions: Object.freeze([]),
  pendingDeletion: null,
  sourceRevision: null,
});

// Suggesting reads the page's threads again once typing pauses.
const SUGGESTION_RELOAD_DELAY_MS = 1500;

function nativeComment(record: CommentRecord): NativeComment {
  return {
    id: record.id,
    workspaceId: record.workspaceId,
    pageId: record.pageId,
    blockId: record.blockId || null,
    parentId: record.parentId || null,
    authorId: record.authorId,
    body: record.body,
    resolved: record.resolved,
    ...(record.attachments?.length ? { attachments: record.attachments } : {}),
    reactions: record.reactions ?? [],
    repliesMuted: record.repliesMuted || undefined, originalGone: record.originalGone || undefined,
    revision: record.revision,
    markedUnread: record.unread || undefined,
    new: record.new || undefined,
    createdAt: record.createdAt,
    updatedAt: record.updatedAt,
    editedAt: record.editedAt || undefined,
  };
}

function errorCode(error: unknown) {
  return error && typeof error === "object" && typeof Reflect.get(error, "code") === "string"
    ? String(Reflect.get(error, "code"))
    : "";
}

function byCreation(left: NativeComment, right: NativeComment) {
  return (left.createdAt ?? "").localeCompare(right.createdAt ?? "")
    || left.id.localeCompare(right.id);
}

export class CommentService {
  private readonly pages = new Map<string, PageOwner>();
  private readonly reloads = new Map<string, ReturnType<typeof setTimeout>>();
  /** Each comment with changes on their way: what the server last said of it, and how many go. */
  private readonly updating = new Map<string, { confirmed: NativeComment; count: number }>();

  private readonly reactionStore: reactions.CommentReactionStore;

  constructor(
    readonly session: Session,
    readonly transport: CommentApi,
  ) {
    this.reactionStore = {
      session, transport,
      supersedeRead: (pageId) => this.supersedeRead(pageId),
      comments: (pageId) => this.owner(pageId).snapshot.comments,
      pageReactions: (pageId) => this.owner(pageId).snapshot.pageReactions,
      replace: (pageId, comment) => this.replace(pageId, comment),
      land: (pageId, record) => this.replace(pageId, nativeComment(record)),
      showPageReactions: (pageId, pageReactions) => this.publish(pageId, { pageReactions }),
    };
  }

  private owner(pageId: string) {
    let owner = this.pages.get(pageId);
    if (!owner) {
      owner = {
        controller: null,
        flight: null,
        generation: 0,
        listeners: new Set(),
        stopFollowing: null,
        refreshQueued: false,
        refreshTo: 0,
        snapshot: EMPTY_SNAPSHOT,
      };
      this.pages.set(pageId, owner);
    }
    return owner;
  }

  private publish(pageId: string, change: Partial<CommentPageSnapshot>) {
    const owner = this.owner(pageId);
    // A thread is unread while marked so or holding a comment new to the reader.
    if (change.comments) change = { ...change, comments: withThreadUnread(change.comments) };
    owner.snapshot = { ...owner.snapshot, ...change };
    for (const listener of owner.listeners) listener();
  }

  private supersedeRead(pageId: string) {
    const owner = this.owner(pageId);
    owner.controller?.abort();
    owner.generation += 1;
    if (owner.snapshot.loading) this.publish(pageId, { loading: false });
  }

  private async throwAfterRevisionRefresh(pageId: string, error: unknown): Promise<never> {
    if (errorCode(error) === commentErrorCodes.revisionConflict) {
      await this.load(pageId, true).catch(() => {});
    }
    throw error;
  }

  getSnapshot = (pageId: string) => this.owner(pageId).snapshot;

  subscribe = (pageId: string, listener: () => void) => {
    const owner = this.owner(pageId);
    const first = owner.listeners.size === 0;
    owner.listeners.add(listener);
    if (first) {
      const stopRevision = onCommentRevision(({ pageId: changed, revision }) => {
        if (changed === pageId) this.followRevision(pageId, revision);
      });
      const stopRefresh = onCommentRefresh(pageId, () => this.refresh(pageId));
      // A comment's notice is made after the comment, and says it is new to
      // the reader: the open page's threads are read again when one comes.
      const stopNotices = onNoticeArrival(() => this.refresh(pageId));
      owner.stopFollowing = () => { stopRevision(); stopRefresh(); stopNotices(); };
      // A cached page may have changed while it had no readers (or room).
      if (owner.snapshot.sourceRevision !== null || owner.controller?.signal.aborted) this.refresh(pageId);
    }
    return () => {
      owner.listeners.delete(listener);
      if (owner.listeners.size === 0) {
        owner.stopFollowing?.();
        owner.stopFollowing = null;
        owner.controller?.abort();
      }
    };
  };

  /**
   * A wake/change during a read needs one read after it, not its old result;
   * a revision the room announced (`to`) needs one only if the read on its way
   * then doesn't reach it (2026-11-12: a page's comments were read twice as
   * it opened, the second for the revision the first was bringing).
   */
  private refresh(pageId: string, to = Number.POSITIVE_INFINITY) {
    const owner = this.owner(pageId);
    if (owner.listeners.size === 0) return;
    owner.refreshTo = Math.max(owner.refreshTo, to);
    if (owner.refreshQueued) return;
    owner.refreshQueued = true;
    const afterRead = async () => {
      while (owner.flight) await owner.flight.catch(() => {});
      // Nor before this tab's own comment changes on their way have landed:
      // read sooner, the list would show them undone (ownWritesInFlight.ts).
      await afterOwnWrites();
      owner.refreshQueued = false;
      const reached = owner.snapshot.sourceRevision;
      const needed = owner.refreshTo;
      owner.refreshTo = 0;
      if (owner.listeners.size === 0 || (reached !== null && reached >= needed)) return;
      return this.load(pageId, true);
    };
    void afterRead().catch(() => {});
  }

  /**
   * A page's comments reached a revision, as its room announced: shown, they
   * are read again when behind it; not shown, the next read starts afresh.
   */
  followRevision(pageId: string, revision: number) {
    const owner = this.pages.get(pageId);
    // One shown but never read in full, as a page just made whose first read
    // came before it was stored, is read now too.
    if (!owner || owner.snapshot.sourceRevision !== null && owner.snapshot.sourceRevision >= revision) return;
    if (owner.listeners.size > 0) {
      this.refresh(pageId, revision);
      return;
    }
    owner.snapshot = { ...owner.snapshot, sourceRevision: null };
  }

  load(pageId: string, restart = false): Promise<void> {
    const owner = this.owner(pageId);
    if (!restart && owner.snapshot.sourceRevision !== null) return Promise.resolve();
    return this.readPage(pageId, restart);
  }

  /** A page's comments come whole with its read (commentPageReads.ts): this waits for the rest. */
  loadMore(pageId: string): Promise<void> {
    const owner = this.owner(pageId);
    return owner.flight ?? (owner.snapshot.hasMore ? this.readPage(pageId, true) : Promise.resolve());
  }

  private readPage(pageId: string, restart: boolean): Promise<void> {
    const owner = this.owner(pageId);
    if (owner.flight && !restart) return owner.flight;
    owner.controller?.abort();
    owner.generation += 1;
    const generation = owner.generation;
    const controller = new AbortController();
    owner.controller = controller;
    // What shows stays until the read is whole, so nothing shown leaves meanwhile;
    // a page showing none yet shows each part as it comes.
    const growing = owner.snapshot.sourceRevision === null && owner.snapshot.comments.length === 0;
    const show = (parts: readonly CommentListResponse[], whole: boolean) => {
      if (controller.signal.aborted || generation !== owner.generation) return;
      this.publish(pageId, {
        actorProfiles: keptProfiles(owner.snapshot.actorProfiles, parts),
        comments: keptComments(owner.snapshot.comments, parts.flatMap((part) => part.items.map(nativeComment)).sort(byCreation)),
        error: null,
        hasMore: !whole,
        loading: !whole,
        pageReactions: parts[0]?.pageReactions ?? [],
        sourceRevision: whole ? parts[0]?.sourceRevision ?? null : null,
      });
    };
    this.publish(pageId, { error: null, loading: true });
    const flight = readWholeCommentPage(
      this.transport, this.session, pageId, controller.signal,
      growing ? (parts) => { if (parts[parts.length - 1]?.hasMore) show(parts, false); } : undefined,
    ).then((parts) => show(parts, true)).catch((error) => {
      if (generation !== owner.generation) return;
      if (controller.signal.aborted) {
        this.publish(pageId, { loading: false });
        return;
      }
      this.publish(pageId, { error, loading: false });
      throw error;
    }).finally(() => {
      if (owner.flight === flight) {
        owner.controller = null;
        owner.flight = null;
      }
    });
    owner.flight = flight;
    return flight;
  }

  async create(
    pageId: string,
    input: { blockId?: string | null; body: CommentBody; parentId?: string | null },
  ) {
    this.supersedeRead(pageId);
    // It shows in its thread as it is sent, as Notion's does at Enter, and
    // goes again if the server refuses it (2026-11-07: it came only with the
    // server's answer, seconds later under load).
    const id = newId(), now = new Date().toISOString();
    this.merge(pageId, {
      id, workspaceId: this.owner(pageId).snapshot.comments[0]?.workspaceId ?? "", pageId,
      blockId: input.blockId ?? "", parentId: input.parentId ?? "", authorId: this.session.user.id,
      body: input.body, resolved: false, revision: 0, createdAt: now, updatedAt: now, editedAt: "",
    });
    try {
      // Sent after the page's comments sent before it, in the order written (ownWritesInFlight);
      // shown it keeps the moment it was written, so one written while it went stays after it.
      const response = await sendInOrder(pageId, () => this.transport.createComment(this.session, pageId, {
        id, requestId: newId(), body: input.body,
        ...(input.blockId ? { blockId: input.blockId } : {}),
        ...(input.parentId ? { parentId: input.parentId } : {}),
      }));
      return this.merge(pageId, { ...response.comment, createdAt: now });
    } catch (error) {
      const owner = this.owner(pageId);
      this.publish(pageId, { comments: owner.snapshot.comments.filter((comment) => comment.id !== id) });
      throw error;
    }
  }

  private merge(pageId: string, record: CommentRecord) {
    const comment = nativeComment(record);
    const owner = this.owner(pageId);
    const comments = [...owner.snapshot.comments.filter((item) => item.id !== comment.id), comment].sort(byCreation);
    // Replying reads the thread, as the server keeps it.
    this.publish(pageId, { comments: comment.parentId ? readThread(comments, comment.parentId) : comments });
    return comment;
  }

  /**
   * Suggests an edit to a block's text. The thread it starts or grows comes
   * back at once, told to the page (ownSuggestionEvents.ts); other threads the
   * edit changed (the person's own suggested text it removed) are read again soon.
   */
  async suggest(pageId: string, { askedAtPress, ...input }: PressMarked<Omit<SuggestionCreateInput, "id" | "requestId">>) {
    this.supersedeRead(pageId);
    const response = await this.transport.createSuggestion(this.session, pageId, {
      id: newId(), requestId: newId(), ...input,
    });
    this.scheduleReload(pageId);
    return announceOwnSuggestion(pageId, this.merge(pageId, response.comment), askedAtPress);
  }

  /**
   * Accepts or rejects a suggestion. Its thread shows settled at once, and
   * its words and blocks in the page (suggestionSettling.ts) till the page
   * draws them as the server settled them (2026-11-25: thread and words came
   * with the server, 153–187 ms after the press under the NAS condition,
   * blocks kept or taken away about 350 ms after it); it goes as update's
   * changes go, and a refusal puts both back.
   */
  async settle(pageId: string, comment: NativeComment, decision: SuggestionDecision) {
    const drawing = showSuggestionSettling(pageId, comment.id, { decision, removed: suggestionRemovedBlocks(comment) });
    try {
      const settled = await this.change(pageId, comment, (shown) => suggestionSettled(shown, decision), suggestionRestored,
        (expectedRevision) => this.transport.resolveSuggestion(this.session, pageId, comment.id, {
          requestId: newId(), decision, expectedRevision,
        }));
      drawing.landed();
      return settled;
    } catch (error) {
      drawing.refused();
      throw error;
    }
  }

  private scheduleReload(pageId: string) {
    clearTimeout(this.reloads.get(pageId));
    this.reloads.set(pageId, setTimeout(() => {
      this.reloads.delete(pageId);
      void this.load(pageId, true).catch(() => {});
    }, SUGGESTION_RELOAD_DELAY_MS));
  }

  /**
   * Edits a comment, or resolves or reopens its thread. The change shows at
   * once (2026-11-25: a resolve showed with the server's answer, 134–151 ms
   * after the press under the NAS condition) and goes after the page's other
   * comment changes on their way (ownWritesInFlight), from the revision the
   * last of them left; a refusal puts back what the server last said of it.
   */
  update(pageId: string, comment: NativeComment, patch: CommentPatch) {
    return this.change(pageId, comment, (shown) => commentPatched(shown, patch), (shown, confirmed) => commentRestored(shown, confirmed, patch),
      (expectedRevision) => this.transport.updateComment(this.session, pageId, comment.id, { requestId: newId(), expectedRevision, ...patch }));
  }

  /** A comment's change shown at once and sent in the page's order, as update and settle describe. */
  private async change(
    pageId: string, comment: NativeComment, show: (shown: NativeComment) => NativeComment,
    restore: (shown: NativeComment, confirmed: NativeComment) => NativeComment,
    send: (expectedRevision: number) => Promise<{ comment: CommentRecord }>,
  ) {
    this.supersedeRead(pageId);
    const owner = this.owner(pageId);
    const shown = () => owner.snapshot.comments.find((item) => item.id === comment.id);
    const sending = this.updating.get(comment.id) ?? { confirmed: shown() ?? comment, count: 0 };
    sending.count += 1;
    this.updating.set(comment.id, sending);
    this.replace(pageId, show(shown() ?? comment));
    try {
      // Shown changes keep the revision the server last gave, as each answer leaves it.
      const response = await sendInOrder(pageId, () => send(shown()?.revision ?? sending.confirmed.revision));
      sending.confirmed = nativeComment(response.comment);
      const latest = shown();
      // A later change on its way keeps what it shows; the answer doesn't say what the reader has read.
      return this.replace(pageId, sending.count > 1 && latest ? { ...latest, revision: sending.confirmed.revision }
        : { ...sending.confirmed, new: latest?.new, markedUnread: latest?.markedUnread });
    } catch (error) {
      const latest = shown();
      if (sending.count === 1 && latest) this.replace(pageId, restore(latest, sending.confirmed));
      return this.throwAfterRevisionRefresh(pageId, error);
    } finally {
      sending.count -= 1;
      if (sending.count === 0) this.updating.delete(comment.id);
    }
  }

  /** The person's emoji on a comment, shown at once (commentReactions.ts). */
  react(pageId: string, comment: NativeComment, emoji: string, reacted: boolean) {
    return reactions.reactToComment(this.reactionStore, pageId, comment, emoji, reacted);
  }

  /** The person's emoji on the page itself, shown at once (commentReactions.ts). */
  reactToPage(pageId: string, emoji: string, reacted: boolean) {
    return reactions.reactToPage(this.reactionStore, pageId, emoji, reacted);
  }

  /** The page's reactions resolved together, gone at once (commentReactions.ts). */
  resolvePageReactions(pageId: string) {
    return reactions.resolvePageReactions(this.reactionStore, pageId);
  }

  /**
   * Sets the person's own state of a thread, given by its first comment. The
   * thread shows it at once; a refusal puts back what it showed before.
   */
  async setThreadState(pageId: string, comment: NativeComment, change: { repliesMuted?: boolean; unread?: boolean }) {
    this.supersedeRead(pageId);
    const owner = this.owner(pageId);
    const before = owner.snapshot.comments;
    // Marking it read reads the whole thread; unread marks its first comment.
    const marked = change.unread === undefined ? {} : { markedUnread: change.unread || undefined };
    const shown = before.map((item) => item.id === comment.id
      ? { ...item, ...(change.repliesMuted === undefined ? {} : { repliesMuted: change.repliesMuted }), ...marked } : item);
    this.publish(pageId, { comments: change.unread === false ? readThread(shown, comment.id) : shown });
    try {
      const response = await this.transport.setCommentThreadState(this.session, pageId, comment.id, change);
      return this.replace(pageId, nativeComment(response.comment));
    } catch (error) {
      if (owner.snapshot.comments.some((item) => item.id === comment.id)) this.publish(pageId, { comments: before });
      throw error;
    }
  }

  private replace(pageId: string, comment: NativeComment) {
    const owner = this.owner(pageId);
    this.publish(pageId, {
      comments: owner.snapshot.comments.map((item) => item.id === comment.id ? comment : item),
    });
    return comment;
  }

  async remove(pageId: string, comment: NativeComment) {
    this.supersedeRead(pageId);
    const response = await this.transport.deleteComment(this.session, pageId, comment.id, {
      requestId: newId(),
      expectedRevision: comment.revision,
    }).catch((error) => this.throwAfterRevisionRefresh(pageId, error));
    const owner = this.owner(pageId);
    // The comment alone goes; its replies stay in their discussion.
    this.publish(pageId, {
      comments: commentsAfterDelete(owner.snapshot.comments, comment.id),
      pendingDeletion: response.deletion.status === "pending" ? response.deletion : null,
    });
    if (response.deletion.status === "pending") void this.drainDeletion(pageId, response.deletion.id);
    return response.deletion;
  }

  private async drainDeletion(pageId: string, jobId: string) {
    try {
      let response = await this.transport.readCommentDeletion(this.session, pageId, jobId);
      while (response.deletion.status === "pending") {
        response = await this.transport.continueCommentDeletion(this.session, pageId, jobId);
        this.publish(pageId, { pendingDeletion: response.deletion });
      }
      this.publish(pageId, { pendingDeletion: null });
    } catch (error) {
      this.publish(pageId, { error });
    }
  }
}

const services = new WeakMap<Session, CommentService>();

export function commentServiceFor(session: Session, transport: CommentApi) {
  const existing = services.get(session);
  if (existing) return existing;
  const service = new CommentService(session, transport);
  services.set(session, service);
  return service;
}

const rowServices = new WeakMap<Session, CommentService>();

export function databaseRowCommentServiceFor(session: Session, transport: CommentApi) {
  const existing = rowServices.get(session);
  if (existing) return existing;
  const service = new CommentService(session, databaseRowCommentTransport(transport));
  rowServices.set(session, service);
  return service;
}
