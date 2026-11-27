import { i18next } from "@/i18n";
import { pageNamesNow } from "@/lib/pageNames";
import type { Page } from "@/types";
import { confirmAction } from "./ConfirmActionDialog";

/**
 * Undoing a wiki whose pages are its database's pages takes the database
 * away, so it asks first, from whichever menu it is chosen, as Notion's (seen
 * 2026-11-11 and 11-13): the shared card in the window's middle, the menu
 * closed behind it — the page named in the question, what goes and that it
 * cannot be undone under it, a filled red "확인" over "취소". Escape, the
 * shade and "취소" leave the wiki as it is.
 */
export function confirmWikiUndo(page: Page): Promise<boolean> {
  return confirmAction({
    message: i18next.t("topBar:undoWikiQuestion", { title: pageNamesNow.title(page) }),
    detail: i18next.t("topBar:undoWikiConfirm"),
    confirmLabel: i18next.t("topBar:undoWikiOk"),
    cancelLabel: i18next.t("common:actions.cancel"),
    danger: "filled",
  });
}
