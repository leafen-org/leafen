"use client";

import type { ProductStatusSink } from "@/lib/productStatus";
import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { X } from "@/icons/leafen";
import type { NativeComment } from "@/lib/commentService";
import type { Block } from "@/lib/types";
import { escapeLayerOver, useOverlayLayer } from "@/lib/overlayLayer";
import { NativeTab, useNativeTabs } from "@/lib/nativeTabs";
import type { Session } from "@/types";
import styles from "./CommentsPanel.module.css";
import titleStyles from "./comments/TitleDiscussion.module.css";
import tabStyles from "./CommentsPanelTabs.module.css";
import allStyles from "./comments/AllDiscussions.module.css";
import cardStyles from "./comments/BlockDiscussionCard.module.css";
import { usePendingCommentMark } from "./comments/pendingCommentMark";
import { commentTextPlaces, type CommentQuotePlaces } from "./comments/commentQuotePlaces";
import { ANCHORED_COMPOSER_WIDTH, ANCHORED_WIDTH, useAnchoredPlace } from "./comments/anchoredCardPlace";
import { showOpenCommentCard } from "./comments/openCommentCard";
import { discussionMarks, setActiveCommentThreads } from "./comments/commentPanelEvents";
import { CommentThreadCard } from "./comments/CommentThreadCard";
import { CommentComposer } from "./comments/CommentComposer";
import { CommentReactionChips, useReactionPicker } from "./comments/CommentReactions";
import { isReactionThread } from "./comments/commentModel";
import { discussionFilterPeople } from "./comments/discussionListModel";
import { DiscussionFilterMenu } from "./comments/DiscussionFilterMenu";
import { PageNotifyBell } from "./comments/PageNotifyBell";
import discussionStyles from "./comments/PageDiscussions.module.css";
import { useCommentsPanelController, type CommentsPanelControllerInput } from "./comments/useCommentsPanelController";
import sheetStyles from "./comments/PhoneCommentSheet.module.css";
import { usePhoneSheetTop } from "./comments/phoneCommentSheet";
import { useSidebarLayout } from "./shell/AppShell";
import { TouchDiscussionSheet } from "./comments/TouchDiscussionSheet";
import { GoneDiscussionCard, goneDiscussionLink } from "./comments/GoneDiscussionCard";
import { DiscussionSide } from "./comments/DiscussionSideView";
import { giveNewCommentCaret } from "./editor/openedFieldCaret";
import { focusedOpener } from "@/lib/lastPress";
import { giveFocusBack } from "@/lib/focusGiveBack";
import { imeComposing } from "@/lib/fieldKeyBoundary";

export type CommentsPanelProps = Pick<CommentsPanelControllerInput, "tableRowIds"> & {
  activeCommentId?: string;
  /** The comment was opened at its address: no ring on it (OpenCommentsDetail). */
  activeQuiet?: boolean;
  /** Only these threads of the block: the ones on the text that was pressed. */
  threadIds?: readonly string[];
  /** "All discussions": every thread, resolved ones too, by day and filtered. */
  all?: boolean;
  /** Focuses the new comment's field when the panel opens; a page's discussion shown with the page doesn't. */
  autoFocus?: boolean;
  blockId?: string | null;
  blocks?: Block[];
  /** A new comment is being started on the block, not its discussions read. */
  compose?: boolean;
  /** The words it quotes take no pending mark while it is written (OpenCommentsDetail). */
  unmarked?: boolean;
  initialQuote?: string;
  initialQuoteEnd?: number;
  initialQuoteStart?: number;
  /** Where the quote lies beyond the block's own text: its caption, the blocks it runs on into. */
  initialQuotePlaces?: CommentQuotePlaces;
  inline?: boolean;
  /** Drawn for its thread open in a view of its own alone (usePageDiscussionPlace): nothing shows under the title. */
  sideOnly?: boolean;
  /**
   * The id of the place an inline panel sits in, which its page or database
   * page made for it; the same page shown twice has two.
   */
  inlineHostId?: string;
  onClose: () => void;
  onStatus?: ProductStatusSink;
  organizationId?: string;
  pageId: string;
  /** A database row's own discussion, on the page that holds the row. */
  rowId?: string;
  /**
   * The row or card a row's new comment is written under, as Notion's row
   * menu "댓글" opens a field there (seen 2026-10-11), in a block's card.
   */
  anchorElement?: HTMLElement | null;
  session: Session;
  /** Whether the viewer may change the page, which settling others' suggestions needs. */
  canEditPage?: boolean;
  /** Whether the viewer may comment; one who may only view reads the discussion without writing in it. */
  canComment?: boolean;
  /**
   * "All discussions" beside the page, as Notion's (seen 2026-10-01): it
   * takes the page's right side without covering it, the page's bar closes
   * it, and the sidebar folds when the page would be left too narrow.
   */
  docked?: boolean;
  /** The page's workspace, where files attached to comments go; without it none can be attached. */
  workspaceId?: string;
};

/** Whether a key or the focus is on a comment's own buttons ("…", a reaction, resolve). */
function onCommentButtons(target: EventTarget | Element | null) {
  return target instanceof Element && Boolean(target.closest("[data-comment-hover-actions]"));
}

/** Whether the focus is on nothing: on the page's body, or gone with what held it. */
function focusOnNothing() {
  const active = document.activeElement;
  return !active || active === document.body || !active.isConnected;
}

/** A block's discussion opened at an address after it was deleted whole is its block's card saying so (GoneDiscussionCard). */
export function CommentsPanel(props: CommentsPanelProps) {
  return props.blockId && goneDiscussionLink(props.activeCommentId) ? <GoneDiscussionCard blockId={props.blockId} onClose={props.onClose} /> : <DiscussionsPanel {...props} />;
}

function DiscussionsPanel({
  inline = false, inlineHostId: givenInlineHostId, autoFocus = true, onClose, canEditPage = false, canComment = true, ...input
}: CommentsPanelProps) {
  const all = input.all === true && !inline;
  const docked = all && input.docked === true;
  // On a phone a new comment, the page's or a line's, is written in a sheet
  // at the foot of the screen over the page darkened, as Notion's phone
  // (seen 2026-11-10: the caret tools' comment in a title or a line, a
  // line's "댓글"); a row's new comment from its menu stays under the row.
  const { phone, touch } = useSidebarLayout(true);
  const sheet = phone && input.compose === true && !inline && !all && !input.anchorElement;
  // A block's discussions open in a card under the block, as Notion's do
  // (seen 2026-10-08), not in a drawer over the page; so does a row's new comment.
  const anchored = !sheet && !inline && !all && Boolean(input.blockId || input.anchorElement);
  // On a touch screen the card opened to read a block's discussion, from its
  // side mark or its pressed text, is Notion's "토론" sheet (TouchDiscussionSheet).
  const readSheet = touch && anchored && !input.anchorElement && input.compose !== true && !input.initialQuote?.trim();
  const modal = !inline && !docked && !anchored;
  // The words the new comment quotes, in every block they run across.
  const quotePlaces = input.blockId && input.initialQuoteStart !== undefined && input.initialQuoteEnd !== undefined
    ? commentTextPlaces(input.blockId, input.initialQuoteStart, input.initialQuoteEnd, input.initialQuotePlaces) : null;
  usePendingCommentMark((anchored || sheet) && input.compose && !input.unmarked ? quotePlaces : null);
  const { t } = useTranslation(["commentsPanel", "common"]);
  const titleId = useId();
  const listId = useId();
  // Under a page's title, the discussion whose thread opens in a view of its own (DiscussionSide).
  const sideOwner = useId();
  const restoreFocusRef = useRef<HTMLElement | null>(null);
  const controller = useCommentsPanelController({ ...input, readsShownOnClose: anchored && !readSheet && input.compose !== true });
  const anchoredPlace = useAnchoredPlace(anchored && !readSheet ? input.blockId : null, input.compose === true && !input.anchorElement,
    anchored && !readSheet ? input.anchorElement : null, quotePlaces, input.threadIds, controller.panelRef,
    input.compose === true ? ANCHORED_COMPOSER_WIDTH : ANCHORED_WIDTH);
  const sheetTop = usePhoneSheetTop(controller.panelRef, sheet, input.blockId);
  // A comment sent from a sheet leaves the caret nowhere and the keyboard
  // goes, as Notion's (seen 2026-11-10); closed unsent, the caret goes back.
  const sent = useRef(false);
  // The page's own reactions (a feed post's) head its comments, as Notion's.
  const pageReactionPicker = useReactionPicker((emoji) => controller.reactToPage(emoji, true),
    input.workspaceId ? { workspaceId: input.workspaceId, pageId: input.pageId } : undefined);
  // Open and resolved threads are two tabs over the same list.
  const statusTabsRef = useRef<HTMLDivElement>(null);
  const statusTabs = useNativeTabs<"open" | "resolved">({
    tabs: (["open", "resolved"] as const).map((view) => ({ key: view, text: t(`commentsPanel:status.${view}`) })),
    selected: controller.commentView, onSelect: controller.setCommentView,
    label: t("commentsPanel:status.aria"), listRef: statusTabsRef,
  });
  // Whether the panel was opened to write in, as it was when it opened.
  const autoFocusRef = useRef(autoFocus);
  // Inline, the panel sits in its page's (or database page's) place for it,
  // which may be drawn in the same pass.
  const inlineHostId = givenInlineHostId ?? `page-comments-inline-${input.rowId ?? input.pageId}`;
  const [inlineHost, setInlineHost] = useState<HTMLElement | null>(
    () => (inline && typeof document !== "undefined" ? document.getElementById(inlineHostId) : null),
  );
  useLayoutEffect(() => {
    if (inline && !inlineHost?.isConnected) setInlineHost(document.getElementById(inlineHostId));
  });
  const close = useCallback(() => onClose(), [onClose]);
  // On a comment's own buttons ("…", a reaction, resolve) Escape leaves the
  // card and the focus where they are, as Notion's (seen 2026-11-06: a second
  // Escape after closing a comment's "…" menu); from its reply field or the
  // card itself it closes the card.
  const escape = useCallback(() => { if (!onCommentButtons(document.activeElement)) close(); }, [close]);
  // The drawer over the page is a modal layer: it holds the page still and
  // Escape closes it once nothing opened over it takes the key. A block's card
  // is a layer a press outside it closes. Docked or in the page, it is part of
  // the page and Escape closes it from inside it. A new comment's card under
  // its block or row floats over the page, which takes no pointer under it (pageHoverShield.ts): a press anywhere
  // beside it, a page line, a to-do's box, the top bar's "공유", only closes it, no block shows its handle under the
  // pointer and the wheel leaves the page still, as Notion's (2026-11-26, judged after the press by the screen and one
  // typed letter, two tabs each: a block's at Chrome 1000, the box left unticked; a row's at 1440, the letter nowhere).
  // A card reading a block's threads lets the press go on to the page whole (pressGoesOn), as Notion's does: the caret
  // where pressed, and the top bar's "공유" or "⋯" opening at that one press, its field taking the focus (2026-11-26,
  // Chrome 1000 wide, judged after the press by the screen: "공유" two tabs, "⋯" two tabs).
  const { zIndex } = useOverlayLayer(modal || (anchored && !readSheet), {
    kind: "card", ref: controller.panelRef, modal, modalOwned: modal ? () => [controller.backdropRef.current, controller.panelRef.current] : undefined,
    restoreFocus: false, onEscape: escape, onOutsidePress: anchored ? close : undefined, floats: anchored && input.compose === true,
    pressGoesOn: anchored && input.compose !== true,
    inside: () => anchored && input.threadIds ? discussionMarks(controller.comments.filter((comment) => !comment.parentId && !comment.resolved).map((comment) => comment.id)) : [],
  });
  // A block's card writes a new comment when one was asked for, quotes text,
  // or the block has none open yet; else it shows the block's open threads.
  // Taking back the last reaction of the only threads it showed, all begun by
  // reactions, turns the card into a new comment's field where it stands,
  // the caret in it, as Notion's does (seen 2026-11-09).
  const [afterReaction, setAfterReaction] = useState(false);
  // A block's card steps aside as its comment goes, which counts at once
  // (commentService create), and comes back with its words if refused.
  const [sending, setSending] = useState(false);
  const composing = sheet || anchored && (input.compose === true || Boolean(input.initialQuote?.trim()) || afterReaction
    || (!input.threadIds && !controller.loading && controller.visibleThreads.length === 0));

  useEffect(() => {
    restoreFocusRef.current = focusedOpener();
    // A new comment's card under its block, closed unsent by Escape or a press outside, leaves the focus on nothing,
    // the letter typed next going nowhere, as Notion's (2026-11-26, Chrome 1000 wide, two tabs, judged after the action
    // by the screen and one typed letter; 1440 wide, its margin field the same) and this page's wide margin field.
    const leavesFocus = anchored && input.compose === true && !input.anchorElement;
    const frame = window.requestAnimationFrame(() => {
      if (!autoFocusRef.current || docked || input.threadIds) return;
      if (inline || sheet || input.initialQuote?.trim() || (anchored && input.compose)) giveNewCommentCaret(controller.panelRef.current, () => controller.draftRef.current?.focus());
      // A block's card opened by its mark leaves the focus on the mark (below).
      else if (!anchored || readSheet || input.activeCommentId) controller.panelRef.current?.focus();
    });
    return () => {
      window.cancelAnimationFrame(frame);

      const restore = restoreFocusRef.current;
      restoreFocusRef.current = null;
      // Back where it was, the page staying where it is (a card's closing or
      // sending moved the page 220–344px when the editor scrolled to its focus),
      // only when the closing left it on nothing: a press that closed a card reading a block's threads went on to the
      // page and its caret stays where pressed, and a window opened as it closed keeps it, as Notion's (2026-11-26,
      // Chrome 1000 wide, the card opened from its 💬 mark: another line pressed, the letter typed next going into it,
      // three tabs; the top bar's "공유" pressed, its field keeping the focus, one tab — judged after the press).
      if (autoFocusRef.current && restore?.isConnected && !sent.current && !leavesFocus) giveFocusBack({ to: restore, keepScroll: true, when: focusOnNothing });
    };
  }, [anchored, controller.backdropRef, controller.draftRef, controller.panelRef, docked, inline, input.compose, input.initialQuote, modal]);
  // Under the title, asked to write once it already shows (the page's
  // reactions alone: "댓글 추가" in the head), the field under them takes the
  // caret, as Notion's. Measured after the press only (no script in the page before it), Notion in Chrome 1440:
  // with no reactions (2026-11-15) and with the title's reaction alone (2026-11-16, two tabs 6 minutes apart),
  // the head's "댓글 추가" pressed, "z" typed went into the field under the title (Leafen the same).
  const askedToWrite = useRef(autoFocus);
  useEffect(() => {
    if (!autoFocus) {
      askedToWrite.current = false;
      return undefined;
    }
    if (!inline || askedToWrite.current) return undefined;
    const frame = window.requestAnimationFrame(() => {
      askedToWrite.current = true;
      giveNewCommentCaret(controller.panelRef.current, () => controller.draftRef.current?.focus());
    });
    return () => window.cancelAnimationFrame(frame);
  }, [autoFocus, controller.draftRef, inline]);

  // A block's card reading its threads leaves its reply fields unfocused,
  // with "댓글 추가" showing, as Notion's does from the block's mark or its
  // text; and once its last open thread is resolved or gone it closes, as
  // Notion's does, rather than turning into a new comment's field (both seen
  // 2026-11-06) — unless all it showed were begun by reactions (above).
  const showedThreads = useRef(false);
  const showedReactionsOnly = useRef(false);
  useEffect(() => {
    if (!anchored || input.compose || controller.loading) return;
    if (controller.visibleThreads.length > 0) {
      showedThreads.current = true;
      showedReactionsOnly.current = controller.visibleThreads.every(isReactionThread);
    } else if (showedThreads.current && !afterReaction) {
      if (showedReactionsOnly.current) setAfterReaction(true);
      else close();
    }
  }, [afterReaction, anchored, close, controller.loading, controller.visibleThreads, input.compose]);
  useEffect(() => {
    if (!afterReaction) return;
    const frame = window.requestAnimationFrame(() => controller.draftRef.current?.focus());
    return () => window.cancelAnimationFrame(frame);
  }, [afterReaction, controller.draftRef]);
  // The block's mark shows its card open, and a new comment being written
  // counts there with a yellow dot, as Notion's (seen 2026-11-06).
  useEffect(() => {
    if (!(anchored || sheet) || !input.blockId) return;
    return showOpenCommentCard({ blockId: input.blockId, composing: input.compose === true });
  }, [anchored, sheet, input.blockId, input.compose]);
  // "모든 토론" keeps the thread last pressed or reopened chosen, faintly
  // yellow, as Notion's (seen 2026-11-06); the caret going into a thread's
  // reply field chooses it too (seen 2026-11-12). Where the caret goes then
  // is the thread's own (CommentThreadCard).
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const select = useCallback((comment: NativeComment) => setSelectedId(comment.id), []);
  // The text of the threads a card shows stays dark while it is open: those
  // of the pressed text, or all of a block's from its side mark, as Notion's
  // (seen 2026-11-09). In "모든 토론", the chosen thread's and the one the
  // pointer last went over (seen 2026-11-12).
  const [pointedId, setPointedId] = useState<string | null>(null);
  const darkened = (input.threadIds ?? (anchored && input.blockId && !input.compose
    ? controller.visibleThreads.map((thread) => thread.id)
    : all ? [...new Set([selectedId, pointedId].filter((id): id is string => Boolean(id)))] : [])).join(" ");
  useEffect(() => {
    if (!darkened) return;
    setActiveCommentThreads(darkened.split(" "));
    return () => setActiveCommentThreads([]);
  }, [darkened]);

  // The comment an address or a notice points at comes into view and takes
  // focus; in a block's card its thread's reply field takes the caret once,
  // as Notion's does from its inbox (seen 2026-11-06).
  const linkedCaret = useRef(false);
  const commentsNow = useRef(controller.comments);
  commentsNow.current = controller.comments;
  useEffect(() => {
    // Opened by its mark ("💬 1" by a line where the page has no room for the
    // margin), a block's card leaves the focus on the mark and its reply
    // field empty, as Notion's. Measured 2026-11-16 after the press only (no script in the page before it), Notion in
    // Chrome 1024: on a settled page the mark kept the focus, "z" typed went nowhere and ringed it, Escape closed the
    // card (4 of 4, three tabs over 20 minutes); pressed while the page still loaded, the caret went into "댓글 추가"
    // (2 of 2) — the 2026-11-06 sighting. Opened by pressing its text, the caret stays in the text. Measured 2026-11-15 after the
    // press only, Notion in Chrome 1440: the words pressed, "q" typed went into them, the card lit with its reply
    // field empty (Leafen the same).
    // Once the caret is in the card's reply field, new comments don't take it.
    if (!input.activeCommentId || (anchored && linkedCaret.current)) return;
    const frame = window.requestAnimationFrame(() => {
      const list = controller.listRef.current;
      const target = Array.from(list?.querySelectorAll<HTMLElement>("[data-comment-id]") ?? [])
        .find((element) => element.dataset.commentId === input.activeCommentId);
      if (!target) return;
      target.scrollIntoView({ block: "nearest" });
      const linked = commentsNow.current.find((comment) => comment.id === input.activeCommentId);
      const reply = anchored && autoFocusRef.current && !linkedCaret.current && linked
        ? list?.querySelector<HTMLElement>(`[data-comment-id="${CSS.escape(linked.parentId || linked.id)}"] [data-comment-reply-input]`)
        : null;
      if (reply) linkedCaret.current = true;
      (reply ?? target).focus({ preventScroll: true });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [anchored, controller.commentView, controller.comments.length, controller.listRef, input.activeCommentId]);

  function onPanelKeyDown(event: React.KeyboardEvent<HTMLElement>) {
    if (event.defaultPrevented || imeComposing(event)) return;
    if (event.key === "Escape") {
      if (modal) return;
      event.preventDefault();
      // A comment's menu or picker drawn over the card takes the key, and
      // the card stays open as Notion's (seen 2026-11-06).
      if (escapeLayerOver(event.currentTarget, event.nativeEvent)) { event.stopPropagation(); return; }
      if (onCommentButtons(event.target)) { event.stopPropagation(); return; }
      close();
      return;
    }

  }

  // Under a page's title the field starts a thread, each answered from its own box (CommentCard), as Notion's (seen 2026-11-25).
  const composer = canComment && (
    <CommentComposer
      // A comment line under a title, on a database page or in a block's card has Notion's 24px picture
      // (seen 2026-10-25, 10-28, 11-09).
      avatarSize={inline || anchored || sheet ? 24 : undefined}
      attachments={controller.draftAttachments}
      blockId={controller.blockId}
      currentUserLabel={input.session.user.name || input.session.user.email || t("commentsPanel:mention.you")}
      currentUserId={input.session.user.id}
      draft={controller.draftStore}
      fieldRef={controller.draftRef}
      inline={inline || anchored || sheet}
      escapeLeaves={inline}
      pageId={input.pageId}
      submitting={controller.submitting}
      onSubmit={() => {
        if (anchored || sheet) setSending(true);
        void controller.submit().then((made) => {
          sent.current = Boolean(made && sheet);
          if (made && (anchored || sheet)) close(); else setSending(false);
        });
      }}
      onUpdate={controller.setDraft}
    />
  );

  // "모든 토론" draws its threads as under a page's title, without "▯ 열기"
  // (Notion's, seen 2026-11-25 in Chrome at 1440, two tabs).
  const card = (comment: NativeComment) => (
    <CommentThreadCard key={comment.id} comment={comment} controller={controller} userId={input.session.user.id}
      canEditPage={canEditPage} canComment={canComment} activeCommentId={input.activeCommentId} activeQuiet={input.activeQuiet}
      listed={all} under={anchored} flat={inline || all} sideOwner={inline ? sideOwner : undefined}
      selected={all && comment.id === selectedId} onSelect={all ? select : undefined}
      linkTarget={input.rowId ?? input.pageId} onStatus={input.onStatus} />
  );

  const panel = (
    <>
      {modal && <button ref={controller.backdropRef} type="button" className={`${styles.backdrop} ${sheet ? sheetStyles.shade : ""}`}
        style={zIndex === undefined ? undefined : { zIndex: zIndex - 1 }}
        onClick={close} tabIndex={-1} aria-label={t("commentsPanel:closeAria")} data-comments-backdrop />}
      <aside
        ref={controller.panelRef}
        className={`${styles.panel} ${inline || anchored || sheet ? styles.inlinePanel : ""} ${inline ? titleStyles.titleDiscussion : ""} ${docked ? allStyles.docked : ""} ${anchored ? cardStyles.card : ""} ${sheet ? sheetStyles.sheet : ""}`}
        data-side-only={input.sideOnly || undefined}
        style={input.sideOnly ? { display: "none" } : readSheet ? undefined : anchored ? { ...anchoredPlace, zIndex, visibility: anchoredPlace && !sending ? undefined : "hidden" }
          : sheet ? { top: sheetTop ?? 0, zIndex, visibility: sheetTop !== null && !sending ? undefined : "hidden" }
          : zIndex === undefined ? undefined : { zIndex }}
        role={inline || readSheet ? "region" : docked ? "complementary" : "dialog"}
        aria-modal={modal ? "true" : undefined}
        aria-label={inline || anchored || sheet ? t("commentsPanel:title") : undefined}
        aria-labelledby={inline || anchored || sheet ? undefined : titleId}
        tabIndex={-1}
        onKeyDown={onPanelKeyDown}
        data-comments-panel
        data-comments-mode={inline ? "page-inline" : all ? "all-discussions" : anchored ? "block-card" : sheet ? "phone-sheet" : "block-drawer"}
        data-docked={docked ? "true" : undefined}
        data-page-side-panel={docked ? "docked" : undefined}
      >
        {all ? (
          // As Notion's "All discussions": its filter and the page's notifications.
          // Docked beside the page, headed "토론" as Notion's (seen 2026-11-25 in two tabs, AllDiscussions).
          <div className={`${styles.header} ${docked ? allStyles.head : ""}`}>
            <div id={titleId} className={styles.title}>{t(docked ? "commentsPanel:discussions.title" : "commentsPanel:discussions.all")}</div>
            <div className={discussionStyles.headerActions}>
              <DiscussionFilterMenu filter={controller.filter} onChange={controller.setFilter}
                people={discussionFilterPeople(controller.people, input.session.user.id, controller.actorProfiles)} />
              <PageNotifyBell pageId={input.rowId ?? input.pageId} session={input.session} onFailure={(message) => input.onStatus?.(message, "error")} />
              {/* Docked, the page's bar has the "»" that closes it. */}
              {!docked && (
                <button type="button" className={styles.close} onClick={close} aria-label={t("commentsPanel:closeAria")}><X size="regular" /></button>
              )}
            </div>
          </div>
        ) : !inline && !anchored && !sheet && (
          <div className={styles.header}>
            <div><div id={titleId} className={styles.title}>{t("commentsPanel:title")}</div>
              <div className={styles.subtitle}>{controller.blockId ? t("commentsPanel:subtitle.block") : t("commentsPanel:subtitle.page")}</div></div>
            <button type="button" className={styles.close} onClick={close} aria-label={t("commentsPanel:closeAria")}><X size="regular" /></button>
          </div>
        )}
        {/* Under a page's title, as Notion's, its open threads show without tabs. */}
        {!inline && !all && !anchored && !sheet && (
          <div {...statusTabs.tabListProps} ref={statusTabsRef} className={styles.tabs}>
            {(["open", "resolved"] as const).map((view) => (
              <NativeTab key={view} state={statusTabs.state} tabKey={view} aria-controls={listId}
                data-comment-tab={view} className={tabStyles.tab}>
                <span>{t(`commentsPanel:status.${view}`)}</span>
                <span>{view === "open" ? controller.openCount : controller.resolvedCount}</span>
              </NativeTab>
            ))}
          </div>
        )}
        {controller.blockId && !anchored && !sheet && (
          <div className={styles.anchorPreview}>
            <span>{controller.pendingQuote
              ? t("commentsPanel:anchor.commentingOnSelectedText")
              : t("commentsPanel:anchor.commentingOn")}</span>
            <strong>{controller.pendingQuote || controller.targetPreview || t("commentsPanel:anchor.selectedBlock")}</strong>
          </div>
        )}
        {!inline && !all && !anchored && !sheet && composer}
        {inline ? <CommentReactionChips reactions={controller.pageReactions} userId={input.session.user.id}
          workspaceId={input.workspaceId} canReact={canComment} variant="page"
          onToggle={controller.reactToPage} onOpenPicker={pageReactionPicker.open}
          onResolve={canComment ? controller.resolvePageReactions : undefined} /> : null}
        {pageReactionPicker.picker}
        {/* A sheet writes a new comment alone. */}
        {sheet ? null : <div id={listId} className={styles.list} ref={controller.listRef} role="list" data-comments-list="" aria-label={t("commentsPanel:list.threadsAria")}
          onMouseOver={all ? (event) => {
            for (let node = event.target instanceof HTMLElement ? event.target : null; node && node !== event.currentTarget; node = node.parentElement) {
              const id = node.dataset.commentId;
              if (id && controller.visibleThreads.some((thread) => thread.id === id)) { setPointedId(id); return; }
            }
          } : undefined}>
          {!inline && !anchored && controller.ordered.length === 0 && !controller.loading && (
            <div className={styles.empty} role="status">{t("commentsPanel:list.empty")}</div>
          )}
          {/* Under a page's title, as Notion's, resolved threads leave no line saying none are open. */}
          {!inline && !anchored && controller.ordered.length > 0 && controller.visibleThreads.length === 0 && (
            <div className={styles.empty} role="status">
              {all
                ? t("commentsPanel:discussions.noMatch")
                : controller.commentView === "open" ? t("commentsPanel:list.noOpen") : t("commentsPanel:list.noResolved")}
            </div>
          )}
          {composing && (input.compose || input.initialQuote?.trim()) ? null : controller.visibleThreads.map(card)}
        </div>}
        {/* Under a page's title the new comment's field follows the threads, as Notion's; a block's card writes one alone. */}
        {(inline || composing) && composer}
        {inline ? (
          <DiscussionSide threads={controller.ordered} owner={sideOwner} placeRef={controller.panelRef}>
            {(thread) => <CommentThreadCard comment={thread} controller={controller} userId={input.session.user.id} canEditPage={canEditPage}
              canComment={canComment} activeCommentId={input.activeCommentId} activeQuiet linkTarget={input.rowId ?? input.pageId}
              onStatus={input.onStatus} alone />}
          </DiscussionSide>
        ) : null}
      </aside>
    </>
  );

  if (readSheet) return <TouchDiscussionSheet onClose={close}>{panel}</TouchDiscussionSheet>;
  if (typeof document === "undefined") return panel;
  const portalTarget = inline ? inlineHost : document.body;
  return portalTarget ? createPortal(panel, portalTarget) : null;
}
