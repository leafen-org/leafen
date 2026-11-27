"use client";

import { type CSSProperties, type ReactNode, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { PageHistoryDatabase } from "@/pageHistoryWire.generated";
import {
  type FeedPostBody,
  type FeedPostComments,
  type FeedPostService,
  type FeedPostViewers,
} from "@/lib/feedPostService";
import type { CommentReaction, CommentRecord } from "@/commentWire.generated";
import { CreatedTime } from "@/components/CreatedTime";
import { DotsHorizontal, SmileIcon } from "@/icons/leafen";
import { reactionsAfter } from "@/lib/commentReactions";
import { hoverRestsAfterLayer } from "@/lib/hoverRest";
import { newId } from "@/lib/ids";
import { afterOwnWrites } from "@/lib/ownWritesInFlight";
import { timeTodayLabel } from "@/lib/relativeTime";
import type { Session } from "@/types";
import { PersonAvatar } from "../people/PersonAvatar";
import { useCommentAttachmentDraft } from "../comments/commentAttachmentDraft";
import { CommentComposer } from "../comments/CommentComposer";
import { useCommentDraftStore } from "../comments/commentDraftStore";
import { commentDraftEmpty, sentCommentSpans } from "../comments/commentModel";
import type { CommentFieldHandle } from "../comments/CommentRichField";
import { AddReactionButton, CommentReactionChips, useReactionPicker } from "../comments/CommentReactions";
import { PageAnalyticsObserver } from "../page/PageAnalyticsObserver";
import { FeedPostBlocks } from "./FeedPostBlocks";
import { FeedPostBodyBox } from "./FeedPostBodyBox";
import panelStyles from "../CommentsPanel.module.css";
import styles from "./FeedPost.module.css";
import { FeedPostThreads } from "./FeedPostThreads";
import { FeedPostViewersButton } from "./FeedPostViewers";

// A card's bar stays away after its reaction picker closes until the pointer leaves the card (lib/hoverRest.ts).
hoverRestsAfterLayer(styles.post, "leave");

const NO_DATABASES: ReadonlyMap<string, PageHistoryDatabase> = new Map();
const NO_IDS: ReadonlySet<string> = new Set();

type LoadState<T> = { status: "idle" | "loading" | "failed" } | { status: "ready"; value: T };

// A post counts as edited once its page changed 5 seconds after it was
// made, as Notion's (seen 2026-11-03: 4.9s no, 5.5s yes).
const EDITED_AFTER_MS = 5_000;

/**
 * One feed post: its author and time, the row's title and visible
 * properties, its page body (the first blocks, read-only) and its comments
 * with a composer. Body and comments load when the post scrolls into view.
 * Drawn as Notion's card (seen 2026-10-17 and 2026-10-20): 12px round with a
 * faint shadow and no border, 16px in; the author's 24px face in its own
 * column, then at 48px their name (14px/500), when ("10월 5일", "(편집됨)"
 * once edited), the 26px title, the values, the reactions and the page's
 * comment box with the reader's face.
 * Under the pointer its "⋯" opens the row's menu, as Notion's feed (seen
 * 2026-10-11). Over its comments are its reactions and, always, "리액션 추가"
 * (seen 2026-10-11); they are the row page's own, shown over its comments
 * there too. Once it has comments, one line stands for them — the faces of
 * who wrote, "답글 N개" and when the last came — opening its discussion over
 * the post, and the comment box is there (seen 2026-10-21).
 */
export function FeedPost({
  background,
  rowId,
  title,
  untitled = false,
  icon,
  author,
  authorId,
  createdAt,
  updatedAt,
  views,
  analytics,
  properties,
  service,
  session,
  readOnly,
  onOpen,
  onActions,
}: {
  /** The post's conditional color, as a page background. */
  background?: { color: string; value: string };
  rowId: string;
  title: string;
  /** A row without a name shows no title line, as Notion's post (seen 2026-11-03). */
  untitled?: boolean;
  icon?: ReactNode;
  /** The row page's creator, as a display name. */
  author?: string;
  /** Who made it, for their face. */
  authorId?: string;
  createdAt?: string;
  /** When its page last changed, which marks it "(편집됨)". */
  updatedAt?: string;
  /** The post's viewers, when the member may read its analytics. */
  views?: FeedPostViewers;
  /** Where a view of this post is recorded: the page holding the row. */
  analytics?: { workspaceId: string; pageId: string };
  properties: ReactNode;
  service: FeedPostService | null;
  /** Who reads the feed, for the discussion its line opens. */
  session?: Session;
  readOnly: boolean;
  onOpen: () => void;
  /** Opens the row's menu by its "⋯". */
  onActions?: (trigger: HTMLElement) => void;
}) {
  const { t } = useTranslation(["feedPost", "topBar"]);
  const articleRef = useRef<HTMLElement>(null);
  const [visible, setVisible] = useState(false);
  // How many times the post came on screen, each a view of its page.
  const [visits, setVisits] = useState(0);
  const [body, setBody] = useState<LoadState<FeedPostBody>>({ status: "idle" });
  const [comments, setComments] = useState<LoadState<FeedPostComments>>({ status: "idle" });
  const [databases, setDatabases] = useState<ReadonlyMap<string, PageHistoryDatabase>>(NO_DATABASES);
  // The field's words, which the field alone reads as they are typed (commentDraftStore).
  const draftStore = useCommentDraftStore();
  const fieldRef = useRef<CommentFieldHandle | null>(null);
  const attachments = useCommentAttachmentDraft({ workspaceId: analytics?.workspaceId, pageId: rowId, onError: () => setSendFailed(true) });
  const [sendFailed, setSendFailed] = useState(false);
  const [reactions, setReactions] = useState<readonly CommentReaction[]>([]);
  const canReact = !readOnly && Boolean(service);
  const picker = useReactionPicker((emoji) => void react(emoji, true), analytics);
  const summaryRef = useRef<HTMLButtonElement>(null);
  const [threadsOpen, setThreadsOpen] = useState(false);

  useEffect(() => {
    const element = articleRef.current;
    if (!element || visible) return;
    if (typeof IntersectionObserver === "undefined") {
      setVisible(true);
      return;
    }
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) setVisible(true);
    }, { rootMargin: "400px 0px" });
    observer.observe(element);
    return () => observer.disconnect();
  }, [visible]);

  // A post counts as viewed each time half of it comes on screen, as
  // Notion's feed records a visit of the post's page each time it scrolls
  // into view (seen 2026-11-12: a post shown four times read four views in
  // its page's analytics).
  useEffect(() => {
    const element = articleRef.current;
    if (!element || !analytics) return;
    if (typeof IntersectionObserver === "undefined") {
      setVisits((count) => count || 1);
      return;
    }
    let inView = false;
    const observer = new IntersectionObserver((entries) => {
      // Half on screen or more: a post only partly in sight stays out.
      const now = entries.some((entry) => entry.isIntersecting && entry.intersectionRatio >= 0.499);
      if (now && !inView) setVisits((count) => count + 1);
      inView = now;
    }, { threshold: 0.5 });
    observer.observe(element);
    return () => observer.disconnect();
  }, [analytics]);

  useEffect(() => {
    if (!visible || !service) return;
    const controller = new AbortController();
    setBody({ status: "loading" });
    setComments({ status: "loading" });
    service.readBody(rowId, controller.signal)
      .then((value) => {
        if (controller.signal.aborted) return;
        setBody({ status: "ready", value });
        // Its databases' tables are read only where the body holds one.
        const databaseIds = value.blocks.filter((block) => block.type === "inline_database").map((block) => block.id);
        if (databaseIds.length) {
          void service.readDatabases(rowId, databaseIds, controller.signal)
            .then((read) => { if (!controller.signal.aborted) setDatabases(read); })
            .catch(() => {
              // Without them the post shows its other blocks.
            });
        }
      })
      .catch(() => { if (!controller.signal.aborted) setBody({ status: "failed" }); });
    service.listComments(rowId, controller.signal)
      .then((value) => {
        if (controller.signal.aborted) return;
        setComments({ status: "ready", value });
        setReactions(value.reactions);
      })
      .catch(() => { if (!controller.signal.aborted) setComments({ status: "failed" }); });
    return () => controller.abort();
  }, [rowId, service, visible]);

  // Its line is read again once its discussion closes: replies, resolving
  // and deleting happen there. The read waits for what the discussion still
  // sends (ownWritesInFlight.ts): 2026-11-23, a reply sent just before
  // closing landed after that read, and the line kept the count before it.
  function closeThreads() {
    setThreadsOpen(false);
    if (!service) return;
    void afterOwnWrites().then(() => service.listComments(rowId, new AbortController().signal))
      .then((value) => setComments({ status: "ready", value }), () => setComments({ status: "failed" }));
  }

  /** Adds or takes back the reader's emoji: shown at once, put back on a refusal. */
  async function react(emoji: string, reacted: boolean) {
    if (!service) return;
    const before = reactions;
    setReactions(reactionsAfter(before, emoji, service.userId, reacted));
    try {
      setReactions(await service.setReaction(rowId, emoji, reacted));
    } catch {
      setReactions(before);
    }
  }

  /** Changes the post's comments once they are read; before, there are none to change. */
  function withItems(change: (items: CommentRecord[]) => CommentRecord[], authors: Record<string, string> = {}) {
    setComments((current) => current.status === "ready"
      ? { status: "ready", value: { ...current.value, items: change(current.value.items), authors: { ...authors, ...current.value.authors } } }
      : current);
  }

  /**
   * The comment shows under the post as it is sent and the field empties, as
   * a page's comment does (commentService create); the server's answer takes
   * its place, and a refused one goes, its words back in the field.
   */
  function send() {
    const draft = draftStore.get();
    const rich = sentCommentSpans(draft);
    const attachmentIds = attachments.fileIds;
    if ((commentDraftEmpty(rich) && attachmentIds.length === 0) || attachments.uploading || !service) return;
    const sentDraft = draft;
    const id = newId(), now = new Date().toISOString();
    setSendFailed(false);
    draftStore.set([]);
    fieldRef.current?.clear();
    withItems((items) => [...items, {
      id, workspaceId: analytics?.workspaceId ?? "", pageId: rowId, blockId: "", parentId: "", authorId: service.userId,
      body: { rich, ...(attachmentIds.length > 0 ? { attachmentIds: [...attachmentIds] } : {}) },
      resolved: false, revision: 0, createdAt: now, updatedAt: now, editedAt: "",
    }], { [service.userId]: service.userName });
    service.createComment(rowId, rich, attachmentIds, undefined, id).then(({ comment, authors }) => {
      attachments.clear();
      withItems((items) => items.map((item) => (item.id === id ? comment : item)), authors);
    }, () => {
      withItems((items) => items.filter((item) => item.id !== id));
      draftStore.set(sentDraft);
      fieldRef.current?.set(sentDraft);
      setSendFailed(true);
    });
  }

  const edited = Boolean(createdAt && updatedAt && Date.parse(updatedAt) - Date.parse(createdAt) > EDITED_AFTER_MS);
  const blocks = body.status === "ready" ? body.value.blocks : [];
  // As Notion's feed (seen 2026-10-04), a resolved discussion and its replies
  // leave the post.
  const resolvedRoots = comments.status === "ready"
    ? new Set(comments.value.items.filter((comment) => !comment.parentId && comment.resolved).map((comment) => comment.id))
    : NO_IDS;
  const commentItems = comments.status === "ready"
    ? comments.value.items.filter((comment) => !resolvedRoots.has(comment.parentId || comment.id))
    : [];
  const commenters = [...new Set(commentItems.map((comment) => comment.authorId).filter(Boolean))].slice(0, 3);
  const lastCommentAt = commentItems.reduce((latest, comment) => comment.createdAt > latest ? comment.createdAt : latest, "");
  return (
    <article ref={articleRef} className={styles.post} aria-label={title} data-feed-post={rowId} data-row-id={rowId} data-untitled={untitled && !icon ? "true" : undefined}
      data-conditional-color={background?.color}
      style={background ? { background: background.value, "--feed-post-ground": background.value } as CSSProperties : undefined}>
      {visits > 0 && analytics ? (
        <PageAnalyticsObserver key={visits} input={{
          kind: "member", workspaceId: analytics.workspaceId, pageId: analytics.pageId, rowId, inTrash: false,
        }} />
      ) : null}
      {onActions ? (
        <div className={styles.actions} data-feed-post-actions>
          {/* "리액션" before "⋯", as Notion's (seen 2026-11-02): the same picker as "리액션 추가" under it.
              Pressed, it leaves the focus where it was, and the bar goes while
              the picker is open and after it closes until the pointer leaves
              the card and comes in again (lib/hoverRest.ts), as Notion's (seen
              2026-11-26 in Chrome at 1440 in two tabs at different times: the
              focus on nothing after Escape, a letter typed landing nowhere). */}
          {canReact ? (
            <button type="button" className={styles.action} aria-haspopup="dialog" data-feed-post-react data-press-keeps-focus
              aria-label={t("react")} title={t("react")} onClick={(event) => picker.open(event.currentTarget)}>
              <SmileIcon size="small" aria-hidden="true" />
            </button>
          ) : null}
          <button type="button" className={styles.action} aria-haspopup="menu" data-feed-post-more
            aria-label={t("topBar:moreActions", { pageTitle: title })} onClick={(event) => onActions(event.currentTarget)}>
            <DotsHorizontal size="small" aria-hidden="true" />
          </button>
        </div>
      ) : null}
      {author ? <PersonAvatar className={styles.avatar} userId={authorId} name={author} size={24} /> : null}
      <div className={styles.main}>
      {author || createdAt ? (
        <p className={styles.byline} data-feed-post-byline>
          {author ? <strong>{author}</strong> : null}
          {createdAt ? <CreatedTime value={createdAt} suffix={edited ? ` ${t("edited")}` : ""} /> : null}
        </p>
      ) : null}
      {untitled && !icon ? null : <header className={styles.header}>
        <button type="button" className={styles.title} onClick={onOpen}>
          {icon ? <span className={styles.icon}>{icon}</span> : null}
          {untitled ? null : <span className={styles.titleText}>{title}</span>}
        </button>
      </header>}
      {properties}
      <FeedPostBodyBox className={styles.body}>
        {body.status === "loading" ? <p className={styles.muted}>{t("loading")}</p> : null}
        {body.status === "failed" ? <p className={styles.muted}>{t("bodyFailed")}</p> : null}
        <FeedPostBlocks blocks={blocks} databases={databases}
          saveToggle={!readOnly && service ? (block, collapsed, known) => service.setToggleCollapsed(rowId, block, known, collapsed) : undefined} />
        {body.status === "ready" && body.value.truncated ? (
          <button type="button" className={styles.more} onClick={onOpen}>{t("readMore")}</button>
        ) : null}
      </FeedPostBodyBox>
      <section className={styles.comments} aria-label={t("commentsFor", { title })} data-feed-post-comments>
        {comments.status === "failed" ? <p className={styles.muted}>{t("commentsFailed")}</p> : null}
        <CommentReactionChips reactions={reactions} userId={service?.userId ?? ""} workspaceId={analytics?.workspaceId}
          canReact={canReact} className={styles.reactions} onToggle={(emoji, reacted) => void react(emoji, reacted)}
          onOpenPicker={picker.open}
          addButton={canReact ? <AddReactionButton className={styles.addReaction} onOpen={picker.open} /> : null}
          /* A post with comments shows no viewers, as Notion's (seen 2026-11-03). */
          trailing={views && !commentItems.length ? <FeedPostViewersButton viewers={views} /> : null} />
        {picker.picker}
        {commentItems.length > 0 && comments.status === "ready" ? (
          <button ref={summaryRef} type="button" className={styles.threadLine} aria-haspopup="dialog" aria-expanded={threadsOpen}
            data-feed-post-thread-line disabled={!session || !analytics} onClick={() => setThreadsOpen(true)}>
            <span className={styles.threadFaces}>
              {commenters.map((id) => <PersonAvatar key={id} userId={id} name={comments.value.authors[id] || t("unknownAuthor")} size={24} />)}
            </span>
            <span className={styles.threadCount}>
              {t(comments.value.nextCursor ? "replyCountMore" : "replyCount", { count: commentItems.length })}
            </span>
            <time className={styles.threadTime} dateTime={lastCommentAt}>{timeTodayLabel(lastCommentAt)}</time>
          </button>
        ) : null}
        {threadsOpen && session && analytics && summaryRef.current ? (
          <FeedPostThreads rowId={rowId} holderPageId={analytics.pageId} workspaceId={analytics.workspaceId} session={session}
            canComment={!readOnly} anchor={summaryRef.current} onClose={closeThreads} />
        ) : null}
        {!readOnly && service && commentItems.length === 0 && comments.status !== "loading" ? (
          <div className={`${panelStyles.inlinePanel} ${styles.composer}`} data-feed-post-composer>
            <CommentComposer inline avatarSize={24} attachments={analytics ? attachments : undefined} currentUserId={service.userId}
              currentUserLabel={service.userName} draft={draftStore} fieldRef={fieldRef} pageId={rowId} submitting={false}
              ariaLabel={t("addCommentTo", { title })} onSubmit={send} onUpdate={draftStore.set} />
          </div>
        ) : null}
        {sendFailed ? <p className={styles.error} role="alert">{t("sendFailed")}</p> : null}
      </section>
      </div>
    </article>
  );
}
