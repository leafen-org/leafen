import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { ContentGraph, GraphView } from "@/contentGraph";
import { databaseViewLabels } from "@/components/database/databaseViewLabels";
import { toolbarTemplateName } from "@/components/database/databaseToolbarModel";
import { databaseUntitledName } from "@/components/database/untitledRowName";
import { keptPageTemplate } from "@/components/database/viewDefaultTemplate";
import { untitledRowNameOf } from "@/lib/untitledRowNames";
import { ViewTypeIcon, viewDisplayName } from "@/components/database/databaseViewShared";
import { i18next } from "@/i18n";
import { graphDataSourceName } from "@/lib/dataSourceGraphName";
import { readDatabaseToolbarConfig } from "@/databaseToolbarConfigApi";
import type { DatabaseToolbarTemplateMetadataWire } from "@/databaseToolbarConfigWire.generated";
import { ComposeIcon, DotsHorizontal, FileText, OpenAsPage, SlidersIcon, TableIcon } from "@/icons/leafen";
import { collectContentGraphStructure } from "@/lib/authenticatedContentGraph";
import { pushRoute, usePathname } from "@/lib/router";
import type { Page, Session } from "@/types";
import { workspaceMembershipBounds, type SidebarLayoutShortcut } from "@/workspaceMembershipWire.generated";
import { SidebarActionMenu, type SidebarMenuRow } from "./SidebarActionMenu";
import { sidebarViewDatabaseHref } from "./SidebarDatabaseViewSection";
import { useHolderGraph } from "./sidebarHolderGraph";
import { addSidebarShortcut, canAddSidebarShortcut, newSidebarTabId, SIDEBAR_HOME_TAB, sidebarShortcutOpenMode } from "./sidebarLayoutModel";
import { changeSidebarLayout } from "./useSidebarLayout";
import { SIDEBAR_OPEN_MODES, sidebarOpenModeIcon, sidebarOpenModeLabel, sidebarShortcutArrangeRows } from "./SidebarShortcutMenu";
import { sidebarShortcutView } from "./sidebarViewNewRow";
import { useSidebarCustomizing } from "./sidebarCustomizing";
import { styles } from "./sidebarStyles";
import { focusOpenerAfterPaint } from "@/lib/overlayLayer";
import { isServerUnreachable, readUntilServerAnswers } from "@/lib/serverReach";
import { useGraphChangesHeard } from "@/lib/sidebarGraphFollow";

const MENU_WIDTH = 240;
const LIST_WIDTH = 240;
type Labels = (key: string, options?: Record<string, unknown>) => string;

/**
 * The names of the databases the tab's new-page shortcuts make pages in, by
 * shortcut, read from the pages holding them — again as the address changes,
 * so a database renamed there shows, and as one of those pages changes on
 * another screen, through the API or by an agent (useGraphChangesHeard): a
 * database renamed so kept its old name in the shortcut until the address
 * changed (2026-11-25). A shortcut not read yet has none. One the server
 * doesn't answer for keeps the name read before and is read again once the
 * server may (readUntilServerAnswers): it lost its name, and got it back
 * only at the next address, after the server was back (2026-11-25).
 */
export function useDatabaseShortcutTitles(session: Session | null, shortcuts: readonly SidebarLayoutShortcut[]): ReadonlyMap<string, string> {
  const key = [...new Set(shortcuts.filter((shortcut) => shortcut.kind === "new_database_page").map((shortcut) => shortcut.pageId))].join("\u0000");
  const holders = useMemo(() => (key ? key.split("\u0000") : []), [key]);
  const heard = useGraphChangesHeard(holders);
  const changes = holders.map((pageId) => heard.get(pageId) ?? 0).join(",");
  const address = usePathname();
  const [sources, setSources] = useState<ReadonlyMap<string, string>>(new Map());
  useEffect(() => {
    if (!session || holders.length === 0) return undefined;
    let current = true;
    const stops = holders.map((pageId) => readUntilServerAnswers(async () => {
      const read = await collectContentGraphStructure(session, pageId, () => current).catch((error: unknown) => {
        if (isServerUnreachable(error)) throw error;
        return null;
      });
      // An untitled one goes by what its database calls it (graphDataSourceName).
      return read ? read.graph.dataSources.map((source) => [`${pageId}\u0000${source.id}`, graphDataSourceName(read.graph, source)] as const) : [];
    }, (found) => setSources((was) => new Map([...[...was].filter(([held]) => !held.startsWith(`${pageId}\u0000`)), ...found]))));
    return () => {
      current = false;
      for (const stop of stops) stop();
    };
  // Read again as the address changes and as the pages holding them change elsewhere.
  }, [address, changes, holders, session]);
  return useMemo(() => new Map(shortcuts.flatMap((shortcut) => {
    const title = sources.get(`${shortcut.pageId}\u0000${shortcut.dataSourceId ?? ""}`);
    return title === undefined ? [] : [[shortcut.key, title] as const];
  })), [shortcuts, sources]);
}

/**
 * "사이드바에 단축키 추가" from a database's template menu, as Notion's (seen
 * 2026-10-30): a shortcut at the end of the sidebar's own making a page in
 * the database, from the view's default template (keptPageTemplate), in the
 * view the menu was opened in. A sidebar holding all it can says so instead.
 * Not known yet (`templateId` null: its toolbar hadn't read the templates),
 * the default is read first, once the server answers, and the shortcut added
 * then: added at once, it kept the empty page for good (2026-11-25).
 */
export async function addDatabaseShortcut(session: Session, workspaceId: string,
  { view, templateId, ...target }: { pageId: string; dataSourceId: string; view: GraphView; templateId: string | null },
  onFull: (message: string) => void) {
  const kept = templateId ?? await new Promise<string>((found, failed) => {
    readUntilServerAnswers(() => readDatabaseToolbarConfig(session, target.pageId, target.dataSourceId).catch((error: unknown) => {
      if (!isServerUnreachable(error)) failed(error);
      throw error;
    }), (config) => found(keptPageTemplate(config.templates, view) ?? ""));
  });
  let full = false;
  await changeSidebarLayout(session, workspaceId, (layout) => {
    full = !canAddSidebarShortcut(layout, SIDEBAR_HOME_TAB);
    return full ? layout : addSidebarShortcut(layout, SIDEBAR_HOME_TAB,
      { key: newSidebarTabId(), kind: "new_database_page", ...target, viewId: view.id, templateId: kept });
  });
  if (full) onFull(i18next.t("sidebar:shortcutsFull", { limit: workspaceMembershipBounds.sidebarShortcutsPerTab }));
}

/** A database's views and templates for a sidebar menu's choices, read while it is open. */
export function useSidebarDatabaseChoices(session: Session, pageId: string, dataSourceId: string, opened: boolean, graph?: ContentGraph | null) {
  const [templates, setTemplates] = useState<readonly DatabaseToolbarTemplateMetadataWire[] | null>(null);
  const read = useHolderGraph(session, pageId, opened && !graph);
  const content = graph ?? (read.status === "ready" ? read.graph : null);
  const views = useMemo(() => (content?.views ?? [])
    .filter((candidate) => candidate.dataSourceId === dataSourceId)
    .sort((left, right) => (left.position < right.position ? -1 : left.position > right.position ? 1 : 0)), [content, dataSourceId]);
  useEffect(() => {
    if (!opened) return undefined;
    const controller = new AbortController();
    void readDatabaseToolbarConfig(session, pageId, dataSourceId, { signal: controller.signal })
      .then((config) => setTemplates(config.templates.filter((candidate) => !candidate.deleted)))
      .catch(() => {
        if (!controller.signal.aborted) setTemplates([]);
      });
    return () => controller.abort();
  }, [opened, session, dataSourceId, pageId]);
  return { content, views, source: content?.dataSources.find((candidate) => candidate.id === dataSourceId), templates };
}

type DatabaseChoices = ReturnType<typeof useSidebarDatabaseChoices>;

/**
 * The menu row choosing a database's view, with "보기 검색…" over the views,
 * as Notion's, each view by the name its tab shows — one never named by its
 * kind, "표" (viewDisplayName), as the section's heading names it.
 */
export function sidebarViewChoiceRow(t: Labels, { key, label, data, choices, viewId, query, onQuery, onChoose }: {
  key: string; label: string; data: Record<string, string>; choices: DatabaseChoices; viewId: string;
  query: string; onQuery(query: string): void; onChoose(viewId: string): void;
}): SidebarMenuRow {
  const needle = query.trim().toLocaleLowerCase();
  const chosen = choices.views.find((candidate) => candidate.id === viewId);
  return { key, label, icon: <TableIcon size="regular" aria-hidden="true" />,
    value: chosen ? viewDisplayName(chosen) : undefined, data,
    submenu: { label, width: LIST_WIDTH, radio: true, data: { "data-sidebar-section-choices": "views" },
      search: { label: t("searchViews"), query, onQuery },
      choices: choices.views.filter((candidate) => viewDisplayName(candidate).toLocaleLowerCase().includes(needle)).map((candidate) => ({
        key: candidate.id, label: viewDisplayName(candidate), icon: <ViewTypeIcon type={candidate.type} size="regular" />,
        checked: candidate.id === viewId, data: { "data-sidebar-view-choice": candidate.id }, run: () => onChoose(candidate.id) })) } };
}

/**
 * The menu row choosing the template a new page starts from, "비어 있음"
 * first, each named as the template menu names it — an untitled one as its
 * database's untitled pages ("새 페이지", a task tracker's "신규 작업"), as
 * Notion's (seen 2026-10-30 and 2026-11-11).
 */
export function sidebarTemplateChoiceRow(t: Labels, { data, choices, templateId, onChoose }: {
  data: Record<string, string>; choices: DatabaseChoices; templateId: string; onChoose(templateId: string): void;
}): SidebarMenuRow {
  const template = choices.templates?.find((candidate) => candidate.id === templateId);
  const untitled = databaseUntitledName(choices.templates ?? [], choices.source?.id)
    || untitledRowNameOf({ parentType: "database", parentId: choices.source?.id ?? null }) || databaseViewLabels().templatePageTitlePlaceholder;
  const nameOf = (candidate: DatabaseToolbarTemplateMetadataWire) => toolbarTemplateName(candidate) || untitled;
  return { key: "templates", label: t("viewTemplate"), icon: <FileText size="regular" aria-hidden="true" />,
    value: template ? nameOf(template) : t("templateEmpty"), data,
    submenu: { label: t("viewTemplate"), width: LIST_WIDTH, radio: true, data: { "data-sidebar-section-choices": "templates" },
      choices: [{ id: "", label: t("templateEmpty") }, ...(choices.templates ?? []).map((candidate) => ({
        id: candidate.id, label: nameOf(candidate),
      }))].map((choice) => ({ key: choice.id || "empty", label: choice.label, icon: <FileText size="regular" aria-hidden="true" />,
        checked: templateId === choice.id, data: { "data-sidebar-view-template": choice.id || "empty" },
        run: () => onChoose(choice.id) })) } };
}

/**
 * A database's new-page shortcut's "…" menu, as Notion's (seen 2026-10-30,
 * the shortcut "사이드바에 단축키 추가" makes from a database's template
 * menu): "단축키 이름 변경", "다음에서 열기" (전체 페이지 보기 at first),
 * "<database> 열기", "템플릿" ("비어 있음" first), "보기에 추가" (the
 * source's views under "보기 검색…"), then "위로 옮기기" and "아래로 옮기기"
 * among the tab's shortcuts, "단축키 제거" and "사이드바 사용자 지정".
 * Notion's "탭으로 이동" goes with its tabs (the one-sidebar decision of
 * 2026-10-29).
 */
export function SidebarDatabaseShortcutMenu({ label, shortcut, databaseTitle, session, holder, onRename, onChange, onMove, onRemove, onCustomize }: {
  label: string;
  shortcut: SidebarLayoutShortcut;
  databaseTitle: string;
  session: Session;
  /** The page holding the database, when the reader's pages have it. */
  holder?: Page;
  onRename(): void;
  onChange(change: Partial<Pick<SidebarLayoutShortcut, "openIn" | "viewId" | "templateId">>): void;
  /** Moves it among the tab's shortcuts, where it has a neighbour that way. */
  onMove: { up?(): void; down?(): void };
  onRemove(): void;
  onCustomize?(): void;
}) {
  const { t } = useTranslation("sidebar");
  const button = useRef<HTMLButtonElement>(null);
  const [opened, setOpened] = useState(false);
  const [viewQuery, setViewQuery] = useState("");
  const customizing = useSidebarCustomizing();
  const view = sidebarShortcutView(shortcut);
  const choices = useSidebarDatabaseChoices(session, view.pageId, view.dataSourceId, opened);
  const mode = sidebarShortcutOpenMode(shortcut);

  function close(restoreFocus = true) {
    setOpened(false);
    if (restoreFocus) focusOpenerAfterPaint(button.current, { preventScroll: true });
  }
  const data = (key: string) => ({ "data-sidebar-shortcut-action": key });
  const rows: SidebarMenuRow[] = [
    { key: "rename", label: t("shortcutRename"), icon: <ComposeIcon size="regular" aria-hidden="true" />, data: data("rename"),
      run: () => { close(false); onRename(); } },
    { key: "open-in", label: t("openIn"), icon: sidebarOpenModeIcon(mode), value: sidebarOpenModeLabel(mode, t), data: data("open-in"),
      submenu: { label: t("openIn"), radio: true, data: { "data-sidebar-shortcut-choices": "open" }, choices: SIDEBAR_OPEN_MODES.map((choice) => ({
        key: choice, label: sidebarOpenModeLabel(choice, t), icon: sidebarOpenModeIcon(choice), checked: mode === choice,
        data: { "data-sidebar-shortcut-open": choice }, run: () => { onChange({ openIn: choice }); close(); } })) } },
    { key: "open-place", label: t("openPlace", { title: databaseTitle }), icon: <OpenAsPage size="regular" aria-hidden="true" />,
      data: data("open-place"), run: () => { close(false); pushRoute(sidebarViewDatabaseHref(view, choices.content, holder)); } },
    sidebarTemplateChoiceRow(t, { data: data("templates"), choices, templateId: shortcut.templateId ?? "",
      onChoose: (templateId) => { onChange({ templateId }); close(); } }),
    sidebarViewChoiceRow(t, { key: "views", label: t("addToView"), data: data("views"), choices, viewId: view.viewId,
      query: viewQuery, onQuery: setViewQuery, onChoose: (viewId) => { onChange({ viewId }); close(); } }),
    ...sidebarShortcutArrangeRows(t, { close, onMove, onRemove, onCustomize, customizing }),
  ];
  // A line sets moving and removing apart from the choices, as Notion's.
  const firstAfterViews = rows.findIndex((row) => row.key === "views") + 1;
  if (rows[firstAfterViews]) rows[firstAfterViews] = { ...rows[firstAfterViews], separatorBefore: true };
  return (
    <>
      <button ref={button} type="button" className={styles.sectionAction} data-sidebar-shortcut-menu={shortcut.key}
        aria-label={t("shortcutMenu", { name: label })} title={t("shortcutMenuTitle")} aria-haspopup="menu"
        aria-expanded={opened} onClick={(event) => {
          event.stopPropagation();
          if (opened) { close(); return; }
          setViewQuery("");
          setOpened(true);
        }}>
        {customizing ? <SlidersIcon size="regular" aria-hidden="true" /> : <DotsHorizontal size="regular" aria-hidden="true" />}
      </button>
      {opened ? createPortal(<SidebarActionMenu button={button} label={t("shortcutMenu", { name: label })} width={MENU_WIDTH}
        rows={rows} backdropLabel={t("closeSectionMenu")} close={close} />, document.body) : null}
    </>
  );
}
