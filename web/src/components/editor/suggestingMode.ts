import { createContext, useContext } from "react";
import type { SuggestionCreateInput } from "@/commentWire.generated";
import type { PressMarked } from "@/lib/ownSuggestionEvents";
export type SuggestedReceipt = { id: string };

/** A suggested edit to a block's text, without the IDs the comment service gives it, and when it was asked for. */
export type SuggestedEdit = PressMarked<Omit<SuggestionCreateInput, "id" | "requestId">>;

/**
 * Suggesting mode on a page: while it is on, typing, deleting and formatting
 * in the page's text blocks suggest edits through the page's suggestion
 * route instead of changing the text, and the rest of the page stays as it
 * is.
 */
export type SuggestingMode = {
  active: boolean;
  suggest(edit: SuggestedEdit): Promise<SuggestedReceipt | void>;
  /** Turns suggesting on or off, where the person may suggest edits to the page. */
  onChange?(active: boolean): void;
  /** A database page shown in a peek, whose selection toolbar has no suggesting
   *  pencil — its "댓글" takes the room — as Notion's (seen 2026-11-11; the
   *  page's "…" menu and a page opened full keep it). */
  inPeek?: boolean;
};

const OFF: SuggestingMode = { active: false, suggest: async () => {} };

export const SuggestingModeContext = createContext<SuggestingMode>(OFF);

export function useSuggestingMode() {
  return useContext(SuggestingModeContext);
}

const SETTLED_BLOCK_SUGGESTION = "leafen:settled-block-suggestion";

/**
 * Tells a page's body that a suggestion which made blocks in it settled:
 * the blocks it kept or removed show once the body reads its blocks again.
 */
export function announceSettledBlockSuggestion(pageId: string) {
  window.dispatchEvent(new CustomEvent<string>(SETTLED_BLOCK_SUGGESTION, { detail: pageId }));
}

/** Takes a page body's settled block suggestions; returns the unsubscribe. */
export function onSettledBlockSuggestion(pageId: string, reread: () => void) {
  const listener = (event: Event) => {
    if ((event as CustomEvent<string>).detail === pageId) reread();
  };
  window.addEventListener(SETTLED_BLOCK_SUGGESTION, listener);
  return () => window.removeEventListener(SETTLED_BLOCK_SUGGESTION, listener);
}
