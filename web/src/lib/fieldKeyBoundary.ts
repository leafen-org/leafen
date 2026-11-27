/**
 * Where a key, an edit or a field meets an input method's composition — the
 * one place. Korean, Japanese, Chinese, Vietnamese, a Mac's ⌥ accents and a
 * phone keyboard's words are typed through a composition: the letters being
 * composed are the input method's until it ends them, and so is every key
 * it takes meanwhile (keyCode 229, "Process", isComposing).
 *
 * Browsers end a composition differently, and a key that writes a field's
 * value — sends it, confirms it, moves to another cell, closes it with Esc —
 * must find what was typed already in the field's value whichever way it
 * ended. macOS Chrome's Korean (recorded 2026-11-19 on a person's machine)
 * ends each syllable and starts the next in one key press
 * (compositionend, then compositionstart in the same task), and sends an
 * Enter typed while composing as keydown 229, compositionend and keydown 13
 * together; Safari and the Mac app (WebKit) end a composition before the key
 * that ended it comes: compositionend, then its keydown not composing — the
 * key's own code where the input method passed it on (the Korean input
 * method's Enter in marked-text mode, 13, recorded 2026-11-21 in the app's
 * WebKit), 229 where it kept it; Windows sends
 * every key the input method takes as 229 "Process" first. A field that
 * published its words a frame after a composition ended sent a comment with
 * the words up to its last pause: "내가 눈지아니" sent as "내가".
 *
 * So:
 *  - `imeComposing` is the composition check: a key event, an input event
 *    or an editor's view (`imeStarting` and `imeContinuing` part the input
 *    method's first key from those after it, where a key starts typing);
 *  - `fieldKey` is a key's way into a field: while composing it is the
 *    input method's ("ime", nothing done), otherwise a key that ends, sends,
 *    confirms or moves (Enter, Escape, Tab) has the field's words published
 *    at once — not a frame later — before its meaning comes back;
 *  - `compositionEnds` publishes what a composition finished before the next
 *    one can start, only the syllable still being composed waiting;
 *  - a key that ended a composition and is not the input method's goes to
 *    an editor's keys in WebKit as in Chrome, past ProseMirror's own guard
 *    for Safari (components/editor/compositionEndKey.ts).
 *
 * web/scripts/check-composition-keys.mjs keeps every composition check and
 * every Enter of web/src going through here.
 */

/** A key event (DOM or React's), an input event or an editor's view (ProseMirror's `composing`). */
export type CompositionSource = {
  key?: string;
  keyCode?: number;
  isComposing?: boolean;
  nativeEvent?: { isComposing?: boolean; keyCode?: number; key?: string };
  composing?: boolean;
} | null | undefined;

type SourceFields = NonNullable<CompositionSource>;

/**
 * A field of the source, read only when the source has it. An editor's view
 * not mounted yet, or taken down (a title's field gone while its save was
 * answered), is tiptap's stand-in: it mimics `composing` (false) and throws
 * on any other field it is asked for, an event's among them. Asked with
 * `in`, it answers without throwing, so such a view is "not composing".
 */
function field<K extends keyof SourceFields>(source: SourceFields, key: K): SourceFields[K] {
  return key in source ? source[key] : undefined;
}

/** Whether the event or editor is the input method's now: a composition runs, or the input method took the key. */
export function imeComposing(source: CompositionSource): boolean {
  if (!source) return false;
  const native = field(source, "nativeEvent");
  return Boolean(field(source, "composing") || field(source, "isComposing") || native?.isComposing
    || field(source, "key") === "Process" || native?.key === "Process"
    || field(source, "keyCode") === 229 || native?.keyCode === 229);
}

/**
 * The input method's first key: one it takes (229, "Process") with no
 * composition running yet — typing begins, as a held cell starts editing at it.
 */
export function imeStarting(source: CompositionSource): boolean {
  if (!source || !imeComposing(source)) return false;
  return !field(source, "composing") && !field(source, "isComposing") && !field(source, "nativeEvent")?.isComposing;
}

/**
 * A composition runs already: the key or input goes on with it. Where a key
 * starts typing — into a chosen cell, into what typing is held for — the
 * input method's first key types as a letter does, and only the keys after
 * it are the composition's.
 */
export function imeContinuing(source: CompositionSource): boolean {
  return imeComposing(source) && !imeStarting(source);
}

/** What Enter does in a field. */
export type EnterMeaning = "send" | "newline" | "confirm";

/**
 * A key's meaning to a field once it passes the boundary: "ime" while it is
 * the input method's; Enter's as the field gives it; "escape" and "tab";
 * "other" for every other key (letters, arrows, shortcuts), which publish nothing.
 */
export type FieldKeyMeaning = "ime" | EnterMeaning | "escape" | "tab" | "other";

type KeyLike = NonNullable<CompositionSource> & { key: string };

/**
 * A key reaching a field. While an input method composes, the key is its
 * ("ime"): the field does nothing with it. Otherwise a key that writes the
 * field's value — Enter, Escape, Tab — first has the words the field holds
 * published now (`publish`: the field's own, as a rich field's engine
 * publishes them), and then its meaning comes back; Enter's is `enter`
 * ("confirm" unless given). A field whose value is read from its element
 * (an input's `value`) has nothing to publish.
 */
export function fieldKey(event: KeyLike, field: {
  publish?: () => unknown;
  enter?: EnterMeaning | ((event: KeyLike) => EnterMeaning);
} = {}): FieldKeyMeaning {
  if (imeComposing(event)) return "ime";
  const key = event.key;
  if (key !== "Enter" && key !== "Escape" && key !== "Tab") return "other";
  field.publish?.();
  if (key === "Escape") return "escape";
  if (key === "Tab") return "tab";
  return typeof field.enter === "function" ? field.enter(event) : field.enter ?? "confirm";
}

/** Enter that is the field's (not the input method's), its words published first: `fieldKey` for Enter. */
export function fieldEnter(event: KeyLike, publish?: () => unknown): boolean {
  return event.key === "Enter" && fieldKey(event, { publish }) !== "ime";
}

/** Escape that is the field's, not the input method's (whose Escape drops what it composes). */
export function fieldEscape(event: KeyLike, publish?: () => unknown): boolean {
  return event.key === "Escape" && fieldKey(event, { publish }) === "escape";
}

/**
 * An editor field's compositionend: `ended` runs once the editor has taken
 * the composition's end (its own compositionend handler runs after the
 * field's and reads the last of the composed words in a microtask), and
 * before a next composition can start — macOS Chrome's Korean starts it in
 * the same task — and once more a frame later, for a browser whose last
 * input of the composition comes after its end (Safari). `ended` runs only
 * while no composition runs; what it publishes is the field's to compare.
 */
export function compositionEnds(view: () => { composing: boolean } | null, ended: () => void) {
  // A run asked for before `stop` is called off; a later end asks again.
  let frame = 0, asked = 0;
  const run = (ask: number) => {
    const current = view();
    if (ask === asked && current && !imeComposing(current)) ended();
  };
  return {
    ended() {
      const ask = ++asked;
      queueMicrotask(() => queueMicrotask(() => run(ask)));
      if (frame) cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => { frame = 0; run(ask); });
    },
    /** Calls off what an end asked for (its field taken down, its editor's view gone). */
    stop() {
      asked += 1;
      if (frame) cancelAnimationFrame(frame);
      frame = 0;
    },
  };
}
