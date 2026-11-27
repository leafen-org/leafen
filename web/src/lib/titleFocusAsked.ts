// Pages just made here whose title's ask for the caret (requestPageTitleFocus,
// pageTitleFocus.ts) was answered: their title took the caret, or the person
// pressed elsewhere or in the body while the page was being made. What the
// person does after is theirs, so the caret an opened page takes
// (useEmptyPageTitleCaret) leaves the opening they were made in be: it came
// once the body had loaded, and on a page just made that could be after a
// paste into its title had chosen the pasted blocks, taking the caret back to
// the title over them (2026-11-11, a page made with the sidebar's "+" in a
// center peek). The mark is for that opening alone, which takes it
// (takeTitleFocusAsked) and lets it go as it ends: opened again, by the
// sidebar, back or a link, an untitled page takes the caret in its title as
// any (2026-11-22: a page made with ⌘N, left and opened again from the
// sidebar, took none, and a letter typed went nowhere).
const answered = new Set<string>();
// The pages made in one sitting are few; the oldest go past this many.
const KEPT = 200;

export function noteTitleFocusAsked(pageId: string) {
  answered.delete(pageId);
  answered.add(pageId);
  if (answered.size > KEPT) answered.delete(answered.values().next().value!);
}

/** Whether the page was just made here with its title's ask answered; the opening that asks takes the mark. */
export function takeTitleFocusAsked(pageId: string) {
  return answered.delete(pageId);
}

/** Lets the mark go as the opening it was for ends, taken or not (noted after the opening had read it). */
export function forgetTitleFocusAsked(pageId: string) {
  answered.delete(pageId);
}
