import type { ProductStatusSink } from "@/lib/productStatus";
import { useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { NativeComment } from "@/lib/commentService";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { holdStyleWrite } from "@/components/database/heldStyleWrites";
import { watchPageSize } from "@/components/page/pageSizeWatch";
import type { Session } from "@/types";
import { authorLabel, commentPropertyId, isReactionThread, quoteRange } from "./commentModel";
import { commentBlockElement, commentCardTop, commentDraftTop } from "./commentBlockElement";
import { CommentThreadCard } from "./CommentThreadCard";
import { CommentNewLine } from "./CommentNewLine";
import { firstNewComment } from "@/lib/commentThreadUnread";
import { MarginCommentDraft } from "./MarginCommentDraft";
import { keepCommentDraft, startNewComment } from "./keptDraftQuestion";
import { discussionMarks, setActiveCommentThreads, setMarginThreads, type OpenCommentsDetail } from "./commentPanelEvents";
import { MarginReactionEntry } from "./MarginReactionEntry";
import { MarginRestingEntry } from "./MarginRestingEntry";
import { useRestingCardReaction } from "./restingCardReaction";
import { focusReplyBox } from "./replyRowCaret";
import { onOwnSuggestion } from "@/lib/ownSuggestionEvents";
import { flashBlock } from "@/lib/blockFlash";
import { marginDiscussionsFit, marginObstacles, scrollbarWidth, stackMarginCards, type MarginObstacle } from "./marginDiscussionLayout";
import { useCommentsPanelController } from "./useCommentsPanelController";
import { usePointedCommentThreads } from "./pointedCommentThreads";
import styles from "./MarginDiscussions.module.css";
import { keysFocusHolder } from "@/lib/lastPress";

// A resting card whose open button the keys' focus is on takes its ground (data-keys-within).
keysFocusHolder(styles.collapsed, { part: styles.restOpen });

const COLLAPSED_HEIGHT = 74;
/** A page's column of text with its pads, as wide as it stands at the usual width (PageView .doc --content-max). */
const DOC_COLUMN = 912;
const DRAFT_CARD = "draft";
/** A discussion on a database page's value stands 8px under its property's line (Notion's, seen 2026-11-12). */
const PROPERTY_DROP = 8;
/** What in a resting card answers a press itself: a comment's buttons and menus, the smiley after its reactions, and
 * words being edited. A reaction's chip toggles it and opens the card too (MarginRestingEntry). */
const RESTING_OWN_CONTROLS = "[data-comment-hover-actions], [data-comment-reactions] [data-comment-action=\"react\"], [data-comment-edit-composer]";

function sameNumbers(left: ReadonlyMap<string, number>, right: ReadonlyMap<string, number>) {
  return left.size === right.size && [...right].every(([key, value]) => left.get(key) === value);
}

function sameStretches(left: readonly MarginObstacle[], right: readonly MarginObstacle[]) {
  return left.length === right.length && right.every((stretch, index) => left[index].top === stretch.top && left[index].bottom === stretch.bottom);
}

/**
 * A page's open block discussions beside their blocks, as Notion shows them
 * when the page is wide enough (seen 2026-09-30): the text moves left, each
 * discussion is a card level with its block or just below the card above,
 * and pressing a card opens its thread there with its reply box. Narrower,
 * the blocks show their counts and open the discussions panel.
 */
export function MarginDiscussions({ pageId, rowId, session, workspaceId, canEditPage, canComment, compose, onComposeClose, onStatus, onActiveChange, onFitChange }: {
  pageId: string;
  /** A database page's own discussions, which its row keeps. */
  rowId?: string;
  session: Session;
  workspaceId?: string;
  canEditPage: boolean;
  canComment: boolean;
  /** A new comment being written here, which the page sends here while this has room. */
  compose: OpenCommentsDetail | null;
  onComposeClose: () => void;
  onStatus?: ProductStatusSink;
  onActiveChange: (active: boolean) => void;
  /** Whether the page has room beside its text. */
  onFitChange: (fit: boolean) => void;
}) {
  const { t } = useTranslation("commentsPanel");
  const controller = useCommentsPanelController({ pageId, rowId, session, workspaceId, onStatus });
  // A database page's discussions on its values stand beside their
  // properties too, as Notion's wide database page (seen 2026-11-12). One
  // that lost what it was left on stands in the side view alone, as Notion's
  // (seen 2026-11-27; comment_original_gone.go).
  const threads = useMemo(
    () => controller.comments.filter((comment) => (comment.blockId || (rowId && commentPropertyId(comment.body)))
      && !comment.parentId && !comment.resolved && !comment.originalGone && !isReactionThread(comment)),
    [controller.comments, rowId],
  );
  // Discussions begun by a reaction to words stand among them as chips (MarginReactionEntry).
  const reactions = useMemo(() => controller.comments.filter((comment) => comment.blockId && !comment.parentId && !comment.resolved
    && !comment.originalGone && isReactionThread(comment) && comment.reactions.length > 0), [controller.comments]);
  const [fit, setFit] = useState(false);
  const [anchors, setAnchors] = useState<ReadonlyMap<string, number>>(new Map());
  const [heights, setHeights] = useState<ReadonlyMap<string, number>>(new Map());
  const [obstacles, setObstacles] = useState<readonly MarginObstacle[]>([]);
  const [openId, setOpenId] = useState<string | null>(null);
  const column = useRef<HTMLDivElement>(null);
  const openCard = useRef<HTMLDivElement>(null);
  // A new comment written here, which leaving with words in it keeps beside
  // its block, its card resting and its text fainter, until it is taken up
  // again, emptied and left, or sent (Notion's, seen 2026-11-12).
  const [draft, setDraft] = useState<OpenCommentsDetail | null>(null);
  const [draftOpen, setDraftOpen] = useState(false);
  const [draftHovered, setDraftHovered] = useState(false);
  const [draftFocusTurn, setDraftFocusTurn] = useState(0);
  const draftFilled = useRef(false);
  // Holding words, it is the one draft a new comment anywhere asks about
  // (keptDraftQuestion.tsx): going back opens it with the caret, deleting
  // it closes it.
  const owner = useId();
  const setDraftFilled = useCallback((filled: boolean) => {
    draftFilled.current = filled;
    keepCommentDraft(owner, filled ? {
      back: () => { setOpenId(null); setDraftOpen(true); setDraftFocusTurn((turn) => turn + 1); },
      drop: () => { draftFilled.current = false; setDraft(null); },
    } : null);
  }, [owner]);
  useEffect(() => { if (!draft) setDraftFilled(false); }, [draft, setDraftFilled]);
  useEffect(() => () => keepCommentDraft(owner, null), [owner]);
  const composing = fit ? draft : null;
  const draftActive = composing !== null && draftOpen;
  const keepCaret = useRef(false);
  // The caret a card pressed open asks for its reply field, until it has it.
  const pressedReply = useRef<(() => void) | null>(null);
  const shown = fit && (threads.length > 0 || reactions.length > 0 || composing !== null);

  // Whether the page has room beside its text at the text's own width, as
  // the page's width changes. The column sits in the page's document. As
  // Notion's, the room counts the page's scrollbar too, and the text and
  // cards are centered on what it leaves (seen 2026-10-29): the document
  // gets the scrollbar's width to place itself by.
  useLayoutEffect(() => {
    const element = column.current?.parentElement;
    const page = element?.parentElement;
    if (!element || !page) return;
    // Measured, then written once the page's other measures are done too (pageSizeWatch).
    function measure(write: (change: () => void) => void = holdStyleWrite) {
      if (!element || !page) return;
      const style = getComputedStyle(element);
      // A full-width page has room for them from the same width as a page of
      // the usual width (Notion's, seen 2026-11-12: from 1150px of page),
      // whatever its end gives up to them.
      const widest = parseFloat(style.maxWidth);
      const text = Number.isFinite(widest) ? widest - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight)
        : DOC_COLUMN - 2 * parseFloat(style.paddingInlineStart);
      const scrollbar = scrollbarWidth(page);
      const width = page.clientWidth;
      write(() => element.style.setProperty("--page-scrollbar", `${scrollbar}px`));
      setFit(marginDiscussionsFit(width + scrollbar, text));
    }
    measure((change) => change());
    return watchPageSize(page, measure);
  }, []);

  useEffect(() => onActiveChange(shown), [onActiveChange, shown]);
  useEffect(() => {
    if (!shown) return;
    setMarginThreads(threads.map((thread) => thread.id));
    return () => setMarginThreads([]);
  }, [shown, threads]);
  useEffect(() => () => onActiveChange(false), [onActiveChange]);
  useEffect(() => { onFitChange(fit); return () => onFitChange(false); }, [fit, onFitChange]);
  // Every discussion on pressed text darkens, and the one a card is opened or
  // a pointer rests on (Notion's, seen 2026-11-05).
  const [pressedThreads, setPressedThreads] = useState<readonly string[]>([]);
  const [hoveredId, setHoveredId] = useState<string | null>(null);
  // Words under the pointer light their cards as the cards' own hover does (pointedCommentThreads.ts).
  const threadIds = useMemo(() => threads.map((thread) => thread.id), [threads]);
  const pointedThreads = usePointedCommentThreads(threadIds, shown);
  useEffect(() => {
    setActiveCommentThreads([...new Set([...pressedThreads, ...pointedThreads, ...(openId ? [openId] : []), ...(hoveredId ? [hoveredId] : [])])]);
    return () => setActiveCommentThreads([]);
  }, [hoveredId, openId, pointedThreads, pressedThreads]);
  // Words of a value pressed mark their card, and keep it marked until a
  // press lands elsewhere (Notion's, seen 2026-11-12).
  useEffect(() => {
    if (openId || !pressedThreads.length) return;
    const press = (event: PointerEvent) => {
      const target = event.target instanceof Node ? event.target : null;
      if (target && discussionMarks(pressedThreads).some((mark) => mark.contains(target))) return;
      setPressedThreads([]);
    };
    document.addEventListener("pointerdown", press, true);
    return () => document.removeEventListener("pointerdown", press, true);
  }, [openId, pressedThreads]);
  // Pressing a discussion's text opens the first card there with the caret
  // left in the text (Notion's, seen 2026-11-05). One opened at its address
  // (a notice, a link) opens with the caret in its reply field, its text
  // darker (Notion's, seen 2026-11-11).
  useEffect(() => {
    const asked = compose?.linkedThreadId ? [compose.linkedThreadId] : compose?.threadIds;
    if (!compose || !asked || compose.pageId !== pageId) return;
    const pressed = threads.filter((thread) => asked.includes(thread.id));
    if (pressed.length && compose.pressedOnly) setPressedThreads(pressed.map((thread) => thread.id));
    else if (pressed.length) {
      if (pressed[0].id !== openId && !compose.linkedThreadId) keepCaret.current = true;
      setOpenId(pressed[0].id);
      setPressedThreads(pressed.map((thread) => thread.id));
    }
    onComposeClose();
  }, [compose, onComposeClose, openId, pageId, threads]);
  // A suggestion the person makes here opens its card at once, its words
  // marked as an open card's, with the caret left in the text to go on typing
  // (Notion's, seen 2026-11-15 at 1440 dark in two tabs at different times,
  // judged after typing only: the card stood open with its reply field and
  // the next letters went on into the page). One asked for before a press
  // since — away, or on another card — leaves the cards as that press left
  // them (ownSuggestionEvents.ts).
  const [suggestedId, setSuggestedId] = useState<string | null>(null);
  useEffect(() => onOwnSuggestion((made) => {
    if (made.pageId === pageId && !made.pressedSinceAsked) setSuggestedId(made.threadId);
  }), [pageId]);
  useEffect(() => {
    if (!suggestedId || (fit && !threads.some((thread) => thread.id === suggestedId))) return;
    setSuggestedId(null);
    if (!fit || suggestedId === openId) return;
    keepCaret.current = true;
    setPressedThreads([]);
    setOpenId(suggestedId);
  }, [fit, openId, suggestedId, threads]);
  // A card once open — pressed, its words pressed, or opened at its address —
  // reads its discussion as it closes, as Notion's (seen 2026-11-11 at its
  // address; 2026-11-25 in Chrome at 1440, two tabs at different times: an
  // unread card pressed open kept its dot and "New" while open, and a press
  // beside the page that closed it read it at once). Resting the pointer on it
  // or seeing it beside its block reads nothing (seen the same day, twice).
  // One the person marks unread while it is open stays so.
  const cardOpened = useRef<{ id: string; marked: boolean } | null>(null);
  const latestThreads = useRef(threads); latestThreads.current = threads;
  const latestController = useRef(controller); latestController.current = controller;
  useEffect(() => {
    const was = cardOpened.current;
    const opened = openId ? latestThreads.current.find((thread) => thread.id === openId) : undefined;
    cardOpened.current = openId ? { id: openId, marked: Boolean(opened?.markedUnread) } : null;
    if (!was || was.id === openId) return;
    const closed = latestThreads.current.find((thread) => thread.id === was.id);
    if (closed?.unread && !(closed.markedUnread && !was.marked)) {
      void latestController.current.setThreadState(closed, { unread: false });
    }
  }, [openId]);
  // A new comment asked for beside a block opens its field there and closes
  // a card that was open. With words kept in another, Notion asks first
  // whether to go back to them or delete them; deleted, nothing new opens
  // (seen 2026-11-12).
  useEffect(() => {
    if (!fit || !compose?.compose || !compose.blockId || compose.pageId !== pageId) return;
    onComposeClose();
    startNewComment(() => {
      setDraftFilled(false);
      setDraft(compose);
      setDraftOpen(true);
      setOpenId(null);
    });
  }, [compose, fit, onComposeClose, pageId, setDraftFilled]);
  useEffect(() => { if (!fit) setDraft(null); }, [fit]);

  // Each thread's block top, again whenever the page's blocks move or grow.
  useEffect(() => {
    const element = column.current?.parentElement;
    if (!shown || !element) return;
    let frame = 0;
    function measure() {
      frame = 0;
      if (!element) return;
      const origin = element.getBoundingClientRect().top;
      const next = new Map<string, number>();
      for (const thread of [...threads, ...reactions]) {
        const block = thread.blockId ? commentBlockElement(thread.blockId, element) : null;
        const property = block ? null : element.querySelector<HTMLElement>(`[data-row-property-id="${CSS.escape(commentPropertyId(thread.body))}"]`);
        if (block) next.set(thread.id, commentCardTop(block, origin, quoteRange(thread.body) !== null));
        else if (property) next.set(thread.id, Math.round(property.getBoundingClientRect().top - origin) + PROPERTY_DROP);
      }
      const drafted = composing?.blockId ? commentBlockElement(composing.blockId, element) : null;
      if (drafted) next.set(DRAFT_CARD, commentDraftTop(drafted, origin, composing?.initialQuoteStart !== undefined));
      setAnchors((current) => sameNumbers(current, next) ? current : next);
      // Wide tables and boards the cards make way for.
      const stretches = column.current ? marginObstacles(element, origin, column.current.getBoundingClientRect().left) : [];
      setObstacles((current) => sameStretches(current, stretches) ? current : stretches);
    }
    const schedule = () => { if (!frame) frame = window.requestAnimationFrame(measure); };
    measure();
    const resize = new ResizeObserver(schedule);
    resize.observe(element);
    const mutation = new MutationObserver(schedule);
    mutation.observe(element, { childList: true, subtree: true, attributes: true, attributeFilter: ["data-page-wide"] });
    return () => {
      if (frame) window.cancelAnimationFrame(frame);
      resize.disconnect();
      mutation.disconnect();
    };
  }, [composing, shown, threads, reactions]);

  // Each card's height, as it opens, closes or its thread grows.
  useEffect(() => {
    const host = column.current;
    if (!shown || !host) return;
    function measure() {
      if (!host) return;
      const next = new Map<string, number>();
      for (const card of host.querySelectorAll<HTMLElement>("[data-margin-discussion]")) {
        next.set(card.dataset.marginDiscussion ?? "", card.offsetHeight);
      }
      setHeights((current) => sameNumbers(current, next) ? current : next);
    }
    measure();
    const observer = new ResizeObserver(measure);
    for (const card of host.querySelectorAll<HTMLElement>("[data-margin-discussion]")) observer.observe(card);
    return () => observer.disconnect();
  }, [composing, shown, threads, reactions, openId]);

  // The open card is a layer over the page: a press elsewhere closes it, and
  // its own menus lie over it. A new comment's field holding words rests
  // instead, and so does Escape there (CommentComposer); emptied, it closes
  // as it is left. The press that closes it goes on to do its own work, as
  // Notion's (seen 2026-11-18 in Chrome at 1440, suggesting, the card open
  // for what was typed: one click picked the "/" menu's row, and one click on
  // another line put the caret there). Escape never closes a discussion's
  // card, as Notion's (measured 2026-11-26 in Chrome at 1440 in two tabs at
  // different times, judged after the keys with a letter typed: its empty
  // reply box left by the first, the card staying through every Escape after
  // with the focus on nothing; one opened from its text kept as Escape there
  // chose the line; one with its "⋯" ringed kept the ring). Its keys go on to
  // what holds the focus (escapeGoesOn, escapeLayerOver).
  const leave = () => {
    if (!draftActive) { setOpenId(null); setPressedThreads([]); return; }
    if (draftFilled.current) setDraftOpen(false);
    else setDraft(null);
  };
  useOverlayLayer(openId !== null || draftActive, { kind: "card", ref: openCard, onEscape: draftActive ? leave : undefined, escapeGoesOn: !draftActive,
    onOutsidePress: leave,
    inside: () => draftActive ? [] : discussionMarks(threads.map((thread) => thread.id)) });
  // An opened card gives its reply field the caret, once the field is drawn:
  // one pressed open from the press itself, what is typed meanwhile going in
  // (replyRowCaret.ts), one opened at its address here.
  // Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440: the
  // margin card pressed, "z" typed went into its reply field (Leafen the same). The page stays where it
  // is, the field below the window's end too, until a letter typed there brings its line in (Notion's,
  // seen 2026-11-16 at 1440: a card opened by line 2 with its field at 703–730 of 723 left the page
  // unscrolled; the letter typed scrolled it 50px).
  useEffect(() => {
    if (!openId) return;
    if (keepCaret.current) { keepCaret.current = false; return; }
    let frame = 0;
    let tries = 0;
    const focusReply = () => {
      const reply = openCard.current?.querySelector<HTMLElement>("[data-comment-reply-input]");
      if (reply) reply.focus({ preventScroll: true });
      else if (++tries < 20) frame = window.requestAnimationFrame(focusReply);
    };
    frame = window.requestAnimationFrame(focusReply);
    return () => window.cancelAnimationFrame(frame);
  }, [openId]);
  // A card pressed open — anywhere on it, or its comment's 😊 — takes the caret in its reply field from the press,
  // its block washed as chosen for a moment (lib/blockFlash.ts).
  const openPressed = (thread: NativeComment) => {
    setPressedThreads([]); setOpenId(thread.id);
    if (thread.blockId) flashBlock(thread.blockId);
    keepCaret.current = true;
    pressedReply.current?.();
    pressedReply.current = focusReplyBox(() => openCard.current);
  };
  const restingReaction = useRestingCardReaction(controller, openCard, openPressed);

  if (!shown) return <div ref={column} className={styles.column} hidden />;
  // A block's reaction chips stand before its cards, as Notion's.
  const tops = stackMarginCards([...reactions.map((thread) => thread.id), ...threads.map((thread) => thread.id), ...(composing ? [DRAFT_CARD] : [])]
    .filter((id) => anchors.has(id)).map((id) => ({
      id, anchorTop: anchors.get(id) ?? 0, height: heights.get(id) ?? COLLAPSED_HEIGHT,
    })), draftActive ? DRAFT_CARD : openId, obstacles);
  // A closed card shows the discussion's first comment and its last reply,
  // with how many replies lie between, as Notion's; pressed anywhere but on
  // a comment's own buttons or words being edited, it opens.
  // An unread discussion keeps its dot closed too; its first new reply, the
  // last, has the card's "New" line before it, and one among those it hides
  // the "New" pill after "답글 N개 더 보기" (Notion's, seen 2026-11-25 in
  // Chrome at 1440, two tabs; lib/commentThreadUnread.ts).
  const restEntry = (comment: NativeComment, thread: NativeComment) => (
    <MarginRestingEntry comment={comment} thread={thread} controller={controller} userId={session.user.id}
      canComment={canComment} canEditPage={canEditPage} linkTarget={pageId} onStatus={onStatus}
      onOpenReactionPicker={(button) => restingReaction.open(comment, thread, button)} />
  );
  const collapsed = (thread: NativeComment) => {
    const replies = controller.repliesByParent.get(thread.id) ?? [];
    const last = replies.at(-1);
    const firstNew = firstNewComment([thread, ...replies]);
    const open = () => openPressed(thread);
    return (
      <div className={styles.collapsed} onClick={(event) => {
        const target = event.target instanceof Element ? event.target : null;
        if (target && event.currentTarget.contains(target) && !target.closest(RESTING_OWN_CONTROLS)) open();
      }}>
        <button type="button" className={styles.restOpen} onClick={open}
          aria-label={t("margin.open", { author: authorLabel(thread, session.user.id, controller.actorProfiles) })} />
        {restEntry(thread, thread)}
        {replies.length > 1 ? <button type="button" className={styles.more} tabIndex={-1}>{t("margin.moreReplies", { count: replies.length - 1 })}
          {firstNew && firstNew !== last ? <CommentNewLine shape="more" /> : null}</button> : null}
        {firstNew && firstNew === last ? <CommentNewLine shape="card" /> : null}
        {last ? restEntry(last, thread) : null}
      </div>
    );
  };

  return (
    <div ref={column} className={styles.column} role="complementary" aria-label={t("margin.label")} data-margin-discussions>
      {composing ? <div key={`${DRAFT_CARD}:${composing.blockId}:${composing.initialQuoteStart ?? ""}`} ref={draftOpen ? openCard : undefined}
        className={styles.card} data-margin-discussion={DRAFT_CARD} data-margin-draft data-open={draftOpen || undefined}
        onMouseEnter={() => setDraftHovered(true)} onMouseLeave={() => setDraftHovered(false)}
        // Taking the field up again opens its card; leaving it emptied closes it.
        onFocus={() => { if (!draftOpen) { setOpenId(null); setDraftOpen(true); } }}
        onBlur={(event) => {
          if (!draftFilled.current && document.hasFocus() && !event.currentTarget.contains(event.relatedTarget as Node | null)) setDraft(null);
        }}
        style={{ top: tops.get(DRAFT_CARD) ?? 0, visibility: tops.has(DRAFT_CARD) ? undefined : "hidden" }}>
        <MarginCommentDraft detail={composing} pageId={pageId} rowId={rowId} session={session} workspaceId={workspaceId} onStatus={onStatus}
          kept={!draftOpen && !draftHovered} focusTurn={draftFocusTurn} onFilledChange={setDraftFilled}
          onEscapeDraft={() => setDraftOpen(false)} onClose={() => setDraft(null)} />
      </div> : null}
      {reactions.map((thread) => <MarginReactionEntry key={thread.id} thread={thread} controller={controller} userId={session.user.id}
        canComment={canComment} top={tops.get(thread.id)} />)}
      {threads.map((thread) => (
        <div key={thread.id} ref={openId === thread.id && !draftActive ? openCard : undefined} className={styles.card}
          data-margin-discussion={thread.id} data-open={openId === thread.id || undefined}
          data-pressed={openId !== thread.id && pressedThreads.includes(thread.id) ? "true" : undefined}
          data-pointed={openId !== thread.id && pointedThreads.includes(thread.id) ? "true" : undefined}
          onMouseEnter={() => setHoveredId(thread.id)} onMouseLeave={() => setHoveredId(null)}
          style={{ top: tops.get(thread.id) ?? 0, visibility: tops.has(thread.id) ? undefined : "hidden" }}>
          {openId === thread.id ? (
            <CommentThreadCard comment={thread} controller={controller} userId={session.user.id} canEditPage={canEditPage}
              canComment={canComment} beside linkTarget={pageId} onStatus={onStatus} />
          ) : collapsed(thread)}
        </div>
      ))}
      {restingReaction.picker}
    </div>
  );
}
