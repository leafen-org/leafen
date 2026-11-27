import type { TFunction } from "i18next";
import { i18next } from "@/i18n";

/**
 * A line the sign-in screen shows — its red line or its notice — held as
 * what to say (a key and its values, or the failure the server answered),
 * never as the words: they are made where the line is drawn, in the
 * screen's language then. So a line shown before the sign-in screen's
 * language menu changes the language reads in the new one (2026-11-23: an
 * ended invitation's line stayed Korean on the English screen). "" is no line.
 */
export type AuthLine = { readonly say: (t: TFunction) => string };
export type AuthText = AuthLine | "";

/** A line made by its own words picker, given the language's t where it is drawn. */
export function authLine(say: (t: TFunction) => string): AuthLine {
  return { say };
}

/** A line that is one catalog text (its namespace named), with its values. */
export function authWords(key: string, values?: Record<string, unknown>): AuthLine {
  return { say: (t) => String(t(key, values)) };
}

/** The line's words in the language now, "" for none. */
export function sayAuthLine(line: AuthText): string {
  return line ? line.say(i18next.t) : "";
}
