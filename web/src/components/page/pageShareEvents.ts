import { useReadAgainAfterUncertainWrites } from "@/lib/uncertainWrites";
import { useAccessRevision } from "@/lib/workspaceChangeCheck";

export const OPEN_PAGE_SHARE_EVENT = "leafen:open-page-share";

export type OpenPageShareDetail = {
  pageId: string;
  /** A name or email ready to be invited. */
  invite?: string;
  /** The tab it opens at: sharing, or "웹에 게시". */
  tab?: "share" | "publish";
  /** The control that asked, which the window gives the focus back to as it
   *  closes, where the top bar's "공유" takes it otherwise. */
  opener?: HTMLElement;
};

// A request made while the page is being opened — from settings, say — waits
// a moment for its top bar to appear.
const PENDING_SHARE_MS = 10_000;
let pending: { detail: OpenPageShareDetail; at: number } | null = null;
let asked: { pageId: string; opener?: HTMLElement; at: number } | null = null;

/** Opens the page's share menu, as its top bar's share button does, once the page shows. */
export function openPageShare(detail: OpenPageShareDetail) {
  pending = { detail, at: Date.now() };
  asked = { pageId: detail.pageId, opener: detail.opener, at: pending.at };
  window.dispatchEvent(new CustomEvent<OpenPageShareDetail>(OPEN_PAGE_SHARE_EVENT, { detail }));
}

/**
 * The control a share request for this page came from, taken once by the
 * window it opens: Notion's window opened from its published page's "사이트
 * 설정" gives the ringed focus back to that button at Escape (seen 2026-11-16
 * in Chrome at 1440, no script before the key), as one opened from "공유"
 * gives it to "공유".
 */
export function takePageShareOpener(pageId: string): HTMLElement | null {
  const opener = asked?.pageId === pageId && asked.opener?.isConnected && Date.now() - asked.at < PENDING_SHARE_MS ? asked.opener : null;
  asked = null;
  return opener;
}

/** The share request waiting for this page, taken once. */
export function takePendingPageShare(pageId: string): OpenPageShareDetail | null {
  if (!pending || pending.detail.pageId !== pageId) return null;
  const { detail, at } = pending;
  pending = null;
  return Date.now() - at < PENDING_SHARE_MS ? detail : null;
}

/** Said when a page's public state changes, so its banner reads it again. */
export const PUBLIC_CONTENT_CHANGED_EVENT = "leafen:public-content-changed";

/**
 * Moves each time an open share window reads the page's sharing again: once
 * the server answers after a change sent without its answer, and when the
 * person's own access may have changed elsewhere — an owner raising or
 * lowering them while the window is open — which the window follows as the
 * page's bar does, without being closed and opened again.
 */
export function useShareReadAgain() {
  return useReadAgainAfterUncertainWrites() + useAccessRevision();
}
