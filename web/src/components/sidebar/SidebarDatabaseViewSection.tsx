import { useEffect, useMemo, useRef, useState, type MouseEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { compareGraphOrder, type ContentGraph, type GraphRow } from "@/contentGraph";
import { galleryPresentationPage } from "@/components/database/galleryViewModel";
import { projectRows } from "@/components/database/viewRowProjection";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { PageRenamePopover, renamePopoverAt } from "@/components/page/PageRenamePopover";
import { TitleWithCustomEmoji } from "@/components/page/TitleWithCustomEmoji";
import { presentationBefore, type PagePresentationChange } from "@/components/page/pagePresentation";
import { DotsHorizontal, OpenInNew, Plus, SidebarChevronDown } from "@/icons/leafen";
import { inlineDatabaseFullPageHref } from "@/lib/inlineDatabaseFullPage";
import { pageHref } from "@/lib/navigation";
import { usePageNames } from "@/lib/pageNames";
import { sourceUntitledRowName } from "@/lib/untitledRowNames";
import { pushRoute, usePathname, useSearchParams } from "@/lib/router";
import type { Page, Session } from "@/types";
import type { SidebarLayoutView } from "@/workspaceMembershipWire.generated";
import { useHolderGraph } from "./sidebarHolderGraph";
import { useRowsShownBeside } from "@/lib/rowsShownAtPress";
import { sidebarViewOpenMode } from "./sidebarLayoutModel";
import { openSidebarPageIn } from "./sidebarShortcutOpen";
import { holdTypingForField } from "@/lib/pageTitleFocus";
import {
  changeSidebarViewRowIcon, createSidebarViewRow, focusNewRowTitle, renameSidebarViewRow, sameSidebarViewRowName, sidebarViewRowName,
  sidebarViewRowNameChange, type SidebarViewRowName, useSidebarViewTemplateAhead, viewWithRowsPlaced,
} from "./sidebarViewNewRow";
import { useOpenPageGraphStore } from "@/lib/pageGraphStore";
import { renameShowing, type ShownChange } from "@/lib/renameShownAtPress";
import type { TextSpan } from "@/lib/types";
import { notePageChangeStep } from "@/components/editor/pageUndo";
import { SidebarViewRowControls } from "./SidebarViewRowControls";
import { DataSourceMark } from "../database/DataSourceMark";
import { styles } from "./sidebarStyles";
import { useSidebarViewRowActions } from "./useSidebarViewRowActions";
import { onTreeRowKeyDown } from "./pageTreeDom";
import { viewDisplayName } from "@/components/database/databaseViewShared";
import { graphDataSourceName } from "@/lib/dataSourceGraphName";

/**
 * The view a database view section shows: the one it names, or, once that
 * view is deleted, the first one left of the same data source, so the
 * section goes on listing its database's rows instead of a dead end
 * (2026-11-09: "이 보기를 열 수 없습니다." stayed after its view was deleted).
 */
function sidebarSectionShownView(view: SidebarLayoutView, graph: ContentGraph | null) {
  const same = graph?.views.filter((candidate) => candidate.dataSourceId === view.dataSourceId) ?? [];
  return same.find((candidate) => candidate.id === view.viewId)
    ?? [...same].sort((left, right) => (left.position < right.position ? -1 : left.position > right.position ? 1 : 0))[0];
}

/**
 * A view section's heading: its own name, or "<view> · <database>", an
 * untitled source named as Notion's (seen 2026-11-18, twice): "새
 * 데이터베이스" as its database's only one, "새 데이터 소스" as one of
 * several (graphDataSourceName); `fallback` while its database is read, as
 * Notion's "데이터베이스 보기" before its heading is drawn.
 */
export function sidebarViewHeading(view: SidebarLayoutView, graph: ContentGraph | null, fallback: string) {
  if (view.name.trim()) return view.name.trim();
  const source = graph?.dataSources.find((candidate) => candidate.id === view.dataSourceId);
  const shown = sidebarSectionShownView(view, graph);
  return graph && source && shown ? `${viewDisplayName(shown)} · ${graphDataSourceName(graph, source)}` : fallback;
}

/** A view section's name in the sidebar's customize mode, read as its heading reads. */
export function SidebarViewSectionName({ view, session, fallback }: { view: SidebarLayoutView; session: Session; fallback: string }) {
  const read = useHolderGraph(session, view.pageId);
  return <>{sidebarViewHeading(view, read.status === "ready" ? read.graph : null, fallback)}</>;
}

/**
 * A view section's mark in the customize list: its source's own icon, else
 * the cylinder (DataSourceMark), nothing while its database is read.
 */
export function SidebarViewSectionIcon({ view, session, size = "small" }: { view: SidebarLayoutView; session: Session; size?: "small" | "regular" }) {
  const read = useHolderGraph(session, view.pageId);
  if (read.status === "loading") return <span className={styles.viewSectionIconWaiting} />;
  const source = read.status === "ready" ? read.graph.dataSources.find(({ id }) => id === view.dataSourceId) : undefined;
  return <DataSourceMark source={source ?? {}} size={size} />;
}

/**
 * Where a view section's "데이터베이스 열기" goes: a full-page database's
 * page, or an inline database opened alone, at the section's view.
 */
export function sidebarViewDatabaseHref(view: SidebarLayoutView, graph: ContentGraph | null, holder: Page | undefined) {
  const viewId = sidebarSectionShownView(view, graph)?.id ?? view.viewId;
  if (holder?.kind === "database") return pageHref(view.pageId, new URLSearchParams({ v: viewId }));
  const block = graph?.blocks.find((candidate) => candidate.type === "inline_database" && candidate.content.dataSourceId === view.dataSourceId);
  return block ? inlineDatabaseFullPageHref(view.pageId, block.id, viewId) : pageHref(view.pageId);
}

/**
 * A database view's rows as a sidebar section, as Notion's (seen 2026-10-01
 * and 2026-10-02): a 12px gray heading "<view> · <database>" that folds the
 * section, and under the pointer "데이터베이스 열기", "새 페이지" — a new
 * row first among them, opened where the section opens its rows — and "…";
 * then the rows the view shows, in its order, as page rows with their icons,
 * up to the section's count and "모두 보기" past it, which opens the
 * database at the section's view, each row opening beside the
 * open page, over its center or as a full page as "다음에서 열기" says, with
 * a page row's "…" and "+" at their ends under the pointer.
 */
export function SidebarDatabaseViewSection({ view, limit, session, workspaceId, currentUserId, holder, menu }: {
  view: SidebarLayoutView;
  limit: number;
  session: Session;
  workspaceId: string;
  currentUserId: string;
  /** The page holding the database, when the reader's pages have it. */
  holder?: Page;
  /** The section's "…", given what the section read. */
  menu: (graph: ContentGraph | null) => ReactNode;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["sidebar", "pageTreeItem"]);
  const [revision, setRevision] = useState(0);
  // The rows are read again as the address changes — a row's page opened
  // from here and closed, or another page opened — so names changed there show.
  const address = `${usePathname()}?${useSearchParams().toString()}`;
  const read = useHolderGraph(session, view.pageId, true, `${revision}:${address}`);
  const [collapsed, setCollapsed] = useState(false);
  const [creating, setCreating] = useState(false);
  // The row whose name the rename popover changes, opened at its "…" as a sidebar page's is.
  const [renaming, setRenaming] = useState<{ rowId: string; at: { top: number; left: number } } | null>(null);
  // A row moved, sent to the trash or brought back anywhere in the app —
  // here, the open page's table, a peek, a favorite — shows so here at the
  // press, as on the open page (useRowsShownBeside).
  const graph = useRowsShownBeside(read.status === "ready" ? read.graph : null);
  // Its template is read ahead, so "새 페이지" opens with it at once.
  useSidebarViewTemplateAhead(session, view, read.status === "ready" && read.canEdit);
  const shownView = sidebarSectionShownView(view, graph);
  // A row made here shows first in the section at once, until the section
  // reads the server's own rows (user decision 2026-11-12: a press answers
  // on the next frame).
  const [made, setMade] = useState<readonly GraphRow[]>([]);
  useEffect(() => {
    if (!graph) return;
    const held = new Set(graph.rows.map(({ id }) => id));
    setMade((current) => current.some(({ id }) => held.has(id)) ? current.filter(({ id }) => !held.has(id)) : current);
  }, [graph]);
  const properties = useMemo(() => graph?.properties.filter((property) => property.dataSourceId === view.dataSourceId) ?? [], [graph, view.dataSourceId]);
  const titleProperty = properties.find((property) => property.type === "title");
  // A row without a name goes by its database's name for them, its kind's word last, as its path names it (untitledRowNames.ts).
  const shownSource = graph?.dataSources.find((source) => source.id === view.dataSourceId);
  const untitledName = shownSource ? sourceUntitledRowName(shownSource) : undefined;
  // Worked out as the rows or the view change, not as the section's menus open and close.
  const rows = useMemo(() => {
    if (!graph || !shownView) return [];
    const held = new Set(graph.rows.map(({ id }) => id));
    const madeNow = made.filter(({ id }) => !held.has(id));
    const shownRows = (madeNow.length ? [...graph.rows, ...madeNow].sort(compareGraphOrder) : graph.rows)
      .filter((row) => row.dataSourceId === view.dataSourceId && !row.archivedAt && !row.subitemParentId);
    return projectRows({
      currentUserId, pageId: view.pageId, properties, workspaceId, rows: shownRows,
      // A row made here goes in its place among them, first for "새 페이지", in the view's own order too.
      view: madeNow.length ? viewWithRowsPlaced(shownView, shownRows, madeNow.map(({ id }) => id)) ?? shownView : shownView,
    });
  }, [currentUserId, graph, made, properties, shownView, view.dataSourceId, view.pageId, workspaceId]);
  const listed = rows.slice(0, limit);
  const titles = new Map(rows.map((row, index) => [
    row.id, pageNames.title({ ...galleryPresentationPage({ row, titleProperty, workspaceId, position: index }), untitledName }),
  ]));
  // A rename is one step of the ⌘Z of the page holding the database, from
  // the name before the popover opened to the last one it saved, as Notion's
  // (seen 2026-11-09); a step reads the rows as they are when it is taken.
  const renamed = useRef<{ rowId: string; before: SidebarViewRowName; last: Promise<SidebarViewRowName | null> } | null>(null);
  // A name is written on the rows as the server gave them, not as shown:
  // shown, a typed name was already the row's and went unsaved.
  const served = read.status === "ready" ? read.graph : null;
  const latestServed = useRef(served);
  latestServed.current = served;
  // A row's name typed and icon picked show at once here, on the open page
  // and wherever the row is drawn, until the server answers; refused, they
  // go back (lib/renameShownAtPress.ts). One showing for the popover open.
  const store = useOpenPageGraphStore();
  const rowShowing = useRef<{ rowId: string; by: ReturnType<typeof renameShowing> } | null>(null);
  const openShowing = (rowId: string) => rowShowing.current?.rowId === rowId ? rowShowing.current.by : undefined;
  const showingOf = (rowId: string) => renameShowing(store, (): ShownChange[] => {
    const row = latestServed.current?.rows.find(({ id }) => id === rowId);
    return row ? [{ kind: "upsertRow", row }, { kind: "rowIcon", rowId, icon: row.icon ?? "", iconType: row.iconType ?? "none" }] : [];
  });
  const rowActions = useSidebarViewRowActions({
    session, workspaceId, rowTitle: (rowId) => titles.get(rowId) ?? "", onChanged: () => setRevision((value) => value + 1),
    rowMark: (rowId) => {
      const row = rows.find(({ id }) => id === rowId);
      return { icon: row?.icon, iconType: row?.iconType };
    },
    onRename: (rowId) => {
      const before = graph ? sidebarViewRowName(view, graph, rowId) : null;
      renamed.current = before === null ? null : { rowId, before, last: Promise.resolve(null) };
      rowShowing.current = { rowId, by: showingOf(rowId) };
      setRenaming({ rowId, at: renamePopoverAt(document.querySelector(
      `[data-sidebar-view-row="${CSS.escape(rowId)}"]`)?.parentElement?.querySelector("[data-sidebar-view-row-more]")) ?? { top: 16, left: 12 } });
    },
  });
  const heading = sidebarViewHeading(view, graph, t("databaseView"));
  const databaseHref = sidebarViewDatabaseHref(view, graph, holder);
  const openRow = (rowId: string) => openSidebarPageIn(rowId, sidebarViewOpenMode(view), { pages: [], openPage: () => {} });

  async function addRow() {
    if (!graph || creating || read.status !== "ready" || !read.canEdit) return;
    setCreating(true);
    try {
      // What is typed before its page opens goes into its title.
      await holdTypingForField(async () => {
        // Made here, it shows first in the section at once, and the section reads its rows again once the server has it.
        let madeHere = false;
        const { rowId, kept } = await createSidebarViewRow(session, view, graph, { workspaceId, shown: (row, saved) => {
          madeHere = true;
          setMade((current) => [...current, row]);
          void saved.then((ok) => {
            if (!ok) setMade((current) => current.filter(({ id }) => id !== row.id));
            setRevision((value) => value + 1);
          });
        } });
        if (!madeHere) setRevision((value) => value + 1);
        // One kept for a server out of reach shows here until it is sent:
        // its page is read from the server, and can't open meanwhile.
        if (kept) return false;
        openRow(rowId);
        focusNewRowTitle(rowId);
        return true;
      });
    } catch {
      // The section stays as it was; the database's page says why.
    } finally {
      setCreating(false);
    }
  }

  // Each key's name shows at once, saved once the typing stops.
  function showRowTitle(rowId: string, title: string, spans?: TextSpan[]) {
    const change = served && sidebarViewRowNameChange(view, served, rowId, { title, ...(spans ? { spans } : {}) });
    if (change) openShowing(rowId)?.show([change.operation]);
  }
  // A name or ⌘Z's step written, shown from the press until the server answers.
  function writeRowName(rowId: string, now: ContentGraph, name: SidebarViewRowName, again = false, by = showingOf(rowId)) {
    const change = sidebarViewRowNameChange(view, now, rowId, name);
    const saving = renameSidebarViewRow(session, view, now, rowId, name, again);
    if (change) {
      if (by !== openShowing(rowId)) by.show([change.operation]);
      by.saving([change.operation], saving);
    }
    return saving;
  }
  // The name saves as it is typed and when the popover closes; the row keeps its focus after.
  async function saveRowTitle(rowId: string, title: string, spans?: TextSpan[]) {
    if (!served) return;
    const saving = writeRowName(rowId, served, { title, ...(spans ? { spans } : {}) }, false, openShowing(rowId));
    const rename = renamed.current;
    if (rename?.rowId === rowId) {
      const before = rename.last;
      rename.last = Promise.all([before, saving.catch(() => null)]).then(([earlier, written]) => written ?? earlier);
    }
    try {
      if (await saving === null) return;
      setRevision((value) => value + 1);
    } catch {
      // The row keeps its name; its page says why it can't be changed.
    }
  }
  // The icon picked is a step of the history, as a rename is.
  async function saveRowIcon(rowId: string, change: PagePresentationChange, before?: PagePresentationChange) {
    try {
      if (change.iconType === undefined) return false;
      // Shown in the press's frame here, on the open page right behind.
      const shown: ShownChange[] = [{ kind: "rowIcon", rowId, icon: change.icon ?? "", iconType: change.iconType as GraphRow["iconType"] }];
      const by = openShowing(rowId) ?? showingOf(rowId);
      by.show(shown);
      const saving = changeSidebarViewRowIcon(session, rowId, change, view.pageId);
      by.saving(shown, saving);
      if (!await saving) return false;
      setRevision((value) => value + 1);
      if (before) notePageChangeStep(rowId, before, change, (step) => saveRowIcon(rowId, step));
      return true;
    } catch {
      // The row keeps its icon; its page says why it can't be changed.
      return false;
    }
  }
  // Closed, the popover leaves focus to the page, as Notion's does.
  function endRename() {
    setRenaming(null);
    rowShowing.current?.by.end();
    rowShowing.current = null;
    const rename = renamed.current;
    renamed.current = null;
    // Taken once the popover's last save is, which its closing may have started.
    void rename?.last.then((after) => {
      if (after === null || sameSidebarViewRowName(after, rename.before)) return;
      notePageChangeStep(view.pageId, rename.before, after, async (name) => {
        const now = latestServed.current;
        if (!now || await writeRowName(rename.rowId, now, name, true) === null) return false;
        setRevision((value) => value + 1);
        return true;
      });
    });
  }

  function openDatabase(event: MouseEvent<HTMLAnchorElement>) {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) return;
    event.preventDefault();
    pushRoute(databaseHref);
  }

  return (
    <section className={styles.section} data-sidebar-view-section={view.key} data-section-folded={collapsed || undefined}>
      <div className={styles.sectionLabel}>
        <button type="button" className={`${styles.sectionToggle} ${styles.privateSectionToggle}`} aria-expanded={!collapsed}
          data-sidebar-view-toggle onClick={() => setCollapsed((value) => !value)}>
          <span className={`${styles.sectionTitle} ${styles.viewSectionTitle}`}>{heading}</span>
          <SidebarChevronDown className={styles.sectionChevron} size="micro" aria-hidden="true" />
        </button>
        <div className={styles.sectionActions} data-section-actions="view">
          <a className={styles.sectionAction} href={databaseHref} aria-label={t("openDatabase")} title={t("openDatabase")}
            data-sidebar-view-open-database onClick={openDatabase}>
            <OpenInNew size="small" aria-hidden="true" />
          </a>
          <button type="button" className={`${styles.sectionAction} ${styles.sectionAdd}`} aria-label={t("newPage")} title={t("newPage")}
            data-sidebar-view-new-row disabled={!graph || !shownView || creating || read.status !== "ready" || !read.canEdit} onClick={() => void addRow()}>
            <Plus size="small" aria-hidden="true" />
          </button>
          {menu(graph)}
        </div>
      </div>
      {collapsed ? null : (
        <div className={styles.tree} role="tree" aria-label={heading}>
          {read.status === "missing" || (graph && !shownView) ? (
            <p className={styles.sectionEmpty}>{t("databaseViewMissing")}</p>
          ) : null}
          {listed.map((row, index) => {
            const page = galleryPresentationPage({ row, titleProperty, workspaceId, position: index });
            const title = titles.get(row.id) ?? pageNames.title(page);
            const icon = view.showIcons ? (
              <span className={styles.viewRowIcon}>
                <PageIconGlyph page={{ ...page, icon: page.icon ?? "", iconType: page.iconType ?? "none" }} size={18} />
              </span>
            ) : null;
            return (
              <div key={row.id} className={styles.viewRowItem} onMouseEnter={() => rowActions.approachRow(row.id)}>
                <button type="button" role="treeitem" className={styles.viewRow} data-sidebar-view-row={row.id}
                  onClick={() => openRow(row.id)} onKeyDown={(event) => onTreeRowKeyDown(event, { open: () => openRow(row.id) })}>
                  {icon}
                  <span className={styles.viewRowTitle}><TitleWithCustomEmoji text={title} named={page} workspaceId={workspaceId} /></span>
                </button>
                {renaming?.rowId === row.id ? (
                  <PageRenamePopover page={{ ...page, icon: page.icon ?? "", iconType: page.iconType ?? "none" }} title={page.title} titleSpans={page.titleSpans} untitled={pageNames.title({ ...page, title: "", untitledName })} at={renaming.at}
                    onTitleChange={(next, spans) => void saveRowTitle(row.id, next, spans)} onTitleShown={(next, spans) => showRowTitle(row.id, next, spans)} onClose={endRename}
                    onPresentationChange={read.status === "ready" && read.canEdit ? (change) => void saveRowIcon(row.id, change, presentationBefore({ icon: page.icon ?? "", iconType: page.iconType ?? "none" }, change)) : undefined} />
                ) : null}
                <SidebarViewRowControls rowId={row.id} title={title}
                  onMenu={(trigger, point) => rowActions.openRowMenu(row.id, trigger, point)}
                  onAdd={() => void rowActions.addSubpage(row.id)} />
              </div>
            );
          })}
          {rowActions.rowMenu}
          {/* Past its count "모두 보기" opens the database at the section's
              view, as "데이터베이스 열기" does, the section left as it is, as
              Notion's (seen 2026-11-13: a full page, whatever the section
              opens its rows in). */}
          {rows.length > limit ? (
            <button type="button" className={`${styles.showAll} ${styles.viewShowAll}`} data-sidebar-view-show-all
              onClick={() => pushRoute(databaseHref)}>
              <DotsHorizontal size="regular" aria-hidden="true" />
              <span>{t("showAll")}</span>
            </button>
          ) : null}
        </div>
      )}
    </section>
  );
}
