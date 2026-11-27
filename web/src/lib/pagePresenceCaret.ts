import { publishPageAwareness } from "./pagePresenceController";

const AWAY_AFTER_MS = 20_000;

/** The line a caret is on, or the table or callout it is in: they show one place. */
function presenceBlockOf(node: Node | null | undefined) {
  const element = node instanceof Element ? node : node?.parentElement;
  const block = element?.closest('[data-block-type="table"], [data-block-type="callout"]') ?? element?.closest("[data-block-id]");
  return block?.getAttribute("data-block-id") ?? null;
}

/**
 * Tells the page's room which line this person's caret is on, as Notion's
 * (seen 2026-11-05): the line and nothing finer, so typing sends nothing. A
 * block chosen with Escape keeps the line; a field elsewhere, a database cell
 * among them, takes the person off the page's lines; so does being away from
 * the window for 20 seconds, until they come back: the window's focus, or a
 * press or key in the page where no focus came (a window another program
 * drives, as the s239 audit's Chrome did, pressed without being brought up).
 */
export function trackPagePresenceCaret(pageId: string, root: HTMLElement) {
  let place: string | null = null;
  let sent: string | null = null;
  let away = false;
  let awayTimer: number | undefined;

  function send() {
    const blockId = away ? null : place;
    if (blockId === sent) return;
    sent = blockId;
    publishPageAwareness({ pageId, mode: blockId ? "editing" : "idle", blockId, selectedBlockIds: [] });
  }

  function look() {
    const active = document.activeElement;
    // Nothing has the focus when Escape chose the block or a press landed on nothing.
    if (!active || active === document.body || active === root) return;
    // A database's cells are its own fields, even where they are editors.
    const editing = root.contains(active) && active.classList.contains("ProseMirror")
      && !active.closest('[data-block-type="inline_database"]');
    place = editing ? presenceBlockOf(document.getSelection()?.anchorNode) ?? place : null;
    send();
  }

  function left() {
    window.clearTimeout(awayTimer);
    awayTimer = window.setTimeout(() => {
      awayTimer = undefined;
      away = true;
      send();
    }, AWAY_AFTER_MS);
  }

  function back() {
    window.clearTimeout(awayTimer);
    awayTimer = undefined;
    away = false;
    look();
    send();
  }

  // Only while away or about to be: a key's press otherwise costs nothing.
  function pressed() {
    if (away || awayTimer !== undefined) back();
  }

  function visibility() {
    if (document.visibilityState === "hidden") left();
    else if (document.hasFocus()) back();
  }

  document.addEventListener("selectionchange", look);
  document.addEventListener("focusin", look);
  document.addEventListener("visibilitychange", visibility);
  root.addEventListener("pointerdown", pressed, true);
  root.addEventListener("keydown", pressed, true);
  window.addEventListener("blur", left);
  window.addEventListener("focus", back);
  return () => {
    document.removeEventListener("selectionchange", look);
    document.removeEventListener("focusin", look);
    document.removeEventListener("visibilitychange", visibility);
    root.removeEventListener("pointerdown", pressed, true);
    root.removeEventListener("keydown", pressed, true);
    window.removeEventListener("blur", left);
    window.removeEventListener("focus", back);
    window.clearTimeout(awayTimer);
    place = null;
    away = false;
    send();
  };
}
