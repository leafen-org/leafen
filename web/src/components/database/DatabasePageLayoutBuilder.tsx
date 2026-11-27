import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import { confirmAction, confirmChoice } from "@/components/ConfirmActionDialog";
import { SidebarActionMenu, type SidebarMenuRow } from "@/components/sidebar/SidebarActionMenu";
import { ArrowDown, ArrowLeft, ArrowRight, ArrowUp, CheckIcon, ChevronDown, DatabaseCylinderIcon, EyeSlashIcon, LayoutIcon, RectangleWidePlusIcon, SplitChevronDown, Trash, XMarkIcon } from "@/icons/leafen";
import {
  applyDatabasePageLayout, defaultDatabasePageLayout, pageLayoutPlacedProperties, resetDatabasePageLayout, useDatabasePageLayout,
  type DatabasePageLayout, type PageLayoutModule, type PageLayoutTab,
} from "@/lib/databasePageLayout";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { useProductToast } from "@/lib/productToast";
import type { Page, PropertyType } from "@/lib/types";
import type { Session } from "@/types";
import { DatabasePropertyEditPanel, type DatabasePropertyEditing } from "./DatabasePropertyEditPanel";
import { CONTENT_TAB_SELECTION, PageLayoutCanvas, TAB_SELECTION, type PageLayoutSelection } from "./PageLayoutCanvas";
import { PageLayoutSidebar } from "./PageLayoutSidebar";
import { PageLayoutCardPopover } from "./PageLayoutCardPopover";
import { PageLayoutContentTabFields } from "./PageLayoutContentTabFields";
import { RowPagePropertyMenu } from "./RowPagePropertyMenu";
import popoverStyles from "./PageLayoutCardPopover.module.css";
import type { PropertyValueReadContext } from "./PropValue";
import { PageLayoutAddMenu } from "./PageLayoutAddMenu";
import { PageLayoutTabMenu } from "./PageLayoutTabMenu";
import { PhonePageLayoutBuilder, type PageLayoutCanvasShared } from "./PhonePageLayoutBuilder";
import { RelationPropertyCreationDialog } from "./RelationPropertyCreationDialog";
import { createRelationPropertyCommand, type RelationDatabaseOption } from "@/lib/relationPropertyCreationModel";
import { afterMadePropertiesSaved } from "@/lib/nativeFormProductAdapter";
import { usePageLayoutPropertyDraft } from "./usePageLayoutPropertyDraft";
import { layoutTabActions } from "./pageLayoutTabOrder";
import { PageLayoutTabDraftContext } from "./pageLayoutTabEnvironment";
import { newId } from "@/lib/ids";
import type { PageLayoutTabNames } from "./PageLayoutTabOptions";
import { relationIsTwoWay } from "@/lib/relationPairConfig";
import { shownDatabasePageTab, showDatabasePageTab } from "@/lib/databasePageTab";
import {
  addPropertyModule, canMoveModule, dropModule, moduleLocation, moveModule, moveModuleTo, pageLayoutAddCandidates, pageLayoutId, pinProperty, removeModule,
  dropGroupRelation, relationIntoGroup, removeGroupRelation, setRelationDisplay, takeRelationOut, type PageLayoutPlace,
} from "./pageLayoutModel";
import { PageLayoutCardAdvanced } from "./PageLayoutCardAdvanced";
import { PageLayoutRelationsCard } from "./PageLayoutRelationsCard";
import { propertyTypeLabel } from "./propertyTypes";
import { newRowPageProperty } from "./rowPagePropertyAdd";
import styles from "./DatabasePageLayoutBuilder.module.css";
import { useLayoutBuilderPanel } from "./useLayoutBuilderPanel";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";
import { PageIconGlyph } from "../page/PageIcon";
import { pageIconSource } from "./DatabaseFilterValueInput";

// The property group's "관계형" line while "+" › "관계형" asks for its database.
const RELATION_LINE = "relation-line";

export type DatabasePageLayoutBuilderProps = {
  session: Session;
  /** The database's page and the data source whose pages the layout is for. */
  pageId: string;
  dataSourceId: string;
  title: string;
  properties: readonly GraphProperty[];
  /** The pages the bar's "미리보기" offers, in the order its list gives them. */
  previewRows: readonly Page[];
  /** The page shown first, none a new page with nothing chosen (Notion's from a view in groups); else the first offered. */
  previewRowId?: string | null;
  propertyValueContext: PropertyValueReadContext;
  /** Changes the database's properties: their visibility, and new ones, made when the layout is applied. */
  editing?: DatabasePropertyEditing;
  onClose(): void;
};

/**
 * Notion's "레이아웃 사용자 지정" (seen 2026-10-12): a window over the app
 * that lays out every page of a database on one of them, then applies it to
 * all ("모든 페이지에 적용") or puts the original page back, its note
 * saying the properties stay. Leaving with changes asks first whether to
 * throw them away.
 */
export function DatabasePageLayoutBuilder({ session, pageId, dataSourceId, title, properties, previewRows, previewRowId, propertyValueContext, editing, onClose }: DatabasePageLayoutBuilderProps) {
  const { t } = useTranslation(["pageLayout", "common"]);
  const { notify } = useProductToast();
  const saved = useDatabasePageLayout(session, pageId, dataSourceId);
  const [draft, setDraft] = useState<DatabasePageLayout | null>(null);
  const [initial, setInitial] = useState("");
  const [selected, setSelected] = useState<PageLayoutSelection>(null);
  // A data tab's view settings asked for (PageLayoutCanvas): added, or its pill pressed again.
  const [tabSettings, setTabSettings] = useState<{ tabId: string; request: number } | null>(null);
  const openTabSettings = (tabId: string) => setTabSettings((current) => ({ tabId, request: (current?.request ?? 0) + 1 }));
  const [previewId, setPreviewId] = useState(previewRowId === null ? "" : previewRowId ?? previewRows[0]?.id ?? "");
  const [previewQuery, setPreviewQuery] = useState("");
  // An add menu's "관계형" asked for marks it, its line standing in the property group while the menu stays.
  const [menu, setMenu] = useState<{ kind: "add"; place: PageLayoutPlace; index: number; relation?: boolean } | { kind: "preview" } | { kind: "save" } | null>(null);
  const [busy, setBusy] = useState(false);
  const [tabMenu, setTabMenu] = useState(false);
  // The panel's column opens unfolded over 900px, "»" folding it for this look alone (useLayoutBuilderPanel;
  // Notion's unfolded again on every opening, seen 2026-11-12); a module moved into it unfolds it.
  const [panelShown, setPanelShown] = useLayoutBuilderPanel();
  const panelCount = useRef(0);
  const [relationFor, setRelationFor] = useState<RelationDatabaseOption | null>(null);
  // "+"'s "관계형": where its card goes once the relation window makes it, the menu staying under it.
  const [relationAdd, setRelationAdd] = useState<{ place: PageLayoutPlace; index: number } | null>(null);
  // Property changes, new relations among them, wait in the draft until it's applied.
  const propertyDraft = usePageLayoutPropertyDraft(editing, properties, dataSourceId);
  const { shownProperties, draftEditing, relationCreation, draftRelationCreation, addProperty } = propertyDraft;
  // A relations group's relation whose own settings its card shows.
  const [newProperty, setNewProperty] = useState<{ pin: boolean } | null>(null);
  // A property just made opens its settings under its row, as Notion's (seen 2026-11-01).
  const [madeProperty, setMadeProperty] = useState<{ id: string; anchor: DOMRect } | null>(null);
  const madeId = useRef<string | null>(null);
  const newPropertyButton = useRef<HTMLElement | null>(null);
  const menuButton = useRef<HTMLButtonElement | null>(null);
  const tabButton = useRef<HTMLButtonElement | null>(null);
  const previewButton = useRef<HTMLButtonElement>(null);
  const saveButton = useRef<HTMLButtonElement>(null);
  // The draft starts from the applied layout, or the original page, once it's read.
  useEffect(() => {
    if (!saved.loaded || draft) return;
    const start = saved.layout ?? defaultDatabasePageLayout();
    setDraft(start);
    setInitial(JSON.stringify(start));
    // On the tab its pages show, as Notion's (seen 2026-11-12), that tab's settings closed and the page's on the right.
    const shown = shownDatabasePageTab(dataSourceId);
    if (start.structure === "tabbed" && start.tabs.some((tab) => tab.id === shown)) setSelected(`${TAB_SELECTION}${shown}`);
  }, [dataSourceId, draft, saved.layout, saved.loaded]);
  const dirty = draft !== null && (JSON.stringify(draft) !== initial || propertyDraft.changed);
  useEffect(() => {
    const count = draft?.panel.length ?? 0;
    if (count > panelCount.current) setPanelShown(true);
    panelCount.current = count;
  }, [draft]);
  const row = previewRows.find((candidate) => candidate.id === previewId) ?? (previewId || previewRowId !== null ? previewRows[0] : undefined);
  const listed = useMemo(() => shownProperties.filter((property) => property.type !== "title"), [shownProperties]);

  async function leave() {
    if (dirty && !await confirmAction({ message: t("discardConfirm"), confirmLabel: t("discard"), danger: true })) return;
    onClose();
  }
  // A tab view's settings or menus close first, and on a phone an open sheet.
  const root = useRef<HTMLDivElement>(null);
  const phoneEscape = useRef<(() => boolean) | null>(null);
  // A wide touch screen's builder is the phone's, its two columns side by side (PhonePageLayoutBuilder `wide`).
  const screen = useSidebarLayout(true), wide = screen.touch && !screen.phone, phone = screen.phone || wide;
  // A window of its own over the app, so a center peek it was opened from
  // goes inert behind it and not it (2026-11-01: nothing in it took a press).
  // Escape closes what is over it, a phone's sheet; on the builder itself it
  // does nothing, as Notion's (seen 2026-11-12: changed or not, the layout
  // stayed open — "취소" leaves), and goes no further, to a peek under it.
  // A touch screen's takes the focus itself, no control ringed or keyboard raised, as Notion's iPad (seen 2026-11-14).
  useOverlayLayer(true, { kind: "dialog", ref: root, initialFocus: phone ? root : undefined, onEscape: () => { phoneEscape.current?.(); } });

  async function apply() {
    if (!draft) return;
    setBusy(true);
    try {
      // The relations made here first, then the properties: the layout may name a new one, so it goes once
      // they are written (2026-11-14: a relation "+" made, shown before it was saved, had the layout refused).
      // Each relation made stays the draft's, by its own ID, should a later step be refused.
      const layout = await propertyDraft.commit(draft, setDraft);
      await afterMadePropertiesSaved();
      await applyDatabasePageLayout(session, pageId, dataSourceId, saved.revision, layout);
      // Its pages show the tab it showed last, as Notion's (seen 2026-11-12); a phone's keeps theirs.
      const shownTab = typeof selected === "string" && selected.startsWith(TAB_SELECTION) ? selected.slice(TAB_SELECTION.length) : null;
      if (!phone) showDatabasePageTab(dataSourceId, layout.structure === "tabbed" && layout.tabs.some((tab) => tab.id === shownTab) ? shownTab : null);
      propertyDraft.deleteRemoved();
      onClose();
    } catch {
      notify(t("applyFailed"), "error");
    } finally {
      setBusy(false);
    }
  }

  // Notion asks first whether to go back to the original layout (seen 2026-10-12).
  async function reset() {
    setMenu(null);
    if (!await confirmAction({ message: t("resetConfirm"), detail: t("resetNote"), confirmLabel: t("resetAction") })) return;
    setBusy(true);
    try {
      await resetDatabasePageLayout(session, pageId, dataSourceId, saved.revision);
      onClose();
    } catch {
      notify(t("applyFailed"), "error");
    } finally {
      setBusy(false);
    }
  }

  // The title's or the property group's "속성 추가": Notion's name and type first, the title's pinned.
  const onNewProperty = addProperty ? (button: HTMLElement, pin: boolean) => { newPropertyButton.current = button; setNewProperty({ pin }); } : undefined;
  const newPropertyMenu = (phoneSheet: boolean) => newProperty && addProperty ? (
    <PageLayoutAddMenu createOnly phone={phoneSheet} button={newPropertyButton} label={t("newPropertyTitle")} candidates={[]} canCreate
      onPick={() => undefined} onClose={() => setNewProperty(null)}
      onCreate={(type, name, icon) => {
        const id = addProperty(type, name, icon);
        if (newProperty.pin) setDraft((current) => current && pinProperty(current, id));
        setNewProperty(null);
        madeId.current = id;
      }} />
  ) : null;

  const menuRows = (): SidebarMenuRow[] => {
    if (!draft || !menu) return [];
    if (menu.kind === "preview") {
      // Every row to preview with, found by its title (2026-10-13: only the
      // first 50 were offered), its page's icon before its name, a page's
      // glyph where it has none (Notion's, seen 2026-11-14).
      const query = previewQuery.trim().toLocaleLowerCase();
      return previewRows.filter((candidate) => !query || (candidate.title || untitledPageDisplayTitle(candidate)).toLocaleLowerCase().includes(query))
        .map((candidate) => ({
        key: candidate.id, label: candidate.title || untitledPageDisplayTitle(candidate), checked: candidate.id === row?.id,
        icon: <span className={styles.previewIcon} aria-hidden="true"><PageIconGlyph page={pageIconSource(candidate)} size={18} box={20} /></span>,
        run: () => { setPreviewId(candidate.id); setMenu(null); },
      }));
    }
    if (menu.kind === "save") {
      return [{ key: "reset", label: t("reset"), description: t("resetNote"), disabled: saved.revision === 0, run: () => void reset() }];
    }
    return [];
  };
  // A card without settings of its own lists what it can do, as Notion's
  // and the phone's sheet (seen 2026-11-01): the moves it can make, a
  // property's own card to the panel and back or into the property group,
  // the page's discussion hidden. A property's card leaves the layout after
  // asking whether it leaves the database too.
  async function removeProperty(moduleId: string, propertyId: string) {
    const deletable = Boolean(editing?.writable && editing.onDeleteProperty);
    const choice = await confirmChoice({
      message: t("removeConfirm"), confirmLabel: t("remove"), danger: true,
      choices: [
        { value: "layout", label: t("removeLayoutOnly"), description: t("removeLayoutOnlyNote") },
        ...(deletable ? [{ value: "database", label: t("removeEverywhere"), description: t("removeEverywhereNote") }] : []),
      ],
    });
    if (!choice) return;
    setDraft((current) => current && removeModule(current, moduleId));
    setSelected(null);
    if (choice === "database") propertyDraft.remove(propertyId);
  }
  const moduleActions = (module: PageLayoutModule) => {
    const at = draft && moduleLocation(draft, module.id);
    if (!draft || !at) return [];
    const change = (next: DatabasePageLayout, keep = true) => { setDraft(next); if (!keep) setSelected(null); };
    return [
      ...(canMoveModule(draft, module.id, -1)
        ? [{ key: "up", label: t("moveUp"), icon: <ArrowUp size="regular" />, run: () => change(moveModule(draft, module.id, -1)) }] : []),
      ...(canMoveModule(draft, module.id, 1)
        ? [{ key: "down", label: t("moveDown"), icon: <ArrowDown size="regular" />, run: () => change(moveModule(draft, module.id, 1)) }] : []),
      ...(module.kind === "property" ? [{
        key: "place", label: t(at.place === "page" ? "moveToPanel" : "moveToPage"),
        icon: at.place === "page" ? <ArrowRight size="regular" /> : <ArrowLeft size="regular" />,
        run: () => change(moveModuleTo(draft, module.id, at.place === "page" ? "panel" : "page")),
      }] : []),
      ...(module.kind === "property" ? [{
        key: "group", label: t("addToPropertyGroup"), icon: <RectangleWidePlusIcon size="regular" />, run: () => change(removeModule(draft, module.id), false),
      },
      // A relation's "고급 ›": its card into a relations group, nothing chosen after (PageLayoutCardAdvanced).
      ...(shownProperties.find((property) => property.id === module.propertyId)?.type === "relation"
        ? [{ key: "advanced", label: t("advanced"), icon: null, run: () => change(relationIntoGroup(draft, module.id), false) }] : []), {
        key: "remove", label: t("removeFromLayout"), icon: <Trash size="regular" />, run: () => void removeProperty(module.id, module.propertyId),
      }] : module.kind === "relations" ? [{
        // A relations group as Notion's (seen 2026-11-06): its relations back into the property group, or off the layout.
        key: "group", label: t("addToPropertyGroup"), icon: <RectangleWidePlusIcon size="regular" />, run: () => change(removeModule(draft, module.id), false),
      }, {
        key: "remove", label: t("removeFromLayout"), icon: <Trash size="regular" />, run: () => change(removeModule(draft, module.id), false),
      }] : module.kind === "comments" ? [{
        // Notion's outlined eye (seen 2026-11-12), the phone sheet's.
        key: "hide", label: t("hidePageComments"), icon: <EyeSlashIcon size="regular" weight="regular" />,
        run: () => change({ ...draft, options: { ...draft.options, pageComments: false } }, false),
      }] : []),
    ];
  };
  const placed = draft ? pageLayoutPlacedProperties(draft) : null;
  const addCandidates = draft ? pageLayoutAddCandidates(draft, listed) : listed;
  // A tab joins the end of the tabs and opens.
  // A source not yet read with the database shows from its own page until the layout is applied.
  const [previewPages, setPreviewPages] = useState<Record<string, string>>({});
  function addTab(tab: Omit<PageLayoutTab, "id">, previewPageId?: string) {
    setTabMenu(false);
    setRelationFor(null);
    if (!draft) return;
    const id = pageLayoutId("tab");
    if (previewPageId) setPreviewPages((current) => ({ ...current, [id]: previewPageId }));
    setDraft({ ...draft, tabs: [...draft.tabs, { id, ...tab }] });
    setSelected(`${TAB_SELECTION}${id}`);
    openTabSettings(id);
  }
  // "보기 및 관계형 추가": a two-way relation to the source named as the tab,
  // its related property named in the menu, then the tab of its pages.
  async function addRelationTab(source: RelationDatabaseOption, names: PageLayoutTabNames) {
    if (!draftRelationCreation) return;
    const { context, snapshot } = draftRelationCreation;
    try {
      const property = await context.commit(createRelationPropertyCommand({
        bounds: context.bounds, contextIdentity: context.identity, databaseId: dataSourceId, mutationId: newId(), name: names.name,
        reciprocalName: names.relatedName, reciprocalPropertyId: newId(), snapshotIdentity: snapshot.snapshotIdentity,
        targetDatabaseId: source.id, twoWay: true,
      }));
      if (!property) { context.notifyFailure(); return; }
      addTab({ dataSourceId: source.id, relationPropertyId: property.id, name: property.name }, source.pageId);
    } catch {
      context.notifyFailure();
    }
  }
  // The chosen card, which its settings open beside; a tab's stay in the right column.
  const tabSelected = typeof selected === "string" && selected.startsWith(TAB_SELECTION);
  const [card, setCard] = useState<HTMLElement | null>(null);
  useLayoutEffect(() => {
    setCard(selected && !tabSelected && !phone ? root.current?.querySelector<HTMLElement>(selected === CONTENT_TAB_SELECTION
      ? '[data-layout-tab="content"]' : `[data-layout-module-id="${CSS.escape(selected)}"]`) ?? null : null);
  }, [selected, tabSelected, phone, draft]);
  // A property just made, once its row is drawn in the card's settings.
  useLayoutEffect(() => {
    const id = madeId.current;
    const row = id ? document.querySelector<HTMLElement>(`[data-layout-card-popover] [data-layout-list-property="${CSS.escape(id)}"]`) : null;
    if (!id || !row) return;
    madeId.current = null;
    setMadeProperty({ id, anchor: row.getBoundingClientRect() });
  }, [propertyDraft.pending, draft]);
  // A property's card added where it was asked for, chosen, so its settings
  // open beside it at once, as Notion's (seen 2026-11-12: "패널에 추가" and
  // "페이지에 추가", an existing property or a new one).
  function addCard(place: PageLayoutPlace, index: number, propertyId: string) {
    if (!draft) return;
    const next = addPropertyModule(draft, place, index, propertyId);
    setDraft(next);
    setSelected(next[place].find((module) => module.kind === "property" && module.propertyId === propertyId)?.id ?? null);
  }
  // "레이아웃에 추가": the property's own card after the property group, chosen.
  function addToLayout(propertyId: string) {
    setMadeProperty(null);
    if (!draft) return;
    const group = (["page", "panel"] as const).map((place) => ({ place, index: draft[place].findIndex((module) => module.kind === "properties") }))
      .find((spot) => spot.index >= 0);
    addCard(group?.place ?? "page", group ? group.index + 1 : draft.page.length, propertyId);
  }
  // "+" › "관계형" stands a "관계형" line in the property group while its
  // database is asked for and the menu stays open, as Notion's (seen
  // 2026-11-14: in its place by name, "비어 있음"; gone as the menu closes,
  // or as the relation's card is made where "+" was).
  const relationLine = relationAdd !== null || (menu?.kind === "add" && menu.relation === true);
  const lineProperty = useMemo(() => ({ ...newRowPageProperty("relation", dataSourceId, shownProperties), id: RELATION_LINE, name: propertyTypeLabel("relation") }),
    [dataSourceId, shownProperties]);
  const tabActions = layoutTabActions(draft, setDraft, (id) => setSelected(id ? `${TAB_SELECTION}${id}` : null));
  const canvas: PageLayoutCanvasShared = {
    properties: relationLine ? [...shownProperties, lineProperty] : shownProperties, row, propertyValueContext, previewPages, viewer: { id: session.user.id, name: session.user.name }, session, databasePageId: pageId,
    onDropModule: (moduleId, targetId, after) => setDraft((current) => current && dropModule(current, moduleId, targetId, after)),
    onAddTab: (button) => { tabButton.current = button ?? null; setTabMenu(true); },
    // A copy is chosen, its settings closed, as Notion's (seen 2026-11-12); a tab moves by its row in "N개 더 보기".
    onTabActions: tabActions.tab, onMoveTab: tabActions.move,
    onTabView: (tabId, view, name) => setDraft((current) => current && {
      ...current, tabs: current.tabs.map((tab) => tab.id === tabId ? { ...tab, view, ...(name ? { name } : {}) } : tab),
    }),
  };
  const tabMenus = tabMenu && draft ? (
    <PageLayoutTabMenu button={tabButton} session={session} databasePageId={pageId} databaseTitle={title} ownSourceId={dataSourceId} phone={phone}
      relations={properties.filter((property) => property.type === "relation" && typeof property.config.relationDatabaseId === "string"
        && relationIsTwoWay(property.config))}
      searchSources={relationCreation?.context.searchDatabases} canRelate={Boolean(relationCreation)}
      onRelationTab={(property) => addTab({ dataSourceId: String(property.config.relationDatabaseId), relationPropertyId: property.id, name: property.name })}
      onNewRelation={(source, names) => {
        if (names) return addRelationTab(source, names);
        setTabMenu(false);
        setRelationFor(source);
      }}
      onViewTab={(source, name) => addTab({ dataSourceId: source.id, name: name ?? source.title }, source.pageId)}
      onClose={() => setTabMenu(false)} />
  ) : relationFor && relationCreation ? (
    <RelationPropertyCreationDialog context={draftRelationCreation!.context} databaseId={dataSourceId} snapshot={relationCreation.snapshot}
      initialTarget={relationFor} onCancel={() => setRelationFor(null)}
      onCreated={(property) => addTab({ dataSourceId: relationFor.id, relationPropertyId: property.id, name: property.name }, relationFor.pageId)} />
  ) : null;

  // "+"'s "관계형" asks which database first, as Notion's builder (seen 2026-11-14): over the menu's
  // line on a desktop, over the screen on a phone; made, its card goes where "+" was, chosen.
  const relationAddWindow = relationAdd && relationCreation && draftRelationCreation ? (
    <RelationPropertyCreationDialog anchor={phone ? null : document.querySelector<HTMLElement>('[data-layout-add-menu] [data-layout-add-type="relation"]')}
      context={draftRelationCreation.context} databaseId={dataSourceId} snapshot={relationCreation.snapshot}
      initialName={propertyTypeLabel("relation")} onCancel={() => setRelationAdd(null)}
      onCreated={(property) => {
        setRelationAdd(null);
        setMenu(null);
        if (phone) setDraft((current) => current && addPropertyModule(current, relationAdd.place, relationAdd.index, property.id));
        else addCard(relationAdd.place, relationAdd.index, property.id);
      }} />
  ) : null;
  const onNewRelation = draftRelationCreation ? (place: PageLayoutPlace, index: number) => setRelationAdd({ place, index }) : undefined;
  // Its tabs show their sources' properties as the draft has them (usePageLayoutPropertyDraft).
  const drafted = (children: ReactNode) => <PageLayoutTabDraftContext.Provider value={propertyDraft.draftTabEnvironment}>{children}</PageLayoutTabDraftContext.Provider>;
  if (phone) {
    return createPortal(draft ? drafted(
      <>
        <PhonePageLayoutBuilder rootRef={root} escapeRef={phoneEscape} title={title || untitledPageDisplayTitle("database")} layout={draft} onLayout={setDraft} canvas={canvas}
          properties={shownProperties} editing={draftEditing} onAddProperty={addProperty} onNewProperty={onNewProperty} onNewRelation={onNewRelation}
          candidates={addCandidates} busy={busy} canReset={saved.revision !== 0}
          onApply={() => void apply()} onReset={() => void reset()} onLeave={() => void leave()}
          wide={wide ? { previewLabel: t("previewOf", { title: row?.title || untitledPageDisplayTitle(row ?? "page") }), previewRef: previewButton,
            previewDisabled: previewRows.length === 0, onPreview: () => { setPreviewQuery(""); setMenu({ kind: "preview" }); },
            previewMenu: menu?.kind === "preview" ? (
              <SidebarActionMenu button={previewButton} label={t("moduleMenu")} width={300} backdropLabel={t("common:actions.close")}
                close={() => setMenu(null)} rows={menuRows()} search={{ label: t("previewSearch"), query: previewQuery, onQuery: setPreviewQuery }} />
            ) : null } : undefined} />
        {tabMenus}
        {newPropertyMenu(true)}
        {relationAddWindow}
      </>
    ) : null, document.body);
  }

  const chosenModule = draft && selected && !tabSelected && selected !== "heading" && selected !== CONTENT_TAB_SELECTION
    ? (() => { const at = moduleLocation(draft, selected); return at ? draft[at.place][at.index] : null; })() : null;
  const chosenProperty = chosenModule?.kind === "property" ? shownProperties.find((property) => property.id === chosenModule.propertyId) : undefined;
  const made = madeProperty ? shownProperties.find((property) => property.id === madeProperty.id) : undefined;
  const builder = (
    <div ref={root} className={styles.builder} role="dialog" aria-modal="true" aria-label={t("title")} data-page-layout-builder>
      <div className={styles.bar}>
        <span />
        <div className={styles.barTitle}>
          <span className={styles.barName}><DatabaseCylinderIcon size="regular" aria-hidden="true" /><span>{title || untitledPageDisplayTitle("database")}</span></span>
          <button ref={previewButton} type="button" className={styles.previewPicker} aria-haspopup="menu" disabled={previewRows.length === 0}
            onClick={() => { setPreviewQuery(""); setMenu({ kind: "preview" }); }} data-layout-preview-picker>
            {t("previewOf", { title: row?.title || untitledPageDisplayTitle(row ?? "page") })}<ChevronDown size="tiny" aria-hidden="true" />
          </button>
        </div>
        <div className={styles.barActions}>
          <button type="button" className={styles.cancel} onClick={() => void leave()} data-layout-cancel>
            <XMarkIcon size="small" trim glyphWidth={9.66} aria-hidden="true" />{t("common:actions.cancel")}
          </button>
          <span className={styles.apply}>
            <button type="button" disabled={busy || !draft} onClick={() => void apply()} data-layout-apply>
              <CheckIcon size="small" aria-hidden="true" />{t("apply")}
            </button>
            <button ref={saveButton} type="button" aria-label={t("moreSaveOptions")} aria-haspopup="menu" disabled={busy}
              onClick={() => setMenu({ kind: "save" })} data-layout-save-options>
              <SplitChevronDown aria-hidden="true" />
            </button>
          </span>
        </div>
      </div>
      <div className={styles.canvas} onClick={(event) => { if (event.target === event.currentTarget) setSelected(null); }} data-layout-canvas>
        {draft ? (
          <PageLayoutCanvas {...canvas} layout={draft}
            selected={selected} onSelect={setSelected} panelShown={panelShown} onPanel={setPanelShown} adding={menu?.kind === "add" ? menu.place : null}
            tabSettings={tabSettings} onTabSettings={openTabSettings}
            onModuleMenu={(module) => setSelected(module.id)}
            onAdd={(place, index, button) => { menuButton.current = button; setMenu({ kind: "add", place, index }); }} />
        ) : null}
      </div>
      {draft ? (
        // The right column keeps the page's settings whatever is chosen, a tab's view settings opening on the canvas, as Notion's (seen 2026-11-12).
        <PageLayoutSidebar layout={draft} properties={shownProperties} selected={null} editing={draftEditing}
          onLayout={setDraft} onNewProperty={onNewProperty} onSelect={setSelected} />
      ) : <div />}
      {draft && card && selected === CONTENT_TAB_SELECTION ? (
        <PageLayoutCardPopover card={card} under onClose={() => setSelected(null)} label={t("tabSettings")}>
          <div className={popoverStyles.head}><span>{t("tabSettings")}</span></div>
          <PageLayoutContentTabFields tab={draft.contentTab} onChange={(contentTab) => setDraft({ ...draft, contentTab })} />
        </PageLayoutCardPopover>
      ) : draft && card && (selected === "heading" || chosenModule) ? (
        <PageLayoutCardPopover card={card} onClose={() => setSelected(null)} menu={chosenModule?.kind === "comments" || chosenModule?.kind === "content"}
          label={selected === "heading" ? t("modules.heading") : chosenModule?.kind === "properties" ? t("modules.properties") : t("moduleMenu")}>
          {selected === "heading" || chosenModule?.kind === "properties" ? (
            <PageLayoutSidebar popover layout={draft} properties={shownProperties} selected={selected} editing={draftEditing}
              onLayout={setDraft} onNewProperty={onNewProperty} onSelect={setSelected} />
          ) : chosenModule ? (<>
            {chosenModule.kind === "property" && draftEditing && chosenProperty ? (<>
              {/* Its type over its name, as Notion's (seen 2026-11-12: "숫자" over a number's). */}
              <div className={`${popoverStyles.head} ${popoverStyles.typeHead}`}><span>{propertyTypeLabel(chosenProperty.type as PropertyType)}</span></div>
              <DatabasePropertyEditPanel property={chosenProperty} editing={draftEditing} layoutCard onClose={() => setSelected(null)} />
            </>) : null}
            {chosenModule.kind === "relations" ? (
              <PageLayoutRelationsCard module={chosenModule} properties={shownProperties} editing={draftEditing}
                onDisplay={(propertyId, display) => setDraft(setRelationDisplay(draft, chosenModule.id, propertyId, display))}
                onMove={(from, to, after) => setDraft(dropGroupRelation(draft, chosenModule.id, from, to, after))}
                onTakeOut={(propertyId) => { setDraft(takeRelationOut(draft, chosenModule.id, propertyId)); setSelected(null); }}
                onDelete={editing?.writable && editing.onDeleteProperty ? (propertyId) => {
                  setDraft(removeGroupRelation(draft, chosenModule.id, propertyId));
                  propertyDraft.remove(propertyId);
                  setSelected(null);
                } : undefined} />
            ) : null}
            <div className={popoverStyles.actions}>
              {moduleActions(chosenModule).map((action) => action.key === "advanced" ? <PageLayoutCardAdvanced key={action.key} onGroup={action.run} /> : (
                <button key={action.key} type="button" role={chosenModule.kind === "comments" || chosenModule.kind === "content" ? "menuitem" : undefined}
                  className={popoverStyles.action} onClick={action.run} data-layout-module-action={action.key}>
                  {action.icon}<span>{action.label}</span>
                </button>
              ))}
            </div>
          </>) : null}
        </PageLayoutCardPopover>
      ) : null}
      {made && madeProperty && draftEditing ? (
        <RowPagePropertyMenu property={made} anchor={madeProperty.anchor} editing={draftEditing} opened="new"
          extraActions={placed?.has(made.id) ? [] : [{ key: "addToLayout", label: t("addToLayout"), icon: <LayoutIcon size="regular" />, run: () => addToLayout(made.id) }]}
          onClose={() => setMadeProperty(null)} />
      ) : null}
      {tabMenus}
      {newPropertyMenu(false)}
      {relationAddWindow}
      {menu?.kind === "add" && draft ? (
        <PageLayoutAddMenu button={menuButton} slot={() => root.current?.querySelector<HTMLElement>("[data-layout-add-slot]") ?? null}
          label={t(menu.place === "panel" ? "addToPanel" : "addToPage")}
          candidates={addCandidates} canCreate={Boolean(addProperty)} canRelate={Boolean(onNewRelation)}
          onPick={(propertyId) => { setMenu(null); addCard(menu.place, menu.index, propertyId); }}
          onCreate={(type) => {
            if (type === "relation") { setMenu({ ...menu, relation: true }); onNewRelation?.(menu.place, menu.index); return; }
            setMenu(null);
            if (addProperty) addCard(menu.place, menu.index, addProperty(type));
          }}
          onClose={() => setMenu(null)} />
      ) : menu && draft ? (
        <SidebarActionMenu button={menu.kind === "preview" ? previewButton : menu.kind === "save" ? saveButton : menuButton}
          label={t("moduleMenu")}
          // The preview picker is 300 wide, as Notion's (seen 2026-11-14).
          width={menu.kind === "save" ? 260 : menu.kind === "preview" ? 300 : 220} backdropLabel={t("common:actions.close")}
          close={() => setMenu(null)} rows={menuRows()}
          search={menu.kind === "preview" ? { label: t("previewSearch"), query: previewQuery, onQuery: setPreviewQuery } : undefined} />
      ) : null}
    </div>
  );
  return createPortal(drafted(builder), document.body);
}
