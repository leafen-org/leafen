import { relationPickerOffers, type RelationPickerScope } from "./RelationPickerFilter";
import { type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { openRelationPickers, relationPickerKey } from "./otherRowsRead";
import { RelationPickerProperties, RelationPickerValues, relationPickerShownIds } from "./RelationPickerProperties";
import type { PropertyValueReadContext } from "./PropValue";
import { pageHref } from "@/lib/navigation";
import { usePageNames } from "@/lib/pageNames";
import type { DbProperty, Page } from "@/lib/types";
import { unreadableRelationIds, valueAsIds } from "./rollup";
import { abortWhileRunning, capturePropertyCellRequest, type PropertyCellEnvironment, type PropertyCellPresentation } from "./propertyCellTypes";
import { useRelatedPageOpener } from "./relatedPageOpen";
import { PropertyRelationCellView } from "./PropertyRelationCellView";
import { useRelationTargetRowName } from "./useRelationTargetRowName";
import { useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import { useTranslation } from "react-i18next";
import { restoreFocusUnlessChosen } from "./tableCellKeys";
import { confirmAction } from "../ConfirmActionDialog";
import { ArrowUpRightIcon } from "@/icons/leafen";
import { relationPagesFound, relationPagesInOrder, useRelationPageVisits } from "./relationPageOrder";
import { useCellReadOnly } from "./cellReadOnly";
import type { RelationNewPage } from "./relationSection";
import { afterNextPaint } from "@/lib/afterPress";

// The picker's last row, which makes a page under the name searched for.
const CREATE_CHOICE = "__create";

// The picker shows what its database's rows already hold; it asks for nothing more.
const PICKER_SCHEMA_READ_OWNER = Object.freeze({ request: () => false, retire: () => {} });

export function PropertyRelationCell({ row, prop, onOpenPage, presentation, snapshot, operations, context }: {
  row: Page;
  prop: DbProperty;
  onOpenPage?: (pageId: string) => void;
  presentation?: PropertyCellPresentation;
} & PropertyCellEnvironment) {
  const pageNames = usePageNames();
  const openRelated = useRelatedPageOpener();
  const { t } = useTranslation(["propertyCell", "databaseView"]);
  const [open, setOpen] = useState(false);
  const readOnly = useCellReadOnly();
  // Open, its picker lists the database's pages: the cell is drawn again for any change (otherRowsRead).
  useLayoutEffect(() => {
    if (!open) return;
    const key = relationPickerKey(row.id, prop.id);
    openRelationPickers.add(key);
    return () => { openRelationPickers.delete(key); };
  }, [open, prop.id, row.id]);
  const [queryText, setQueryText] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const popoverRef = useRef<HTMLDivElement>(null);
  const targetDbId = prop.config?.relationDatabaseId ?? prop.databaseId;
  const targetDb = snapshot.pagesById[targetDbId];
  const targetRowsLoaded = snapshot.loadedDataSourceIds.has(targetDbId);
  const targetRowsPage = snapshot.relationPagesByDataSourceId[targetDbId];
  const ordinaryTargetRowsPending = targetDbId !== prop.databaseId
    && !targetRowsLoaded
    && !targetRowsPage?.error;
  const ordinaryTargetRowsFailed = targetDbId !== prop.databaseId
    && !targetRowsLoaded
    && Boolean(targetRowsPage?.error);
  const selectedIds = valueAsIds(row.properties?.[prop.id], snapshot.formulaBounds.rollup);
  const selectedSet = new Set(selectedIds);
  const selectedPages = selectedIds
    .map((id) => snapshot.pagesById[id])
    .filter((page): page is Page => Boolean(page && !page.inTrash));
  const loadedSelectedIds = new Set(selectedPages.map((page) => page.id));
  const unresolvedSelectedIds = selectedIds.filter((id) => !loadedSelectedIds.has(id));
  const pendingSelectedCount = unresolvedSelectedIds.filter((id) => !snapshot.hydratedRelationTargetIds.has(id)).length;
  const missingSelectedCount = unresolvedSelectedIds.length - pendingSelectedCount;
  const unreadableSelectedCount = unreadableRelationIds(selectedIds, snapshot.pagesById, snapshot.hydratedRelationTargetIds).length;
  // An archived row keeps its relations but is not offered as a new one until the picker's filter says.
  const [scope, setScope] = useState<RelationPickerScope>("active");
  const targetProperties = (targetDbId ? snapshot.propertiesByDataSourceId?.[targetDbId] : undefined) ?? [];
  const shownIds = relationPickerShownIds(prop);
  const valueContext = useMemo<PropertyValueReadContext>(() => ({
    bounds: snapshot.formulaBounds, contextIdentity: `relation-picker:${prop.id}`, hydratedRelationTargetIds: snapshot.hydratedRelationTargetIds,
    pagesById: snapshot.pagesById, propsByDatabaseId: snapshot.propertiesByDataSourceId, schemaReadOwner: PICKER_SCHEMA_READ_OWNER,
    snapshotIdentity: `relation-picker:${prop.id}`, userId: snapshot.userId ?? undefined,
  }), [prop.id, snapshot.formulaBounds, snapshot.hydratedRelationTargetIds, snapshot.pagesById, snapshot.propertiesByDataSourceId, snapshot.userId]);
  const verificationIds = targetProperties.filter((property) => property.type === "verification").map((property) => property.id);
  // The reader's visits, read once each time the picker opens.
  const visited = useRelationPageVisits(open, context.workspaceId);
  // Notion's order (relationPagesInOrder), read while the picker is open
  // alone, and again when what it reads changes: a closed cell drew every
  // page the view had read each time it drew (2026-11-11, a view of 100,000
  // rows).
  const verificationKey = verificationIds.join("\n");
  const candidates = useMemo(() => {
    if (!open) return [];
    const verifications = verificationKey ? verificationKey.split("\n") : [];
    return relationPagesInOrder(Object.values(snapshot.pagesById)
      .filter((page) => page.parentType === "database" && page.parentId === targetDbId && !page.inTrash && relationPickerOffers(scope, page, verifications)),
    visited);
  }, [open, scope, snapshot.pagesById, targetDbId, verificationKey, visited]);
  const unnamedCandidate = useMemo(() => candidates.some((page) => !page.title), [candidates]);
  const targetRowName = useRelationTargetRowName(targetDbId, prop.databaseId, selectedPages.some((page) => !page.title) || (open && unnamedCandidate));
  const titleOf = useCallback((page: Page) => !page.title && targetRowName ? targetRowName : pageNames.title(page), [targetRowName, pageNames]);
  // Found word by word (relationPagesFound), once for each search, not
  // again as the highlight moves; "〈database〉에 새 '〈words〉' 페이지 생성"
  // is offered even where a row bears that name already.
  const filtered = useMemo(() => relationPagesFound(candidates, queryText, titleOf), [candidates, queryText, titleOf]);
  const canCreate = queryText.trim().length > 0 && targetDb?.kind === "database";
  useEffect(() => {
    if (!targetDbId) return;
    const request = capturePropertyCellRequest(context);
    return abortWhileRunning(request, [operations.loadRelation(targetDbId, { rows: open, signal: request.signal }, context)]);
  }, [context, open, operations, prop.databaseId, targetDbId]);

  function commit(ids: string[]) {
    void operations.setRelation(row.id, prop, ids, context);
  }

  // A relation limited to one page takes the page picked in place of its own.
  const single = prop.config?.limit === 1;

  async function toggle(id: string) {
    if (selectedSet.has(id)) {
      commit(selectedIds.filter((item) => item !== id));
      return;
    }
    // A page whose other side takes one page, already related elsewhere, moves
    // here once asked, as Notion's (seen 2026-11-07: a task in another sprint).
    const dualId = typeof prop.config?.relatedPropertyId === "string" ? prop.config.relatedPropertyId : "";
    const dual = dualId ? targetProperties.find((property) => property.id === dualId) : undefined;
    const elsewhere = dual?.config?.limit === 1
      ? valueAsIds(snapshot.pagesById[id]?.properties?.[dualId], snapshot.formulaBounds.rollup).filter((other) => other !== row.id) : [];
    if (elsewhere.length > 0) {
      const database = snapshot.pagesById[prop.databaseId];
      const asked = await confirmAction({
        icon: <ArrowUpRightIcon size="regular" aria-hidden="true" />,
        message: t("relationMoveHere", { name: pageNames.title(row) }),
        detail: t("relationMoveLeaves", { database: database ? pageNames.title(database) : prop.name }),
        hint: t("relationMoveHint"), rememberKey: `relation-move:${snapshot.userId ?? ""}`,
        confirmLabel: t("relationMoveContinue"), accent: true, move: true,
      });
      if (!asked) return;
    }
    commit(single ? [id] : [...selectedIds, id]);
    // Limited to one page, the picker closes on the page picked, its cell
    // chosen, as a select's does (Notion's, seen 2026-11-16).
    if (single) closeMenu(true);
  }

  // The row made here changes what the cell shows, so what follows it goes
  // by the cell as it is then, not as the row was made from.
  const shown = useRef({ context, selectedIds });
  shown.current = { context, selectedIds };

  // The page a section's "새 페이지 추가하기" made, its own line left out while its title line stands for it.
  const [namingId, setNamingId] = useState<string | null>(null);

  async function createRelatedPage(named?: string, onMade?: (id: string) => void): Promise<string | null> {
    if (!targetDb || targetDb.kind !== "database") return null;
    const request = capturePropertyCellRequest(context);
    try {
      const related = await operations.createRelatedRow(targetDb.id, { signal: request.signal }, context);
      if (request.signal.aborted || !related) return null;
      onMade?.(related.id);
      const title = (named ?? queryText).trim();
      if (title) await operations.renameRelatedRow(related.id, title, shown.current.context);
      if (request.signal.aborted) return null;
      const now = shown.current;
      void operations.setRelation(row.id, prop, single ? [related.id] : [...now.selectedIds.filter((id) => id !== related.id), related.id], now.context);
      if (named === undefined) setQueryText("");
      return related.id;
    } finally {
      request.abort();
    }
  }

  // A page section's "새 페이지 추가하기" makes the page at the press, related
  // here, as Notion's (seen 2026-11-14), after the frame that draws its title
  // line; left named, it takes the name typed, and left unnamed by Enter or
  // Escape it is taken back — by a press elsewhere it stays, untitled.
  function startRelatedPage(): RelationNewPage {
    let left = false;
    const made = new Promise<string | null>((resolve) => {
      afterNextPaint(() => { createRelatedPage("", setNamingId).then(resolve, () => resolve(null)); });
    });
    const leave = (id: string | null, title: string, keepUnnamed: boolean) => {
      if (!id) return;
      const now = shown.current;
      const done = title ? operations.renameRelatedRow(id, title, now.context) : keepUnnamed ? undefined : Promise.all([
        operations.setRelation(row.id, prop, now.selectedIds.filter((other) => other !== id), now.context),
        operations.discardRelatedRow?.(id, now.context),
      ]);
      void Promise.resolve(done).finally(() => setNamingId((current) => current === id ? null : current));
    };
    return {
      finish(title, keepUnnamed) {
        if (left) return;
        left = true;
        // The line goes with the key's frame; the name or taking it back follows.
        afterNextPaint(() => { void made.then((id) => leave(id, title, keepUnnamed)); });
      },
    };
  }

  function openMenu() {
    if (!readOnly) setOpen(true);
  }

  // The picker opens on an empty search each time, as Notion's.
  function closeMenu(restoreFocus = false) {
    setOpen(false);
    setQueryText("");
    if (restoreFocus) restoreFocusUnlessChosen(triggerRef.current);
  }

  // The chosen pages come first, as Notion's "N개 선택함" over "추가 선택".
  // The chosen ones in the relation's own order, which their handles change (Notion's ⠿, seen 2026-11-07).
  // Made again only when the rows or the chosen ones change, so the
  // highlight moves over the same rows (2026-11-11, 100,000 rows).
  const selectedKey = selectedIds.join("\n");
  const createLabel = canCreate && targetDb ? t("newRelation", { title: queryText.trim(), database: pageNames.database(targetDb) }) : "";
  const { chosen, others, choices } = useMemo(() => {
    const ids = selectedKey ? selectedKey.split("\n") : [], picked = new Set(ids);
    const pickedById = new Map(filtered.filter((page) => picked.has(page.id)).map((page) => [page.id, page]));
    const chosen = ids.flatMap((id) => pickedById.get(id) ?? []);
    const others = filtered.filter((page) => !picked.has(page.id));
    const choices = [...chosen, ...others].map((page) => ({ id: page.id, label: titleOf(page) }));
    if (createLabel) choices.push({ id: CREATE_CHOICE, label: createLabel });
    return { chosen, others, choices };
  }, [createLabel, filtered, selectedKey, titleOf]);
  const picker = useNativeAutocomplete({ items: choices, query: queryText, onQuery: setQueryText, open, inputRef, listRef, popoverRef,
    keyOf: item => item.id, textOf: item => item.label,
    onPick: item => { void (item.id === CREATE_CHOICE ? createRelatedPage() : toggle(item.id)); },
    onClose: () => closeMenu(true), label: t("searchRelatedPages") });

  // A related page is no link, as Notion's (seen 2026-11-12): in a table
  // cell pressing it opens the picker, its typing going to the search, as
  // Notion's cell (seen 2026-11-07: opening the page there let what was typed
  // next rename it); on a row's page it opens the page in a peek
  // (useRelatedPageOpener), ⌘ or Ctrl in a new tab.
  function openPage(pageId: string, event: ReactMouseEvent<HTMLElement> | ReactKeyboardEvent<HTMLElement>) {
    event.stopPropagation();
    event.preventDefault();
    if (!open && event.currentTarget.closest("[data-table-cell]")) {
      openMenu();
      return;
    }
    if (event.metaKey || event.ctrlKey) window.open(pageHref(pageId), "_blank", "noopener");
    else if (onOpenPage) onOpenPage(pageId);
    else openRelated(pageId);
  }

  return (
    <PropertyRelationCellView
      titleOf={titleOf}
      rowDetail={presentation === "rowDetail"}
      row={row}
      prop={prop}
      targetDb={targetDb}
      selectedIds={selectedIds}
      selectedPages={namingId ? selectedPages.filter((page) => page.id !== namingId) : selectedPages}
      selectedSet={selectedSet}
      pendingSelectedCount={pendingSelectedCount}
      missingSelectedCount={missingSelectedCount}
      unreadableSelectedCount={unreadableSelectedCount}
      filtered={filtered}
      chosen={chosen}
      others={others}
      ordinaryTargetRowsPending={ordinaryTargetRowsPending}
      ordinaryTargetRowsFailed={ordinaryTargetRowsFailed}
      canCreate={canCreate}
      open={open && !readOnly}
      query={queryText}
      triggerRef={triggerRef}
      listRef={listRef}
      popoverRef={popoverRef}
      inputRef={inputRef}
      picker={picker}
      scope={scope}
      onScope={setScope}
      properties={targetProperties.length ? <RelationPickerProperties shownIds={shownIds} properties={targetProperties} writable={!operations.relationShownFixed}
        onShown={(ids) => void operations.updateProperty(prop.id, { config: { ...prop.config, relationShownProperties: ids } as DbProperty["config"] }, context)} /> : null}
      values={(page) => <RelationPickerValues page={page} shownIds={shownIds} properties={targetProperties} context={valueContext} />}
      openMenu={openMenu}
      closeMenu={closeMenu}
      openPage={openPage}
      startPage={targetDb?.kind === "database" ? startRelatedPage : undefined}
      removePage={(pageId) => commit(selectedIds.filter((id) => id !== pageId))}
      reorderPage={selectedIds.length > 1 ? (pageId, targetId, side) => {
        const next = relationIdsMoved(selectedIds, pageId, targetId, side);
        if (next) commit(next);
      } : undefined}
      renamePage={(pageId, title) => void operations.renameRelatedRow(pageId, title, context)}
    />
  );
}

/** The relation's pages with one moved before or after another; null when nothing moves. */
export function relationIdsMoved(ids: readonly string[], pageId: string, targetId: string, side: "before" | "after"): string[] | null {
  if (pageId === targetId || !ids.includes(pageId) || !ids.includes(targetId)) return null;
  const rest = ids.filter((id) => id !== pageId);
  const at = rest.indexOf(targetId) + (side === "after" ? 1 : 0);
  const next = [...rest.slice(0, at), pageId, ...rest.slice(at)];
  return next.every((id, index) => id === ids[index]) ? null : next;
}
