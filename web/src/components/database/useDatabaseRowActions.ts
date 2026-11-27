import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { ContentGraph, GraphRow } from "@/contentGraph";
import { copyText } from "@/lib/copyText";
import type { DatabaseRowPageService } from "@/lib/databaseRowPageService";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import { pageHref } from "@/lib/navigation";
import { isLegalHoldPermanentDeleteError } from "@/lib/nativeTrashLifecycle";
import { chooseStepRows } from "@/lib/stepRowChoice";
import { useOpenPageGraphStore } from "@/lib/pageGraphStore";
import { useProductToast } from "@/lib/productToast";
import { holdRowsShown, rowsHolderPage, rowsWithSubitems, withoutRows, withRowsArchived, withRowsAsDrawn, writeRowPage } from "@/lib/rowsShownAtPress";
import { sendRowTrash } from "@/lib/rowTrashKept";
import { serverUrl } from "@/lib/serverOrigin";
import type { ToastAction } from "@/lib/toastStackView";
import type { Session } from "@/types";
import { notePageChangeStep } from "../editor/pageUndo";
import { presentationBefore, type PagePresentationChange } from "../page/pagePresentation";
import { rowPagePresentationInput } from "./RowPageHeader";

/**
 * What a database view does to its rows' pages, from their menus and their
 * pages' top bars: archive, look, lock, trash, delete for good, link and
 * favorite. Each reads the views again where the row changed in them, and
 * says only what the views don't already show, as Notion's (seen 2026-10-31).
 */
export function useDatabaseRowActions({ session, service, reread, rowPageSnapshot, openedRowPage, rowPageOf, rowsTrashed, closeRow, pageId }: {
  session: Session | null;
  /** The page holding the rows' database, where their trash is kept for a server out of reach (rowTrashKept.ts). */
  pageId?: string | null;
  service: DatabaseRowPageService | null;
  reread: () => Promise<unknown> | undefined;
  /** A row page read, the opened row's own, and each row's last (useRowPageReads). */
  rowPageSnapshot: DatabaseRowPageSnapshot | null;
  openedRowPage?: DatabaseRowPageSnapshot | null;
  rowPageOf?: (rowId: string) => DatabaseRowPageSnapshot | null;
  rowsTrashed?: (rowIds: readonly string[], drawn?: readonly GraphRow[], letGo?: () => void) => (() => void) | void;
  closeRow: () => void;
}) {
  const { t } = useTranslation(["rowMenu", "pageTreeItem", "databaseView", "tableView", "pageArchive"]);
  const { notify, dismissMessage } = useProductToast();
  const [busy, setBusy] = useState(false);
  // The open page's graph, which the views draw: a row sent to the trash or
  // the archive leaves them at the press.
  const store = useOpenPageGraphStore();
  // Rows sent to the trash from here with their sub-items, as the views drew
  // them: the toast's "복원하기" puts them back at the press.
  const trashedHere = useRef(new Map<string, readonly GraphRow[]>());
  async function run(action: () => Promise<unknown>, success: string | null, failure: string | ((error: unknown) => string), undo?: ToastAction) {
    setBusy(true);
    try {
      await action();
      if (success) notify(success, "success", undo);
      return true;
    } catch (error) {
      notify(typeof failure === "string" ? failure : failure(error), "error");
      return false;
    } finally {
      setBusy(false);
    }
  }
  // Trash and archive show on the views at the press (`shown`, held over
  // every graph the page reads meanwhile); the server is written after and
  // the views read the page again. One the server refuses is taken back
  // (`back`) before that read, and the toast says so (user decision
  // 2026-11-12: a press answers on the next frame, the server is reconciled
  // last). The row's page, open beside the views, reads its own again.
  // What it says comes at the press too, as Notion's ("휴지통으로 이동됨 ·
  // 복원하기", seen 2026-11-13), and goes when the server refuses; what it
  // offers ("복원하기", "보관 취소") waits for the write and the read, whose
  // hold would undo it meanwhile. Kept for a server out of reach (the
  // trash, rowTrashKept.ts), it stays as shown, the outbox showing it so, and
  // what it offers comes at once; the page is read once the server has it.
  // `held` hears how to stop holding it shown early: a ⌘Z taking it back meanwhile.
  async function shownFirst(shown: (graph: ContentGraph) => ContentGraph, back: (graph: ContentGraph) => ContentGraph, rowIds: readonly string[],
    write: (session: Session, rowIds: readonly string[]) => Promise<"sent" | "kept">, success: string | null, failure: string, undo?: ToastAction,
    atOnce = false, held?: (release: () => void) => void) {
    const release = holdRowsShown(store, shown, back, atOnce);
    held?.(() => release());
    let written = false, kept = false, settle = () => {};
    const settled = new Promise<void>((resolve) => { settle = resolve; });
    if (success) notify(success, "success", undo && { ...undo, onClick: () => { void settled.then(undo.onClick); } });
    try {
      if (!session) throw new Error("Database row writes need a session.");
      kept = (await write(session, rowIds)) === "kept";
      written = true;
    } catch {
      release(true);
      if (success) dismissMessage(success);
      notify(failure, "error");
    }
    const reading = Promise.resolve(reread()).catch(() => undefined);
    if (kept) release("kept");
    else await reading;
    release();
    settle();
    const open = (openedRowPage ?? rowPageSnapshot)?.row.id;
    if (open && rowIds.includes(open)) void service?.refresh(open).catch(() => {});
    return written;
  }
  async function setArchived(rowId: string, archived: boolean): Promise<boolean> {
    const drawn = store?.get().rows.filter(({ id }) => id === rowId) ?? [];
    return shownFirst(withRowsArchived(new Set([rowId]), archived, session?.user.id), withRowsAsDrawn(drawn), [rowId],
      async (writer, ids) => {
        for (const id of ids) await writeRowPage(writer, id, archived ? "archive" : "unarchive");
        return "sent";
      },
      archived ? t("toast.archivedPage") : null, t(archived ? "toast.couldntArchivePage" : "toast.couldntUnarchivePage"),
      // "보관된 페이지 · 보관 취소", as Notion's.
      archived ? { label: t("pageArchive:unarchive"), onClick: () => { void setArchived(rowId, false); } } : undefined);
  }
  // An icon or cover is the row page's; the page and the views read it again.
  async function savePresentation(rowId: string, change: PagePresentationChange) {
    try {
      const holder = rowsHolderPage(store?.get(), store?.get().rows.filter(({ id }) => id === rowId) ?? [], pageId);
      const refreshed = await service?.setPresentation(rowId, rowPagePresentationInput(change), holder);
      if (refreshed) await reread();
      return Boolean(refreshed);
    } catch {
      notify(t("databaseView:toast.couldntSaveChanges"), "error");
      return false;
    }
  }
  async function changePresentation(rowId: string, change: PagePresentationChange) {
    const shown = (rowPageOf?.(rowId) ?? [rowPageSnapshot, openedRowPage].find((read) => read?.row.id === rowId))?.row ?? null;
    const before = shown ? presentationBefore(shown as PagePresentationChange, change) : null;
    const saved = await savePresentation(rowId, change);
    if (saved && before) notePageChangeStep(rowId, before, change, (step) => savePresentation(rowId, step));
    return saved;
  }
  // A locked row page keeps its values in the views too.
  async function changeLock(change: () => Promise<unknown> | undefined, failed: string) {
    try {
      if (!await change()) return;
      await reread();
    } catch {
      notify(failed, "error");
    }
  }
  // Trash hides the rows with their sub-items, selected rows together; ones
  // trashed from here come back as they were drawn. `atOnce` hides them in
  // one drawing, not behind the frames that follow (holdRowsShown).
  async function changeTrash(rowIds: string | readonly string[], trash: boolean, atOnce = false): Promise<boolean> {
    const ids = typeof rowIds === "string" ? [rowIds] : rowIds;
    const rows = store?.get().rows ?? [];
    const leaving = rowsWithSubitems(rows, ids), drawn = rows.filter(({ id }) => leaving.has(id));
    for (const id of trash ? ids : []) {
      const own = rowsWithSubitems(drawn, [id]), its = drawn.filter((row) => own.has(row.id));
      if (its.length) trashedHere.current.set(id, its);
    }
    const coming = ids.flatMap((id) => trashedHere.current.get(id) ?? []);
    const holder = rowsHolderPage(store?.get(), trash ? drawn : coming, pageId);
    // Rows trashed are a step of the page's ⌘Z from the press, which brings them back as drawn at once (Notion's,
    // 2026-10-21; 2026-11-27: recorded at the server's answer, a ⌘Z pressed as they left found nothing and was
    // lost): taken back before the answer, they are no longer kept hidden for it, the server told after the trash.
    // Refused, the step goes.
    let letGo = () => {};
    const stepped = trash ? rowsTrashed?.(ids, drawn, () => letGo()) : undefined;
    const moved = await shownFirst(trash ? withoutRows(leaving) : withRowsAsDrawn(coming),
      trash ? withRowsAsDrawn(drawn) : withoutRows(new Set(coming.map(({ id }) => id))), ids,
      (writer, rowIds) => sendRowTrash(writer, rowIds, trash, { pageId: holder, drawn: trash ? drawn : coming }),
      trash ? t("toast.movedToTrash") : null, t(trash ? "toast.couldntMoveToTrash" : "toast.couldntRestorePage"),
      trash ? { label: t("trashDatabase.restore"), onClick: () => { void restoreChosen(ids); } } : undefined, atOnce,
      (release) => { letGo = () => release(); });
    if (!moved) stepped?.();
    if (moved && !trash) for (const id of ids) trashedHere.current.delete(id);
    return moved;
  }
  // Rows the toast's "복원하기" brings back are chosen again in their places,
  // as the page's ⌘Z leaves them, the way Notion's keep "1개 선택됨" (seen
  // 2026-11-07): at once where they come back drawn as they were, else once
  // the server has them.
  async function restoreChosen(ids: readonly string[]) {
    const choose = () => chooseStepRows(ids);
    const shownBack = ids.every((id) => trashedHere.current.has(id));
    const restoring = changeTrash(ids, false);
    if (shownBack) choose();
    if (await restoring && !shownBack) choose();
  }
  async function deleteForever(rowId: string) {
    const deleted = await run(async () => {
      await service?.deletePermanently(rowId);
      await reread();
    }, t("toast.deletedPageForever"), (error) => t(isLegalHoldPermanentDeleteError(error) ? "toast.legalHoldKeepsPage" : "toast.couldntDeletePageForever"));
    if (deleted) closeRow();
    return deleted;
  }
  // The row's link and favorite, from its menu and its page's top bar.
  const copyLink = async (rowId: string) => {
    if (await copyText(serverUrl(pageHref(rowId)))) notify(t("toast.copiedLink"), "success");
    else notify(t("toast.couldntCopyLink"), "error");
  };
  const setFavorite = async (rowId: string, favorite: boolean) => { await run(
    async () => { await service?.setFavorite(rowId, favorite); },
    null, t("toast.couldntUpdateFavorites"),
  ); };
  return { busy, run, setArchived, changePresentation, changeLock, changeTrash, deleteForever, copyLink, setFavorite };
}
