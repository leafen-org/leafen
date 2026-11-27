import { writeDatabaseViewChoice } from "@/lib/databaseViewChoice";
import { memo, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type {
  GraphBlock,
  GraphDataSource,
  GraphProperty,
  GraphRow,
  GraphView,
} from "@/contentGraph";
import type { NativeFormService } from "@/lib/nativeFormService";
import { newId } from "@/lib/ids";
import { nativeEditorCommandIsLatest, recordCommittedNativeEditorCommand } from "../editor/nativeEditorUndoOwner";
import { choosePageBlocks, requestPageUndo } from "../editor/pageUndo";
import { requestDatabaseStartPanel } from "@/lib/databaseStartPanel";
import type { Page, Session } from "@/types";
import type { NativeBoardProductAdapter } from "@/lib/nativeBoardProductAdapter";
import type { NativeDatabaseViewProductAdapter } from "@/lib/nativeDatabaseViewProductAdapter";
import type { NativeTableProductAdapter } from "@/lib/nativeTableProductAdapter";
import type { NativeTimelineProductAdapter } from "@/lib/nativeTimelineProductAdapter";
import type { DatabaseFilterGraph } from "./databaseFilterSnapshot";
import { NativeDatabaseViewMount } from "./NativeDatabaseViewMount";
import { databaseBlockMembers, databaseListedOwnSourceIds, databaseNamingSourceIds, databaseOwnNamingSourceIds, databaseSourceInTrash, databaseViewBlock } from "@/lib/nativeDataSourceManagement";
import { databaseViewLabels } from "./databaseViewLabels";
import { i18next } from "@/i18n";
import { DatabaseDataSourcesContext, type DatabaseDataSourcesSurface } from "./databaseDataSourcesContext";
import { DatabaseFirstViewLinkContext, DatabaseFullPageLinkContext, DatabaseOwnPageContext, DatabasePageTitleNamesSourceContext } from "./databaseFullPageLink";
import { DatabaseLockContext, DatabaseSourceLockContext, DatabaseStructureLockContext, type DatabaseLockControls } from "./databaseStructureLock";
import { hasExplicitPageIcon } from "../page/PageIcon";
import { PageIconPicker } from "../page/PageIconPicker";
import { DatabaseTitleIcon, INLINE_TITLE_ICON } from "./DatabaseTitleIcon";
import { useInlineDatabaseRequests, useInlineDatabaseTitleJoin } from "@/lib/inlineDatabaseRequests";
import { inlineDatabaseTitleDisplay, linkedDatabaseName, untitledDataSourceTitle } from "../editor/databaseTitles";
import { PageRenamePopover } from "../page/PageRenamePopover";
import { ViewBlockNameEditor, viewBlockNameAt } from "./ViewBlockNameEditor";
import { databaseNamingSourceTitles, inlineDatabaseFullPageHref, inlineDatabaseName, inlineDatabaseOpenedAlone, linkedDatabasePresentation } from "@/lib/inlineDatabaseFullPage";
import { publishOpenedAloneDatabaseLock, withdrawOpenedAloneDatabaseLock } from "./openedAloneDatabaseLock";
import databaseStyles from "./database.module.css";
import editorStyles from "../editor/editorStyles.generated.module.css";
import formHeadStyles from "./InlineDatabaseFormHead.module.css";
import { DatabaseArchivedContext } from "./databaseArchive";
import { pushRoute } from "@/lib/router";
import { layoutDefaults } from "./viewLayoutDefaults";
import { InlineDatabaseLinkedTitle, useLinkedSourceHref } from "./InlineDatabaseLinkedTitle";
import { POPOVER_MARGIN } from "@/lib/popoverPlacement";
import { chordLetter, primaryModifier } from "@/lib/keyboardShortcuts";
import type { TextSpan } from "@/lib/types";
import { DatabaseTitleField, DatabaseTitleText, respelledName } from "./DatabaseTitleField";
import { cellValueFromSpans } from "./richTextCellModel";
import type { RichTextCellHandle } from "./RichTextCellEditor";
import { useDatabaseHolderMarks } from "./databaseBlockRowMarks";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

const styles = { ...databaseStyles, ...editorStyles };

type InlineDatabaseProps = {
  block: GraphBlock;
  source: GraphDataSource;
  /** First member source; its title names a multi-source database. */
  titleSource?: GraphDataSource;
  dataSources: GraphDataSource[];
  filterGraph: DatabaseFilterGraph;
  views: GraphView[];
  /** Widget views of this database's dashboards. */
  dashboardWidgets?: GraphView[];
  activeView: GraphView;
  properties: GraphProperty[];
  rows: GraphRow[];
  boardAdapter?: NativeBoardProductAdapter;
  databaseViewAdapter: NativeDatabaseViewProductAdapter;
  tableAdapter?: NativeTableProductAdapter;
  timelineAdapter?: NativeTimelineProductAdapter;
  pageId: string;
  workspaceId: string;
  session: Session | null;
  formService?: NativeFormService;
  onAddProperty?: (
    source: GraphDataSource,
    type: GraphProperty["type"],
    name: string,
    config?: Record<string, unknown>,
  ) => Promise<GraphProperty | null>;
  onConfiguredView?: (view: GraphView) => void;
  configurationReady?: boolean;
  readOnly?: boolean;
  /** Read only by its page's lock alone, for someone who may edit it: its own "잠금" still unlocks it. */
  lockableUnderPageLock?: boolean;
  /** The database is locked: its rows stay editable, its views and properties don't. */
  structureLocked?: boolean;
  staticReadOnlySemantics?: boolean;
  /** Where the database opens alone as a page; none on a database's own page. */
  fullPageHref?: string;
  /** The database is its page's, whose title names it, so it has no title of its own. */
  ownPage?: boolean;
  /** On its own page, the page's lock, which its view settings lock and unlock for everyone. */
  pageLock?: DatabasePageLock;
};

export type DatabasePageLock = { locked: boolean; set(locked: boolean): void };

function InlineDatabaseView({
  block,
  source,
  titleSource = source,
  databaseViewAdapter,
  configurationReady = true,
  readOnly = false,
  lockableUnderPageLock = false,
  structureLocked = false,
  fullPageHref,
  ownPage = false,
  pageLock,
  ...viewProps
}: InlineDatabaseProps) {
  // An untitled source is "새 데이터 소스" once the database shows more than
  // one, its own or one it links (databaseNamingSourceIds).
  const namingSources = databaseNamingSourceIds(block);
  const untitledTitle = untitledDataSourceTitle(namingSources.length);
  // With several sources its title names the open view's source, as
  // Notion's (seen 2026-11-02); on its own page that is the heading under
  // the database's name, which its sources name while it has none.
  const headSource = namingSources.length > 1 && namingSources.includes(source.id) ? source : titleSource;
  const titleField = useRef<RichTextCellHandle>(null);
  // A linked database goes by its own name where it has one, as Notion's does.
  const linkedName = block.content.linkedDatabaseSource === true ? linkedDatabasePresentation(block).title : "";
  const titleDisplay = inlineDatabaseTitleDisplay({ ownTitle: linkedName || headSource.title, placeholder: untitledTitle });
  // Its own lock locks its views and properties, unless the reader unlocked
  // it for themselves; a database's own page locks with its page. An inline
  // database opened alone as a page keeps its own lock. On a locked page its
  // "잠금" still unlocks it, for the reader or everyone, the page staying
  // locked, as Notion's (seen 2026-11-16 in Chrome, two tabs).
  const unlockable = !readOnly || lockableUnderPageLock;
  const blockLocked = block.content.locked === true;
  const linkedBlock = block.content.linkedDatabaseSource === true;
  // A view block — linked, waiting for its source, or a dashboard — locks its
  // views, not a database, and is renamed in a box, as Notion's (databaseViewBlock).
  const viewBlock = databaseViewBlock(block, [...viewProps.views, ...(viewProps.dashboardWidgets ?? [])]);
  const [unlockedForMe, setUnlockedForMe] = useState(false);
  useEffect(() => { if (!blockLocked) setUnlockedForMe(false); }, [blockLocked]);
  // Opened alone, its lock shows in the top bar, as Notion's (openedAloneDatabaseLock.ts).
  const alone = !pageLock && inlineDatabaseOpenedAlone() === block.id;
  // Its lock and its sources' surface are the same while what they show is,
  // so the views under them aren't drawn again as a row's values change.
  const lock = useMemo<DatabaseLockControls | null>(() => ownPage && pageLock ? (!readOnly ? {
    locked: pageLock.locked,
    page: true,
    views: viewBlock,
    unlockedForMe: false,
    lock: () => pageLock.set(true),
    unlockForEveryone: () => pageLock.set(false),
    relock: () => {},
  } : null) : {
    locked: blockLocked,
    views: viewBlock,
    topBar: alone,
    unlockedForMe,
    lock: readOnly ? undefined : () => databaseViewAdapter.setLocked(block, true),
    unlockForEveryone: unlockable ? () => databaseViewAdapter.setLocked(block, false) : undefined,
    unlockForMe: unlockable ? () => setUnlockedForMe(true) : undefined,
    relock: () => setUnlockedForMe(false),
  }, [alone, block, blockLocked, databaseViewAdapter, ownPage, pageLock, readOnly, unlockable, unlockedForMe, viewBlock]);
  const latestLock = useRef(lock);
  latestLock.current = lock;
  useEffect(() => {
    if (!alone || !latestLock.current) return undefined;
    const current = latestLock.current;
    publishOpenedAloneDatabaseLock({ blockId: block.id, lock: {
      ...current, lock: () => latestLock.current?.lock?.(), unlockForEveryone: current.unlockForEveryone && (() => latestLock.current?.unlockForEveryone?.()),
      unlockForMe: current.unlockForMe && (() => latestLock.current?.unlockForMe?.()), relock: () => latestLock.current?.relock(),
    } });
    return () => withdrawOpenedAloneDatabaseLock(block.id);
  }, [alone, block.id, blockLocked, unlockedForMe, readOnly, unlockable]);
  const locked = structureLocked || (blockLocked && !unlockedForMe);
  // A view block's lock — its block's, or its own page's — locks its views, not its source.
  const sourceLocked = (structureLocked && !(ownPage && viewBlock)) || (blockLocked && !unlockedForMe && !viewBlock);
  // Its block menu's "아이콘 편집" and "이름 바꾸기" open the icon picker below
  // its title and select the title, showing a hidden title meanwhile. Its
  // title's "⋯ › 제목 편집" does the same, but a linked view's names its
  // source in place under its "↗" (`title`), as Notion's (seen 2026-11-28 in
  // Chrome in two tabs at two times: the source's name chosen in the head and
  // renamed there, the source's own head with it).
  const [editing, setEditing] = useState<"icon" | "rename" | "title" | null>(null);
  // Drawn twice (opened alone in a peek over its page), the copy that shows answers.
  const copy = { blockId: block.id, alone: ownPage };
  useInlineDatabaseRequests(block.id, (request) => {
    if ((request !== "icon" && request !== "rename" && request !== "title") || readOnly || locked) return;
    setEditing(request !== "title" ? request : databaseBlockMembers(block).linked.some((member) => member.dataSourceId === source.id) ? "title" : "rename");
  }, copy);
  // Its name has the caret as "이름 바꾸기" draws it, in the same step (RenameTitleField's rule).
  useLayoutEffect(() => {
    if (editing === "rename" || editing === "title") titleField.current?.selectAll();
  }, [editing]);
  // A linked database's name and icon are its own, its source's staying as
  // they are (Notion's, seen 2026-10-15): "이름 바꾸기" opens Notion's small
  // box under its "↗ 〈source〉", and "아이콘 편집" its icon's picker. A view
  // block with no name row — a dashboard — opens either over its tabs, the
  // row staying hidden (ViewBlockNameEditor).
  const wrapperRef = useRef<HTMLDivElement>(null);
  useDatabaseHolderMarks(wrapperRef);
  const [renameAt, setRenameAt] = useState<{ top: number; left: number } | null>(null);
  useEffect(() => {
    const header = wrapperRef.current?.querySelector("[data-inline-database-header]");
    if (!viewBlock || !(editing === "rename" || (editing === "icon" && !header))) { setRenameAt(null); return; }
    const box = (header ?? wrapperRef.current?.querySelector("[data-database-chrome]") ?? wrapperRef.current)?.getBoundingClientRect();
    if (box) setRenameAt(header ? { top: box.bottom + 2, left: Math.max(POPOVER_MARGIN, box.left - 8) } : viewBlockNameAt(box.left, box.bottom, POPOVER_MARGIN));
    // Its rename leaves nothing chosen, before Escape or after (Notion's, seen 2026-11-06).
    if (editing === "rename") choosePageBlocks(viewProps.pageId, []);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editing, viewBlock]);
  const changeIcon = (icon: string, iconType: NonNullable<GraphDataSource["iconType"]>) => {
    if (linkedBlock) databaseViewAdapter.setLinkedPresentation(block, { icon: { icon, iconType } });
    else databaseViewAdapter.changeIcon(headSource, { icon, iconType });
    setEditing(null);
  };
  const manager = databaseViewAdapter.dataSources;
  const linkedSourceHref = useLinkedSourceHref();
  // A reader lists the sources too, changing nothing, as Notion's (seen 2026-10-12).
  const naming = namingSources.length;
  const dataSources = useMemo<DatabaseDataSourcesSurface | null>(() => {
    if (!manager) return null;
    const members = databaseBlockMembers(block);
    const titleOf = (id: string) => viewProps.dataSources.find((candidate) => candidate.id === id)?.title ?? "";
    // A view block's unnamed source isn't among its sources (databaseListedOwnSourceIds).
    return {
      databaseId: block.id,
      pageId: viewProps.pageId,
      workspaceId: viewProps.workspaceId,
      session: viewProps.session,
      readOnly: locked || readOnly,
      own: databaseListedOwnSourceIds(block, [...viewProps.views, ...(viewProps.dashboardWidgets ?? [])]).map((id) => {
        const source = viewProps.dataSources.find((candidate) => candidate.id === id) ?? { id, title: "" };
        return databaseSourceInTrash(block, source) ? { ...source, sourceInTrash: true } : source;
      }),
      linked: members.linked.map(({ dataSourceId, sourceContainerId }) => {
        const linked = viewProps.dataSources.find((candidate) => candidate.id === dataSourceId);
        return { dataSourceId, title: titleOf(dataSourceId), href: linked && linkedSourceHref(linked, sourceContainerId), sourceContainerId, pageId: linked?.pageId };
      }),
      add: async () => {
        const labels = databaseViewLabels();
        // The new source's view shows at once, under Notion's "새 데이터
        // 소스" panel (seen 2026-10-30 and 2026-11-02), unnamed as Notion
        // leaves it (seen 2026-11-11: "Untitled" through its API).
        const id = newId(), viewId = newId();
        const added = await manager.add(block, {
          title: "", titleProperty: labels.dataSourceTitleProperty, view: "", id, viewId,
        });
        if (added) {
          writeDatabaseViewChoice(viewProps.session?.user.id, block.id, viewId);
          requestDatabaseStartPanel(block.id, { dataSourceId: id });
        }
        return added;
      },
      // A linked source comes as a new view that asks which of its views to take (Notion's, seen 2026-11-02).
      link: async (candidate) => {
        const viewId = newId();
        const linked = await manager.link(block, candidate, { viewId, name: "" });
        if (linked) writeDatabaseViewChoice(viewProps.session?.user.id, block.id, viewId);
        return linked;
      },
      viewCount: viewProps.views.length,
      views: viewProps.views,
      naming,
      ...(naming > 1 ? { sourceTitles: databaseNamingSourceTitles(block, namingSources.map((id) => ({ id, title: titleOf(id) }))),
        databaseTitle: inlineDatabaseName(block, namingSources.map((id) => ({ id, title: titleOf(id) }))).title } : {}),
      viewCounts: viewProps.views.reduce<Record<string, number>>((counts, view) => ({ ...counts, [view.dataSourceId]: (counts[view.dataSourceId] ?? 0) + 1 }), {}),
      // A dashboard's widget turns too, from its settings' "데이터베이스" (Notion's, seen 2026-11-03).
      switchView: (viewId, candidate) => {
        const view = [...viewProps.views, ...(viewProps.dashboardWidgets ?? [])].find(({ id }) => id === viewId);
        return view ? manager.switchView(block, viewId, candidate, layoutDefaults(view.type, [])) : Promise.resolve(false);
      },
      unlink: (dataSourceId) => manager.unlink(block, dataSourceId),
      move: (dataSourceId, target) => manager.move(dataSourceId, target),
      moveBack: (dataSourceId, back) => manager.moveBack(dataSourceId, { ...back, databaseId: block.id }),
      trash: (dataSourceId) => manager.trash(dataSourceId),
      restore: (dataSourceId) => manager.restore(dataSourceId),
      loadCandidates: manager.loadCandidates,
      loadSourceViews: manager.loadSourceViews,
      adoptView: manager.adoptView,
      loadPages: manager.loadPages,
      reread: manager.reread,
    };
  }, [block, linkedSourceHref, locked, manager, naming, readOnly, viewProps.dashboardWidgets, viewProps.dataSources, viewProps.pageId,
    viewProps.session, viewProps.views, viewProps.workspaceId]);
  // A view of a source another database owns is titled by that source (InlineDatabaseLinkedTitle).
  const linkedMembers = databaseBlockMembers(block).linked;
  const linkedMember = linkedMembers.find((member) => member.dataSourceId === source.id);
  // A database's own page names its one source in the page's title (InlineDatabasePageTitle). Once it shows
  // another — its own or one another database owns — each view names its source under it, "↗" and the name for
  // another's (Notion's, seen 2026-11-12: its own source's name under the page's on its other views too).
  // With all its own sources in the trash, the page's title is the open view's source, alone (useTrashedSourcesHead).
  const ownMembers = databaseBlockMembers(block).own;
  const allOwnTrashed = ownMembers.length > 1 && linkedMembers.length === 0 && ownMembers.every((id) => databaseSourceInTrash(block, { id }));
  const pageTitleNamesSource = ownPage && !viewBlock && (allOwnTrashed
    || namingSources.length < 2 && databaseOwnNamingSourceIds(block).includes(titleSource.id));
  // A dashboard's title row, where it has one, is "새 대시보드" whatever its block is named, shown only on its own
  // page: inline its switch shows nothing (Notion's, seen 2026-11-09 — its name, its icon, its window); so is a
  // database page's dashboard view's, never its source's name (seen 2026-11-18).
  const dashboardTitle = (viewBlock && !linkedBlock) || (ownPage && viewProps.activeView.type === "dashboard");
  const titleIcon = { icon: headSource.icon ?? "", iconType: headSource.iconType ?? "none", workspaceId: viewProps.workspaceId };
  const nameField = (
    // Its name is rich text, as Notion's (seen 2026-11-07: ⌘B bolds, ⌘K opens the link box).
    <DatabaseTitleField handleRef={titleField} className={styles.inlineDatabaseTitle} value={headSource.title}
      spans={headSource.titleSpans as TextSpan[] | undefined} placeholder={untitledTitle}
      ariaLabel={i18next.t("databaseView:databaseTitle")} dataAttributes={{ "data-inline-database-title": "" }}
      onTitleChange={(title, spans) => {
        titleTyped({ title: headSource.title, spans: headSource.titleSpans as TextSpan[] | undefined }, { title, spans });
        databaseViewAdapter.changeTitle(headSource, title, spans);
      }}
      onFinish={() => { titleBurst.current = null; if (editing === "rename" || editing === "title") setEditing(null); }}
      // Enter keeps the caret in the title, as Notion's (seen 2026-11-03), unless it was only opened to rename.
      // Escape takes the caret to the name's start, and from there leaves it (Notion's, seen 2026-11-06).
      onKeyDown={(event) => {
        const at = titleField.current?.range();
        if (event.key === "Escape" && editing !== "rename" && at && at.start + at.end > 0) {
          titleField.current?.caretAt(0);
          return true;
        }
        if (fieldEnter(event) && editing === "rename") {
          (event.target as HTMLElement).blur(); return true;
        }
        if (primaryModifier(event) && !event.altKey && (chordLetter(event) === "z" || chordLetter(event) === "y")) {
          // Its ⌘Z is the page's, what was typed here one step of it, as Notion's (seen 2026-11-05).
          titleBurst.current = null;
          requestPageUndo(viewProps.pageId, event.shiftKey || chordLetter(event) === "y");
          return true;
        }
        return fieldEnter(event) ? true : undefined;
      }} />
  );
  // The view's "데이터 소스 제목 표시" switch hides the title, leaving its tabs first, as Notion's (seen 2026-09-30);
  // on its own page the page's title is the database's.
  // ⌥ makes no other press of its name: Notion's puts the caret in it where pressed, or opens a locked one alone, as
  // without ⌥ (seen 2026-11-25 in Chrome in two tabs, a letter typed after the press landing in the name; its
  // handle's ⌥-press opened its menu and a row name's its cell, as without ⌥) — "사이드 보기에서 열기" opens its peek.
  const titleOn = viewProps.activeView.config.showDataSourceTitle !== false;
  const title = !pageTitleNamesSource && (dashboardTitle ? ownPage && titleOn : titleOn || (editing && !viewBlock)) ? (
    <div className={viewProps.activeView.type === "form" ? `${styles.inlineDatabaseHeader} ${formHeadStyles.formHead}` : styles.inlineDatabaseHeader} data-inline-database-header>
      {!linkedMember && !dashboardTitle && hasExplicitPageIcon(titleIcon) && (
        <DatabaseTitleIcon className={INLINE_TITLE_ICON.className} icon={titleIcon} size={INLINE_TITLE_ICON.size(titleIcon)} onIconChange={!readOnly && !locked ? (next) => changeIcon(next.icon, next.iconType) : undefined}
          uploadTarget={{ workspaceId: viewProps.workspaceId, pageId: linkedBlock ? viewProps.pageId : headSource.pageId ?? viewProps.pageId }} />
      )}
      {dashboardTitle ? (
        <span className={styles.inlineDatabaseTitle} data-inline-database-title data-read-only="true">{i18next.t("databaseTitles:dashboardTitle")}</span>
      ) : linkedMember ? (
        <InlineDatabaseLinkedTitle source={source} containerId={linkedMember.sourceContainerId} className={styles.inlineDatabaseTitle}
          workspaceId={viewProps.workspaceId} field={editing === "title" ? nameField : undefined} />
      ) : readOnly || locked ? (
        // Held by its own lock, its name opens it alone, as Notion's (seen 2026-11-18 in Chrome, two tabs); unnamed, faint as the field's.
        <span className={styles.inlineDatabaseTitle} data-inline-database-title data-read-only="true" data-inline-database-placeholder={titleDisplay.isPlaceholder || undefined}
          onClick={() => { if (blockLocked && !unlockedForMe && fullPageHref) pushRoute(fullPageHref); }}>
          {linkedName ? titleDisplay.text : <DatabaseTitleText title={headSource.title} spans={headSource.titleSpans as TextSpan[] | undefined} fallback={titleDisplay.text} />}
        </span>
      ) : nameField}
      {editing === "icon" && !readOnly && !locked && (!linkedMember || linkedBlock) ? (
        <div className={styles.inlineDatabaseIconPicker}>
          <PageIconPicker placement="anchored" current={linkedBlock ? linkedDatabasePresentation(block) : headSource}
            uploadTarget={{ workspaceId: viewProps.workspaceId, pageId: linkedBlock ? viewProps.pageId : headSource.pageId ?? viewProps.pageId }}
            onPick={(emoji) => changeIcon(emoji, "emoji")}
            onPickImage={(url) => changeIcon(url, "image")}
            onPickCustomEmoji={(emoji) => changeIcon(emoji.id, "custom_emoji")}
            onPickBuiltInIcon={(value) => changeIcon(value, "icon")}
            onRemove={() => changeIcon("", "none")}
            onClose={() => setEditing(null)} />
        </div>
      ) : null}
    </div>
  ) : undefined;
  // A run of typing in the title is one step of the page's history, its
  // formatting with it.
  type TitleValue = { title: string; spans?: TextSpan[] };
  const titleBurst = useRef<{ key: string; to: TitleValue } | null>(null);
  const titleWriter = useRef({ set: (value: TitleValue) => databaseViewAdapter.changeTitle(headSource, value.title, value.spans ?? []) });
  titleWriter.current.set = (value) => {
    titleField.current?.set(value.spans?.length ? value.spans : value.title ? [{ text: value.title }] : []);
    databaseViewAdapter.changeTitle(headSource, value.title, value.spans ?? []);
  };
  const titleTyped = (from: TitleValue, to: TitleValue) => {
    const scope = `${viewProps.workspaceId}/${viewProps.pageId}/${viewProps.session?.user.id ?? ""}`;
    const burst = titleBurst.current;
    if (burst && nativeEditorCommandIsLatest(scope, burst.key)) { burst.to = to; return; }
    const next = { key: `database-title:${block.id}:${newId()}`, to };
    titleBurst.current = next;
    recordCommittedNativeEditorCommand(scope, next.key, async (forward) => {
      titleWriter.current.set(forward ? next.to : from);
      return true;
    });
  };
  // ⌫ after it and ⌦ before it join a line with its title (nativeTiptapMergeKeys), when the title is its own to edit.
  useInlineDatabaseTitleJoin(block.id, () => !title || linkedMember || dashboardTitle || readOnly || locked || !titleOn ? null : {
    title: headSource.title,
    set: (next, caretAt) => {
      // The words a line brings, or its Undo takes, leave the name's formatting as it was.
      const spans = respelledName(headSource.titleSpans as TextSpan[] | undefined, headSource.title, next);
      titleField.current?.set(spans);
      databaseViewAdapter.changeTitle(headSource, next, cellValueFromSpans(spans).spans ?? []);
      if (caretAt !== undefined) requestAnimationFrame(() => titleField.current?.caretAt(caretAt));
    },
  });
  // Opened alone from where it stands, it can also open at its first view.
  const firstView = viewProps.views[0];
  const firstViewHref = fullPageHref && firstView ? inlineDatabaseFullPageHref(viewProps.pageId, block.id, firstView.id) : null;
  return (
    <DatabaseStructureLockContext.Provider value={locked}>
    <DatabaseSourceLockContext.Provider value={sourceLocked}>
    <DatabaseLockContext.Provider value={lock}>
    <DatabaseOwnPageContext.Provider value={ownPage}>
    <DatabasePageTitleNamesSourceContext.Provider value={pageTitleNamesSource}>
    <DatabaseFullPageLinkContext.Provider value={fullPageHref ?? null}>
    <DatabaseFirstViewLinkContext.Provider value={firstViewHref}>
    <DatabaseDataSourcesContext.Provider value={dataSources}>
    <DatabaseArchivedContext.Provider value={block.content.archived === true}>
    <div
      className={`${styles.inlineDatabase} inline-database`}
      contentEditable={false}
      data-contained="true"
      data-native-v2="true"
      data-inline-database-wrapper
      data-block-id={block.id}
      ref={wrapperRef}
    >
      <NativeDatabaseViewMount
        {...viewProps}
        block={block}
        source={source}
        displayTitle={titleDisplay.text}
        title={title}
        databaseViewAdapter={databaseViewAdapter}
        configurationReady={configurationReady}
        readOnly={readOnly}
      />
      {renameAt && editing === "rename" && linkedBlock && !readOnly && !locked ? (
        <PageRenamePopover at={renameAt} iconFallback="database" title={linkedDatabasePresentation(block).title} untitled={linkedDatabaseName(undefined, source.title)}
          titleSpans={linkedDatabasePresentation(block).titleSpans as TextSpan[] | undefined}
          page={{ ...linkedDatabasePresentation(block), id: block.id, workspaceId: viewProps.workspaceId } as unknown as Page} asTyped
          onTitleChange={(title, spans) => databaseViewAdapter.setLinkedPresentation(block, { title, ...(spans ? { titleSpans: spans as GraphDataSource["titleSpans"] } : {}) })}
          onPresentationChange={(change) => {
            if (change.iconType) databaseViewAdapter.setLinkedPresentation(block, { icon: { icon: change.icon ?? "", iconType: change.iconType as NonNullable<GraphDataSource["iconType"]> } });
          }}
          onClose={() => setEditing(null)} />
      ) : renameAt && editing && editing !== "title" && viewBlock && !readOnly && !locked ? (
        <ViewBlockNameEditor editing={editing} at={renameAt} blockId={block.id} workspaceId={viewProps.workspaceId}
          uploadPageId={linkedBlock ? viewProps.pageId : headSource.pageId ?? viewProps.pageId} title={headSource.title}
          icon={linkedBlock ? linkedDatabasePresentation(block).icon : headSource.icon ?? ""}
          iconType={(linkedBlock ? linkedDatabasePresentation(block).iconType : headSource.iconType ?? "none") as NonNullable<GraphDataSource["iconType"]>}
          titleSpans={headSource.titleSpans as TextSpan[] | undefined}
          onTitleChange={(title, spans) => databaseViewAdapter.changeTitle(headSource, title, spans as GraphDataSource["titleSpans"])} onIconChange={changeIcon}
          onClose={() => setEditing(null)} />
      ) : null}
    </div>
    </DatabaseArchivedContext.Provider>
    </DatabaseDataSourcesContext.Provider>
    </DatabaseFirstViewLinkContext.Provider>
    </DatabaseFullPageLinkContext.Provider>
    </DatabasePageTitleNamesSourceContext.Provider>
    </DatabaseOwnPageContext.Provider>
    </DatabaseLockContext.Provider>
    </DatabaseSourceLockContext.Provider>
    </DatabaseStructureLockContext.Provider>
  );
}

/**
 * An inline database, drawn again only when what it is given changes: the
 * page around it draws for many other reasons (a page list read again, a
 * status line, presence), and drawing a table of 100,000 rows again took
 * 0.25-0.3 seconds each time (2026-11-11).
 */
export const InlineDatabase = memo(InlineDatabaseView);
