import { useState, useSyncExternalStore } from "react";
import type { TextSpan } from "@/lib/types";

/**
 * The words being typed in a comment field, read where they are shown.
 * They live here rather than in what holds the field, so a letter typed
 * redraws the field alone: held in a panel's state, each letter redrew every
 * thread of the page (64–72 ms a letter on a page of 303 comments).
 */
export type CommentDraftSource = {
  get(): TextSpan[];
  subscribe(listener: () => void): () => void;
};

export type CommentDraftStore = CommentDraftSource & {
  set(next: TextSpan[]): void;
};

const noSpans: TextSpan[] = [];

export function createCommentDraftStore(initial: TextSpan[] = noSpans): CommentDraftStore {
  let value = initial;
  const listeners = new Set<() => void>();
  return {
    get: () => value,
    set(next) {
      if (next === value) return;
      value = next;
      for (const listener of [...listeners]) listener();
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => { listeners.delete(listener); };
    },
  };
}

/** A store of its own for the component's life. */
export function useCommentDraftStore(): CommentDraftStore {
  return useState(() => createCommentDraftStore())[0];
}

/** The words now in the draft; the component redraws as they change. */
export function useCommentDraft(source: CommentDraftSource): TextSpan[] {
  return useSyncExternalStore(source.subscribe, source.get, source.get);
}
