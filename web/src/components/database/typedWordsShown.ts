// How many of a field's typed words are kept at most: one per key typed since its last save.
const KEPT = 512;

/**
 * The words a field has named its page by as they were typed, since it last
 * saved (richTextCellEngine `shown`): given back where the page is named,
 * each is the field's own typing, not a title saved elsewhere — at once or a
 * render behind the keys. Typed at once into a new row's title (held while
 * the row was made, pageTitleFocus.ts), the page named "One way ta" came back
 * while the field held "One way tas"; taken as a title saved elsewhere, it
 * became the title the typing was made on, and the row's title as the server
 * still held it, empty, read as those words taken away: the title kept "sk"
 * (2026-11-27, a template's view whose row opens in the template window's
 * place). None of them is the server's until it is saved.
 */
export function typedWordsShown() {
  const words = new Set<string>();
  return {
    /** The words the page is named by now, typed here and not saved. */
    note(value: string) {
      words.delete(value);
      words.add(value);
      if (words.size > KEPT) words.delete(words.values().next().value as string);
    },
    /** Whether these are words typed here and not saved since. */
    has(value: string) { return words.has(value); },
    /** Saved: what is typed next is noted afresh. */
    clear() { words.clear(); },
  };
}
