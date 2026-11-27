import { mergeTitleValues, sameTitle, type TitleValue } from "./titleMerge";

/*
 * The one rule a typed title goes to the server by — a page's, a wiki's
 * page's, a database row's: written over the value it was typed on (`base`),
 * and, the server holding another value since (the page's cursor moved, the
 * row's value changed), merged with the value the server holds now and
 * written again over that one, so what was typed here and what was saved
 * elsewhere meanwhile — this person's other window, another person, an agent
 * — both stay (titleMerge.ts; 2026-11-10: a window's title typed on the old
 * one replaced another's typing; 2026-11-11: two saves 20 ms apart both read
 * the same title, and the later wrote over the other).
 */

// How many times a title meets one saved in between before it gives up (or writes over it, `overLast`).
const CHANGED_TRIES = 4;

export async function sendTitleOverBase<Now, Result>(options: {
  /** The value the typing was made on, as the server held it. */
  base: TitleValue;
  typed: TitleValue;
  /**
   * Writes `title` held to what the server holds: the base the typing began
   * on (`now` undefined), the value just read (`now`), or nothing (`now`
   * null: the read failed, or the last try of a write that may go over what
   * is there).
   */
  write(title: TitleValue, now: Now | null | undefined): Promise<Result>;
  /** What the server holds now, or null when it can't be read. */
  readNow(): Promise<Now | null>;
  titleOf(now: Now): TitleValue;
  /** Whether a refusal says the server's value moved on since the one written over. */
  changed(error: unknown): boolean;
  /** The last try writes over whatever is there (a row's value); without, its refusal stands. */
  overLast?: boolean;
}): Promise<Result> {
  const { base, typed } = options;
  try {
    return await options.write(typed, undefined);
  } catch (error) {
    if (!options.changed(error)) throw error;
  }
  for (let tries = 2; ; tries += 1) {
    const now = await options.readNow();
    const held = now === null ? null : options.titleOf(now);
    const title = held && !sameTitle(held, base) ? mergeTitleValues(base, typed, held) : typed;
    try {
      return await options.write(title, options.overLast && tries >= CHANGED_TRIES ? null : now);
    } catch (error) {
      if (!options.changed(error) || tries >= CHANGED_TRIES) throw error;
    }
  }
}
