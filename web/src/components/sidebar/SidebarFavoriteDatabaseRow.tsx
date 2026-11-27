import { useEffect, useId, useRef, useState, type CSSProperties, type MouseEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { Separator } from "react-aria-components";
import { blockInPlace, type GraphDataSource, type GraphDataSourceWrite, type GraphOperation } from "@/contentGraph";
import styles from "@/components/RowMenu.module.css";
import { RowMenuEditedFoot } from "@/components/RowMenuEditedFoot";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { PageRenamePopover, renamePopoverAt } from "@/components/page/PageRenamePopover";
import { graphNameKept } from "@/lib/typedGraphChanges";
import type { PagePresentationChange } from "@/components/page/pagePresentation";
import type { GraphDataSourceIconType } from "@/contentGraphWire.generated";
import { type MovePopoverAnchor, movePopoverAnchorOf } from "@/components/movePopoverPlacement";
import { BlockMoveToDialog } from "@/components/editor/BlockMoveToDialog";
import { inlineDatabaseViews } from "@/components/editor/InlineDatabaseBlock";
import { viewDisplayName } from "@/components/database/databaseViewShared";
import { ArchiveIcon, ComposeIcon, DotsHorizontal, LinkedViewIcon, LinkIcon, MoveIcon, OpenInNew, Plus, SidebarChevronRight, SidePeekIcon, Star, StarFilled, TableIcon } from "@/icons/leafen";
import { upsertBootstrapPage } from "@/lib/bootstrapPageProjection";
import { copyText } from "@/lib/copyText";
import { addressToCopy } from "@/lib/databasePageLink";
import { chosenDatabaseView, readDatabaseViewChoice } from "@/lib/databaseViewChoice";
import { addInlineDatabaseRow } from "@/lib/fullPageDatabase";
import { holdTypingWhile, requestPageTitleFocus } from "@/lib/pageTitleFocus";
import { collectContentGraphStructure } from "@/lib/authenticatedContentGraph";
import { sendContentGraphChange } from "@/lib/contentGraphSend";
import { notePageChangeStep } from "@/components/editor/pageUndo";
import { inlineDatabaseFullPageHref, inlineDatabaseName, writtenInlineDatabaseName } from "@/lib/inlineDatabaseFullPage";
import { titleWithClosedMentions } from "@/lib/closedMentions";
import { TitleWithCustomEmoji } from "@/components/page/TitleWithCustomEmoji";
import { NativeActionMenu, NativeActionMenuItem } from "@/lib/nativeActionMenu";
import { readPageBlockLastEdit } from "@/lib/pageHistoryService";
import { pageHref } from "@/lib/navigation";
import { accessHeldStyle, useAccessHold, usePageActionAccess } from "@/lib/pageActionAccessContext";
import { inlineDatabasePeekHref, newPagePeekHref } from "@/lib/pagePeek";
import { markFreshPage } from "@/components/page/freshPages";
import { placeAtPoint, popoverBackdropStyle, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { useOptionalProductToast } from "@/lib/productToast";
import { pushRoute } from "@/lib/router";
import { databaseNamingSourceIds, databaseViewBlock } from "@/lib/nativeDataSourceManagement";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { untitledDatabaseSourcesName, type DatabaseSourceTitle } from "@/components/editor/databaseTitles";
import type { Session } from "@/types";
import type { DatabaseFavoriteProjection } from "@/workspaceMembershipWire.generated";
import { useProductPages } from "@/components/product/productPagesContext";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { treeMenuPoint } from "./pageTreeModel";
import { onTreeRowKeyDown } from "./pageTreeDom";
import { useHolderGraph } from "./sidebarHolderGraph";
import { useOpenPageGraphStore } from "@/lib/pageGraphStore";
import { renameShowing, showDatabaseHead, useShownDatabaseHead, type DatabaseHead, type ShownChange } from "@/lib/renameShownAtPress";
import { styles as treeStyles } from "./sidebarStyles";
import { treeRowIndent } from "./pageTreeIndent";
import { menuKeyHint } from "@/lib/menuKeyHint";
import type { TextSpan } from "@/lib/types";
import { sameTitle } from "@/lib/titleMerge";
import { addFailedNotice } from "./addFailedNotice";

/** A database's name with its spans. */
type Name = { title: string; titleSpans?: GraphDataSource["titleSpans"] };

/** A name or icon given, as the head a sidebar row draws. */
function headChange(change: Partial<GraphDataSource>): Partial<DatabaseHead> {
  return {
    ...(change.title !== undefined ? { title: change.title, titleSpans: change.titleSpans as TextSpan[] | undefined } : {}),
    ...(change.iconType !== undefined ? { icon: change.icon ?? "", iconType: change.iconType } : {}),
  };
}

function sameName(a: Name, b: Name) {
  return sameTitle({ title: a.title, spans: a.titleSpans as TextSpan[] | undefined }, { title: b.title, spans: b.titleSpans as TextSpan[] | undefined });
}

// A database written from here names the fields it changes alone, its name
// only when it is one of them, the rest staying as stored (withDataSourceChange).
function sourceWritten(source: GraphDataSource): GraphDataSourceWrite {
  return { id: source.id, ...(source.pageId ? { pageId: source.pageId } : {}) };
}

/**
 * An inline database among favorites, as Notion's (seen 2026-10-11): its
 * table glyph turns into ">" under the pointer, which lists its views, and
 * "⋯" opens its menu headed "데이터베이스" — removing it from favorites;
 * copying its link, renaming, moving and archiving it; opening it in a new
 * tab or the side peek — over who edited it last and when. Its page is read
 * only once it is opened or its menu is. It shows its database's icon, which
 * the rename popover's icon button changes, as Notion's (seen 2026-10-21).
 * Its name and icon are the workspace's list's (the server's, read again as
 * they change anywhere), a rename's shown over them from the press until
 * the list holds the server's answer (lib/renameShownAtPress.ts).
 */
export function SidebarFavoriteDatabaseRow({ database, session, depth = 0 }: {
  database: DatabaseFavoriteProjection & { sourceTitles?: readonly DatabaseSourceTitle[] };
  session: Session;
  /**
   * Under its page in the tree, a level further in than the page (seen
   * 2026-11-02, Notion's 30px row with the table glyph); among favorites,
   * at the top level.
   */
  depth?: number;
}) {
  const productPages = useProductPages();
  const setFavorite = productPages?.setDatabaseFavorite;
  const { t } = useTranslation(["rowMenu", "blockHandle", "pageArchive", "pageTreeItem", "databaseView"]);
  const toast = useOptionalProductToast();
  const { phone } = useSidebarLayout(true);
  // Who may edit the page holding it may add a row to it.
  const access = usePageActionAccess(database.pageId);
  const [expanded, setExpanded] = useState(false);
  const titleId = useId();
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  // Where the rename popover opens, at the row's "…", while it is open.
  const [renaming, setRenaming] = useState<{ top: number; left: number } | null>(null);
  const moreRef = useRef<HTMLButtonElement>(null);
  // The page is read again after each change made here.
  const [revision, setRevision] = useState(0);
  const store = useOpenPageGraphStore();
  const [moving, setMoving] = useState<{ anchor: MovePopoverAnchor | null } | null>(null);
  const [edited, setEdited] = useState<{ name: string; at?: string } | null>(null);
  const holder = useHolderGraph(session, database.pageId, expanded || menu !== null || renaming !== null, revision);
  const graph = holder.status === "ready" ? holder.graph : null;
  const block = graph?.blocks.find((candidate) => candidate.id === database.containerId && candidate.type === "inline_database");
  const views = graph && block ? inlineDatabaseViews(graph, block).views : [];
  const source = graph?.dataSources.find((candidate) => candidate.id === block?.content.dataSourceId);
  const editable = holder.status === "ready" && holder.canEdit && !!block && block.content.locked !== true;
  // Its menu's rows follow that read, made as it opens, and its foot the last edit read after it: it shows once they are in (useAccessHold).
  const menuHeld = useAccessHold(menu ? database.pageId : null, holder.status === "loading" || (editable && edited === null));
  // One of several sources — its own and those it links — goes by its own
  // name, kept on its block, its sources' names standing in while it has
  // none (inlineDatabaseName).
  const ownSources = graph && block ? databaseNamingSourceIds(block).flatMap((id) => graph.dataSources.filter((candidate) => candidate.id === id)) : [];
  const ownName = block && ownSources.length > 1 ? inlineDatabaseName(block, ownSources) : null;
  const sourceTitles = database.sourceTitles
    ?? productPages?.inlineDatabases?.find((item) => item.containerId === database.containerId)?.sourceTitles;
  const named: Name | undefined = ownName ?? source;
  // Its name and icon as the workspace lists them, a rename's shown over them.
  const listed: DatabaseHead = { title: database.title, titleSpans: database.titleSpans, icon: database.icon ?? "",
    iconType: (database.iconType ?? "none") as GraphDataSourceIconType };
  const head = useShownDatabaseHead(database.containerId, listed);
  const latestListed = useRef(listed);
  latestListed.current = listed;
  const title = head.title;
  const titleSpans = head.titleSpans as TextSpan[] | undefined;
  const icon = { icon: head.icon, iconType: head.iconType };
  const iconPage = { id: database.containerId, workspaceId: database.workspaceId, ...icon };
  // An untitled database with several sources is named by them, as its page
  // is; an untitled view block — a dashboard, a link waiting for its source —
  // "제목 없음", by its block once read, else as the server says (viewBlock).
  const viewBlock = block && graph ? databaseViewBlock(block, graph.views) : database.viewBlock;
  // A view block's row with no icon of its own shows a table with an arrow, as
  // Notion's linked database rows (seen 2026-11-09); a database its table.
  const tableIcon = viewBlock ? <LinkedViewIcon size="small" aria-hidden="true" /> : <TableIcon size="small" aria-hidden="true" />;
  const untitled = untitledPageDisplayTitle({ kind: "database", viewBlock });
  // A name naming a page closed to the reader says so, as a plain title does (closedMentions.ts).
  const shown = titleWithClosedMentions(title || (ownName?.placeholder ?? (sourceTitles && sourceTitles.length > 1
    ? untitledDatabaseSourcesName(sourceTitles) : untitled)), titleSpans);
  const rowIndent = treeRowIndent(depth);
  const href = inlineDatabaseFullPageHref(database.pageId, database.containerId);
  // Its foot says who edited the database's block last, as the block's own menu does.
  useEffect(() => {
    if (!menu || !editable) return undefined;
    const controller = new AbortController();
    readPageBlockLastEdit(session, database.pageId, [database.containerId], controller.signal).then((edit) => setEdited({
      name: edit.editorName ?? "", at: edit.editedAt ? new Date(edit.editedAt).toISOString() : undefined,
    }), () => setEdited(null));
    return () => controller.abort();
  }, [menu, editable, session, database.pageId, database.containerId]);
  // Kept for a server out of reach, a change shows here and waits to be sent.
  const write = (operations: GraphOperation[]) =>
    sendContentGraphChange(session, database.pageId, operations).then(() => setRevision((value) => value + 1));
  const latestSource = useRef(source);
  latestSource.current = source;
  const latestBlock = useRef(block);
  latestBlock.current = block;
  // The database as the server had it when read, which a refused change goes back to.
  const settled = (): ShownChange[] => {
    const now = latestSource.current, held = latestBlock.current;
    return [
      ...(now ? [{ kind: "upsertDataSource", dataSource: { ...sourceWritten(now), title: now.title, titleSpans: now.titleSpans ?? [],
        icon: now.icon ?? "", iconType: now.iconType ?? "none" } } satisfies GraphOperation] : []),
      ...(held ? [{ kind: "upsertBlock", block: held } satisfies GraphOperation] : []),
    ];
  };
  // A change is shown at once wherever the database is drawn — this row and
  // its others in the press's frame, the open page right behind — and saved;
  // refused, it goes back (lib/renameShownAtPress.ts). A rename's popover
  // shows each key's name through its own showing, a pick or a ⌘Z step
  // through one of its own.
  const showAndWrite = (operations: GraphOperation[], change: Partial<DatabaseHead>, by = renameShowing(store, settled)) => {
    by.show(operations);
    const shown = showDatabaseHead(database.containerId, change, latestListed.current);
    const saving = write(operations);
    by.saving(operations, saving);
    shown(saving);
    return saving;
  };
  // A rename and an icon given are steps of the ⌘Z of the page holding the
  // database, as Notion's take them back (seen 2026-11-09); a step changes
  // the database as it is then, read again.
  const renamedFrom = useRef<{ before: Name; last: Promise<Name | null> } | null>(null);
  const showing = useRef<ReturnType<typeof renameShowing> | null>(null);
  const stepSource = (change: Partial<GraphDataSource>) => async () => {
    const id = latestSource.current?.id;
    const read = await collectContentGraphStructure(session, database.pageId).catch(() => null);
    const now = read?.graph.dataSources.find((candidate) => candidate.id === id) ?? latestSource.current;
    if (!now) return false;
    await showAndWrite([{ kind: "upsertDataSource", dataSource: { ...sourceWritten(now), ...change } }], headChange(change));
    return true;
  };
  // A name goes to the database's block while several sources name it
  // (databaseNamingSourceIds), else to its one source, the database as it is
  // then, read again.
  const nameWritten = (name: Name, held: typeof block) => held && databaseNamingSourceIds(held).length > 1
    ? [{ kind: "upsertBlock", block: blockInPlace({ ...held, content: { ...held.content,
      pageTitleSpans: writtenInlineDatabaseName(name.title, name.titleSpans as TextSpan[] | undefined, held) } }) } satisfies GraphOperation]
    : null;
  const nameOperations = (name: Name, held: typeof block, now: GraphDataSource): GraphOperation[] =>
    nameWritten(name, held) ?? [{ kind: "upsertDataSource", dataSource: { ...sourceWritten(now), ...name } }];
  const stepName = (name: Name) => async () => {
    const read = await collectContentGraphStructure(session, database.pageId).catch(() => null);
    const held = read?.graph.blocks.find((candidate) => candidate.id === database.containerId && candidate.type === "inline_database");
    const operations = nameWritten(name, held);
    if (!operations) return stepSource(name)();
    await showAndWrite(operations, headChange(name));
    return true;
  };
  function archive(archived: boolean) {
    if (!block) return;
    const content = { ...block.content };
    if (archived) content.archived = true; else delete content.archived;
    void write([{ kind: "upsertBlock", block: blockInPlace({ ...block, content }) }]).then(() => {
      if (archived) toast?.notify(t("pageArchive:archivedPages"), "default", { label: t("pageArchive:unarchive"), onClick: () => archive(false) });
    }, () => toast?.notify(t("databaseView:toast.couldntSaveChanges"), "error"));
  }
  // The icon picked shows at the press, here, in the popover and on the open page.
  function changeIcon(change: PagePresentationChange) {
    if (!source || change.iconType === undefined) return;
    const next = { icon: change.icon ?? "", iconType: change.iconType as GraphDataSourceIconType };
    const before = { icon: icon.icon, iconType: icon.iconType };
    void showAndWrite([{ kind: "upsertDataSource", dataSource: { ...sourceWritten(source), ...next } }], next, showing.current ?? undefined).then(() => {
      notePageChangeStep(database.pageId, before, next, (shown) => stepSource(shown)());
    }, () => toast?.notify(t("databaseView:toast.couldntSaveChanges"), "error"));
  }
  // "+" adds a row at its end and opens it over the page open, as Notion's
  // (seen 2026-11-02); a phone opens it as the page, as a page row's "+" does.
  function addRow(event: MouseEvent) {
    event.stopPropagation();
    void holdTypingWhile(() => addInlineDatabaseRow(session, database.pageId, database.containerId, database.workspaceId).then(
      // Its center peek offers where it goes, and closed untouched it goes (RowPeekAddTo).
      (rowId) => {
        requestPageTitleFocus(rowId);
        if (!phone) markFreshPage(rowId);
        pushRoute(phone ? pageHref(rowId) : newPagePeekHref(rowId));
      },
      (error: unknown) => toast?.notify(...addFailedNotice(error, t("databaseView:toast.couldntSaveChanges"))),
    ));
  }
  // Each key's name shows at once, saved once the typing stops (rename).
  function showName(value: string, spans?: TextSpan[]) {
    if (!source) return;
    const next: Name = { title: value, titleSpans: spans as GraphDataSource["titleSpans"] };
    showing.current?.show(nameOperations(next, block, source));
    showDatabaseHead(database.containerId, headChange(next), latestListed.current);
  }
  // A name renamed keeps the mentions the rename box still holds, written with it.
  function rename(value: string, spans?: TextSpan[]) {
    const next: Name = { title: value, titleSpans: spans as GraphDataSource["titleSpans"] };
    if (!source || !named || sameName(next, { title: named.title, titleSpans: named.titleSpans })) return;
    const saving = showAndWrite(nameOperations(next, block, source), headChange(next), showing.current ?? undefined).then(() => next, () => {
      toast?.notify(t("databaseView:toast.couldntSaveChanges"), "error");
      return null;
    });
    const open = renamedFrom.current;
    if (open) {
      const before = open.last;
      open.last = Promise.all([before, saving]).then(([earlier, written]) => written ?? earlier);
    }
  }
  // One rename is one step, from the name before the popover opened to the last it saved.
  function startRename(at: { top: number; left: number }) {
    renamedFrom.current = named ? { before: { title: named.title, titleSpans: named.titleSpans ?? [] }, last: Promise.resolve(null) } : null;
    showing.current = renameShowing(store, settled);
    setRenaming(at);
  }
  function endRename() {
    setRenaming(null);
    showing.current?.end();
    showing.current = null;
    const opened = renamedFrom.current;
    renamedFrom.current = null;
    // Taken once the popover's last save is, which its closing may have started.
    void opened?.last.then((after) => {
      if (after === null || sameName(after, opened.before)) return;
      notePageChangeStep(database.pageId, opened.before, after, (name) => stepName(name)());
    });
  }
  // The view it opens at for this person, which its link and its peek's address name.
  const openingViewId = () => (block && chosenDatabaseView(views, session.user.id, block.id, block.content.viewId)?.id)
    ?? readDatabaseViewChoice(session.user.id, database.containerId) ?? undefined;
  return (
    <>
      <div className={treeStyles.treeRow} role="treeitem" aria-level={depth + 1} aria-expanded={expanded}
        data-favorite-database={depth === 0 ? database.containerId : undefined} data-tree-database={database.containerId}
        style={{ paddingInlineStart: rowIndent }} tabIndex={0} onMouseEnter={access.approach} onFocus={access.approach}
        onClick={(event) => { if (renaming === null) { if (event.metaKey || event.ctrlKey) window.open(href, "_blank", "noopener"); else pushRoute(event.altKey ? inlineDatabasePeekHref(database.pageId, database.containerId, "side", openingViewId()) : href); } }}
        onKeyDown={(event) => onTreeRowKeyDown(event, {
          open: (newTab) => (newTab ? window.open(href, "_blank", "noopener") : pushRoute(href)),
          expanded, setExpanded, menu: () => moreRef.current?.click(),
        })}>
        <span className={treeStyles.treeLeading} data-tree-leading="true">
          <button type="button" className={`${treeStyles.disclosure} ${expanded ? treeStyles.disclosureOpen : ""}`}
            aria-label={t(expanded ? "pageTreeItem:hideInside" : "pageTreeItem:showInside")} aria-describedby={titleId}
            aria-expanded={expanded} data-tree-disclosure="true"
            onClick={(event) => { event.stopPropagation(); setExpanded(!expanded); }}>
            <SidebarChevronRight size="micro" />
          </button>
          <span className={treeStyles.treeIcon} data-tree-icon="true"
            data-tree-emoji={iconPage.iconType === "emoji" && iconPage.icon ? "true" : undefined}><PageIconGlyph page={iconPage} size={18} fallback={tableIcon} /></span>
        </span>
        <span id={titleId} className={treeStyles.treeTitle} data-tree-title="true">
          <TitleWithCustomEmoji text={shown} named={{ title, titleSpans }} workspaceId={database.workspaceId} /></span>
        <span className={treeStyles.treeActions}>
          {access.canEdit ? (
            <button type="button" className={treeStyles.iconBtn} title={t("pageTreeItem:addInsideTitle")}
              aria-label={t("pageTreeItem:addInside", { title: shown })} data-tree-database-add onClick={addRow}><Plus size="small" /></button>
          ) : null}
          <button ref={moreRef} type="button" className={treeStyles.iconBtn} aria-label={t("pageTreeItem:openActions", { title: shown })}
            onClick={(event) => { event.stopPropagation(); setMenu(treeMenuPoint(event)); }}>
            <DotsHorizontal size="small" />
          </button>
        </span>
      </div>
      {expanded ? views.map((view) => (
        <div key={view.id} className={treeStyles.treeRow} role="treeitem" aria-level={depth + 2} data-favorite-database-view={view.id}
          style={{ paddingInlineStart: `calc(${rowIndent} + 22px)` }} tabIndex={0}
          onClick={() => pushRoute(inlineDatabaseFullPageHref(database.pageId, database.containerId, view.id))}
          onKeyDown={(event) => onTreeRowKeyDown(event, { open: (newTab) => {
            const viewHref = inlineDatabaseFullPageHref(database.pageId, database.containerId, view.id);
            if (newTab) window.open(viewHref, "_blank", "noopener"); else pushRoute(viewHref);
          } })}>
          <span className={treeStyles.treeLeading} aria-hidden="true">•</span>
          <span className={treeStyles.treeTitle}>{viewDisplayName(view)}</span>
        </div>
      )) : null}
      {renaming ? (
        <PageRenamePopover page={iconPage} iconFallback={tableIcon} title={title} titleSpans={titleSpans} untitled={untitled} at={renaming}
          // Kept on the device at each key as the change saving it writes (lib/typedGraphChanges.ts).
          kept={graphNameKept(`database:${database.containerId}/name`, database.pageId, (typed) => (source
            ? nameOperations({ title: typed.title, titleSpans: typed.spans as GraphDataSource["titleSpans"] }, block, source) : null))}
          onTitleChange={rename} onTitleShown={showName} onPresentationChange={editable && source ? changeIcon : undefined} onClose={endRename} />
      ) : null}
      {menu ? (
        <FavoriteDatabaseMenu anchor={menu} held={menuHeld} title={shown} editable={editable} archived={block?.content.archived === true} favorite={database.favorite}
          edited={editable ? edited : null}
          onClose={() => setMenu(null)}
          onUnfavorite={() => {
            setMenu(null);
            void setFavorite?.(database, !database.favorite).catch(() => toast?.notify(t("rowMenu:toast.couldntUpdateFavorites"), "error"));
          }}
          onCopyLink={() => {
            setMenu(null);
            // It names the view the address names, else the one it opens at for this person (addressToCopy).
            void copyText(addressToCopy(database.containerId, openingViewId() ?? null)).then((ok) => toast?.notify(ok ? t("blockHandle:database.copiedLink") : t("blockHandle:notifications.couldNotCopyLink"), ok ? "success" : "error"));
          }}
          onRename={() => { setMenu(null); startRename(renamePopoverAt(moreRef.current) ?? { top: 16, left: 12 }); }}
          onMove={(anchor) => { setMenu(null); setMoving({ anchor }); }}
          onArchive={() => { setMenu(null); archive(block?.content.archived !== true); }}
          onOpenInNewTab={() => { setMenu(null); window.open(href, "_blank", "noopener,noreferrer"); }}
          onOpenInSidePeek={() => { setMenu(null); pushRoute(inlineDatabasePeekHref(database.pageId, database.containerId, "side", openingViewId())); }} />
      ) : null}
      {moving && productPages ? (
        // It goes onto a page; a database is no choice, as Notion's (seen 2026-11-11).
        <BlockMoveToDialog blockId={database.containerId} sourcePageId={database.pageId} workspaceId={database.workspaceId}
          pages={productPages.pages} session={session} anchor={moving.anchor} databaseMoving onClose={() => setMoving(null)}
          onMoved={() => void productPages.refreshPages?.()}
          onPageChange={(page) => productPages.setData((current) => upsertBootstrapPage(current, page))}
          onStatus={productPages.onStatus} />
      ) : null}
    </>
  );
}

export function FavoriteDatabaseMenu({ anchor, held = false, title, editable, archived, favorite, fullPage = false, edited, onClose, onUnfavorite, onCopyLink, onRename, onMove, onArchive, onOpenInNewTab, onOpenInSidePeek }: {
  anchor: { x: number; y: number }; title: string; editable: boolean; archived: boolean;
  /** Waiting for the read its rows follow, unseen (useAccessHold). */
  held?: boolean;
  /** Under its page it may not be a favorite yet: the first line adds it. */
  favorite: boolean;
  /** A database that is its own page says "즐겨찾기 해제" as a page's row does (RowMenuSidebarActions). */
  fullPage?: boolean;
  /** Who edited it last — with editorId their face and name on a line of their own, as a page's row (RowMenuEditedFoot). */
  edited: { name: string; at?: string; editorId?: string | null } | null;
  onClose(): void; onUnfavorite(): void; onCopyLink(): void; onRename(): void; onMove(anchor: MovePopoverAnchor | null): void;
  onArchive(): void; onOpenInNewTab(): void; onOpenInSidePeek(): void;
}) {
  const { t } = useTranslation(["rowMenu", "blockHandle", "pageArchive"]);
  const menuRef = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(menuRef, (size, viewport) => placeAtPoint(anchor, size, viewport), [anchor], undefined,
    { kind: "menu", onEscape: onClose, onOutsidePress: onClose });
  const item = (key: string, icon: ReactNode, label: string, onClick: () => void, hint?: string) => (
    <NativeActionMenuItem key={key} textValue={label} className={styles.item} data-menu-item data-favorite-database-action={key} role="menuitem" onClick={onClick}>
      {icon}<span>{label}</span>{hint ? <span className={styles.itemHint}>{hint}</span> : null}
    </NativeActionMenuItem>
  );
  return createPortal(
    <>
      <button type="button" className={styles.backdrop} style={popoverBackdropStyle(place)} tabIndex={-1} aria-label={t("rowMenu:aria.closePageActions")} onClick={onClose} />
      <div ref={menuRef} className={`${styles.menu} ${styles.contextMenu}`} data-row-menu-variant="sidebar" data-favorite-database-menu
        style={{ "--menu-x": `${anchor.x}px`, "--menu-y": `${anchor.y}px`, ...(place ? popoverPlacementStyle(place) : {}), ...(held ? accessHeldStyle : {}) } as CSSProperties}
        onClick={(event) => event.stopPropagation()}>
        <div className={styles.sectionLabel}>{t("blockHandle:database.title")}</div>
        <NativeActionMenu aria-label={t("rowMenu:aria.pageActionsFor", { title })} autoFocus>
          {favorite ? item("favorite", <StarFilled size="regular" aria-hidden="true" />, t(fullPage ? "rowMenu:actions.unfavorite" : "rowMenu:actions.removeFromFavorites"), onUnfavorite)
            : item("favorite", <Star size="regular" aria-hidden="true" />, t("rowMenu:actions.addToFavorites"), onUnfavorite)}
          <Separator className={styles.divider} />
          {item("copy-link", <LinkIcon size="regular" aria-hidden="true" />, t("rowMenu:actions.copyLink"), onCopyLink)}
          {editable ? item("rename", <ComposeIcon size="regular" aria-hidden="true" />, t("rowMenu:actions.rename"), onRename, menuKeyHint("⌘⇧R")) : null}
          {/* "옮기기" opens where the menu was, as a page row's does. */}
          {editable ? item("move", <MoveIcon size="regular" aria-hidden="true" />, t("rowMenu:actions.moveTo"), () => {
            const rect = movePopoverAnchorOf(menuRef.current);
            onMove(rect ? { ...rect, top: rect.top - 4, bottom: rect.top - 4 } : null);
          }, menuKeyHint("⌘⇧P")) : null}
          {editable ? item("archive", <ArchiveIcon size="regular" aria-hidden="true" />, archived ? t("pageArchive:unarchive") : t("rowMenu:actions.archive"), onArchive) : null}
          <Separator className={styles.divider} />
          {item("new-tab", <OpenInNew size="regular" aria-hidden="true" />, t("rowMenu:actions.openInNewTab"), onOpenInNewTab, menuKeyHint("⌘⇧↵"))}
          {item("side-peek", <SidePeekIcon size="regular" aria-hidden="true" />, t("rowMenu:actions.openInSidePeek"), onOpenInSidePeek, menuKeyHint("⌥Click"))}
        </NativeActionMenu>
        {edited ? <RowMenuEditedFoot name={edited.name} editorId={edited.editorId} at={edited.at} marker="favorite-database-edited" /> : null}
      </div>
    </>,
    document.body,
  );
}
