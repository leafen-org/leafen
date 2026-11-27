import { type ComponentProps, Suspense, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { createPortal } from "react-dom";
import type { GraphProperty } from "@/contentGraph";
import type { DatabaseToolbarTemplateMetadataWire, DatabaseToolbarTemplateRepeatWire } from "@/databaseToolbarConfigWire.generated";
import type { DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import { readDatabaseRowPage, setDatabaseRowPageFavorite, setDatabaseRowPageLock, setDatabaseRowPageLockForMe,
  setDatabaseRowPagePresentation } from "@/databaseRowPageApi";
import { PageIconGlyph, type PageIconSource } from "@/components/page/PageIcon";
import type { Page, PropertyType, TextSpan } from "@/lib/types";
import { copyText } from "@/lib/copyText";
import { createPageAnalyticsPanelService } from "@/lib/pageAnalyticsPanelService";
import { lazyScreen } from "@/lib/lazyScreen";
import { requestPageLayoutBuilder } from "@/lib/databasePageLayout";
import { lockForMeShownFirstIn } from "@/lib/lockForMeShownFirst";
import { requestPageTitleFocus } from "@/lib/pageTitleFocus";
import { usePathname } from "@/lib/router";
import { serverUrl } from "@/lib/serverOrigin";
import type { Session } from "@/types";
import { useProductToast } from "@/lib/productToast";
import { keepTemplateDraft, keptTemplateDraft, sendTemplateDraftNow, templateDraftPatch } from "@/lib/templateDraftSend";
import { templateMadeHere, templateTitleAskedAtPress } from "@/lib/templatesMadeHere";
import { useRowTemplateChangedHere } from "@/lib/rowTemplateHere";
import { keptIconPicture } from "../teamspace/KeptIconGlyph";
import { CommentsPanel } from "../CommentsPanel";
import { PageAnalyticsObserver } from "../page/PageAnalyticsObserver";
import { openWholeWithUpdates } from "../page/usePageSidePanel";
import type { OpenCommentsDetail } from "../comments/commentPanelEvents";
import type { PagePresentationChange } from "../page/pagePresentation";
import { storedCustomEmojiIcon, storedIconPresentation } from "../page/nativeIcons";
import { rowPageActionsAnchor, type RowPageMenuExtras } from "./DatabaseRowMenu";
import { TemplatePageMenu } from "./TemplatePageMenu";
import { RowPageWhileReading } from "./rowPageWhileReading";
import { TemplateEditingBanner } from "./TemplateEditingBanner";
import { useTemplatePageScrolledPastOptions } from "./templatePagePhoneScroll";
import { ROW_PAGE_READING } from "@/lib/rowPageHandoff";
import { templatePageProperties, useDatabaseTemplateCells, type DatabaseTemplatePropertyCells } from "./DatabaseTemplateProperties";
import { openTemplateWhole, showTemplateWindowMode, templateAddress, templateWindowMode, type TemplateWindowMode } from "./useTemplateAddress";
import { newRowPageProperty } from "./rowPagePropertyAdd";
import { rowPagePresentationInput } from "./RowPageHeader";
import { FILTER_PAGE_CHIP_ICON, FilterThisPageContext, type FilterThisPage } from "./filterThisPage";
import { PageLayoutTabHolderContext } from "./pageLayoutTabEnvironment";
import { OpenDatabasePagesContext } from "./databaseRowOpeners";
import { TemplateWindowContext, type TemplateWindow } from "./templateWindow";
import { useRowPageDockedPanel } from "./useRowPageDockedPanel";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { useNamedPage } from "@/lib/pageNames";
import { useSidebarLayout } from "@/components/shell/AppShell";

/**
 * What a template keeps for a picked icon. Its icon is one bare value, read
 * by its form (storedIconType; the server's databaseTemplateRowIcon), so a
 * picture is kept as its address — an uploaded one as its workspace file's,
 * as a teamspace's is (keptIconPicture) — and a workspace emoji as
 * "custom_emoji:<id>", which its page and a page made from it show as the
 * emoji, as Notion's (seen 2026-11-11).
 */
function templateIconOf(change: PagePresentationChange) {
  if (change.iconType === "image") return keptIconPicture(change.icon ?? "");
  if (change.iconType === "custom_emoji" && change.icon) return storedCustomEmojiIcon(change.icon);
  return change.icon ?? "";
}

/**
 * A template's page as its menu holds it, its body still to be read: its
 * page opens on the press with this, to read only until the read comes
 * (rowPageWhileReading.tsx). Read first, it opened only once the server had
 * answered — a new template's, once the server had made it too. One made
 * here (`made`, its maker and its database's page) is its maker's to write
 * from its first frame, its body empty, as a row made here is
 * (rowPageMadeHere): drawn to read only, the name typed right after "새
 * 템플릿" went nowhere until the server had made it and its read came.
 */
function templateWhileReading(template: DatabaseToolbarTemplateMetadataWire, properties: readonly GraphProperty[],
  workspaceId: string, made?: { maker: string; pageId: string }): DatabaseRowPageSnapshot {
  const dataSourceId = properties.find((property) => property.type === "title")?.dataSourceId ?? "";
  const { icon, iconType } = storedIconPresentation(template.icon);
  return {
    snapshotIdentity: `${made ? "made-here:" : ROW_PAGE_READING}${template.id}`, containerSnapshotRevision: 0, bodySnapshotRevision: 0,
    row: {
      id: template.id, containerPageId: made?.pageId ?? "", dataSourceId, subitemParentId: null, position: template.position, values: {},
      icon: icon ?? "", iconType: (iconType ?? "none") as DatabaseRowPageSnapshot["row"]["iconType"],
      cover: "", coverPosition: 50, font: "default", smallText: false, fullWidth: false, tableOfContentsDisplay: "default",
      favorite: false, locked: false, unlockedForMe: false, archivedAt: "", archivedBy: "",
      trashedAt: template.deleted ? template.updatedAt : "", trashedBy: "",
      createdAt: template.updatedAt, createdBy: made?.maker ?? "", updatedAt: template.updatedAt, lastEditedBy: made?.maker ?? "", authorityRevision: 0,
    },
    body: [], nextCursor: null, hasMore: false,
    properties: properties.filter((property) => property.dataSourceId === dataSourceId)
      .map(({ id, name, type, config, position, access }) => ({ id, name, type, config: config ?? {}, position, ...(access ? { access } : {}) })),
    accessRole: made ? "edit" : "view", workspaceId, ...(made ? { template: true } : {}),
  };
}

/**
 * A template as its page shows its title: one with a name and no title (made with a name alone, or a copy of one,
 * "Rpt" → "Rpt (1)") has its name as its title's text, Notion's one name for a template, what is typed going on
 * after it (seen 2026-11-16: a copy "NT1 (1)" opened at its title's end, "Zq" typed → "NT1 (1)Zq").
 */
function titledTemplate(template: DatabaseToolbarTemplateMetadataWire) {
  return template.title.trim() || !template.name.trim() ? template : { ...template, title: template.name };
}

const PageAnalyticsPanel = lazyScreen(() => (
  import("../PageAnalyticsPanel").then(({ PageAnalyticsPanel: Panel }) => ({ default: Panel }))
));

/**
 * A database template opened for editing, as Notion opens it: a page — the
 * row page window over the database (decided 2026-10-30, templates are the
 * database's hidden pages) — under a band naming the database with the
 * template's "복제 주기", its title, look and body a page's, its property
 * values the template's own ("오늘", "나" when duplicated). Notion saves as
 * it goes and has no "저장": a title or value is kept as it is written, as
 * any edit is (templateDraftSend.ts), however the editor is left.
 */
export function DatabaseTemplateEditor({
  session,
  template,
  databaseTitle,
  databaseIcon,
  properties,
  propertyCells,
  writable,
  workspaceId,
  pageId,
  dataSourceId,
  onClose,
  onSetRepeat,
  onCommitProperties,
  onDuplicate,
  onDelete,
  onBodyContentChange,
  onRestore,
  onDeleteForever,
  focusTitle,
  whole,
  pathDatabase,
}: {
  session: Session | null;
  template: DatabaseToolbarTemplateMetadataWire;
  /** The database the template belongs to, which the banner names, as Notion's
   * "다음에서 템플릿을 편집 중입니다. 〈database〉" does (seen 2026-10-06). */
  databaseTitle: string;
  /** The database's icon, of any kind, beside its name in the banner; its
   *  database glyph and an underlined name without one, as Notion's (seen
   *  2026-11-07: a task tracker's ✅ and its plain name). */
  databaseIcon?: ComponentProps<typeof PageIconGlyph>["page"];
  /** The database's properties, whose values the template holds. */
  properties: readonly GraphProperty[];
  propertyCells: DatabaseTemplatePropertyCells;
  writable: boolean;
  workspaceId: string;
  /** The page holding the database, and its data source, whose settings keep the template. */
  pageId: string;
  dataSourceId: string;
  onClose(): void;
  onSetRepeat(repeat: DatabaseToolbarTemplateRepeatWire | null): void;
  /**
   * Saves the database's properties, as its pages' property editing does; none where the reader may not change them.
   * Its "속성 추가" and layout builder come from it here, in one place, so every way a template opens — its
   * database's templates menu, an empty database page's "템플릿을 생성하세요" where no view of it shows
   * (StandaloneRowTemplates), its address, the trash — shows the same property rows, as Notion's template page does.
   */
  onCommitProperties: ((properties: readonly GraphProperty[]) => unknown) | undefined;
  /** Its page menu's "복제" and "휴지통으로 이동", the template's duplicate and delete. */
  onDuplicate?(): void;
  onDelete?(): void;
  /** Whether its body holds a block, as it changes, for its icon in the menu. */
  onBodyContentChange?(hasContent: boolean): void;
  /** A deleted template's trash banner: "페이지 복원" and "영구 삭제하기". */
  onRestore?(): Promise<boolean>;
  onDeleteForever?(): Promise<boolean>;
  /** A template just made opens with the caret in its title, as Notion's. */
  focusTitle?: boolean;
  /** Opened at its own address, whole (StandaloneRowTemplates), its path naming its database there. */
  whole?: boolean;
  pathDatabase?: ComponentProps<typeof RowPageWhileReading>["pathDatabase"];
}) {
  const { t } = useTranslation(["databaseView", "rowMenu"]);
  const { notify } = useProductToast();
  const [draft, setDraft] = useState(() => titledTemplate(template));
  const [read, setSnapshot] = useState<DatabaseRowPageSnapshot | null>(null);
  // Shown on the press as the template holds it, until its page is read; one made here, its maker's to write.
  const maker = session && templateMadeHere(template.id) ? session.user.id : undefined;
  const shownFirst = useMemo(() => read?.row.id === template.id ? null
    : templateWhileReading(template, properties, workspaceId, maker ? { maker, pageId } : undefined),
  // eslint-disable-next-line react-hooks/exhaustive-deps
  [read?.row.id, template.id, template.icon, template.deleted, properties, workspaceId, maker, pageId]);
  const snapshot = shownFirst ?? read;
  const reading = shownFirst?.snapshotIdentity.startsWith(ROW_PAGE_READING) ? shownFirst : null;
  // Its row page takes the caret in its title when it opens: asked at the press
  // that made it (openTemplateAtItsTitle), before the page is drawn, else here.
  useEffect(() => {
    if (focusTitle && !templateTitleAskedAtPress(template.id)) requestPageTitleFocus(template.id);
  }, [focusTitle, template.id]);
  // A deleted template opens from the trash as a trashed page does: to read,
  // restore or delete for good.
  const editable = writable && !snapshot?.row.trashedAt;
  // "속성 추가" adds one to its database after its last, as a database page's does (rowPagePropertyAdd.ts).
  const addProperty = editable && onCommitProperties ? (type: PropertyType, name?: string, icon?: string) => {
    const property = newRowPageProperty(type, dataSourceId, properties, name, icon);
    void onCommitProperties([property]);
    return property.id;
  } : undefined;
  // Its comments, as a database page's (comment_policy.go takes a template's).
  const [comments, setComments] = useState<{ detail?: OpenCommentsDetail } | null>(null);
  // Its window opens as the address names it, beside or in the middle; ⤢ and its view menu's "전체 페이지 보기" open it whole at
  // its own address, as Notion's (useTemplateAddress), where it opens whole (`whole`), and "새로운 탭" there in a new tab. A touch
  // screen opens it full, a phone's as a tablet's, however it was opened, as
  // Notion's iPad (seen 2026-11-13 at 834) and its phone (seen 2026-11-25 on a
  // Galaxy A90's Chrome at 411, in two tabs: made from an empty row page's
  // "템플릿을 생성하세요" or its templates sheet's "새 템플릿", and its "⋯ ›
  // 편집" and "복제" there), under its phone band (TemplateEditingBanner).
  const { touch, phone } = useSidebarLayout(true);
  const [mode, setMode] = useState<TemplateWindowMode>(() => whole || touch ? "full" : templateWindowMode(template.id));
  const changeMode = (next: TemplateWindowMode | "new_tab") => {
    if (next === "new_tab") window.open(templateAddress(template.id), "_blank", "noopener,noreferrer");
    else if (next !== "full") { setMode(next); showTemplateWindowMode(template.id, next); } else if (whole || touch) setMode("full");
    else openTemplateWhole(draftNow.current);
  };
  // Its "…", Notion's page menu (seen 2026-10-30), with what a template's page does so far.
  const [actions, setActions] = useState<{ anchor: { x: number; y: number }; extras: RowPageMenuExtras } | null>(null);
  // Its updates and analytics, as a database page's (row_page_analytics.go takes a template's), from its "…" and its bar's last edit time.
  const [analyticsOpen, setAnalyticsOpen] = useState(false);
  const analyticsService = useMemo(() => (session ? createPageAnalyticsPanelService(session) : null), [session]);
  // Open full, it docks its "모든 토론" or its updates beside it as a database page does, the one opened last, kept open over its
  // visits until closed there; in its window either opens it full with that one docked (openWholeWith). So does Notion's, open
  // at its own address (seen 2026-11-25 in Chrome at 1440, two tabs: its window's "모두 보기" and "업데이트와 애널리틱스", aside 1055·0·385).
  const sidePanel = useRowPageDockedPanel({ rowId: template.id, whole: mode === "full", read: Boolean(read), keep: true,
    updates: { open: analyticsOpen, show: analyticsService ? () => setAnalyticsOpen(true) : undefined, close: () => setAnalyticsOpen(false) },
    all: { open: Boolean(comments?.detail?.all), show: () => setComments({ detail: { pageId: template.id, all: true } }),
      close: () => setComments((open) => open?.detail?.all ? null : open) } }), analyticsDocked = sidePanel.docked;
  const setProperty = useCallback((propertyId: string, value: unknown, spans?: TextSpan[]) => setDraft((current) => {
    const next = { ...current.properties };
    const nextSpans = { ...current.propertySpans };
    if (value === null) delete next[propertyId];
    else next[propertyId] = value;
    // A text value keeps the formatting it was written with, and a value
    // without formatting keeps none.
    if (value !== null && spans?.length) nextSpans[propertyId] = spans;
    else delete nextSpans[propertyId];
    return { ...current, properties: next, propertySpans: nextSpans };
  }), []);
  // Its values on its page wherever the page shows them — a wiki's line over
  // the title, a layout's pinned ones — edit the template's own.
  const spans = useMemo(() => (draft.propertySpans ?? {}) as Record<string, TextSpan[]>, [draft.propertySpans]);
  const templateCells = useDatabaseTemplateCells({ templateId: template.id, workspaceId, title: draft.title, properties,
    values: draft.properties, spans, cells: propertyCells, readOnly: !editable, onChange: setProperty });
  // Its page shows its title and the properties it takes a value for.
  const pageProperties = useMemo(() => templatePageProperties(properties), [properties]);

  // The draft follows another template, not a fresh copy of this one, which
  // would drop what is being typed; a change kept here and not yet sent —
  // left by a reload, or while the server was out of reach — shows in it.
  const keptFor = useRef({ id: template.id, patch: JSON.stringify(templateDraftPatch(titledTemplate(template))) });
  useEffect(() => {
    setDraft(titledTemplate(template));
    keptFor.current = { id: template.id, patch: JSON.stringify(templateDraftPatch(titledTemplate(template))) };
    if (!session) return;
    let live = true;
    void keptTemplateDraft(session, template.id).then((kept) => {
      if (live && kept) setDraft((current) => current.id === template.id ? { ...current, ...kept } : current);
    });
    return () => { live = false; };
  }, [template.id]);
  // What it holds is kept as it changes and sent behind, as any edit is.
  const editableNow = useRef(editable);
  editableNow.current = editable;
  const draftNow = useRef(draft), keep = useRef((_held: typeof draft) => {});
  draftNow.current = draft;
  keep.current = (held) => {
    if (!session || !editableNow.current || keptFor.current.id !== held.id) return;
    const patch = templateDraftPatch(held);
    const text = JSON.stringify(patch);
    if (text === keptFor.current.patch) return;
    keptFor.current.patch = text;
    keepTemplateDraft(session, { templateId: held.id, pageId, dataSourceId, patch, revision: template.templateAuthorityRevision,
      onRefused: () => notify(t("databaseView:toast.couldntSaveChanges"), "error") });
  };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => keep.current(draft), [draft.id, draft.icon, draft.name, draft.title, draft.properties, draft.propertySpans]);
  // Left any way — closed, another address, the tab hidden or gone — what it holds goes at once.
  useEffect(() => {
    if (!session) return;
    const now = () => sendTemplateDraftNow(session, template.id);
    const hidden = () => { if (document.visibilityState === "hidden") now(); };
    window.addEventListener("pagehide", now);
    document.addEventListener("visibilitychange", hidden);
    return () => {
      window.removeEventListener("pagehide", now);
      document.removeEventListener("visibilitychange", hidden);
      now();
    };
  }, [session, template.id]);
  // Changed here, wherever it is open, it isn't copied into new rows from what was read of it before (rowTemplateHere.ts).
  useRowTemplateChangedHere(session, template.id);
  // The template's page as the row page window reads one (database_template_row_page.go).
  useEffect(() => {
    if (!session) return;
    const controller = new AbortController();
    void readDatabaseRowPage(session, template.id, { signal: controller.signal }).then(setSnapshot).catch(() => {});
    return () => controller.abort();
  }, [session, template.id]);

  // A template is a page, which keeps what was written however it is left, as
  // Notion's does: what it holds was kept as it was written, and goes now.
  const close = () => {
    if (session) sendTemplateDraftNow(session, template.id);
    onClose();
  };
  // Going elsewhere — its path's pages, back — leaves it, as leaving a page does.
  const pathname = usePathname();
  const openedAt = useRef(pathname), latestClose = useRef(close);
  latestClose.current = close;
  useEffect(() => {
    if (pathname !== openedAt.current) latestClose.current();
  }, [pathname]);

  // A view in its body names the template itself first among its database's pages, by its name and icon,
  // as Notion's (seen 2026-11-12): each page made from it then reads as its own (database_template_linked_views.go).
  const shownIcon = snapshot && draft.icon === snapshot.row.icon ? { icon: snapshot.row.icon, iconType: snapshot.row.iconType } : storedIconPresentation(draft.icon);
  const ownSourceId = properties.find((property) => property.type === "title")?.dataSourceId, ownName = useNamedPage({ id: template.id, title: draft.title }).title;
  // A row a view of its body makes by "this page" is its on a two-way relation's other side, as Notion's template lists it
  // there (templateBodyRows.ts): its value takes the row as any value it is given, and each page made from it the row.
  // It is kept at once too: the row's page opens in the window's place as it is made (templateWindow.ts), and the window
  // gone before it drew the row, its value would go unkept.
  const takeRow = useCallback((propertyId: string, rowId: string) => {
    if (!properties.some((property) => property.id === propertyId && property.dataSourceId === dataSourceId && property.type === "relation")) return;
    const taken = (current: typeof draft) => {
      const held = Array.isArray(current.properties?.[propertyId]) ? current.properties[propertyId] as unknown[] : [];
      return held.includes(rowId) ? current : { ...current, properties: { ...current.properties, [propertyId]: [...held, rowId] } };
    };
    draftNow.current = taken(draftNow.current);
    keep.current(draftNow.current);
    setDraft(taken);
  }, [dataSourceId, properties]);
  // A row a view of its body opens beside or over the page closes it, as its ✕ does (templateWindow.ts).
  const windowShown = useMemo<TemplateWindow>(() => ({ leave: () => latestClose.current() }), []);
  const thisPage = useMemo<FilterThisPage | null>(() => ownSourceId ? {
    rowId: template.id, dataSourceId: ownSourceId, name: ownName.trim() || untitledPageDisplayTitle("page"), template: true,
    icon: <PageIconGlyph page={{ ...shownIcon, workspaceId, kind: "page" }} size={20} />,
    chipIcon: <PageIconGlyph page={{ ...shownIcon, workspaceId, kind: "page" }} size={FILTER_PAGE_CHIP_ICON} />,
    takeRow: editable ? takeRow : undefined,
  } : null, [editable, ownName, ownSourceId, shownIcon.icon, shownIcon.iconType, takeRow, template.id, workspaceId]);
  // The views in its body are inside its window: they don't open it, nor any template, from the address (useTemplateAddress).
  const above = useContext(OpenDatabasePagesContext), openedPages = useMemo(() => [...above, template.id], [above, template.id]);
  const holderPageId = snapshot?.row.containerPageId;
  const tabHolder = useMemo(() => session && holderPageId ? { session, pageId: holderPageId, workspaceId } : null,
    [holderPageId, session, workspaceId]);
  useTemplatePageScrolledPastOptions(template.id, mode === "full" && phone && Boolean(snapshot), read);
  if (!session || !snapshot) return null;
  // The icon is the template's own, kept with its other changes; the cover
  // and style are its page's, set at once.
  const changeLook = async (change: PagePresentationChange) => {
    if (change.icon !== undefined) {
      const icon = templateIconOf(change);
      setDraft((current) => ({ ...current, icon }));
    }
    const { icon: _icon, iconType: _iconType, ...look } = rowPagePresentationInput(change);
    if (!Object.keys(look).length) return true;
    try {
      await setDatabaseRowPagePresentation(session, template.id, { ...look, expectedAuthorityRevision: snapshot.row.authorityRevision });
      setSnapshot(await readDatabaseRowPage(session, template.id));
      return true;
    } catch {
      return false;
    }
  };
  // Its page's lock, as a database page's: for everyone, or undone for oneself.
  const changeLock = async (change: () => Promise<unknown>, failed: string) => {
    try {
      await change();
      setSnapshot(await readDatabaseRowPage(session, template.id));
    } catch {
      notify(failed, "error");
    }
  };
  const lock = editable ? {
    locked: snapshot.row.locked, unlockedForMe: snapshot.row.unlockedForMe,
    onLock: (locked: boolean) => void changeLock(() => setDatabaseRowPageLock(session, template.id,
      { locked, expectedAuthorityRevision: snapshot.row.authorityRevision }),
    t("rowMenu:toast.couldntChangePageLock")),
    // Undone for oneself, it shows at the press (lockForMeShownFirst.ts).
    onLockForMe: (unlocked: boolean) => void changeLock(() => lockForMeShownFirstIn(setSnapshot, template.id, unlocked,
      () => setDatabaseRowPageLockForMe(session, template.id, { unlocked })), t("rowMenu:toast.couldntChangeLockForMe")),
  } : undefined;
  const titleProperty = properties.find((property) => property.type === "title");
  // Its page keeps a workspace emoji by its ID beside its kind, as the server's databaseTemplateRowIcon gives it.
  const look = draft.icon === snapshot.row.icon ? { icon: snapshot.row.icon, iconType: snapshot.row.iconType }
    : storedIconPresentation(draft.icon);
  // Untitled, it goes by the name it was made with, as its database's menus name it. Its values are the
  // template's own, which its page lays out as its database's pages are — pinned, in sections, hidden.
  const shown: DatabaseRowPageSnapshot = { ...snapshot, untitledName: draft.name, row: { ...snapshot.row, ...look,
    values: { ...templateCells.values, ...(titleProperty ? { [titleProperty.id]: { type: "title", value: draft.title } } : {}) } } };
  const valueRow: Page = { ...templateCells.row, title: draft.title, icon: snapshot.row.icon, iconType: "none" };
  const page = (<>
    <RowPageWhileReading mode={mode} onOpenMode={changeMode} properties={pageProperties} snapshot={shown} propertyValueContext={propertyCells.valueContext}
      sidePanelShown={analyticsDocked} pathDatabase={pathDatabase}
      valueRow={valueRow} editableRow={editable ? valueRow : undefined} titleCommitOn="input"
      propertyCellEnvironment={templateCells.environment}
      onRichTextChange={editable ? (property, value, textSpans) => {
        if (property.type === "title") setDraft((current) => ({ ...current, title: value }));
        else setProperty(property.id, value === "" ? null : value, textSpans);
      } : undefined}
      onPresentationChange={editable ? changeLook : undefined} onAddProperty={addProperty} onBodyContentChange={onBodyContentChange}
      // The builder opens over the template's window, which stays under it: "취소" and "모든 페이지에 적용" go
      // back to the template, as Notion's (seen 2026-11-16 at 1440), as a row page's do (useDatabasePageLayoutBuilder).
      onCustomizeLayout={addProperty ? () => {
        requestPageLayoutBuilder({ pageId: snapshot.row.containerPageId, dataSourceId: snapshot.row.dataSourceId, rowId: template.id,
          preview: { ...valueRow, ...look, hasContent: snapshot.body.length > 0 } });
      } : undefined}
      onOpenComments={(detail) => setComments({ detail })}
      onOpenActions={(trigger, extras) => setActions({ anchor: rowPageActionsAnchor(trigger), extras })}
      topBar={{ lock, onOpenUpdates: analyticsService ? () => setAnalyticsOpen(true) : undefined, onClosePanel: sidePanel.onClosePanel, discussionsDocked: sidePanel.discussionsDocked,
        // ☆ keeps the template's page among favorites; the library opens it over its database.
        onFavorite: (favorite) => void setDatabaseRowPageFavorite(session, template.id,
          { favorite, expectedAuthorityRevision: snapshot.row.authorityRevision })
          .then(() => readDatabaseRowPage(session, template.id)).then(setSnapshot)
          .catch(() => notify(t("rowMenu:toast.couldntUpdateFavorites"), "error")),
        onChangeMode: changeMode, onCopyLink: () => void copyText(serverUrl(templateAddress(template.id))).then((copied) =>
        notify(copied ? t("rowMenu:toast.copiedLink") : t("rowMenu:toast.couldntCopyLink"), copied ? "success" : "error")) }}
      onClose={close} containerTitle={databaseTitle}
      session={session} workspaceId={workspaceId} bodyReadOnly={!editable} currentUserId={session.user.id}
      onRestore={writable && snapshot.row.trashedAt && onRestore ? async () => {
        const restored = await onRestore();
        if (restored) setSnapshot(await readDatabaseRowPage(session, template.id));
        return restored;
      } : undefined}
      onDeleteForever={writable && snapshot.row.trashedAt && onDeleteForever ? async () => {
        const deleted = await onDeleteForever();
        if (deleted) onClose();
        return deleted;
      } : undefined}
      // A template in the trash keeps its repeat button, as Notion's (seen 2026-10-30).
      banner={<TemplateEditingBanner databaseTitle={databaseTitle} databaseIcon={databaseIcon} viewing={!writable}
        repeat={writable ? { template, onSave: onSetRepeat } : undefined} />}
      template />
    {actions ? (
      <TemplatePageMenu session={session} snapshot={shown} title={draft.title} anchor={actions.anchor} extras={actions.extras}
        readOnly={!editable} lock={lock} onClose={() => setActions(null)}
        onCopyLink={async () => {
          const copied = await copyText(serverUrl(templateAddress(template.id)));
          notify(copied ? t("rowMenu:toast.copiedLink") : t("rowMenu:toast.couldntCopyLink"), copied ? "success" : "error");
          setActions(null);
        }}
        onDuplicate={editable && onDuplicate ? async () => { onDuplicate(); setActions(null); } : undefined}
        onTrash={editable && onDelete ? async () => { setActions(null); onDelete(); } : undefined}
        onAnalytics={analyticsService ? () => {
          const whole = actions.extras.openWhole;
          setActions(null);
          if (whole) openWholeWithUpdates(template.id, whole); else setAnalyticsOpen(true);
        } : undefined} />
    ) : null}
    {reading ? null : <PageAnalyticsObserver input={{ kind: "member", workspaceId, pageId: snapshot.row.containerPageId, rowId: template.id, inTrash: Boolean(snapshot.row.trashedAt) }} />}
    {analyticsOpen && analyticsService ? (
      <Suspense fallback={null}>
        <PageAnalyticsPanel initialTab="updates" docked={analyticsDocked} pageId={snapshot.row.containerPageId} rowId={template.id} workspaceId={workspaceId}
          service={analyticsService} onClose={() => setAnalyticsOpen(false)}
          updates={{ pageTitle: draft.title, path: databaseTitle, pathLinks: [{ id: snapshot.row.containerPageId, title: databaseTitle }], session,
            titleLabel: properties.find((property) => property.type === "title")?.name, properties }} />
      </Suspense>
    ) : null}
    {comments ? (
      <CommentsPanel key={`${comments.detail?.all ? "all" : comments.detail?.blockId ?? ""}:${comments.detail?.activeCommentId ?? ""}:${comments.detail?.compose ? "new" : ""}`}
        pageId={snapshot.row.containerPageId} rowId={template.id} blockId={comments.detail?.blockId} compose={comments.detail?.compose}
        all={comments.detail?.all} docked={sidePanel.discussionsDocked} workspaceId={workspaceId}
        activeCommentId={comments.detail?.activeCommentId} initialQuote={comments.detail?.initialQuote}
        threadIds={comments.detail?.threadIds} initialQuoteStart={comments.detail?.initialQuoteStart} initialQuoteEnd={comments.detail?.initialQuoteEnd}
        initialQuotePlaces={comments.detail?.initialQuotePlaces}
        session={session} canEditPage={editable} canComment={snapshot.accessRole !== "view" && !snapshot.row.trashedAt}
        onClose={() => setComments(null)} onStatus={(message) => notify(message, "error")} />
    ) : null}
  </>);
  // Its layout's tabs show their views as a database page's do, read from the page holding its database where
  // no view around gives them theirs — a template opened from the database's "새로 만들기" — with filter, sort,
  // search, "새로 만들기" and an empty view's "필터 편집하기 · + 새 페이지" (Notion's, seen 2026-11-12).
  const own = <OpenDatabasePagesContext.Provider value={openedPages}><PageLayoutTabHolderContext.Provider value={tabHolder}>
    <FilterThisPageContext.Provider value={thisPage}><TemplateWindowContext.Provider value={windowShown}>{page}</TemplateWindowContext.Provider></FilterThisPageContext.Provider>
  </PageLayoutTabHolderContext.Provider></OpenDatabasePagesContext.Provider>;
  return typeof document === "undefined" ? own : createPortal(own, document.body);
}

/**
 * The database's own icon in its templates' band: a full-page database's
 * page's, else its source's — never the page holding an inline database, as
 * Notion's (seen 2026-11-15: an inline database in a 👋 page shows the plain
 * database mark, one with 📘 its 📘). Unread yet, none: the plain mark.
 */
export function templateBandDatabaseIcon(holder: PageIconSource | undefined,
  source: { icon?: string | null; iconType?: string | null } | undefined, workspaceId?: string): PageIconSource | undefined {
  if (holder?.kind === "database") return holder;
  return source ? { icon: source.icon ?? "", iconType: source.iconType ?? "none", workspaceId } : undefined;
}
