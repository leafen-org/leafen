import { api } from "@/api";
import { newId } from "@/lib/ids";
import type { Page, Session } from "@/types";

// A page the sidebar's "+" makes opens in the center peek; closed with
// nothing in it — no title, icon or cover and no more than empty lines, what
// was written in it undone too — it leaves nothing, not even in the trash, as
// Notion's (seen 2026-11-01: no page stayed under the row; 2026-11-13: a
// title typed and undone, a line typed and undone, an empty line, a
// database's row alike). Opening it as a full page keeps it.

const fresh = new Set<string>();
// The window each fresh page is open in, whose words decide at its close.
const windows = new Map<string, Element>();

/** Marks a page just made from the sidebar. */
export function markFreshPage(pageId: string) {
  fresh.add(pageId);
}

export function isFreshPage(pageId: string) {
  return fresh.has(pageId);
}

/** The person did something with the page: it stays. */
export function keepFreshPage(pageId: string) {
  fresh.delete(pageId);
}

/** Follows a fresh page's window, read as it closes. Answers the following's end. */
export function followFreshPageEdits(root: HTMLElement, pageId: string) {
  windows.set(pageId, root);
  return () => { if (windows.get(pageId) === root) windows.delete(pageId); };
}

const blankText = (element: Element | null | undefined) => !element?.textContent?.replace(/[\s​﻿]/g, "");

/**
 * Whether a fresh page's window shows nothing written: no words in its
 * title and nothing in its body but empty lines. Read from the window, where
 * words typed show before they are saved.
 */
export function freshPageBlank(root: Element | null | undefined) {
  if (!root || !blankText(root.querySelector("[data-page-title-editor], [data-row-page-title]"))) return false;
  return [...root.querySelectorAll<HTMLElement>("[data-native-block]")]
    .every((block) => block.dataset.kind === "paragraph" && blankText(block) && !block.querySelector("img, video, iframe"));
}

/** Lets a fresh page go when its peek closes with nothing in it: to the trash and out of it. */
export async function leaveFreshPage(session: Session, page: Page, trash: (page: Page) => Promise<unknown>) {
  const blank = freshPageBlank(windows.get(page.id));
  if (!fresh.delete(page.id) || !blank || page.icon || page.cover) return;
  await trash(page);
  await api.deletePagePermanently(session, page.id, { requestId: newId() }).catch(() => undefined);
}
