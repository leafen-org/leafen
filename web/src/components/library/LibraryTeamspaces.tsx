import { Fragment, useEffect, useMemo, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { CaretRightFill, DotsHorizontal, DragHandleIcon, MoveToTopIcon, Plus } from "@/icons/leafen";
import { sortSidebarPages } from "@/components/sidebar/sidebarLayoutModel";
import { TeamspaceDialog } from "@/components/TeamspaceDialog";
import { TeamspaceKindIcon } from "@/components/teamspace/TeamspaceSecurityIcon";
import { TopBar } from "@/components/page/TopBar";
import { openAddPageWindow } from "@/components/sidebar/addPageWindowStore";
import { useSidebarLayout } from "@/components/sidebar/useSidebarLayout";
import { useSidebarLayout as useShellLayout } from "@/components/shell/AppShell";
import { useSidebarTeamspaces } from "@/components/sidebar/useSidebarTeamspaces";
import { useSidebarTeamspaceActions, type SidebarTeamspaceAction } from "@/components/sidebar/useSidebarTeamspaceActions";
import { canAddTeamspacePage, movedTeamspaceOrder, orderTeamspaces, teamspaceSecurity, type DialogTeamspace } from "@/components/teamspace/teamspaceDialogModel";
import { teamspaceGlyph } from "@/components/teamspace/KeptIconGlyph";
import { setDocumentChrome } from "@/lib/documentChrome";
import { useStoredIconFaviconHref } from "@/components/page/pageFavicon";
import { libraryHref, pushRoute, type LibraryViewId } from "@/lib/router";
import { useDragReorder } from "@/lib/useDragReorder";
import type { Page, Session, Workspace } from "@/types";
import type { LibraryPage } from "@/workspaceMembershipWire.generated";
import { libraryChildPages, libraryParentIds, libraryTreeRows, type LibraryColumn } from "./libraryModel";
import { LibraryRows, LibrarySelectBox } from "./LibraryRows";
import { LibrarySearch } from "./LibrarySearch";
import { LibraryTeamspaceBrowseRow, LibraryTeamspaceDescription, LibraryTeamspaceGroup, LibraryTeamspaceGroupBody, LibraryTeamspaceGroupEmpty, LibraryTeamspaceHead } from "./LibraryTeamspaceBrowse";
import { LibraryViewTabs, type LibraryViewListActions } from "./LibraryViewTabs";
import panelStyles from "./LibraryPanel.module.css";
import selectionStyles from "./LibrarySelection.module.css";
import styles from "./LibraryView.module.css";
import teamspaceStyles from "./LibraryTeamspaces.module.css";

type OpenDialog = { view: "create" } | { view: "manage"; teamspaceId: string } | null;

// A teamspace's pages are rows of the other views' kind with only their names,
// their "열기" under the teamspace's glyph, as Notion's (seen 2026-10-07).
const NAME_ONLY: readonly LibraryColumn[] = ["name"];
const NO_DETAILS = new Map<string, LibraryPage>();
const NO_VISITS = new Map<string, string>();
const NO_SOURCES = { teamspace: () => "", private: "" };
const PAGE_INDENT = 20;

/**
 * The library's teamspaces, as Notion's (`/library/teamspaces`, seen
 * 2026-10-07): "새 팀스페이스" beside the title, then a table of the teamspaces
 * the reader is in — name, description, kind and members, across the page —
 * in the reader's own sidebar order. A row's handle drags it to another
 * place, which the sidebar follows; "▶" lists its pages in place, each
 * opening as the other views' rows do, with "+ 새로 추가" (the add window,
 * as the sidebar's); "…" opens its
 * settings. Each column's head has its filled mark, and "검색" at the tabs'
 * right finds teamspaces by name, as Notion's (seen 2026-10-07) — gone from a
 * narrow row — beside "설정", whose "보관된 페이지 포함" lists the archived
 * pages too (seen 2026-11-14 at 1440). Notion's "열기", which opens a
 * teamspace's home page, isn't here: Leafen makes no home page. Where the
 * reader may join others, the table folds into "내 팀스페이스" and, folded
 * at first, "팀스페이스 더 보기" with those, each joined from its "⋯" — then
 * last in the reader's sidebar order, said at the window's foot, its row
 * among their own (Notion's, seen 2026-11-12; LibraryTeamspaceBrowse) — each
 * open group with the table's head under its name (Notion's code, read
 * 2026-11-16).
 */
export function LibraryTeamspaces({ session, workspace, views, list, pages, addFailed, settingsTool, settingsPanel, onOpenPage, onAddInside }: {
  session: Session; workspace: Workspace; views: readonly LibraryViewId[]; list?: LibraryViewListActions;
  /** The workspace's pages the view lists, of which each teamspace lists its own. */
  pages: readonly Page[];
  /** "설정" at the row's end, and the settings it opens at the table's right, as the other views'. */
  settingsTool: ReactNode;
  settingsPanel: ReactNode;
  onOpenPage: (page: Page, newTab: boolean) => void;
  /** Makes a page inside an open page, as the other views' "새 페이지 추가하기". */
  onAddInside: (parentId: string) => void;
  /** What the last page made inside one said when it couldn't be (addFailedNotice). */
  addFailed: string | null;
}) {
  const { t } = useTranslation(["library", "teamspaceDialog", "pageTreeItem"]);
  const teamspaces = useSidebarTeamspaces({ session, workspace });
  const sidebar = useSidebarLayout(session, workspace.id);
  const { phone, touch } = useShellLayout(true);
  const [dialog, setDialog] = useState<OpenDialog>(null);
  const [open, setOpen] = useState<ReadonlySet<string>>(() => new Set());
  const [openPages, setOpenPages] = useState<ReadonlySet<string>>(() => new Set());
  const parents = useMemo(() => libraryParentIds(pages), [pages]);
  const children = useMemo(() => libraryChildPages(pages), [pages]);
  const [searching, setSearching] = useState(false);
  const [query, setQuery] = useState("");
  const [narrow, setNarrow] = useState(false);
  const [selected, setSelected] = useState<ReadonlySet<string>>(() => new Set());
  const [mineOpen, setMineOpen] = useState(true);
  const [moreOpen, setMoreOpen] = useState(false);
  const ordered = orderTeamspaces(teamspaces.joinedTeamspaces, sidebar.layout.teamspaceOrder);
  const needle = query.trim().toLowerCase();
  const shown = needle ? ordered.filter((teamspace) => teamspace.name.toLowerCase().includes(needle)) : ordered;
  const browse = needle ? teamspaces.browseTeamspaces.filter((teamspace) => teamspace.name.toLowerCase().includes(needle))
    : teamspaces.browseTeamspaces;
  const grouped = teamspaces.browseTeamspaces.length > 0;
  const actions = useSidebarTeamspaceActions({ session, workspace, teamspaces: [...ordered, ...teamspaces.browseTeamspaces],
    onChanged: teamspaces.refresh, onOpenSettings: (teamspaceId) => setDialog({ view: "manage", teamspaceId }) });
  const chosen = shown.filter((teamspace) => selected.has(teamspace.id));
  const allChosen = shown.length > 0 && chosen.length === shown.length;
  const reorder = useDragReorder("library-teamspace", (movedId, targetId, side) => {
    const target = ordered.findIndex((teamspace) => teamspace.id === targetId);
    const from = ordered.findIndex((teamspace) => teamspace.id === movedId);
    if (target < 0 || from < 0) return;
    // Taken out first, a row moved down lands one place earlier.
    const index = target + (side === "after" ? 1 : 0) - (from < target ? 1 : 0);
    sidebar.change({ ...sidebar.layout, teamspaceOrder: movedTeamspaceOrder(ordered, movedId, index) });
  });

  const favicon = useStoredIconFaviconHref(workspace.icon, workspace.id);
  useEffect(() => {
    setDocumentChrome({ title: t("library:title"), iconHref: favicon });
  }, [t, favicon]);
  useEffect(() => {
    if (!narrow) return;
    setQuery("");
    setSearching(false);
  }, [narrow]);

  // Escape lets the chosen teamspaces go, as Notion's, unless a field or a window has the key.
  useEffect(() => {
    if (selected.size === 0 || dialog) return undefined;
    const keydown = (event: globalThis.KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      if (event.key !== "Escape" || event.defaultPrevented || target?.closest("input:not([type='checkbox']), textarea, [role='dialog'], [role='menu']")) return;
      event.preventDefault();
      setSelected(new Set());
    };
    document.addEventListener("keydown", keydown);
    return () => document.removeEventListener("keydown", keydown);
  }, [dialog, selected.size]);

  // "팀스페이스 상단으로 이동" puts the chosen teamspaces first in the
  // reader's sidebar order, in the order they were.
  function moveChosenToTop() {
    const ids = new Set(chosen.map((teamspace) => teamspace.id));
    const order = [...ordered.filter((teamspace) => ids.has(teamspace.id)), ...ordered.filter((teamspace) => !ids.has(teamspace.id))];
    sidebar.change({ ...sidebar.layout, teamspaceOrder: order.map((teamspace) => teamspace.id) });
    setSelected(new Set());
  }

  function closeSearch() {
    setQuery("");
    setSearching(false);
  }

  // One joined here comes last in the reader's order, as Notion's sidebar
  // puts it at the section's end (seen 2026-11-12); put there before the
  // server answers, which the order keeps harmlessly if it refuses.
  function act(teamspace: DialogTeamspace, action: SidebarTeamspaceAction) {
    if (action === "join") sidebar.change({ ...sidebar.layout, teamspaceOrder: [...ordered.map((joined) => joined.id), teamspace.id] });
    void actions.act(teamspace, action);
  }

  // A phone's has "검색" alone, as Notion's phone (seen 2026-11-16 at 402).
  const tabActions = (
    <div className={panelStyles.tabActions} data-library-tab-actions>
      {narrow ? null : <LibrarySearch open={searching} value={query} placeholder={t("library:searchPlaceholder")}
        onOpen={() => setSearching(true)} onChange={setQuery} onClose={closeSearch} />}
      {phone ? null : settingsTool}
    </div>
  );

  const flip = (id: string) => (current: ReadonlySet<string>) => {
    const next = new Set(current);
    if (!next.delete(id)) next.add(id);
    return next;
  };

  // An open teamspace's pages under its row, with "+ 새로 추가" where the
  // reader may add one; a teamspace to join opens to its pages too, as
  // Notion's (its code, read 2026-11-16: every row's ▶ lists its team's pages).
  function teamspacePages(teamspace: DialogTeamspace) {
    const roots = sortSidebarPages(pages.filter((page) => page.teamspaceId === teamspace.id && !page.parentId && !page.inTrash), "manual");
    return <>
      <LibraryRows tree={libraryTreeRows(roots, children, openPages)} columns={NAME_ONLY} pages={pages} sharedPageIds={[]}
        details={NO_DETAILS} visitedAt={NO_VISITS} sourceNames={NO_SOURCES} wrapped={[]} open={openPages}
        parents={parents} onToggle={(pageId) => setOpenPages(flip(pageId))} onOpenPage={onOpenPage}
        onAddInside={onAddInside} indentFrom={PAGE_INDENT} />
      {canAddTeamspacePage(teamspace) ? (
        <div className={styles.row} role="row" data-library-teamspace-add={teamspace.id}>
          <span className={`${styles.nameColumn} ${teamspaceStyles.add}`} role="cell">
            <button type="button" className={panelStyles.addInside}
              onClick={() => openAddPageWindow({ place: { kind: "teamspace", teamspaceId: teamspace.id }, mode: "page" })}>
              <Plus size="small" aria-hidden="true" /><span>{t("library:addTeamspacePage")}</span>
            </button>
          </span>
        </div>
      ) : null}
    </>;
  }

  // Grouped, each open group's head and rows stand in a body of their own.
  const wrapGroup = (group: "mine" | "more", body: ReactNode) =>
    grouped ? <LibraryTeamspaceGroupBody group={group}>{body}</LibraryTeamspaceGroupBody> : body;

  // A group with nothing to list says so under its head, each its own way
  // (LibraryTeamspaceGroupEmpty) — the reader's own too when there are none
  // to join and the table isn't grouped, as Notion's (its code, read 2026-11-17).
  function groupEmpty(group: "mine" | "more") {
    return <LibraryTeamspaceGroupEmpty group={group} onClearSearch={needle ? () => setQuery("") : undefined}
      onNew={teamspaces.canCreateTeamspace ? () => setDialog({ view: "create" }) : undefined} />;
  }

  function closeDialog() {
    setDialog(null);
    teamspaces.refresh();
  }

  return (
    <>
      {/* The bar names nothing; the page heads itself, as Notion's library (seen 2026-10-31). */}
      <TopBar title={t("library:title")} crumbs={<></>} />
      <div className={styles.wrap} data-library-view="teamspaces">
        <header className={styles.header}>
          <h1 className={styles.title}>{t("library:title")}</h1>
          {teamspaces.canCreateTeamspace ? (
            <button type="button" className={styles.newPage} data-library-new-teamspace onClick={() => setDialog({ view: "create" })}>
              {t("library:newTeamspace")}
            </button>
          ) : null}
        </header>
        <LibraryViewTabs views={views} current="teamspaces" onSelect={(next) => pushRoute(libraryHref(next))} actions={tabActions}
          list={list} onNarrow={setNarrow} />
        {chosen.length > 0 ? (
          <div className={`${selectionStyles.selectionAnchor} ${selectionStyles.teamspaceAnchor}`} data-grouped={grouped ? "true" : undefined}>
            <div className={selectionStyles.selectionBar} role="toolbar" aria-label={t("library:selection.actions", { count: chosen.length })}
              data-library-selection-bar>
              <button type="button" className={selectionStyles.selectionCount} data-library-selection-count
                aria-label={t("library:selection.clear", { count: chosen.length })} onClick={() => setSelected(new Set())}>
                {t("library:selection.count", { count: chosen.length })}
              </button>
              <button type="button" className={selectionStyles.selectionAction} data-library-teamspaces-top onClick={moveChosenToTop}>
                <MoveToTopIcon size="regular" trim aria-hidden="true" />{t("library:selection.moveTeamspacesTop")}
              </button>
            </div>
          </div>
        ) : null}
        <div className={panelStyles.body}>
          <div className={styles.tableScroll}>
            <div className={`${styles.table} ${styles.teamspaceTable} ${teamspaceStyles.table}`} role="table" aria-label={t("library:table", { view: t("library:views.teamspaces") })}>
              {grouped ? <LibraryTeamspaceGroup group="mine" open={mineOpen} onToggle={() => setMineOpen((open) => !open)} /> : null}
              {grouped && !mineOpen ? null : wrapGroup("mine", <>
              <LibraryTeamspaceHead selectBox={<LibrarySelectBox checked={allChosen} indeterminate={chosen.length > 0 && !allChosen}
                shown={chosen.length > 0} label={t("library:selection.selectAll")}
                onChange={() => setSelected(allChosen ? new Set() : new Set(shown.map((teamspace) => teamspace.id)))} />} />
              {shown.map((teamspace, index) => {
                const expanded = open.has(teamspace.id);
                return (
                  <Fragment key={teamspace.id}>
                    <div className={teamspaceStyles.row} role="row" data-library-teamspace={teamspace.id} {...reorder.row(teamspace.id)}
                      data-selected={selected.has(teamspace.id) ? "true" : undefined}>
                      <button type="button" className={teamspaceStyles.handle} aria-label={t("library:teamspaceHandle", { name: teamspace.name })}
                        {...reorder.handle(teamspace.id, true, { above: shown[index - 1]?.id, below: shown[index + 1]?.id })}>
                        <DragHandleIcon size="regular" aria-hidden="true" />
                      </button>
                      <LibrarySelectBox checked={selected.has(teamspace.id)} label={t("library:selection.select", { title: teamspace.name })}
                        onChange={(next) => setSelected((current) => {
                          const updated = new Set(current);
                          if (next) updated.add(teamspace.id);
                          else updated.delete(teamspace.id);
                          return updated;
                        })} />
                      <span className={`${styles.nameColumn} ${teamspaceStyles.nameCell}`} role="cell">
                        <button type="button" className={`${panelStyles.rowToggle} ${teamspaceStyles.toggle}`} aria-expanded={expanded}
                          aria-label={t("library:openTeamspace", { name: teamspace.name })} data-library-teamspace-toggle={teamspace.id}
                          onClick={() => setOpen(flip(teamspace.id))}>
                          <CaretRightFill aria-hidden="true" />
                        </button>
                        <span className={teamspaceStyles.glyph} aria-hidden="true">{teamspaceGlyph(teamspace)}</span>
                        <span className={teamspaceStyles.name}>{teamspace.name}</span>
                        <button type="button" className={teamspaceStyles.more} aria-label={t("library:teamspaceSettings", { name: teamspace.name })}
                          data-library-teamspace-settings onClick={() => setDialog({ view: "manage", teamspaceId: teamspace.id })}>
                          <DotsHorizontal size="small" aria-hidden="true" />
                        </button>
                      </span>
                      <LibraryTeamspaceDescription teamspace={teamspace} />
                      <span className={`${styles.column} ${teamspaceStyles.security}`} role="cell">
                        <TeamspaceKindIcon security={teamspaceSecurity(teamspace)} size="regular" />
                        <span className={styles.cellText}>{t(`teamspaceDialog:security.${teamspaceSecurity(teamspace)}`)}</span>
                      </span>
                      <span className={`${styles.column} ${teamspaceStyles.members}`} role="cell">{teamspace.summary.memberCount}</span>
                    </div>
                    {expanded ? teamspacePages(teamspace) : null}
                  </Fragment>
                );
              })}
              {teamspaces.loaded && shown.length === 0 ? groupEmpty("mine") : null}
              </>)}
              {grouped ? <LibraryTeamspaceGroup group="more" open={moreOpen} onToggle={() => setMoreOpen((open) => !open)} /> : null}
              {grouped && moreOpen ? wrapGroup("more", <>
                <LibraryTeamspaceHead />
                {browse.map((teamspace) => (
                  <Fragment key={teamspace.id}>
                    <LibraryTeamspaceBrowseRow teamspace={teamspace} canCreate={teamspaces.canCreateTeamspace}
                      expanded={open.has(teamspace.id)} onToggle={() => setOpen(flip(teamspace.id))} touch={touch}
                      onManage={(teamspaceId) => setDialog({ view: "manage", teamspaceId })} onAct={act} />
                    {open.has(teamspace.id) ? teamspacePages(teamspace) : null}
                  </Fragment>
                ))}
                {browse.length === 0 ? groupEmpty("more") : null}
              </>) : null}
            </div>
            {addFailed ? <p className={styles.empty} role="status">{addFailed}</p> : null}
          </div>
          {settingsPanel}
        </div>
      </div>
      {actions.archiveConfirm}
      {dialog ? (
        <TeamspaceDialog session={session} workspace={workspace} initialView={dialog.view}
          initialTeamspaceId={dialog.view === "manage" ? dialog.teamspaceId : undefined} onClose={closeDialog} />
      ) : null}
    </>
  );
}
