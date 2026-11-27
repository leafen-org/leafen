const OWN_SUGGESTION_EVENT = "leafen:own-suggestion";

/**
 * A suggestion the person made from here: the page or row it is on, its
 * discussion, and whether they pressed somewhere since it was asked for —
 * typed, or picked from a menu or the toolbar —, its card then staying as
 * that press left it (2026-11-22: a press away right after typing closed the
 * card, and the last letters' suggestion, answered after it, opened it
 * again), as a caret asked for gives way to a press meanwhile
 * (caretOnceAnswered).
 */
export type OwnSuggestion = { pageId: string; threadId: string; pressedSinceAsked: boolean };

/** A suggestion's edit with when it was asked for (pressMark), which the page's route doesn't take. */
export type PressMarked<T> = T & { askedAtPress?: number };

// The person's presses on the page, counted.
let presses = 0, counting = false;

/** The presses so far, to tell later whether one came since (an edit asked for now). */
export function pressMark(): number {
  if (!counting && typeof document !== "undefined") {
    counting = true;
    document.addEventListener("pointerdown", (event) => { if (event.isTrusted) presses += 1; }, { capture: true, passive: true });
  }
  return presses;
}

/**
 * Tells the page an edit the person suggested from here came back as a
 * discussion (CommentService.suggest), whose card opens beside its block
 * (MarginDiscussions); gives the comment back.
 */
export function announceOwnSuggestion<T extends { id: string; parentId?: string | null }>(pageId: string, comment: T, askedAtPress?: number): T {
  if (typeof window !== "undefined") {
    window.dispatchEvent(new CustomEvent<OwnSuggestion>(OWN_SUGGESTION_EVENT, {
      detail: { pageId, threadId: comment.parentId || comment.id, pressedSinceAsked: askedAtPress !== undefined && presses > askedAtPress },
    }));
  }
  return comment;
}

/** Takes the person's own suggestions as they come back; returns the unsubscribe. */
export function onOwnSuggestion(listener: (made: OwnSuggestion) => void) {
  pressMark();
  const take = (event: Event) => listener((event as CustomEvent<OwnSuggestion>).detail);
  window.addEventListener(OWN_SUGGESTION_EVENT, take);
  return () => window.removeEventListener(OWN_SUGGESTION_EVENT, take);
}
