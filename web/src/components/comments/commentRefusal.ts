import i18next from "i18next";
import { writeRefusalText } from "@/lib/settingsErrors";

// commentReadGone holds for a read the server refused because the page is no
// longer the reader's: its screen turns to the card saying so, which a
// failure notice over it would contradict (Notion's goes to its card at once,
// seen 2026-11-07).
export function commentReadGone(error: unknown): boolean {
  const status = (error as { status?: unknown } | null)?.status;
  return status === 403 || status === 404;
}

// commentChangeRefusal says in the reader's words why the server refused a
// change to a discussion: not allowed, or the discussion changed meanwhile.
// A sensitive term the organization keeps out says so and what to do; any
// other failure reads as the change not made, or the server not reached.
export function commentChangeRefusal(error: unknown): string {
  const status = (error as { status?: unknown } | null)?.status;
  if (status === 403) return i18next.t("commentsPanel:errors.notAllowed");
  if (status === 409) return i18next.t("commentsPanel:errors.changed");
  return writeRefusalText(error, i18next.t("commentsPanel:errors.failed"));
}
