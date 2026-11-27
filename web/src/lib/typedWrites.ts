import { afterNextPaint } from "@/lib/afterPress";
import { pageLeaving } from "@/lib/pageLeaving";

// A value typed into a field — a database cell's text, People's About — is
// one edit, saved once: its writes are held while the typing goes on and go
// together once it pauses for TYPING_PAUSE_MS, as one change on the server
// that sets off a database's automations once, as Notion's does (seen
// 2026-11-15: "Design" typed into a People row's property ran its automation
// once; here it ran six times, a save for each letter, and an ordinary
// table's text cell the same). The value shows as it is typed; only its save
// waits. What is held goes at once when the typing ends otherwise: the field
// left (focus moving away; Enter, Escape or Tab), ⌘Z or ⌘⇧Z pressed, or the
// page hidden or going — and any other change the same place sends takes it
// along first, so changes reach the server in the order made.

/** How long typing pauses before what it wrote is saved. */
export const TYPING_PAUSE_MS = 300;

const holding = new Set<() => void>();
let listening = false;

/**
 * Holds a typed write's sending: `send` runs once the typing ends otherwise
 * than by pausing (its holder sends it after the pause itself). What it gives
 * lets the write go from here, sent or given up.
 */
export function holdUntilTypingEnds(send: () => void): () => void {
  holding.add(send);
  listen();
  // Held as the page hides or goes — a field saving what was typed then —
  // it goes at once, once its holder holds it: what sends the held writes
  // heard the page go already (2026-11-27: a row page's title, taken back
  // with ⌘Z and saved as its tab closed, was held for good).
  if (pageLeaving() || document.hidden) queueMicrotask(() => { if (holding.delete(send)) send(); });
  return () => { holding.delete(send); };
}

// A field saved as it closes whose page going closes it (typedName.ts): sent as the page goes, and not before.
const untilPageGoes = new Set<() => void>();

/** Holds a field's saving until the page goes (pagehide), as one closed then; what it gives lets it go. */
export function holdUntilPageGoes(send: () => void): () => void {
  untilPageGoes.add(send);
  listen();
  return () => { untilPageGoes.delete(send); };
}

type HeldWrite = { write: () => Promise<boolean>; answers: Array<(saved: boolean) => void>; timer: number; release: () => void };

/**
 * Typed writes of a place that saves each field on its own (People's About):
 * a field's write is held until the typing pauses or ends, and only its
 * latest goes, once. Each write held answers whether that save was taken,
 * the latest first, so a refused burst unwinds back to what was there before
 * it. `now` sends a field's held write at once, the one just given with it
 * (a step of ⌘Z, a value pasted) standing for what was typed.
 */
export function heldTypedWrites() {
  const fields = new Map<string, HeldWrite>();
  const send = (field: string) => {
    const held = fields.get(field);
    if (!held) return;
    fields.delete(field);
    window.clearTimeout(held.timer);
    held.release();
    void held.write().catch(() => false).then((saved) => {
      for (const answer of [...held.answers].reverse()) answer(saved);
    });
  };
  const hold = (field: string, write: () => Promise<boolean>) => new Promise<boolean>((answer) => {
    const held = fields.get(field) ?? { write, answers: [], timer: 0, release: holdUntilTypingEnds(() => send(field)) };
    window.clearTimeout(held.timer);
    held.write = write;
    held.answers.push(answer);
    held.timer = window.setTimeout(() => send(field), TYPING_PAUSE_MS);
    fields.set(field, held);
  });
  return { hold, now: send };
}

function sendHeld() {
  for (const send of [...holding]) {
    holding.delete(send);
    send();
  }
}

// A key that ends the typing, or takes a step of the page's history, sends
// what was held after its own frame: the press shows first, and a change the
// key itself sends meanwhile takes the held writes ahead of it.
const ENDING_KEYS = new Set(["Enter", "Escape", "Tab"]);
/** Whether a key ends the typing: Enter, Escape or Tab, or a step of the page's history (⌘Z, ⌘⇧Z, ⌘Y). */
export function endsTyping(event: KeyboardEvent) {
  if (ENDING_KEYS.has(event.key)) return true;
  const key = event.key.toLowerCase();
  return (event.metaKey || event.ctrlKey) && (key === "z" || key === "y");
}

function listen() {
  if (listening || typeof window === "undefined") return;
  listening = true;
  let sending: (() => void) | null = null;
  const soon = () => {
    if (!holding.size || sending) return;
    sending = afterNextPaint(() => {
      sending = null;
      sendHeld();
    });
  };
  window.addEventListener("keydown", (event) => { if (holding.size && endsTyping(event)) soon(); }, { capture: true, passive: true });
  window.addEventListener("focusout", soon, { capture: true, passive: true });
  // Hidden or going, the page sends at once: it may not paint again.
  document.addEventListener("visibilitychange", () => { if (document.hidden) sendHeld(); });
  window.addEventListener("pagehide", () => {
    sendHeld();
    for (const send of [...untilPageGoes]) {
      untilPageGoes.delete(send);
      send();
    }
  });
}
