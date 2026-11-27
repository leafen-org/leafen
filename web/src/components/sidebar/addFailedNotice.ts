import { i18next } from "@/i18n";
import { isServerUnreachable } from "@/lib/serverReach";

/**
 * What a "+" that adds a page or a row inside another — a sidebar row's, a
 * favorite's, a library row's — says when it couldn't: with the server out
 * of reach (isServerUnreachable), that it needs the server, as a note, not a
 * failure; else its own refusal (`refused`: no access, the change not
 * saved). What it adds is made on this device and kept to go whenever it
 * can be — a page, a row of a database this device holds; only what needs
 * the server first can't be: a row of a database never opened on this
 * device, whose properties it doesn't hold (fullPageDatabase.ts
 * addDatabaseBlockRow), a page the server places (a wiki's). Told as no
 * access, an offline "+" on such a database read as a permission the person
 * lacked (2026-11-25).
 */
export function addFailedNotice(error: unknown, refused: string): readonly [message: string, tone: "default" | "error"] {
  return isServerUnreachable(error) ? [i18next.t("pageTreeItem:addOffline"), "default"] : [refused, "error"];
}
