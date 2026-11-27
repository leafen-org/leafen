import { useContext, useMemo, useRef, type MouseEvent, type ReactNode } from "react";
import { blockChildrenDirection } from "@/lib/textDirection";
import { openComments } from "@/components/comments/commentPanelEvents";
import { commentOnOwnBlock } from "./blockOwnComment";
import { useOpenCommentCard } from "@/components/comments/openCommentCard";
import { BlockHandle } from "./BlockHandleGate";
import { ReadOnlyBlockHandle } from "./ReadOnlyBlockHandle";
import { useBlockCommentCount, useBlockCommentUnread, useBlockDiscussionKind, useBlockReactionThreads } from "./blockCommentCounts";
import { requestEditorBlockCommand } from "./editorCommands";
import { pressedPoint } from "@/lib/pressedPoint";
import { blockItemText, blockTypeLabel } from "./blockItemLabels";
import { blockAnchorId } from "./blockHandleModel";
import { ChatBubblesIcon, CommentSmallIcon, SmileIcon, SuggestEditIcon } from "@/icons/leafen";
import { dropTextRangeIn, editorRootOf } from "./editorScope";
import { SyncedMirror } from "./syncedBlockContext";
import { nextBlockSelection } from "./editorSelectionModel";
import { spanBlockSelection } from "./blockSelectionKeyboard";
import { chosenPlace, placeChoice } from "./blockOccurrences";
import { caretBlockIn, usePageTakesPress } from "./caretShiftPress";
import type { BlockItemProps, EditorSelection } from "./editorRuntimeTypes";
import type { BlockRun } from "./blockRun";
import { caretBesideLine } from "./caretBesideLine";
import { keysFocusHolder, pressedByFinger } from "@/lib/lastPress";
import { phoneScreen } from "@/lib/screenKind";
import { softKeyboardShown } from "@/lib/softKeyboard";
import { BlockCommentAdd, leaveBlockEnd, offersCommentAdd, placeBlockMarks, trackBlockEnd } from "./BlockCommentAdd";
import { blockTakesComments } from "./blockMenuShape";
import { usePageBlockHalo } from "@/lib/pageBlockHalo";
import { useBlockFlash } from "@/lib/blockFlash";
import { useBlockLongPress } from "./useBlockLongPress";
import styles from "./editorStyles.generated.module.css";

// A row the keys' focus is in shows its handles (data-keys-within on them).
keysFocusHolder(styles.blockRow, { on: styles.gutter });

/** A block's own text, where a right click keeps the text menu. */
const BLOCK_TEXT_FIELD = "[data-rt-editable], [data-native-caption], [data-native-table-cell], pre";

/** A line's block's own text — a paragraph's, a heading's, a list item's, code's — not a
 *  caption's or a nested block's. */
function ownText(group: HTMLElement) {
  return [...group.querySelectorAll<HTMLElement>('[data-native-text="true"], [data-native-code="true"]')]
    .find((field) => field.closest(`.${styles.blockGroup}`) === group) ?? null;
}

/** A chosen block's editor holds no caret and no text range (useBlockSelectionKeys). */
function leaveNoCaret(root: Element | null) {
  if (document.activeElement instanceof HTMLElement && document.activeElement.isContentEditable) document.activeElement.blur();
  dropTextRangeIn(root);
}

export function BlockFrame({
  adapter,
  block,
  children,
  depth,
  selected,
  selection,
  setSelection,
  nested,
  run,
  occurrence = block.id,
}: BlockItemProps & { children: ReactNode; nested?: ReactNode; run?: BlockRun; occurrence?: string }) {
  // A block shown again in a synced copy leaves the page's link to it to
  // the original.
  const mirrored = useContext(SyncedMirror);
  // A full-page database's database is its page: it isn't moved, turned or
  // removed as a block.
  const fixed = adapter.fixedBody === true && !block.parentId;
  // A simple table's mark counts and opens its rows' discussions too (useBlockCommentCount).
  const tableRowIds = useMemo(() => block.type === "table" ? adapter.graph.blocks
    .filter((candidate) => candidate.parentId === block.id && candidate.type === "table_row").map((candidate) => candidate.id) : undefined,
  [adapter.graph.blocks, block.id, block.type]);
  const commentCount = useBlockCommentCount(block.id, tableRowIds);
  const openCard = useOpenCommentCard(block.id);
  // A new comment being written here counts on the mark, with a dot.
  const markCount = commentCount + (openCard?.composing ? 1 : 0);
  const reactionThreads = useBlockReactionThreads(block.id);
  const unreadDiscussion = useBlockCommentUnread(block.id, tableRowIds);
  const discussionKind = useBlockDiscussionKind(block.id, tableRowIds);
  const flashTurn = useBlockFlash(block.id);
  const suggestionsOnly = discussionKind === "suggestion";
  // A line with no discussion offers "💬+" at its end to whoever may comment
  // on the page, as Notion's (seen 2026-11-12); one with a mark or a card
  // there offers none.
  const offersComment = !mirrored && !fixed && Boolean(adapter.session) && (adapter.database.canComment ?? !adapter.readOnly)
    && offersCommentAdd(block.type) && blockTakesComments(block) && markCount === 0 && reactionThreads.length === 0;
  // Whether the press began in an inline database: a tab chosen as it is
  // pressed may draw the database anew (a view waiting for its source among
  // linked ones) before the press's mousedown, which then reaches the block.
  const pressedInDatabase = useRef(false);
  // Its row, whose handles are drawn while the pointer is at it (drawnHandles).
  const row = useRef<HTMLDivElement>(null);
  // The block chosen alone where it is drawn here — in a synced copy that
  // draws it again, there (blockOccurrences) — or as it is when it already is.
  const chooseHere = (current: EditorSelection) => current.has(block.id) && (current.size > 1 || chosenPlace(current, block.id) === occurrence)
    ? current : placeChoice(new Set([block.id]), [[block.id, occurrence]]);
  function select(event: MouseEvent<HTMLDivElement>) {
    if (fixed) return;
    // A right press on a chosen block keeps the chosen blocks for its menu
    // (openMenu), which works on all of them, as Notion's (seen 2026-11-13:
    // three chosen, a right click on the divider among them opened "블록 3개").
    if (event.button !== 0 && selection.has(block.id)) return;
    if (event.target instanceof Element && event.target.closest("[data-rt-editable],button,input,a")) return;
    // A press in a block's own panel — a button's settings — is the panel's: its message box takes the caret where
    // pressed and its room chooses nothing, as Notion's (seen 2026-11-21 in a page's button, a letter typed after).
    if (event.target instanceof Element && event.target.closest("[data-block-panel]")) return;
    // A press in an inline database works its cells, views and menus, as
    // Notion's, and leaves the block unselected; its handle selects it, and
    // Shift with the caret in the page's text the blocks up to it (below).
    if (pressedInDatabase.current || (event.target instanceof Element && event.target.closest("[data-inline-database-wrapper]"))) return;
    // A click in an editor inside this block is that editor's.
    if (editorRootOf(event.target) !== editorRootOf(event.currentTarget)) return;
    // The innermost block pressed is the one chosen — a picture nested under a
    // line, not the line — as Notion's (seen 2026-11-07); the press reaches the
    // blocks around it after, and a column's gap still chooses its columns.
    const inner = event.target instanceof Element ? event.target.closest(`.${styles.blockGroup}`) : null;
    if (inner && inner !== event.currentTarget && !(inner instanceof HTMLElement && inner.dataset.blockType === "column")) return;
    // A finger's tap anywhere on a line's block — beside its text, on its
    // bullet or number, in the room above or below it — puts the caret in its
    // text, nearest the tap; the block is chosen from its own ways, its
    // handle and its menu (2026-11-09: a tap on the 6px past a line's end on
    // a phone chose the block). The mouse's click there chooses it, as
    // Notion's (seen 2026-11-09 in Chrome). On a phone a bullet or number
    // tapped with the keyboard down puts nothing down and raises no keyboard,
    // as Notion's Android app (seen 2026-11-21 on a Galaxy A90, w2270, twice;
    // with the keyboard up the caret goes to the line's start, as here).
    if (pressedByFinger()) {
      const text = ownText(event.currentTarget);
      if (text) {
        event.preventDefault();
        const markerAtRest = phoneScreen() && !softKeyboardShown() && event.target instanceof Element && event.target.closest("[data-list-marker]");
        if (!adapter.readOnly && !markerAtRest) caretBesideLine(text, { x: event.clientX, y: event.clientY });
        return;
      }
    }
    // Shift with the caret or words in another block chooses the blocks from
    // there to this one, as a Shift+press in this one's text does
    // (nativeTiptapProductKeys; Notion's, seen 2026-11-11).
    const caretBlock = event.shiftKey && !selection.size ? caretBlockIn(editorRootOf(event.currentTarget)) : null;
    const span = caretBlock && caretBlock !== occurrence ? spanBlockSelection(adapter.graph, caretBlock, occurrence, null, adapter.pageId) : null;
    const orderedIds = adapter.graph.blocks.map((candidate) => candidate.id);
    if (span && span.selection.size > 1) setSelection(placeChoice(span.selection, span.places));
    else setSelection((current) => {
      const next = nextBlockSelection(current, block.id, { additive: event.metaKey || event.ctrlKey, range: event.shiftKey }, orderedIds);
      return next.size === 1 && next.has(block.id) ? placeChoice(next, [[block.id, occurrence]]) : next;
    });
    // Chosen, it leaves no caret: the engine's press would keep the keys that
    // are the chosen block's (Tab, ⌘/, ⌫), as Notion's press doesn't (seen 2026-11-04),
    // and no text range where an IME's syllable would go (2026-11-05). The
    // caret goes with the press and its release themselves, not after them: a
    // letter typed as the press ended went to the caret the browser had put
    // at the nearest text — the code's end, the line under a picture — while
    // it was taken away only on a timer, which Chrome runs after the input
    // waiting (2026-11-20). In the block's editable room the browser puts none
    // down and the focus leaves whatever had it, as the press's own move to
    // the editor did; on a part drawn uneditable (a picture, a player) the
    // press keeps its own focus, and the engine's release, which chooses a
    // picture or an equation as its node and focuses the page for it, is let
    // go of as it ends.
    const root = editorRootOf(event.currentTarget);
    if (!adapter.readOnly && event.target instanceof HTMLElement && event.target.isContentEditable) {
      event.preventDefault();
      if (document.activeElement instanceof HTMLElement && document.activeElement !== document.body) document.activeElement.blur();
    }
    leaveNoCaret(root);
    window.addEventListener("mouseup", () => leaveNoCaret(root), { once: true });
  }

  // Shift and a press on an item or row of this block's inline database, the
  // page's text holding the caret, chooses the blocks from the caret's through
  // this one, as Shift and a press in this one's text would; the view under
  // it never has the press (caretShiftPress.ts).
  const frame = useRef<HTMLDivElement>(null);
  usePageTakesPress(frame, (from) => {
    if (from === occurrence) return;
    const span = spanBlockSelection(adapter.graph, from, occurrence, null, adapter.pageId);
    if (span.selection.size > 1) setSelection(placeChoice(span.selection, span.places));
    const root = editorRootOf(frame.current);
    leaveNoCaret(root);
    window.addEventListener("mouseup", () => leaveNoCaret(root), { once: true });
  });

  // Right-clicking a block opens its menu at the pointer, for the selected
  // blocks when it is one of them; buttons and fields keep the browser's own
  // menu, and a part that opened its own menu (a dashboard widget's, a
  // row's) keeps the press. A link the block draws opens its block menu, on
  // its words and beside them, as Notion's (seen 2026-10-31 on a page block;
  // 2026-11-13 on a contents row, a breadcrumb's page, a bookmark's card and
  // a file's name); one in a database keeps the browser's. So does a block's
  // own text, its links in it and beside its words too, chosen or not: there
  // the browser's or the app's text menu (spelling, cut, copy, paste, look
  // up) opens, as Notion's (seen 2026-11-08 in its app and on the web;
  // 2026-11-13 on one and on several chosen blocks, which the press lets go
  // of — Editor's pointed).
  function openMenu(event: MouseEvent<HTMLDivElement>) {
    if (adapter.readOnly || fixed || mirrored || event.defaultPrevented) return;
    if (event.target instanceof Element && event.target.closest("button,input,select,textarea,[data-inline-database-wrapper] a")) return;
    if (event.target instanceof Element && event.target.closest(BLOCK_TEXT_FIELD)) return;
    if (editorRootOf(event.target) !== editorRootOf(event.currentTarget)) return;
    event.preventDefault();
    event.stopPropagation();
    setSelection(chooseHere);
    requestEditorBlockCommand(block.id, "menu", pressedPoint(event), event.currentTarget);
  }

  // A finger resting on a block with no text of its own — a file's or a
  // PDF's row, a picture's frame — opens its menu there, as a right click does.
  const longPress = useBlockLongPress({
    enabled: !adapter.readOnly && !fixed && !mirrored,
    inner: (target, group) => target.closest(`.${styles.blockGroup}`) === group && !ownText(group),
    open: (point, group) => {
      setSelection(chooseHere);
      requestEditorBlockCommand(block.id, "menu", point, group);
    },
  });

  return (
    <div ref={frame} className={styles.blockGroup} id={mirrored ? undefined : blockAnchorId(block.id)} data-block-id={block.id} data-page-id={adapter.pageId}
      // A block suggested in names its suggestion, whose rejecting hides it at the press (SettlingSuggestionMarks).
      data-suggested-by={typeof block.content.suggestionId === "string" && block.content.suggestionId ? block.content.suggestionId : undefined}
      data-depth={depth} data-block-type={block.type} data-run={run} data-selected={selected ? "true" : undefined}
      role="group" aria-label={blockItemText("block.aria", { type: blockTypeLabel(block.type) })} onMouseDown={select}
      onPointerDownCapture={(event) => { pressedInDatabase.current = event.target instanceof Element && event.target.closest("[data-inline-database-wrapper]") !== null; }}
      onContextMenu={openMenu} {...longPress}>
      <div ref={row} className={styles.blockRow} data-type={block.type} role="group" onPointerMove={trackBlockEnd} onPointerLeave={leaveBlockEnd}>
        {/* A column group has no handle of its own, only the blocks in its columns, as Notion's (seen 2026-10-11). */}
        {!adapter.readOnly && !fixed && block.type !== "column_list" ? <BlockHandle adapter={adapter} block={block} selection={selection} row={row}
          onSelect={() => setSelection(chooseHere)} onRelease={() => setSelection(current => current.size ? new Set() : current)} /> : null}
        {/* A page the reader can't change (locked) keeps a short handle in the app; a published page has none. */}
        {adapter.readOnly && adapter.session && !fixed && block.type !== "column_list" ? <ReadOnlyBlockHandle adapter={adapter} block={block} row={row} onSelect={() => {
          setSelection(placeChoice(new Set([block.id]), [[block.id, occurrence]]));
        }} /> : null}
        <div className={styles.blockBody}
          data-block-color={typeof block.content.color === "string" ? block.content.color : undefined}>
          {children}
        </div>
        {offersComment ? <BlockCommentAdd row={row} onComment={() => {
          // The block goes unchosen while its comment is written, as its menu's "댓글".
          setSelection((current) => current.size ? new Set() : current);
          commentOnOwnBlock(adapter, block);
        }} /> : null}
        {/* The block's marks stand just past the text column's right edge, as
            Notion's "💬 1" and "☺ 1" do (seen 2026-11-06), the discussions'
            mark drawn and named by their kind (useBlockDiscussionKind); a reaction shows
            the outline face whatever emoji was picked (seen 2026-11-09). A
            mark keeps the focus as its card opens (CommentsPanel), so it says
            it has the card open, for Escape there or a press on it to close it (overlayLayer, openerPress.ts). */}
        {!mirrored && (markCount > 0 || reactionThreads.length > 0) ? (
          <span ref={placeBlockMarks} className={styles.blockMarks} contentEditable={false}>
            {markCount > 0 ? (
              <button type="button" className={styles.blockCommentPill} data-block-comment-count data-discussion-kind={discussionKind}
                data-open={openCard ? "true" : undefined} aria-haspopup="dialog" aria-expanded={Boolean(openCard)}
                title={blockItemText(suggestionsOnly ? "comments.suggestionCount" : "comments.count", { count: markCount })}
                aria-label={blockItemText(suggestionsOnly ? "comments.unresolvedSuggestionsOnBlock" : "comments.unresolvedOnBlock",
                  { count: markCount, type: blockTypeLabel(block.type) })}
                onClick={() => openComments({ pageId: adapter.pageId, blockId: block.id, ...(tableRowIds?.length ? { tableRowIds } : {}) })}>
                {suggestionsOnly ? <SuggestEditIcon aria-hidden="true" />
                  : discussionKind === "both" ? <ChatBubblesIcon aria-hidden="true" /> : <CommentSmallIcon lines aria-hidden="true" />}
                {openCard?.composing ? <span className={styles.blockMarkDraft} aria-hidden="true" />
                  : unreadDiscussion ? <span className={styles.blockMarkUnread} aria-hidden="true" data-block-comment-unread /> : null}
                <span className={styles.blockMarkCount}>{markCount}</span>
              </button>
            ) : null}
            {reactionThreads.map((thread) => (
              <button key={thread.id} type="button" className={styles.blockCommentPill}
                data-block-reaction-thread={thread.id} aria-haspopup="dialog" aria-expanded={Boolean(openCard)}
                aria-label={blockItemText("comments.reactionThread", { emoji: thread.emoji, people: thread.count })}
                onClick={() => openComments({ pageId: adapter.pageId, blockId: block.id })}>
                <SmileIcon aria-hidden="true" /><span className={styles.blockMarkCount}>{thread.count}</span>
              </button>
            ))}
          </span>
        ) : null}
      </div>
      {/* A block indents its children from its own start (EditorBlocks.module.css):
          one set to a direction, as Notion's (seen 2026-10-02), and one that
          finds it from its words, by them (seen 2026-11-11: an Arabic list
          item's children in from the right in a left-to-right app); each
          keeps its own direction (blockChildrenDirection). */}
      {nested ? (
        <div className={styles.children} data-direction={blockChildrenDirection(block)}>{nested}</div>
      ) : null}
      {/* A block a margin card was pressed open for washes as chosen for a moment (lib/blockFlash.ts). */}
      {selected ? <span className={styles.selectionHalo} aria-hidden="true" />
        : flashTurn ? <span key={flashTurn} className={`${styles.selectionHalo} ${styles.flashHalo}`} data-block-flash aria-hidden="true" />
        : !mirrored && block.type === "child_page" && typeof block.content.childPageId === "string"
          ? <PageMenuHalo pageId={block.content.childPageId} /> : null}
    </div>
  );
}

/** A page block's wash while a menu elsewhere — a sidebar row's — works on its page (pageBlockHalo). */
function PageMenuHalo({ pageId }: { pageId: string }) {
  return usePageBlockHalo(pageId) ? <span className={styles.selectionHalo} data-page-menu-halo aria-hidden="true" /> : null;
}
