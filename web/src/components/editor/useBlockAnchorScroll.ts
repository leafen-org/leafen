import { useEffect, useRef } from "react";
import type { GraphBlock } from "@/contentGraph";
import { TOGGLE_BLOCK_TYPES } from "@/lib/blockSemantics";
import { motionSafeScrollBehavior } from "@/lib/motion";
import { blockIsPageItself, blockLinkTarget } from "@/lib/blockAddress";
import { pageScrollOf } from "@/lib/phoneLineInSight";
import { requestEditorBlockSelection } from "./editorCommands";
import { NODE_VIEWS_DRAWN_EVENT } from "./nativeTiptapLiveState";

// A block the page itself moved the address to (leaving a presentation at
// its slide, as Notion's) is named there without being followed as a link.
let shownByPage = "";

/** Puts a block in the address as already shown: no scroll or flash follows. */
export function noteBlockAnchorShown(pageId: string, blockId: string) {
  shownByPage = `${pageId}:${blockId}`;
}

// Where a linked block comes to rest when a link on its own page is pressed:
// its top this far under the top of the page's scroll, as far as the page
// scrolls (seen 2026-11-11, 635 high: 64).
const LINKED_BLOCK_TOP = 64;

/**
 * Where a page opened at a block puts it — at its address, from a search
 * result: where the page's first block stands with the page at its top, as
 * far as the page scrolls, at once. Notion's scrolls by the blocks above it
 * to put it there, each counted at its list's 32px estimate, so its block
 * lands lower by that estimate's error, which its list partly takes back as
 * it measures the rows it draws (seen 2026-11-11: lines 2, 3, 4 at 184, 192,
 * 200 under a first block at 176, deeper lines at 216 whether the window was
 * 433, 635 or 799 high; a cover page's at 311, 424, 453 under 255, 344, 365
 * as the window grew; tall paragraphs' 208, headings' 239).
 */
function openedBlockTop(scroller: HTMLElement) {
  const first = scroller.querySelector<HTMLElement>("[id^='block-']");
  if (!first) return LINKED_BLOCK_TOP;
  return first.getBoundingClientRect().top - scroller.getBoundingClientRect().top + scroller.scrollTop;
}

/**
 * Scrolls a linked block into view and chooses it, as Notion's leaves a block
 * a link (a reminder's, a block link) opened at chosen until the reader moves
 * on (seen 2026-11-06: its selection halo stayed); one no editor takes is
 * flashed instead. `opened`: its page was opened at it.
 */
export function revealLinkedBlock(target: HTMLElement, opened = false) {
  const scroller = pageScrollOf(target), behavior = opened ? "auto" : motionSafeScrollBehavior();
  if (scroller) {
    const rest = opened ? openedBlockTop(scroller) : LINKED_BLOCK_TOP;
    const top = scroller.scrollTop + target.getBoundingClientRect().top - scroller.getBoundingClientRect().top - rest;
    scroller.scrollTo({ top: Math.max(0, top), behavior });
  } else target.scrollIntoView({ behavior, block: "start", inline: "nearest" });
  const blockId = target.id.replace(/^block-/u, "");
  if (blockId && requestEditorBlockSelection(blockId, false, target)) return;
  target.dataset.commentFlash = "true";
  window.setTimeout(() => delete target.dataset.commentFlash, 1400);
}

/**
 * Opens the closed toggles a block sits in, so a link to it can show it, as
 * Notion's opens them (seen 2026-11-11: a mention of a line in a closed
 * toggle opened the toggle); false when it sits in none.
 */
export function openTogglesAround(
  blocks: readonly GraphBlock[], blockId: string, open: (toggle: GraphBlock) => void,
) {
  const byId = new Map(blocks.map((block) => [block.id, block]));
  let opened = false;
  const seen = new Set<string>();
  for (let parentId = byId.get(blockId)?.parentId; parentId && !seen.has(parentId); parentId = byId.get(parentId)?.parentId) {
    seen.add(parentId);
    const parent = byId.get(parentId);
    if (parent && TOGGLE_BLOCK_TYPES.has(parent.type as never) && parent.content.collapsed === true) {
      open(parent);
      opened = true;
    }
  }
  return opened;
}

// How long a link waits for a block its closed toggles hid to be drawn.
const OPENED_BLOCK_WAIT_MS = 1500;

/**
 * Opening a page at a block link (`#block-<id>`) — its address, a search
 * result — scrolls to that block and chooses it once it has rendered, as
 * Notion's "Copy link to block" links do; a block in closed toggles is shown
 * by opening them (unfold). A later change of the address to another block
 * link, a link on the page pressed, scrolls again. A link at the page itself
 * — its own ID, or a block its body is (`pageItself`: a full-page database's,
 * a database opened alone) — opens the page as it is, nothing chosen, as
 * Notion's does (blockAddress.ts), an address made so before too.
 */
export function useBlockAnchorScroll(
  pageId: string, blockCount: number, enabled = true, unfold?: (blockId: string) => boolean,
  pageItself?: (blockId: string) => boolean,
) {
  const done = useRef("");
  const latestUnfold = useRef(unfold);
  latestUnfold.current = unfold;
  const latestItself = useRef(pageItself);
  latestItself.current = pageItself;
  useEffect(() => {
    if (typeof window === "undefined" || !enabled) return;
    // The block the page was opened at, as against one a link on it goes to.
    const openedAt = blockLinkTarget(window.location.hash);
    let retry = 0;
    function reveal(since?: number) {
      const linked = blockLinkTarget(window.location.hash);
      if (!linked || blockIsPageItself(pageId, linked) || latestItself.current?.(linked)) return;
      const key = `${pageId}:${linked}`;
      if (done.current === key) return;
      if (shownByPage === key) { done.current = key; return; }
      const target = document.getElementById(`block-${linked}`);
      // A block a closed toggle holds is there but not shown.
      if (!target || target.getClientRects().length === 0) {
        // Its toggles open, it is shown a moment later.
        const waiting = since !== undefined && Date.now() - since < OPENED_BLOCK_WAIT_MS;
        if (waiting || since === undefined && latestUnfold.current?.(linked)) {
          window.clearTimeout(retry);
          retry = window.setTimeout(() => reveal(since ?? Date.now()), 50);
        }
        return;
      }
      // A page opened at the block's address: the browser keeps its own
      // scroll to the block (its top at the top) until the page has loaded
      // and drawn once more; the block's place comes after it.
      if (document.readyState !== "complete") {
        window.addEventListener("load", afterLoad, { once: true });
        return;
      }
      done.current = key;
      revealLinkedBlock(target, linked === openedAt);
    }
    const follow = () => reveal();
    let loaded = 0;
    const afterLoad = () => { loaded = window.requestAnimationFrame(() => { loaded = window.requestAnimationFrame(follow); }); };
    const frame = window.requestAnimationFrame(follow);
    window.addEventListener("hashchange", follow);
    // A long page draws its blocks from the top over a few frames; a block
    // further down is there once they all are.
    window.addEventListener(NODE_VIEWS_DRAWN_EVENT, follow);
    return () => {
      window.cancelAnimationFrame(frame);
      window.clearTimeout(retry);
      window.removeEventListener("hashchange", follow);
      window.cancelAnimationFrame(loaded);
      window.removeEventListener("load", afterLoad);
      window.removeEventListener(NODE_VIEWS_DRAWN_EVENT, follow);
    };
  }, [blockCount, enabled, pageId]);
}
