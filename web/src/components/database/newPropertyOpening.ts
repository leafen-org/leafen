// Where Notion's new property sends the keys depends on what opened before it
// since the document loaded (seen 2026-11-17 in Chrome, two tabs, one letter
// typed after each opening): the first window of a database page's or a
// layout's "속성 추가" (NewPropertyMenu) gives them to the types' search,
// every later one to the name, after moving to another page in the app too.
// The view settings' "+ 새 속성" (DatabaseNewPropertyScreen) opened first takes
// that turn, though its own name takes the keys; the table's "+" does not
// (seen once).
let opened = false;

/** Whether no new property has opened since the document loaded. */
export function firstNewPropertyOpening() {
  return !opened;
}

/** Marks a new property window or screen as opened, from its first effect. */
export function markNewPropertyOpened() {
  opened = true;
}
