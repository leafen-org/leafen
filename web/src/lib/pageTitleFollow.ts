import type { TextSpan } from "@/lib/types";
import { mergeTitleValues, sameTitle, type TitleValue } from "./titleMerge";

/** A title the server held, with its spans where it has any, and the page's cursor when it held it. */
export type PageTitleBase = { title: string; cursor: number; titleSpans?: TextSpan[] };

function titleOf(page: PageTitleBase): TitleValue {
  return page.titleSpans?.length ? { title: page.title, spans: page.titleSpans } : { title: page.title };
}

function baseOf(page: { title: string; cursor: number; titleSpans?: TextSpan[] }): PageTitleBase {
  return { title: page.title, cursor: page.cursor, ...(page.titleSpans?.length ? { titleSpans: page.titleSpans } : {}) };
}

// The titles the server answered with, by page. The pages the app holds
// show titles being typed and changes not yet sent at the server's cursor,
// so an open title field learns what the server holds from here alone.
const serverTitles = new Map<string, PageTitleBase>();
const listeners = new Map<string, Set<(title: PageTitleBase) => void>>();

/** Pages as the server answered them: a list read again, or a change it made. */
export function notePageTitlesFromServer(pages: readonly { id: string; title: string; cursor: number; titleSpans?: TextSpan[] }[]) {
  for (const page of pages) {
    const known = serverTitles.get(page.id);
    if (known && known.cursor >= page.cursor) continue;
    const title = baseOf(page);
    serverTitles.set(page.id, title);
    for (const listener of listeners.get(page.id) ?? []) listener(title);
  }
}

/** The page's title as the server last answered it, if it has. */
export function serverPageTitle(pageId: string): PageTitleBase | undefined {
  return serverTitles.get(pageId);
}

/** Hears each newer title the server answers for the page. */
export function followServerPageTitle(pageId: string, listener: (title: PageTitleBase) => void): () => void {
  const set = listeners.get(pageId) ?? new Set();
  set.add(listener);
  listeners.set(pageId, set);
  return () => {
    set.delete(listener);
    if (!set.size) listeners.delete(pageId);
  };
}

/** The open title field the follow keeps. */
type TitleField = {
  /** The field's title as it stands, typed and not yet saved too, with its mentions. */
  read(): TitleValue;
  /** The field is mid-composition (Korean, Japanese …), which nothing may touch. */
  composing(): boolean;
  focused(): boolean;
  /** Puts a title from elsewhere in the field, the caret keeping its place. */
  show(value: TitleValue): void;
  /** The field holds typing not saved yet: it is saved as typing is. */
  unsaved(value: TitleValue): void;
};

// A title the field takes: the server's, with the title this field's text
// was typed on when it answers this field's save (`made`); `page` null puts
// back a save the server refused.
type Waiting = { page: PageTitleBase | null; made?: TitleValue };

/**
 * An open page title follows the page's title, as the body's text follows
 * the page's room: a title saved elsewhere — this person's other window,
 * another person — shows in the field at once, caret in it or not, merged
 * with what is typed here and not yet saved (mergeTitleValues: a mention
 * and formatting go with their words), so neither is lost (2026-11-10: a field holding the caret kept the old title and
 * wrote it back when it was left). A save says the title it was typed on,
 * so a newer title on the server is merged with it rather than replaced
 * (sendKeptPageMetadata). It is saved as typed, the spaces at its ends too
 * (titleMerge.ts).
 */
export function createPageTitleFollow(initial: PageTitleBase, field: TitleField) {
  // The server's title the field's text is made on.
  let base = initial;
  // The last title sent or taken: a save sends only a change from it.
  let last = titleOf(initial);
  // The titles on their way, oldest first.
  const sending: TitleValue[] = [];
  // What came while the field couldn't take it, in order.
  const waiting: Waiting[] = [];

  function take({ page, made }: Waiting) {
    if (page && made === undefined && page.cursor <= base.cursor) return;
    const server = page ?? base;
    const value = field.read();
    const merged = mergeTitleValues(made ?? titleOf(base), value, titleOf(server));
    if (server.cursor > base.cursor) base = server;
    if (!sending.length) last = titleOf(server);
    if (!sameTitle(merged, value)) field.show(merged);
    if (!sameTitle(merged, titleOf(server)) && !sending.length) field.unsaved(merged);
  }

  function drain() {
    while (waiting.length && !field.composing()) {
      // Another's title waits for this field's saves to be answered: until
      // then it can't be told which of this field's typing it holds.
      if (waiting[0].made === undefined && sending.length) return;
      take(waiting.shift()!);
    }
  }

  return {
    /** What a save sends now — the title, its spans and the title it was typed on — or null with nothing new. */
    next(): { value: TitleValue; base: PageTitleBase } | null {
      if (field.composing()) return null;
      const value = field.read();
      if (sameTitle(value, last)) return null;
      const typedOn = sending[sending.length - 1] ?? titleOf(base);
      const made = { title: typedOn.title, cursor: base.cursor, ...(typedOn.spans?.length ? { titleSpans: [...typedOn.spans] } : {}) };
      last = value;
      sending.push(value);
      return { value, base: made };
    },
    /** A save on its way, answered with the page as the server keeps it, a kept change, or false when refused. */
    sent(value: TitleValue, answer: unknown) {
      void Promise.resolve(answer).then((saved) => saved, () => false).then((saved) => {
        const at = sending.indexOf(value);
        if (at >= 0) sending.splice(at, 1);
        const page = saved && typeof saved === "object" ? saved as Partial<PageTitleBase> : null;
        if (saved === false) waiting.push({ page: null, made: value });
        else if (page && typeof page.title === "string" && typeof page.cursor === "number") {
          const answered = baseOf({ title: page.title, cursor: page.cursor, titleSpans: page.titleSpans });
          // The server's titles before its answer are in the answer.
          for (let index = waiting.length - 1; index >= 0; index -= 1) {
            const item = waiting[index];
            if (item.made === undefined && item.page && item.page.cursor <= answered.cursor) waiting.splice(index, 1);
          }
          waiting.push({ page: answered, made: value });
        }
        drain();
      });
    },
    /** A newer title the server answered for the page. */
    heard(page: PageTitleBase) {
      if (page.cursor <= base.cursor) return;
      const held = waiting.length ? waiting[waiting.length - 1] : undefined;
      if (held && held.made === undefined && held.page) {
        if (held.page.cursor < page.cursor) held.page = page;
      } else waiting.push({ page });
      drain();
    },
    /**
     * The page as the app shows it: this person's own change made elsewhere
     * on the screen, before the server has it, shows in a field nobody is
     * writing in.
     */
    shown(page: PageTitleBase) {
      const value = field.read(), shown = titleOf(page);
      if (sameTitle(shown, value) || page.cursor !== base.cursor || sending.length || field.focused() || !sameTitle(value, last)) return;
      field.show(shown);
      last = shown;
    },
    /** The field can take what waited (a composition ended). */
    resume: drain,
    /** A field that only shows the title shows the page's. */
    reset(page: PageTitleBase) {
      base = page; last = titleOf(page); sending.length = 0; waiting.length = 0;
      if (!sameTitle(field.read(), last)) field.show(last);
    },
  };
}
