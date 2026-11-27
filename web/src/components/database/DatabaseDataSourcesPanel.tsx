import { useContext, useEffect, useMemo, useRef, useState, type ComponentProps, type PointerEvent } from "react";
import { useTranslation } from "react-i18next";
import { confirmAction, confirmActions } from "@/components/ConfirmActionDialog";
import { BlockMoveToDialog } from "@/components/editor/BlockMoveToDialog";
import { type MovePopoverAnchor } from "@/components/movePopoverPlacement";
import { popoverRectOf } from "@/lib/popoverPlacement";
import { useProductPages } from "@/components/product/productPagesContext";
import { pageHref } from "@/lib/navigation";
import { foundDatabaseSourcePlace, placeText } from "@/lib/pagePath";
import { inlineDatabaseFullPageHref } from "@/lib/inlineDatabaseFullPage";
import { usePageNames } from "@/lib/pageNames";
import { pushRoute } from "@/lib/router";
import { SidebarActionMenu, type SidebarMenuRow } from "@/components/sidebar/SidebarActionMenu";
import { useOptionalProductToast } from "@/lib/productToast";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import type { Page } from "@/types";
import { Copy, DotsHorizontal, MoveIcon, Plus, Trash, X } from "@/icons/leafen";
import { DataSourceMark, LinkedDataSourceMark } from "./DataSourceMark";
import { useCopyNotice } from "@/lib/copyNotice";
import { newId } from "@/lib/ids";
import { dropNativeEditorCommand, recordCommittedNativeEditorCommand } from "../editor/nativeEditorUndoOwner";
import { DatabaseDataSourcesContext, type DatabaseDataSourcesSurface } from "./databaseDataSourcesContext";
import type { DatabaseSettingsStart } from "./DatabaseMoreSettingsMenu";
import { DatabaseTaskSettings } from "./DatabaseTaskSettings";
import { DatabaseTaskSettingsHeader } from "./DatabaseTaskSettingsHeader";
import { databaseViewLabels } from "./databaseViewLabels";
import { dataSourceDisplayTitle } from "../editor/databaseTitles";
import taskStyles from "./DatabaseTaskSettings.module.css";
import panelMenuStyles from "./DatabasePanelMenu.module.css";
import styles from "./DatabaseDataSourcesPanel.module.css";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { pagesByIdOf } from "@/lib/pageIndex";
import { requestSourceFirstView } from "@/lib/viewTabRequests";
import { useHoverTip } from "../IconTipButton";
import { viewLimitRefusal } from "./databaseViewLimit";
import { EXISTING_COUNTED, existingSourcesShown, useSourceListLimit } from "./linkableSources";

// Manage data sources lists the sources a database owns, each of which can
// move to a page or another database or to trash, and the sources it links
// from other databases, each of which can be removed. It can add a new source
// or link an existing one.

type Screen =
  | { kind: "list" }
  | { kind: "link" };


/**
 * The sources to link where no new view can ask for one, counted as every
 * source window counts them (seen 2026-11-18 in Chrome 1440 from "데이터
 * 소스 관리" › "기존 데이터 소스 연결하기": four and "95개 더 보기" by one
 * source, three by two; a search's first twenty and "51개 더 보기") — the
 * hundred read, searched or not, and the reach kept through searches
 * (useSourceListLimit, existingSourcesShown).
 */
function CandidateSearch({
  placeholder,
  load,
  choose,
  describe,
  ahead,
}: {
  placeholder: string;
  load(query: string, signal: AbortSignal, upTo?: number): Promise<DatabaseContainerSource[]>;
  choose(candidate: DatabaseContainerSource): void;
  describe(candidates: DatabaseContainerSource[]): { key: string; title: string; detail: string; candidate: DatabaseContainerSource }[];
  /** The database's own sources and those it links, not listed but counted first. */
  ahead: number;
}) {
  const labels = databaseViewLabels();
  const { t } = useTranslation("databaseView");
  const [query, setQuery] = useState("");
  const [candidates, setCandidates] = useState<DatabaseContainerSource[]>([]);
  const { limit, more } = useSourceListLimit(query);
  useEffect(() => {
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      void load(query.trim(), controller.signal, EXISTING_COUNTED).then(setCandidates).catch(() => {
        if (!controller.signal.aborted) setCandidates([]);
      });
    }, query ? 120 : 0);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [load, query]);
  const results = describe(candidates);
  const { shown, hidden } = existingSourcesShown(candidates.map(({ dataSourceId }) => dataSourceId), results,
    { searched: query.trim() !== "", limit, ahead, key: ({ candidate }) => candidate.dataSourceId });
  return (
    <div className={styles.search}>
      <input autoFocus={fieldFocusOnOpen()} value={query} placeholder={placeholder} aria-label={placeholder}
        onChange={(event) => setQuery(event.currentTarget.value)} />
      <div className={panelMenuStyles.menuList} role="listbox" aria-label={placeholder}>
        {results.length === 0 ? <p className={styles.empty}>{labels.noDataSourceResults}</p> : shown.map((result) => (
          <button key={result.key} type="button" role="option" aria-selected="false"
            data-data-source-candidate={result.candidate.dataSourceId}
            onClick={() => choose(result.candidate)}>
            <DataSourceMark source={result.candidate} />
            <span>{result.title}</span>
            <small>{result.detail}</small>
            <span />
          </button>
        ))}
        {hidden > 0 ? (
          <button type="button" data-data-source-candidate-more onClick={more}>
            <DotsHorizontal size="regular" aria-hidden="true" /><span>{t("showMoreDataSources", { count: hidden })}</span><small /><span />
          </button>
        ) : null}
      </div>
    </div>
  );
}

export function DatabaseDataSourcesPanel({
  surface,
  onBack,
  onClose,
}: {
  surface: DatabaseDataSourcesSurface;
  onBack(): void;
  /** Closes the view settings: a source just added leaves its "새 데이터 소스" panel alone, as Notion's (seen 2026-11-02). */
  onClose?(): void;
}) {
  const pageNames = usePageNames();
  const labels = databaseViewLabels();
  const { t } = useTranslation(["databaseView", "common"]);
  const toast = useOptionalProductToast();
  const refusal = viewLimitRefusal(surface.viewCount), refusalTip = useHoverTip({ delay: 180 });
  const [screen, setScreen] = useState<Screen>({ kind: "list" });
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  // A copied ID is said in a toast (lib/copyNotice), not in the line under the list.
  const copyNotice = useCopyNotice();
  // A source row's "⋯" opens its menu there, as Notion's (seen 2026-11-02).
  const [menuFor, setMenuFor] = useState<string | null>(null);
  const menuButton = useRef<HTMLButtonElement | null>(null);
  // The source "다음으로 이동:" moves, with where its picker hangs.
  const [moving, setMoving] = useState<{ dataSourceId: string; anchor: MovePopoverAnchor | null } | null>(null);
  const pages = useProductPages()?.pages;
  const pagesById = pagesByIdOf(pages ?? []);
  const members = useMemo(() => new Set([
    ...surface.own.map(({ id }) => id), ...surface.linked.map(({ dataSourceId }) => dataSourceId),
  ]), [surface.linked, surface.own]);
  const titleOf = (dataSourceId: string) => (
    dataSourceDisplayTitle(surface.own.find(({ id }) => id === dataSourceId)?.title, surface.naming)
  );

  async function run(action: () => Promise<unknown>) {
    if (busy) return;
    setBusy(true);
    setNotice("");
    try {
      await action();
    } finally {
      setBusy(false);
    }
  }

  // Notion asks before a source goes to the trash, its action filled red
  // (seen 2026-11-08), then offers to take it back (seen 2026-11-02).
  async function trash(dataSourceId: string) {
    const title = titleOf(dataSourceId);
    const views = surface.viewCounts[dataSourceId] ?? 0;
    if (!await confirmAction({
      message: t("databaseView:dataSourceTrashTitle", { title }), detail: t("databaseView:dataSourceTrashDetail", { count: views }),
      confirmLabel: t("databaseView:dataSourceTrashConfirm"), cancelLabel: t("common:actions.cancel"), danger: "filled",
    })) return;
    await run(async () => {
      if (!await surface.trash(dataSourceId)) return;
      toast?.notify(t("databaseView:dataSourceTrashed", { title }), "default",
        { label: t("databaseView:undo"), onClick: () => { void surface.restore(dataSourceId); } });
    });
  }

  function sourceMenu(dataSourceId: string): SidebarMenuRow[] {
    const copy: SidebarMenuRow = { key: "copy-id", label: labels.copyDataSourceId, icon: <Copy size="regular" />,
      data: { "data-data-source-copy-id": dataSourceId }, run: () => {
        setMenuFor(null);
        void copyNotice(dataSourceId, t("databaseView:dataSourceIdCopied", { name: titleOf(dataSourceId) }));
      } };
    // A reader copies the source's ID and nothing more, as Notion's (seen 2026-10-12).
    if (surface.readOnly) return [copy];
    // Its row, with no "›" at its end, opens the page picker at the menu's
    // start side by the row, as Notion's (seen 2026-11-11: 330×432, its end
    // at the row's start, moved up to show whole).
    const move: SidebarMenuRow = { key: "move", label: t("databaseView:moveDataSourceToMenu"), icon: <MoveIcon size="regular" />,
      data: { "data-data-source-move": dataSourceId }, run: () => {
        const row = popoverRectOf(document.querySelector(`[data-data-source-move="${CSS.escape(dataSourceId)}"]`));
        setMoving({ dataSourceId, anchor: row ? { ...row, beside: true } : null });
        setMenuFor(null);
      } };
    // A database's only source has no "휴지통으로 이동" here, as Notion's; the
    // one left out of the trash beside others in it has (both seen 2026-11-11).
    return surface.own.length < 2 ? [copy, move] : [copy, move,
      { key: "trash", label: labels.moveDataSourceToTrash, icon: <Trash size="regular" />,
        data: { "data-data-source-trash": dataSourceId }, run: () => { setMenuFor(null); void trash(dataSourceId); } },
    ];
  }

  // A page chosen gets a new database holding the source, a full-page
  // database's page takes it into that database; a source with views asks
  // whether they go with it, as Notion's (seen 2026-11-11: "데이터 소스 및 N개
  // 보기 이동" or "데이터 소스만 이동" in a 380px card with a "×"). The list
  // stays open, and a notice says where it went with "이동" there.
  async function move(dataSourceId: string, destination: Page) {
    const title = titleOf(dataSourceId);
    const views = surface.viewCounts[dataSourceId] ?? 0;
    const answer = views === 0 ? "source" : await confirmActions({
      message: t("databaseView:moveDataSourceTitle", { title }), detail: t("databaseView:moveDataSourceViewsDetail", { count: views }),
      icon: <MoveIcon size="regular" />, cancelLabel: null, primary: true, move: true,
      actions: [
        { value: "views", label: t("databaseView:moveDataSourceWithViews", { count: views }) },
        { value: "source", label: t("databaseView:moveDataSourceOnly") },
      ],
    });
    if (answer === null) return;
    const index = surface.own.findIndex(({ id }) => id === dataSourceId);
    // The picker closes once it is chosen, the list saying the source is on
    // its way (user decision 2026-11-12: a press answers on the next frame —
    // the picker stayed up while the server moved it); a refusal says why.
    void run(async () => {
      setNotice(labels.dataSourceMoving);
      const moved = await surface.move(dataSourceId, { pageId: destination.id, moveViews: answer === "views" });
      if (moved?.status !== "moving") setNotice("");
      if (moved?.status !== "moved") return;
      const message = destination.kind === "database" ? t("databaseView:dataSourceMovedTo", { title })
        // A page's new database goes by the source's name, "새 데이터베이스" while it has none (seen 2026-11-11).
        : t("databaseView:dataSourceMovedToPage", { title: dataSourceDisplayTitle(surface.own.find(({ id }) => id === dataSourceId)?.title),
          page: pageNames.title(destination) });
      const undo = offerMoveBack(dataSourceId, message, answer === "views" || !moved.databaseId ? null
        : { fromDatabaseId: moved.databaseId, index, viewId: moved.viewId });
      toast?.notify(message, "default", [
        // A database a page's body holds opens as its own page, a page or a full-page database as itself.
        { label: t("databaseView:dataSourceMovedGo"), onClick: () => pushRoute(destination.kind === "database" && destination.parentId
          && !pages?.some(({ id }) => id === destination.id) ? inlineDatabaseFullPageHref(destination.parentId, destination.id) : pageHref(destination.id)) },
        { label: t("databaseView:undo"), onClick: () => { void undo(); } },
      ]);
    });
  }

  // The notice's "실행 취소" and ⌘Z take a move of the source alone back, as
  // Notion's (seen 2026-11-11, to a page and to a database); one that took
  // the views along Notion refuses, "이 이동은 허용되지 않습니다." with "확인",
  // and so does this when the databases have changed since. Undone from the
  // notice it leaves the page's history; undone with ⌘Z its notice goes.
  function offerMoveBack(dataSourceId: string, message: string, back: Parameters<typeof surface.moveBack>[1] | null) {
    const user = surface.session?.user.id;
    const scope = user ? `${surface.workspaceId}/${surface.pageId}/${user}` : null;
    const key = `data-source-move:${dataSourceId}:${newId()}`;
    let undone = false;
    const refuse = async () => {
      // Asked once the press on the notice has closed the open settings and
      // their focus has settled, which would otherwise close the card at once.
      await new Promise((settled) => requestAnimationFrame(() => requestAnimationFrame(settled)));
      await confirmAction({ message: t("databaseView:dataSourceMoveNotAllowed"),
        confirmLabel: t("databaseView:dataSourceMoveNotAllowedOk"), cancelLabel: null });
      return false;
    };
    const step = async (forward: boolean) => {
      if (forward || undone) return false;
      if (!back) return refuse();
      const status = await surface.moveBack(dataSourceId, back);
      if (status === "refused") return refuse();
      if (status === null) return false;
      if (status === "moving") setNotice(labels.dataSourceMoving);
      undone = true;
      toast?.dismissMessage(message);
      return true;
    };
    if (scope) recordCommittedNativeEditorCommand(scope, key, step);
    return async () => {
      if (await step(false) && scope) dropNativeEditorCommand(scope, key);
    };
  }

  if (screen.kind === "link") {
    return (
      <div className={taskStyles.panel} data-database-data-sources="link" aria-busy={busy}>
        <DatabaseTaskSettingsHeader title={labels.linkExistingDataSource} backLabel={labels.manageDataSources}
          onBack={() => setScreen({ kind: "list" })} />
        <CandidateSearch placeholder={labels.searchDataSources} load={surface.loadCandidates}
          ahead={surface.own.filter(({ sourceInTrash }) => !sourceInTrash).length + surface.linked.length}
          describe={(candidates) => candidates.flatMap((candidate) => members.has(candidate.dataSourceId) ? [] : [{
            // An untitled one by how many its own database goes by, as every source picker names it.
            key: `${candidate.id}:${candidate.dataSourceId}`, title: dataSourceDisplayTitle(candidate.title, candidate.sourceTitles?.length ?? 1),
            // Where it sits, an untitled page holding it by its kind's name (foundDatabaseSourcePlace).
            detail: placeText(foundDatabaseSourcePlace(candidate, pagesById)), candidate,
          }])}
          choose={(candidate) => void run(async () => {
            // The settings close for the new view's "새 보기", as Notion's.
            if (await surface.link(candidate)) { if (onClose) onClose(); else setScreen({ kind: "list" }); }
          })} />
      </div>
    );
  }

  const onlyMember = surface.own.length + surface.linked.length <= 1;
  // At its view limit "데이터 소스 추가" and "기존 데이터 소스 연결하기" are dim and make nothing, saying why under them
  // as the pointer rests there, as Notion's (viewLimitRefusal).
  const refused = refusal ? { "aria-disabled": true, className: styles.refused,
    onPointerEnter: (event: PointerEvent<HTMLButtonElement>) => refusalTip.enter(event.currentTarget, refusal), onPointerLeave: refusalTip.leave } : {};
  // A source in the trash leaves the list unless it is the database's only
  // one, as Notion's (seen 2026-11-11: one of three in the trash isn't
  // listed, nor are all three; an only source in the trash is).
  const listed = surface.own.filter((source) => !source.sourceInTrash || surface.own.length < 2);
  return (
    <div className={taskStyles.panel} data-database-data-sources="list" aria-busy={busy}>
      <DatabaseTaskSettingsHeader title={labels.manageDataSources} backLabel={labels.backToViewSettings} onBack={onBack} />
      {/* The sources with "데이터 소스 추가", then "연결됨" with "기존 데이터 소스 연결하기", as Notion's (seen 2026-10-23);
          a linked block with no source of its own still heads "데이터 소스 추가" with "출처" (seen 2026-11-08). */}
      {(listed.length > 0 || !surface.readOnly) && <div className={styles.section}>{labels.dataSourcesOwn}</div>}
      <div className={styles.group}>
      {listed.map((source) => (
        <div key={source.id} className={`${styles.row} ${styles.sourceRow}`} data-data-source-row={source.id}>
          <DataSourceMark source={source} />
          <span>{dataSourceDisplayTitle(source.title, surface.naming)}</span>
          {/* A source no view shows counts nothing, as Notion's (seen 2026-11-11). */}
          <small>{surface.viewCounts[source.id] ? labels.viewsCount(surface.viewCounts[source.id]) : null}</small>
          <button type="button" aria-label={labels.dataSourceOptions(dataSourceDisplayTitle(source.title, surface.naming))}
            aria-haspopup="menu" aria-expanded={menuFor === source.id} disabled={busy}
            onClick={(event) => { menuButton.current = event.currentTarget; setMenuFor(source.id); }}>
            <DotsHorizontal size="small" />
          </button>
        </div>
      ))}
      {!surface.readOnly && (
        <div className={panelMenuStyles.menuList}>
          <button type="button" disabled={busy} data-data-source-add {...refused}
            onClick={() => { if (!refusal) void run(async () => { if (await surface.add()) onClose?.(); }); }}>
            <Plus size="regular" /><span>{labels.addDataSource}</span><small /><span />
          </button>
        </div>
      )}
      </div>
      {(surface.linked.length > 0 || !surface.readOnly) && <div className={styles.section}>{labels.dataSourcesLinked}</div>}
      <div className={styles.group}>
      {surface.linked.map((member) => (
        <div key={member.dataSourceId} className={`${styles.row} ${styles.sourceRow}`} data-linked-data-source-row={member.dataSourceId}>
          <LinkedDataSourceMark />
          <span>{dataSourceDisplayTitle(member.title)}</span>
          <small>{labels.viewsCount(surface.viewCounts[member.dataSourceId] ?? 0)}</small>
          {!surface.readOnly && (
            <button type="button" aria-label={labels.removeLinkedDataSource(dataSourceDisplayTitle(member.title))}
              disabled={busy || onlyMember} onClick={() => void run(() => surface.unlink(member.dataSourceId))}>
              <X size="small" />
            </button>
          )}
        </div>
      ))}
      {!surface.readOnly && (
        <div className={panelMenuStyles.menuList}>
          {/* "+" and no "›", as Notion's (seen 2026-11-10). Pressed, it makes a new table view whose settings
              open at its sources, "추천" — the database's own when it has several — then "기존 데이터 소스", as
              Notion's (seen 2026-11-15); where no tabs can make one, the sources to link show here. */}
          <button type="button" disabled={busy} data-data-source-link {...refused}
            onClick={() => { if (refusal) return; if (!requestSourceFirstView(surface.databaseId)) setScreen({ kind: "link" }); else onClose?.(); }}>
            <Plus size="regular" /><span>{labels.linkExistingDataSource}</span><small /><span />
          </button>
        </div>
      )}
      </div>
      {notice && <p className={styles.notice} role="status">{notice}</p>}
      {refusalTip.tip}
      {menuFor ? <SidebarActionMenu button={menuButton} label={labels.dataSourceOptions(titleOf(menuFor))} width={200}
        rows={sourceMenu(menuFor)} backdropLabel={t("common:actions.close")} close={() => setMenuFor(null)} /> : null}
      {moving && surface.session ? <BlockMoveToDialog blockId={surface.databaseId} sourcePageId={surface.pageId}
        workspaceId={surface.workspaceId} pages={pages ? [...pages] : []} session={surface.session} sourcePage="always" databases
        // The database it is in is no choice, nor a full-page one's own page (Notion's suggestions leave it out, seen 2026-11-11).
        hiddenPageIds={[surface.databaseId, ...(pages?.some(({ id, kind }) => id === surface.pageId && kind === "database") ? [surface.pageId] : [])]}
        title={t("databaseView:moveDataSourceToMenu")} placeholder={t("databaseView:moveDataSourceSearch")} anchor={moving.anchor}
        onChooseDestination={(_id, _signal, destination) => move(moving.dataSourceId, destination)}
        onClose={() => setMoving(null)} onPageChange={() => {}} /> : null}
    </div>
  );
}

// The view settings' Manage data sources, when the editor can change the
// database's sources.
export function DatabaseDataSourcesScreen({ onBack, onClose }: { onBack(): void; onClose?(): void }) {
  const surface = useContext(DatabaseDataSourcesContext);
  return surface ? <DatabaseDataSourcesPanel surface={surface} onBack={onBack} onClose={onClose} /> : null;
}

// The view settings' More settings: the task features, at the one "추가
// 설정" chose (DatabaseMoreSettingsMenu).
export function DatabaseSettingsMenu(
  { start, ...props }: Omit<ComponentProps<typeof DatabaseTaskSettings>, "initialScreen"> & {
    start?: DatabaseSettingsStart;
  },
) {
  return <DatabaseTaskSettings {...props} initialScreen={start} />;
}
