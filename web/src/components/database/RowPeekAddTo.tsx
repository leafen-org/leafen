import { useContext, useEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from "react";
import { api } from "@/api";
import { useTranslation } from "react-i18next";
import { ChevronDown, DatabaseCylinderIcon } from "@/icons/leafen";
import { useProductPages } from "@/components/product/productPagesContext";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { followFreshPageEdits, freshPageBlank, isFreshPage, keepFreshPage } from "@/components/page/freshPages";
import { movePopoverAnchorOf, type MovePopoverAnchor } from "@/components/movePopoverPlacement";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import { matchesKeyboardShortcut } from "@/lib/keyboardShortcuts";
import { ADD_TO_PEEK_PARAM } from "@/lib/pagePeek";
import { useSearchParams } from "@/lib/router";
import { readUntilServerAnswers } from "@/lib/serverReach";
import { sendRowTrash } from "@/lib/rowTrashKept";
import type { Page, Session } from "@/types";
import { dataSourceDisplayTitle } from "../editor/databaseTitles";
import { DatabaseDataSourcesContext } from "./databaseDataSourcesContext";
import { DatabaseRowMovePopover, databaseRowMoveSubject } from "./DatabaseRowMovePopover";
import styles from "../page/PagePeekAddTo.module.css";

/**
 * A row the sidebar's database "+" just made, opened in a center peek with
 * `showMoveTo`, as Notion's (seen 2026-11-02): "추가 대상 ⌄" in the bar, and
 * closed with nothing in it — no words in its title or body, what was
 * written undone too (2026-11-13), no value, icon or cover set — the row
 * goes to the trash (Notion's API read it in_trash), and a wiki's page out
 * of the sidebar.
 * What its peek's close calls first is `leave`.
 */
export function useFreshRowAddTo(snapshot: DatabaseRowPageSnapshot, panelRef: RefObject<HTMLElement | null>, session?: Session) {
  const search = useSearchParams();
  const rowId = snapshot.row.id;
  const addTo = search.get("p") === rowId && search.get(ADD_TO_PEEK_PARAM) === "true";
  const dataSources = useContext(DatabaseDataSourcesContext);
  const productPages = useProductPages();
  useEffect(() => {
    if (!addTo || !panelRef.current || !isFreshPage(rowId)) return;
    return followFreshPageEdits(panelRef.current, rowId);
  }, [addTo, panelRef, rowId]);
  // A value, icon or cover set in it keeps it, however it was set.
  const made = JSON.stringify([snapshot.row.values, snapshot.row.icon, snapshot.row.cover]);
  const first = useRef<{ rowId: string; made: string } | null>(null);
  useEffect(() => {
    if (!addTo) return;
    if (first.current?.rowId !== rowId) first.current = { rowId, made };
    else if (first.current.made !== made) keepFreshPage(rowId);
  }, [addTo, made, rowId]);
  const leave = () => {
    if (!addTo || !session || !isFreshPage(rowId)) return;
    keepFreshPage(rowId);
    // Words in its title or body, saved or not yet, keep it; written and
    // undone they don't, as a page's peek (leaveFreshPage).
    if (!freshPageBlank(panelRef.current)) return;
    // A wiki's page leaves the sidebar with it, which reads the workspace again. Kept for a server out of reach (rowTrashKept.ts).
    const wiki = productPages?.pages.some((page) => page.wikiDataSourceId === snapshot.row.dataSourceId);
    void sendRowTrash(session, [rowId], true, { pageId: snapshot.row.containerPageId })
      .then(() => Promise.all([dataSources?.reread(), wiki ? productPages?.refreshPages?.() : undefined])).catch(() => undefined);
  };
  return { addTo, leave };
}

// Cmd/Ctrl+Shift+P with no block selected; on a page (`onPage`), not while a peek is open over it, which takes it;
// nothing on a row locked for the reader, whose menu has no 옮기기 (Notion's, seen 2026-11-14).
function useMoveKey(onPage: boolean, press: () => void, locked = false) {
  const pressed = useRef(press);
  pressed.current = press;
  useEffect(() => {
    if (locked) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || !matchesKeyboardShortcut("movePage", event)) return;
      if (onPage && document.querySelector('[data-row-page-mode="side"], [data-row-page-mode="center"]')) return;
      event.preventDefault();
      pressed.current();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onPage, locked]);
}

/**
 * On a wiki's page, Cmd/Ctrl+Shift+P with no block selected moves the page as
 * its "⋯ › 옮기기" does (`move`, the page's move), the list right under the
 * "⋯", as Notion's (seen 2026-11-14: at 1098, 36 under the "⋯" at 1402, 8 to
 * 36, as a page's own); a peek open over it takes the key.
 */
export function PageMoveKey({ move, locked = false }: { move: (anchor: MovePopoverAnchor | null, close: () => void) => ReactNode; locked?: boolean }) {
  const [anchor, setAnchor] = useState<MovePopoverAnchor | null | undefined>(undefined);
  useMoveKey(true, () => setAnchor(movePopoverAnchorOf(document.querySelector<HTMLElement>("[data-row-page-actions]"))), locked);
  return anchor === undefined ? null : <>{move(anchor, () => setAnchor(undefined))}</>;
}

/**
 * A row peek's "move to": for a row just made, "추가 대상 [DB] 〈database〉 ⌄",
 * the database the row is in, read by its source (a row's snapshot names
 * none), its move list hanging under it when pressed; and in any peek,
 * Cmd/Ctrl+Shift+P with no block selected moves the peek's row, not the page
 * behind, as Notion's center peek does (2026-11-03 audit: the database page
 * behind it was moved). On the row's own page (`onPage`) the key moves the
 * row as its "⋯ › 옮기기" does, the list hanging right under the "⋯", as
 * Notion's (seen 2026-11-13; a peek open over it takes the key).
 */
export function RowPeekAddTo({ snapshot, title, containerTitle, session, addTo, moving = false, onPage = false }: {
  /**
   * "내 작업"의 옆 보기: a task moves, as Notion's "옮기기 · T ⌄" (seen
   * 2026-11-03); so does a row that wasn't just made, wherever its peek is
   * (seen 2026-11-14: a home task's center peek), and "추가 대상" is a new one's.
   */
  moving?: boolean;
  onPage?: boolean;
  snapshot: DatabaseRowPageSnapshot;
  title: string;
  containerTitle?: string;
  session?: Session;
  addTo: boolean;
}) {
  const { t } = useTranslation(["sidebar", "databaseView"]);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const [anchor, setAnchor] = useState<MovePopoverAnchor | null | undefined>(undefined);
  const { workspaceId } = snapshot;
  const { dataSourceId } = snapshot.row;
  // Whether the row was just made as its peek opened, which a value set in it doesn't change.
  const madeHere = useMemo(() => isFreshPage(snapshot.row.id), [snapshot.row.id]);
  const [source, setSource] = useState<{ id: string; pageId: string; title: string; icon: string; iconType: string } | null>(null);
  // Read while the server was out of reach — a row made then — it is read again once the server may answer.
  useEffect(() => {
    if (!session || !dataSourceId) return;
    const controller = new AbortController();
    const stop = readUntilServerAnswers(() => api.readAutomationTargetDatabase(session, workspaceId, dataSourceId, controller.signal),
      (database) => setSource({ id: dataSourceId, pageId: database.pageId, title: database.title, icon: database.icon ?? "",
        iconType: database.iconType ?? "none" }));
    return () => { stop(); controller.abort(); };
  }, [dataSourceId, session, workspaceId]);
  useMoveKey(onPage, () => setAnchor(movePopoverAnchorOf(buttonRef.current ?? document.querySelector<HTMLElement>("[data-row-page-actions]"))),
    snapshot.row.locked && !snapshot.row.unlockedForMe);
  // A wiki's page under another of its pages names that page, as Notion's
  // "추가 대상 📄 〈page〉" (seen 2026-11-11); one at its top names the wiki
  // by its database, as Notion's does.
  const pages = useProductPages()?.pages;
  const above = snapshot.row.subitemParentId;
  const wikiPage = above && pages?.some((page) => page.wikiDataSourceId === dataSourceId)
    ? pages.find((page) => page.id === above) : undefined;
  // Its source untitled is called by how many name its database, "새 데이터 소스" as one of an untitled
  // database's several, as a source is anywhere (useDataSourceName; the page list says them, sourceTitles).
  const naming = source ? pages?.find((page) => page.id === source.pageId)?.sourceTitles?.length ?? 1 : 1;
  const name = wikiPage ? wikiPage.title || untitledPageDisplayTitle(wikiPage)
    : source?.id === dataSourceId ? dataSourceDisplayTitle(source.title, naming) : containerTitle?.trim();
  const popover = anchor !== undefined ? (
    <DatabaseRowMovePopover subject={databaseRowMoveSubject(snapshot, title)} anchor={anchor} addTo={addTo} anchorGap={onPage ? 0 : undefined}
      onMoved={addTo ? () => keepFreshPage(snapshot.row.id) : undefined} onClose={() => setAnchor(undefined)} />
  ) : null;
  if (!addTo) return popover;
  return (
    <span className={styles.addTo}>
      {/* Pressed, it leaves the focus where it was, as a page's "추가 대상" (PagePeekAddTo). */}
      <button ref={buttonRef} type="button" className={styles.addToButton} data-row-peek-add-to data-press-keeps-focus
        aria-haspopup="dialog" aria-expanded={anchor !== undefined} onClick={() => setAnchor(movePopoverAnchorOf(buttonRef.current))}>
        <span className={styles.addToLabel}>{t(moving || !madeHere ? "sidebar:addWindow.moveTo" : "sidebar:addWindow.addTo")}</span>
        {/* The database's own icon, as Notion's "옮기기 ✅ 작업 트래커" (seen 2026-11-09); none, the database mark. */}
        {wikiPage ? <span aria-hidden="true" className={styles.addToIcon}><PageIconGlyph page={wikiPage} size={16} /></span>
          : source?.id === dataSourceId && source.icon && source.iconType !== "none"
          ? <span aria-hidden="true" className={styles.addToIcon}><PageIconGlyph page={{ icon: source.icon, iconType: source.iconType as Page["iconType"] }} size={16} fallback="database" /></span>
          : <DatabaseCylinderIcon size="small" aria-hidden="true" />}
        <span className={styles.addToTitle}>{name || t("databaseView:databaseFallbackTitle")}</span>
        <ChevronDown size="tiny" className={styles.addToChevron} aria-hidden="true" />
      </button>
      {popover}
    </span>
  );
}
