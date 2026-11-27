import { Suspense, useEffect, useId, useMemo, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import styles from "@/components/RowMenu.module.css";
import { RowMenuSidebarActions } from "@/components/RowMenuSidebarActions";
import { movePopoverAnchorOf, type MovePopoverAnchor } from "@/components/movePopoverPlacement";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { PageRenamePopover, renamePopoverAt } from "@/components/page/PageRenamePopover";
import { movePageIntoDatabase, readDatabaseRowPage, setDatabaseRowPageFavorite } from "@/databaseRowPageApi";
import type { DatabaseRowPageSnapshot, DatabaseRowPageValue } from "@/databaseRowPageWire.generated";
import { DotsHorizontal, Plus, SidebarChevronRight } from "@/icons/leafen";
import { copyText } from "@/lib/copyText";
import { createDatabaseRowPageService } from "@/lib/databaseRowPageService";
import { lazyScreen } from "@/lib/lazyScreen";
import { useOpenPageGraphStore } from "@/lib/pageGraphStore";
import { holdRowsGone, rowsDrawn } from "@/lib/rowsShownAtPress";
import { sendRowValues } from "@/lib/rowValuesKept";
import { renameShowing, rowNameShown } from "@/lib/renameShownAtPress";
import { dropTypedPageTitle, showTypedPageTitle, usePageName, usePageNames } from "@/lib/pageNames";
import { newId } from "@/lib/ids";
import { holdPageThroughMove } from "@/lib/pageBecomingRow";
import { holdTypingWhile, requestPageTitleFocus } from "@/lib/pageTitleFocus";
import { renameStep } from "@/components/editor/pageUndo";
import { openPagePeek } from "@/lib/pagePeek";
import { personName } from "@/lib/peopleDirectory";
import { placeAtPoint, popoverBackdropStyle, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { useOptionalProductToast } from "@/lib/productToast";
import { pushRoute } from "@/lib/router";
import { serverUrl } from "@/lib/serverOrigin";
import type { Page, Session } from "@/types";
import type { TextSpan } from "@/lib/types";
import { sameTitle } from "@/lib/titleMerge";
import { libraryRowHref, libraryRowLink, libraryRowPage } from "@/components/library/libraryModel";
import { useLibraryPages } from "@/components/library/useLibraryData";
import { PageTreeItem } from "./PageTreeItem";
import { TitleWithCustomEmoji } from "@/components/page/TitleWithCustomEmoji";
import { usePageTreeContext } from "./pageTreeContext";
import { childTreePages, treeMenuPoint } from "./pageTreeModel";
import { onTreeRowKeyDown } from "./pageTreeDom";
import { treeEmptyChildrenIndent, treeRowIndent } from "./pageTreeIndent";
import { styles as treeStyles } from "./sidebarStyles";
import { pageMapOf } from "@/lib/pageIndex";
import { isPeopleDatabasePage, rememberPeopleDatabasePage } from "@/lib/peopleDatabasePage";
import { SidebarFavoritePeopleRow } from "./SidebarFavoritePeopleRow";
import { addFailedNotice } from "./addFailedNotice";

const MoveToDialog = lazyScreen(() => (
  import("@/components/MoveToDialog").then(({ MoveToDialog: Dialog }) => ({ default: Dialog }))
), { typedOnOpen: true });

/**
 * The favorites the sidebar's page list doesn't hold — a database's rows and
 * templates — with what the library reads of them, in the favorites' order.
 */
export function useFavoriteRowPages(session: Session, workspaceId: string | null, favoritePageIds: readonly string[], pages: readonly Page[]) {
  const unlisted = useMemo(() => {
    const known = pageMapOf(pages);
    return favoritePageIds.filter((id) => !known.has(id));
  }, [favoritePageIds, pages]);
  const details = useLibraryPages(session, workspaceId ?? "", workspaceId ? unlisted : []);
  const rows = workspaceId ? unlisted.flatMap((id) => {
    const detail = details.pages.get(id);
    const page = detail ? libraryRowPage(detail, workspaceId) : null;
    return page && detail ? [{ page, template: Boolean(detail.row?.template), href: libraryRowHref(detail), link: libraryRowLink(detail), person: detail.row?.person }] : [];
  }) : [];
  // People among them opens beside a page by its page's address (peoplePeekHref), reloaded too.
  const people = rows.find(({ page }) => isPeopleDatabasePage(page))?.page.id ?? "";
  useEffect(() => { rememberPeopleDatabasePage(people); }, [people]);
  return rows;
}

/**
 * A database's row or template among favorites, as Notion's sidebar lists
 * it (seen 2026-10-30, 2026-10-31): a page's row — its icon, turning "›"
 * under the pointer to show the pages inside it ("하위 페이지 없음" with
 * none), "+" to add one there and "⋯"; pressing it opens the row full in
 * its database, or the template in its window. Its "⋯" is a favorite page's
 * menu: 즐겨찾기 해제, copy link, rename, move — the pages' "옮기기" popover
 * where the menu was, moving the row as its database's menu does — open in
 * a new tab or a side peek, and who edited it last. It opens where the
 * library's would (libraryRowHref) — a People row as its person's profile,
 * in a peek too — and its link is copied as the library's (libraryRowLink).
 */
export function SidebarFavoriteRowPage(props: {
  page: Page; template: boolean; href: string; link: string; person?: string; session: Session;
}) {
  // People's own database lists as People (isPeopleDatabasePage).
  return isPeopleDatabasePage(props.page) ? <SidebarFavoritePeopleRow page={props.page} link={props.link} session={props.session} />
    : <FavoriteRowPage {...props} />;
}

function FavoriteRowPage({ page, template, href, link, person, session }: {
  page: Page; template: boolean; href: string; link: string; person?: string; session: Session;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["rowMenu", "pageTreeItem", "sidebar", "databaseView"]);
  const toast = useOptionalProductToast();
  const { pages, expandedPageIds, setPageExpanded, onCreateSubpage, onOpenPage, onPageChange, onStatus } = usePageTreeContext();
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const [renameAt, setRenameAt] = useState<{ top: number; left: number } | null>(null);
  const [moveAt, setMoveAt] = useState<MovePopoverAnchor | null | undefined>(undefined);
  const rowService = useMemo(() => createDatabaseRowPageService(session, () => {}), [session]);
  // Moved, the row leaves the open page's databases at the press (DatabaseRowMovePopover's way).
  const store = useOpenPageGraphStore();
  const [snapshot, setSnapshot] = useState<DatabaseRowPageSnapshot | null>(null);
  const moreRef = useRef<HTMLButtonElement>(null);
  // The rename open now, one step of the history once closed.
  const renameSteps = useRef<ReturnType<typeof renameStep<RowName>> | null>(null);
  // A name being typed in its rename popover shows here as it is typed (lib/renameShownAtPress.ts).
  const title = usePageName(page);
  const expansionKey = `favorite:${page.id}`;
  const expanded = expandedPageIds.has(expansionKey);
  const titleId = useId();
  const children = childTreePages(pages, page.id);
  const canEdit = snapshot ? snapshot.accessRole === "edit" || snapshot.accessRole === "full_access" : false;
  // The menu reads the row as it opens: whether the person may change it, and who edited it last.
  async function openMenu(at: { x: number; y: number }) {
    setMenu(at);
    try { setSnapshot(await readDatabaseRowPage(session, page.id, { limit: 1 })); } catch { setSnapshot(null); }
  }
  async function unfavorite() {
    setMenu(null);
    try {
      const current = snapshot ?? await readDatabaseRowPage(session, page.id, { limit: 1 });
      await setDatabaseRowPageFavorite(session, page.id, { favorite: false, expectedAuthorityRevision: current.row.authorityRevision });
    } catch {
      toast?.notify(t("rowMenu:toast.couldntUpdateFavorites"), "error");
    }
  }
  // A rename keeps the mentions its box still holds, written with the title,
  // kept in the outbox behind the row's other changes (rowValuesKept.ts).
  async function writeName(next: string, spans?: readonly TextSpan[]) {
    const titleProperty = snapshot?.properties.find((property) => property.type === "title");
    if (!titleProperty) return false;
    try {
      await sendRowValues(session, page.id, { values: { [titleProperty.id]: { type: "title", value: next, ...(spans ? { spans: spans as DatabaseRowPageValue["spans"] } : {}) } } },
        snapshot?.row.containerPageId);
      return true;
    } catch {
      toast?.notify(t("databaseView:toast.couldntSaveChanges"), "error");
      return false;
    }
  }
  // Each key's name shows at once — here, wherever the row page is named,
  // and the row on the open page — saved once the typing stops; refused, it
  // goes back (lib/renameShownAtPress.ts).
  const rowShowing = useRef<ReturnType<typeof renameShowing> | null>(null);
  function showName(next: string, spans?: TextSpan[]) {
    showTypedPageTitle(page.id, next, page.title);
    const shown = rowNameShown(store, page.id, next, spans);
    rowShowing.current ??= renameShowing(store, () => shown.settled);
    rowShowing.current.show(shown.changes);
  }
  function rename(next: string, spans?: TextSpan[]) {
    if (next === page.title && !spans) return;
    const saving = writeName(next, spans);
    rowShowing.current?.saving(rowNameShown(store, page.id, next, spans).changes,
      saving.then((written) => { if (!written) throw new Error("refused"); }));
    void saving.then((written) => { if (!written) dropTypedPageTitle(page.id); });
    renameSteps.current?.saved({ title: next, titleSpans: spans ?? [] }, saving);
  }
  // "+" puts a page inside the row's page, as a page's row does, and opens it
  // at its title, taking what is typed meanwhile.
  async function addInside() {
    await holdTypingWhile(async () => {
      try {
        const created = await onCreateSubpage(page.id, page.workspaceId);
        if (!created) return;
        onPageChange(created);
        setPageExpanded(expansionKey, true);
        requestPageTitleFocus(created.id);
        onOpenPage(created, false);
      } catch (error) {
        toast?.notify(...addFailedNotice(error, t("sidebar:couldntCreatePage")));
      }
    });
  }
  return (
    <>
      <div className={treeStyles.treeRow} role="treeitem" aria-level={1} aria-expanded={expanded} data-favorite-row-page={page.id}
        data-hover-leading="chevron" data-has-children={children.length > 0} style={{ paddingInlineStart: treeRowIndent(0) }} tabIndex={0}
        onClick={(event) => {
          if (event.altKey) openPagePeek(person || page.id);
          else if (event.metaKey || event.ctrlKey) window.open(href, "_blank", "noopener");
          else pushRoute(href);
        }}
        onKeyDown={(event) => onTreeRowKeyDown(event, {
          open: (newTab) => (newTab ? window.open(href, "_blank", "noopener") : pushRoute(href)),
          expanded, setExpanded: (next) => setPageExpanded(expansionKey, next), menu: () => moreRef.current?.click(),
        })}>
        <span className={treeStyles.treeLeading} data-tree-leading="true">
          <button type="button" className={`${treeStyles.disclosure} ${expanded ? treeStyles.disclosureOpen : ""}`}
            aria-label={t(expanded ? "pageTreeItem:hideInside" : "pageTreeItem:showInside")} aria-describedby={titleId}
            aria-expanded={expanded} data-tree-disclosure="true"
            onClick={(event) => { event.stopPropagation(); setPageExpanded(expansionKey, !expanded); }}>
            <SidebarChevronRight size="micro" />
          </button>
          <span className={treeStyles.treeIcon} data-tree-icon="true"
            data-tree-emoji={page.iconType === "emoji" && page.icon ? "true" : undefined}><PageIconGlyph page={page} size={18} fallback="file" /></span>
        </span>
        <span id={titleId} className={treeStyles.treeTitle} data-tree-title="true"><TitleWithCustomEmoji text={title} named={page} workspaceId={page.workspaceId} /></span>
        <span className={treeStyles.treeActions}>
          {template ? null : (
            <button type="button" className={treeStyles.iconBtn} title={t("pageTreeItem:addInsideTitle")}
              aria-label={t("pageTreeItem:addInside", { title })} onClick={(event) => { event.stopPropagation(); void addInside(); }}>
              <Plus size="small" />
            </button>
          )}
          <button ref={moreRef} type="button" className={treeStyles.iconBtn} aria-label={t("pageTreeItem:openActions", { title })}
            onClick={(event) => { event.stopPropagation(); void openMenu(treeMenuPoint(event)); }}>
            <DotsHorizontal size="small" />
          </button>
        </span>
      </div>
      {menu ? (
        <FavoriteRowPageMenu anchor={menu} page={page} canEdit={canEdit} snapshot={snapshot}
          onClose={() => setMenu(null)} onUnfavorite={() => void unfavorite()}
          onCopyLink={() => {
            setMenu(null);
            void copyText(serverUrl(link)).then((ok) => toast?.notify(ok ? t("rowMenu:toast.copiedLink") : t("rowMenu:toast.couldntCopyLink"), ok ? "success" : "error"));
          }}
          onRename={() => {
            setMenu(null);
            renameSteps.current = renameStep(page.id, { title: page.title, titleSpans: page.titleSpans ?? [] }, sameRowName,
              (name) => writeName(name.title, name.titleSpans));
            setRenameAt(renamePopoverAt(moreRef.current) ?? { top: 16, left: 12 });
          }}
          onMove={(at) => { setMenu(null); setMoveAt(at ? { ...at, top: at.top - 4, bottom: at.top - 4 } : null); }}
          onOpenInNewTab={() => { setMenu(null); window.open(href, "_blank", "noopener,noreferrer"); }}
          onOpenInSidePeek={() => { setMenu(null); openPagePeek(person || page.id); }} />
      ) : null}
      {moveAt !== undefined ? (
        <Suspense fallback={null}>
          <MoveToDialog pageId={page.id} pages={pages} session={session} anchor={moveAt} onClose={() => setMoveAt(undefined)}
            onPageChange={onPageChange} onStatus={onStatus}
            row={{ page, leave: () => holdRowsGone(store, rowsDrawn(store, [page.id])),
              preview: (target) => rowService.previewMove(page.id, target), move: (target, accepted) => rowService.move(page.id, target, accepted),
              undo: snapshot?.row.dataSourceId ? async (target) => {
                if (target.kind === "page") {
                  // Open, it is drawn till it is the row's page again (pageBecomingRow.ts).
                  const turned = holdPageThroughMove(session, [page.id]);
                  await movePageIntoDatabase(session, page.id, { requestId: newId(), dataSourceId: snapshot.row.dataSourceId }, new AbortController().signal)
                    .then(() => turned(), (error: unknown) => { turned(true); throw error; });
                } else {
                  await rowService.refresh(page.id);
                  await rowService.move(page.id, { kind: "database", id: snapshot.row.dataSourceId }, []);
                }
              } : undefined }} />
        </Suspense>
      ) : null}
      {renameAt ? (
        <PageRenamePopover page={page} title={page.title} titleSpans={page.titleSpans} untitled={pageNames.title({ ...page, title: "" })} at={renameAt}
          onTitleChange={rename} onTitleShown={showName}
          onClose={() => { setRenameAt(null); renameSteps.current?.close(); renameSteps.current = null; rowShowing.current?.end(); rowShowing.current = null; }} />
      ) : null}
      {expanded ? (
        <div role="group" aria-label={t("pageTreeItem:subpages", { title })}>
          {children.length > 0 ? children.map((child, index) => (
            <PageTreeItem key={child.id} pageId={child.id} depth={1} index={index} setSize={children.length} />
          )) : (
            <div className={treeStyles.treeEmptyChildren} style={{ paddingInlineStart: treeEmptyChildrenIndent(0) }} data-tree-empty-children="true" aria-hidden="true">
              {t("pageTreeItem:noPagesInside")}
            </div>
          )}
        </div>
      ) : null}
    </>
  );
}

type RowName = { title: string; titleSpans: readonly TextSpan[] };

function sameRowName(left: RowName, right: RowName) {
  return sameTitle({ title: left.title, spans: left.titleSpans }, { title: right.title, spans: right.titleSpans });
}

function FavoriteRowPageMenu({ anchor, page, canEdit, snapshot, onClose, onUnfavorite, onCopyLink, onRename, onMove, onOpenInNewTab, onOpenInSidePeek }: {
  anchor: { x: number; y: number }; page: Page; canEdit: boolean; snapshot: DatabaseRowPageSnapshot | null;
  onClose(): void; onUnfavorite(): void; onCopyLink(): void; onRename(): void; onMove(at: MovePopoverAnchor | null): void;
  onOpenInNewTab(): void; onOpenInSidePeek(): void;
}) {
  const { t } = useTranslation("rowMenu");
  const menuRef = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(menuRef, (size, viewport) => placeAtPoint(anchor, size, viewport), [anchor], undefined,
    { kind: "menu", onEscape: onClose, onOutsidePress: onClose });
  const editorId = snapshot?.row.lastEditedBy || snapshot?.row.createdBy || null;
  return createPortal(
    <>
      <button type="button" className={styles.backdrop} style={popoverBackdropStyle(place)} tabIndex={-1} aria-label={t("aria.closePageActions")} onClick={onClose} />
      <div ref={menuRef} className={`${styles.menu} ${styles.contextMenu}`} data-row-menu-variant="sidebar" data-favorite-row-page-menu
        style={{ "--menu-x": `${anchor.x}px`, "--menu-y": `${anchor.y}px`, ...(place ? popoverPlacementStyle(place) : {}) } as CSSProperties}
        onClick={(event) => event.stopPropagation()}>
        <RowMenuSidebarActions busy={false} canEdit={canEdit} favorite favoritesList lockedForMe={false} page={page}
          edited={editorId ? { name: personName(editorId) || "", editorId, at: snapshot?.row.updatedAt } : undefined}
          onCopyLink={onCopyLink} onDuplicate={() => {}} onFavorite={onUnfavorite} onMove={() => onMove(movePopoverAnchorOf(menuRef.current))}
          onOpenInNewTab={onOpenInNewTab}
          onOpenInSidePeek={onOpenInSidePeek} onRename={onRename} onTrash={() => {}} />
      </div>
    </>,
    document.body,
  );
}
