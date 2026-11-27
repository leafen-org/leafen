"use client";
import { pagesByIdOf } from "@/lib/pageIndex";

import { useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { distinctPropertyName, type GraphProperty } from "@/contentGraph";
import { useProductPages } from "@/components/product/productPagesContext";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { newId } from "@/lib/ids";
import { databaseSourcePlace } from "@/lib/pagePath";
import { usePageNames } from "@/lib/pageNames";
import { dataSourceDisplayTitle } from "../editor/databaseTitles";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";
import { useOverlayLayer } from "@/lib/overlayLayer";
import {
  placeAround, popoverPlacementStyle, popoverRectAt, popoverRectOf, usePopoverPlacement,
  type PopoverRequest, type PopoverSize, type PopoverViewport,
} from "@/lib/popoverPlacement";
import {
  createRelationPropertyOperationOwner,
  createRelationPropertyCommand,
  filterRelationDatabases,
  prepareRelationDatabases,
  type RelationDatabaseOption,
  type RelationPropertyCommand,
  type RelationPropertyCreationBounds,
} from "@/lib/relationPropertyCreationModel";
import { SwitchMark } from "@/components/Switch";
import { SettingInfoTip } from "../IconTipButton";
import { ArrowUpRightIcon, LinkedViewIcon } from "@/icons/leafen";
import { ArrowsExpandHorizontal, DotsHorizontal, NumbersIcon } from "../icons";
import { RelationCreationHead } from "./RelationCreationHead";
import { useDatabaseOwnPage } from "./databaseFullPageLink";
import { DataSourceMark } from "./DataSourceMark";
import { RelationCreateTargetRow } from "./RelationCreateTargetRow";
import { scrollOpenerUpForPanel } from "@/lib/openerScroll";
import { toolbarMenuColumn } from "./databaseToolbarModel";
import { NotionSelect } from "./NotionSelect";
import { PropertySettingButton } from "./PropertySettingRow";
import { databaseToolbarOf, toolbarButtonsEnd } from "./useDatabaseToolbarController";
import { useDockedPanelTop } from "./useDockedPanelTop";
import { useSettingsPlaceWindow } from "./relationSettingsPlace";
import { EXISTING_COUNTED, EXISTING_FIRST_SHOWN as FIRST_SHOWN, useSourceListLimit } from "./linkableSources";
import { SearchClearButton } from "../PageActionSearch";
import styles from "./RelationPropertyCreationDialog.module.css";
import dockColumn from "./dockedPanelColumn.module.css";
import { inlineStartX, readsRightToLeft } from "@/lib/readingDirection";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

export type RelationPropertyCreationSnapshot = {
  databases: RelationDatabaseOption[];
  snapshotIdentity: string;
};

export type RelationPropertyCreationContext = {
  bounds: RelationPropertyCreationBounds;
  commit: (command: RelationPropertyCommand, signal: AbortSignal) => Promise<GraphProperty | null>;
  identity: string;
  notifyFailure: () => void;
  // Finds databases on other pages the viewer can read, up to `upTo`
  // searched or not; the snapshot holds the ones this page already shows.
  searchDatabases?: (query: string, signal: AbortSignal, upTo?: number) => Promise<RelationDatabaseOption[]>;
};

const relationDatabaseSearchDelay = 150;
// The databases listed before "N개 더 보기", as Notion's (seen 2026-11-02), which counts the rest of the hundred
// it holds or found and shows twenty more a press, a search the first twenty, the reach kept through searches (seen
// 2026-11-18: 20 + "52개 더 보기", 40 + 32, emptied 40 + 60 — useSourceListLimit), each source by its mark (DataSourceMark).
// The picker's height with those five and "N개 더 보기": its search 48, its
// heading 34, five 45px sources 1px apart, the more line and the foot.
const PICKER_HEIGHT = 48 + 34 + FIRST_SHOWN * 46 - 1 + 29 + 4;

type RelationPlace =
  // `bare`: by what it was opened from, it has no head, as Notion's opened by
  // its new property's button or line (seen 2026-11-14).
  | { kind: "popover"; request: (size: PopoverSize, viewport: PopoverViewport) => PopoverRequest; holder?: HTMLElement; bare?: true }
  | { kind: "docked"; toolbar: HTMLElement | null; top: number | null; holder?: HTMLElement }
  | { kind: "modal" };

/**
 * Where the window opens, as Notion's (seen 2026-11-02 at 1280px): an
 * inline database's where its view settings open, 3px under the settings
 * button from 6px past the toolbar's end; a database's own page's at the
 * window's right under its toolbar row, as its settings panel; from the
 * settings themselves, in their place; from a new property's button or line,
 * by it. A phone, and any other place with no toolbar, keep the window over
 * the screen.
 */
function relationPlace(anchor: HTMLElement | null | undefined, phone: boolean, ownPage: boolean): RelationPlace {
  if (!anchor || phone) return { kind: "modal" };
  // From a database page's "속성 추가" it opens under the button, its start
  // at the button's, 1px off, or over it where the room under it is short; from
  // a layout builder's "+", by its "관계형" line, its middle on the line's, 4px
  // off, as Notion's (seen 2026-11-14 at 1440: a button at 250..284 → 285, one at
  // 554 → 209..553; a 274 line at 730 → 722 under it or over it). Its side is
  // chosen by the picker's full height, which Notion's holds from opening, so
  // it stays as its sources come and as it turns to "새 관계형".
  const row = anchor.closest("[data-row-add-property-row]");
  if (row || anchor.closest("[data-layout-add-menu]")) {
    return { kind: "popover", bare: true, request: (size, viewport) => {
      const opener = popoverRectOf(anchor)!;
      const cap = Math.round(viewport.height * 0.8);
      return placeAround(opener, size, viewport, row
        ? { gap: 1, cap, sideBy: PICKER_HEIGHT }
        : { gap: 4, cap, sideBy: PICKER_HEIGHT, align: "center" });
    } };
  }
  const holder = anchor.closest<HTMLElement>("[data-database-view-settings]");
  if (holder?.dataset.dockedPanel === "true") return { kind: "docked", toolbar: null, top: Math.round(holder.getBoundingClientRect().top), holder };
  if (holder) {
    return { kind: "popover", holder, request: (size, viewport) => {
      const at = holder.getBoundingClientRect();
      return placeAround(popoverRectAt({ top: at.top, bottom: at.top, left: inlineStartX(at, 0, holder), contextElement: holder }), size, viewport,
        { gap: 0, margin: 12, keepSide: true });
    } };
  }
  const toolbar = databaseToolbarOf(anchor);
  if (!toolbar) return { kind: "modal" };
  if (ownPage) return { kind: "docked", toolbar, top: null };
  return { kind: "popover", request: (size, viewport) => {
    const trigger = toolbar.querySelector<HTMLElement>("[data-database-settings-trigger]");
    const opener = trigger && trigger.getBoundingClientRect().width > 0 ? trigger : toolbar;
    const rect = popoverRectOf(opener)!;
    const column = toolbarMenuColumn(rect, "view", viewport.width, toolbarButtonsEnd(opener), undefined, readsRightToLeft(opener));
    return placeAround({ ...rect, left: column.left, right: column.left }, size, viewport, { gap: column.gap, margin: column.margin });
  } };
}

export function RelationPropertyCreationDialog({
  anchor,
  context,
  databaseId,
  existingProperty,
  initialName,
  initialTarget,
  onCancel,
  onCreated,
  snapshot,
  takenNames,
}: {
  /** What it was opened from, inside the database or its settings: where it opens follows (relationPlace). */
  anchor?: HTMLElement | null;
  context: RelationPropertyCreationContext;
  databaseId: string;
  existingProperty?: GraphProperty;
  /** The name typed in the property menu before choosing "관계형". */
  initialName?: string;
  /** The database the relation is to, already chosen (a layout tab's "새 관계형 추가"). */
  initialTarget?: RelationDatabaseOption;
  onCancel: () => void;
  onCreated?: (property: GraphProperty) => void;
  snapshot: RelationPropertyCreationSnapshot;
  /** The names its database's properties have, which a new one's is told apart from (proposedName). */
  takenNames?: readonly string[];
}) {
  const { t } = useTranslation(["propertyTypeConfig", "rowProperties", "databaseView"]), pageNames = usePageNames();
  const { phone } = useSidebarLayout(true);
  const ownPage = useDatabaseOwnPage();
  const [where] = useState(() => relationPlace(anchor, phone, ownPage));
  const [targetDatabaseId, setTargetDatabaseId] = useState<string | null>(initialTarget?.id ?? null);
  const [returnTarget, setReturnTarget] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const { limit: shownCount, more: showMore, reset: showFirst } = useSourceListLimit(search);
  const [propertyName, setPropertyName] = useState(initialTarget ? proposedName(relatedName(initialTarget)) : "");
  const [twoWay, setTwoWay] = useState(false);
  const [reciprocalName, setReciprocalName] = useState("");
  const [limitOne, setLimitOne] = useState(existingProperty?.config?.limit === 1);
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);
  busyRef.current = busy;
  const searchRef = useRef<HTMLInputElement>(null);
  const nameRef = useRef<HTMLInputElement>(null);
  const relatedRef = useRef<HTMLInputElement>(null);
  const operationOwner = useRef(createRelationPropertyOperationOwner());
  const dialogRef = useRef<HTMLDivElement>(null);
  const cancel = () => { if (!busyRef.current) onCancel(); };
  // Over the screen it is a modal layer over the property menu: Escape
  // cancels it, and only it, unless it is creating. In a database's place it
  // is that place's window, which a press elsewhere closes as well.
  const popover = where.kind === "popover";
  useSettingsPlaceWindow(where.kind === "docked" ? where.toolbar : popover && !where.bare && !where.holder && anchor ? databaseToolbarOf(anchor) : null, cancel);
  // From an inline database low on the page it brings the bar up first, as
  // Notion's (seen 2026-11-05: "+ 속성 추가" → 관계형 scrolled the page and
  // opened "관계형 대상" under the bar): the view settings' rule (openerScroll.ts).
  useEffect(() => {
    if (where.kind !== "popover" || where.holder || !anchor) return;
    const frame = window.requestAnimationFrame(() => scrollOpenerUpForPanel(
      databaseToolbarOf(anchor)?.querySelector<HTMLElement>("[data-database-settings-trigger]") ?? null, dialogRef.current?.scrollHeight ?? 0));
    return () => window.cancelAnimationFrame(frame);
    // Once, where it opened.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [where]);
  const placement = usePopoverPlacement(dialogRef, popover ? where.request : null, [where], undefined, {
    kind: "picker", restoreFocus: false, onEscape: cancel, onOutsidePress: cancel,
    // Its limit's choices open over it.
    inside: () => [...document.querySelectorAll("[data-notion-select-menu]")],
  });
  const { zIndex } = useOverlayLayer(!popover, { kind: "dialog", ref: dialogRef, modal: where.kind === "modal",
    modalOwned: () => [dialogRef.current?.parentElement], onEscape: cancel });
  const dockRef = useMemo(() => ({ current: where.kind === "docked" ? where.toolbar : null }), [where]);
  const dockTop = useDockedPanelTop(dockRef, where.kind === "docked" && Boolean(where.toolbar), dialogRef, true);
  // In the settings' place the settings wait under it; a database's own page
  // narrows beside it, as beside its settings panel.
  useEffect(() => {
    const holder = where.kind === "modal" ? undefined : where.holder;
    if (holder) {
      holder.dataset.relationCreating = "true";
      return () => { delete holder.dataset.relationCreating; };
    }
    if (where.kind !== "docked") return;
    const root = document.documentElement;
    root.dataset.databaseViewSettingsOpen = "true";
    return () => { delete root.dataset.databaseViewSettingsOpen; };
  }, [where]);
  // What the window makes its relation in. Not the database's snapshot: the
  // relation made shows as a column before the server answers, which
  // changes it, and the window then waited on (2026-11-14: the column there,
  // the window left open, its settings never shown).
  const currentContext = useRef({ contextIdentity: context.identity, databaseId });
  currentContext.current = { contextIdentity: context.identity, databaseId };

  // What the workspace's search found, and for which search.
  const [found, setFound] = useState<{ search: string; databases: RelationDatabaseOption[] }>({ search: "", databases: [] });
  const foundDatabases = found.databases;
  const pages = useProductPages()?.pages;
  const pagesById = pagesByIdOf(pages ?? []);
  const searchDatabases = context.searchDatabases;
  useEffect(() => {
    if (!searchDatabases) return;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      searchDatabases(search.trim(), controller.signal, EXISTING_COUNTED)
        .then((databases) => { if (!controller.signal.aborted) setFound({ search: search.trim(), databases }); })
        .catch(() => { if (!controller.signal.aborted) setFound({ search: "", databases: [] }); });
    }, relationDatabaseSearchDelay);
    return () => {
      controller.abort();
      window.clearTimeout(timer);
    };
  }, [search, searchDatabases]);

  const databases = useMemo(() => {
    // The database itself first, then the others this page shows, then the ones found elsewhere, each with where it sits, as
    // Notion lists them — the page's own named as the server names them, "… / A 및 B" while their database holds several.
    const named = new Map(foundDatabases.map((found) => [found.id, found]));
    const own = [...snapshot.databases].sort((left, right) => Number(right.id === databaseId) - Number(left.id === databaseId))
      .map((database, position) => ({ ...database, sourceTitles: named.get(database.id)?.sourceTitles, databaseTitle: named.get(database.id)?.databaseTitle, position }));
    const known = new Set(own.map(({ id }) => id));
    const chosen = initialTarget && !known.has(initialTarget.id) ? [{ ...initialTarget, position: own.length }] : [];
    if (initialTarget) known.add(initialTarget.id);
    const merged = [
      ...own,
      ...chosen,
      ...foundDatabases.filter(({ id }) => {
        if (known.has(id)) return false;
        known.add(id);
        return true;
      }),
    ].slice(0, context.bounds.maxDatabases);
    return prepareRelationDatabases(
      merged.map((database) => ({ ...database, title: dataSourceDisplayTitle(database.title, database.sourceTitles?.length ?? 1), // untitled: relationDatabaseSearch
        place: databaseSourcePlace(database.pageId, pagesById, database.sourceTitles, database.databaseTitle) ?? (database.where ? [database.where] : []) })),
      context.bounds,
    );
  }, [context.bounds, databaseId, foundDatabases, initialTarget, pagesById, snapshot.databases]);
  const targetDatabase = targetDatabaseId
    ? databases.find((database) => database.id === targetDatabaseId)
    : undefined;
  const self = targetDatabase?.id === databaseId;
  const filteredDatabases = useMemo(
    () => filterRelationDatabases(databases, search, context.bounds,
      searchDatabases && search.trim() && found.search === search.trim() ? found.databases.map(({ id }) => id) : undefined),
    [context.bounds, databases, found, search, searchDatabases],
  );
  const listedDatabases = filteredDatabases.slice(0, EXISTING_COUNTED), shownDatabases = listedDatabases.slice(0, shownCount);
  // The related property is named "{database}에 다시 관계됨" on its own
  // database, as Notion names it (seen 2026-09-30); on another database the
  // server names it after this one.
  const sourceDatabase = databases.find((database) => database.id === databaseId);
  const pairTip = t(self ? "propertyTypeConfig:relatedPropertyTip" : "propertyTypeConfig:twoWayRelationTip");
  const reciprocalDefault = targetDatabase?.id === databaseId && sourceDatabase
    ? t("propertyTypeConfig:relatedBackName", { name: pageNames.title(sourceDatabase) })
    : sourceDatabase ? pageNames.title(sourceDatabase) : "";

  useEffect(() => {
    operationOwner.current.retire();
    setBusy(false);
  }, [context.identity, databaseId]);

  useEffect(() => () => operationOwner.current.retire(), []);

  // What is typed as the window opens or its database is picked goes into the field it brings (focusOpenedField).
  useEffect(() => focusOpenedField(() => targetDatabaseId && targetDatabase ? nameRef.current : searchRef.current),
    [targetDatabase, targetDatabaseId]);
  // The switch turned on hands the typing to the related property's name, as Notion's. Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440:
  // "관련된 속성 추가하기" switched on, "z" typed went into the related property's name (Leafen the same).
  useEffect(() => { if (twoWay) relatedRef.current?.focus(); }, [twoWay]);

  // A new relation is named after the database it is to, its source's emoji first where it has one; one to its own
  // database is "관계된 {database}", as Notion names them (seen 2026-11-11: "w356 프로젝트 B", "관계된 w356 할일 A"; 2026-11-15: "🍎 사과 DB").
  function relatedName(target: RelationDatabaseOption) {
    const name = pageNames.title({ title: target.title, kind: "database" });
    if (target.id === databaseId) return t("propertyTypeConfig:relatedName", { name });
    return target.mark?.iconType === "emoji" ? `${target.mark.icon} ${name}` : name;
  }

  // A new relation's name, told apart from one its database has with " 1",
  // " 2" …, as Notion's shows it before it is added (seen 2026-11-14: "빈표 1"
  // beside "빈표") and as the server keeps it.
  function proposedName(name: string) {
    return existingProperty ? name : distinctPropertyName(name, new Set(takenNames?.map((taken) => taken.trim())));
  }

  // `back`: by the head's "←", to the database left and the name as it was.
  function chooseTarget(id: string, back = false) {
    const target = databases.find((database) => database.id === id);
    if (!target) return;
    setTargetDatabaseId(id);
    setReturnTarget(null);
    // A new relation keeps the name typed for it, as Notion's (seen 2026-10-06).
    if (!back) setPropertyName(existingProperty?.name ?? proposedName(initialName?.trim() || relatedName(target)));
    setTwoWay(false);
    setReciprocalName("");
  }

  // Back to the databases, their search empty and their list from its start, the one left kept for the head's "←",
  // as Notion's (seen twice 2026-11-15/16 at 1440: one letter typed after it is the search's alone).
  function returnToTargets() {
    setReturnTarget(targetDatabaseId);
    setTargetDatabaseId(null);
    setSearch("");
    showFirst();
  }

  function isCurrentOperation(operation: { isCurrent: () => boolean }) {
    return operation.isCurrent()
      && currentContext.current.contextIdentity === context.identity
      && currentContext.current.databaseId === databaseId;
  }

  async function createRelation() {
    if (!targetDatabase) return;
    const operation = operationOwner.current.begin();
    if (!operation) return;
    const name = propertyName.trim() || initialName?.trim() || relatedName(targetDatabase);
    setBusy(true);
    try {
      const command = createRelationPropertyCommand({
        bounds: context.bounds,
        contextIdentity: context.identity,
        databaseId,
        existingProperty,
        limitOne,
        mutationId: newId(),
        name,
        reciprocalName: twoWay ? reciprocalName.trim() || reciprocalDefault : undefined,
        reciprocalPropertyId: twoWay ? newId() : undefined,
        snapshotIdentity: snapshot.snapshotIdentity,
        targetDatabaseId: targetDatabase.id,
        twoWay,
      });
      const property = await context.commit(command, operation.signal);
      if (!isCurrentOperation(operation)) return;
      if (!property) {
        context.notifyFailure();
        return;
      }
      onCreated?.(property);
    } catch {
      if (isCurrentOperation(operation)) context.notifyFailure();
    } finally {
      if (isCurrentOperation(operation)) {
        operation.complete();
        setBusy(false);
      }
    }
  }

  if (typeof document === "undefined") return null;

  const enterCreates = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (!fieldEnter(event)) return;
    event.preventDefault();
    void createRelation();
  };
  const style: CSSProperties | undefined = where.kind === "popover" ? popoverPlacementStyle(placement)
    : where.kind === "docked" ? { zIndex, ...((where.top ?? dockTop) === null ? {} : { top: `max(var(--page-panel-top, var(--topbar-height)), ${where.top ?? dockTop}px)` }) }
    : undefined;
  const dialog = (
    <div
      ref={dialogRef}
      className={`${styles.relationCreateDialog} ${where.kind === "docked" ? dockColumn.column : ""}`}
      role="dialog"
      aria-modal={where.kind === "modal" ? "true" : undefined}
      aria-label={t("propertyTypeConfig:createRelationProperty")}
      data-place={where.kind}
      style={style}
    >
      {where.kind === "popover" && where.bare ? null : (
        <RelationCreationHead title={t(targetDatabase ? "propertyTypeConfig:newRelation" : "propertyTypeConfig:relationTarget")}
          busy={busy} onCancel={onCancel} onBack={!targetDatabase && returnTarget ? () => chooseTarget(returnTarget, true) : undefined} />
      )}

      {!targetDatabase ? (
        <>
          <div className={styles.relationCreateFieldRow}>
            <label className={styles.relationCreateBox}>
              <input
                ref={searchRef} value={search}
                aria-label={t("propertyTypeConfig:connectDataSource")} placeholder={t("propertyTypeConfig:connectDataSource")}
                maxLength={context.bounds.maxSearchCharacters}
                onChange={(event) => setSearch(event.target.value)}
              />
              <SearchClearButton value={search} inputRef={searchRef} onClear={() => setSearch("")} />
            </label>
          </div>
          <div className={styles.relationCreateTargets} role="group" aria-label={t("propertyTypeConfig:existingDataSources")}>
            <div className={styles.relationCreateSection}>{t("propertyTypeConfig:existingDataSources")}</div>
            {shownDatabases.map((database) => (
              <RelationCreateTargetRow key={database.id} database={database} own={database.id === databaseId} onChoose={chooseTarget} />
            ))}
            {shownDatabases.length < listedDatabases.length && (
              <button type="button" className={styles.relationCreateMore} onClick={showMore}>
                <DotsHorizontal size="regular" aria-hidden="true" />
                <span>{t("databaseView:showMoreDataSources", { count: listedDatabases.length - shownDatabases.length })}</span>
              </button>
            )}
            {filteredDatabases.length === 0 && (
              <div className={styles.relationCreateEmpty}>
                {t("propertyTypeConfig:noDatabasesFound")}
              </div>
            )}
          </div>
        </>
      ) : (
        <>
          {/* Notion's "새 관계형" (seen 2026-11-02): the name, then a row each
              for the database, the limit and the related property's switch,
              which names it under itself. */}
          <div className={styles.relationCreateFieldRow}>
            <label className={styles.relationCreateBox}>
              <input
                ref={nameRef}
                value={propertyName}
                aria-label={t("propertyTypeConfig:propertyName")}
                disabled={busy}
                maxLength={context.bounds.maxNameCharacters}
                onChange={(event) => setPropertyName(event.target.value)}
                onKeyDown={enterCreates}
              />
            </label>
          </div>
          <div className={styles.relationCreateRows}>
            {/* The rows as the relation's own settings draw them (PropertyRelationConfig.tsx). */}
            <PropertySettingButton
              className={styles.relationCreateRow}
              label={t("propertyTypeConfig:relationTarget")}
              icon={<LinkedViewIcon size="small" />}
              valueIcon={self || !targetDatabase ? undefined : <DataSourceMark source={targetDatabase.mark ?? {}} />}
              value={self ? t("propertyTypeConfig:thisDatabaseSuffix") : pageNames.title(targetDatabase)}
              disabled={busy}
              onClick={returnToTargets}
            />
            <NotionSelect
              ariaLabel={t("propertyTypeConfig:relationLimit")}
              settingLabel={t("propertyTypeConfig:relationLimit")}
              settingIcon={<NumbersIcon size="regular" />}
              buttonClassName={styles.relationCreateRow}
              menuClassName={styles.relationCreateLimitMenu}
              value={limitOne ? "one" : "none"}
              disabled={busy}
              options={[
                { value: "one", label: t("propertyTypeConfig:relationLimitOne") },
                { value: "none", label: t("propertyTypeConfig:relationLimitNone") },
              ]}
              onChange={(value) => setLimitOne(value === "one")}
            />
            <button
              type="button"
              role="switch"
              aria-checked={twoWay}
              className={styles.relationCreateSwitch}
              disabled={busy}
              onClick={() => setTwoWay((on) => !on)}
            >
              {/* "↔ 양방향 관계형 ⓘ" to another database, "↗ 관련된 속성 추가하기 ⓘ" to its own, each ⓘ saying what it
                  shows, as Notion's (seen 2026-11-11, 2026-11-14). */}
              <span className={styles.relationCreateSwitchIcon} aria-hidden="true">
                {self ? <ArrowUpRightIcon size="small" /> : <ArrowsExpandHorizontal size="small" />}</span>
              <span className={styles.relationCreateLabel} data-setting-name>
                {t(self ? "propertyTypeConfig:twoWayRelation" : "propertyTypeConfig:twoWayRelationSetting")}
                <SettingInfoTip label={pairTip} className={styles.relationCreateTip} />
              </span>
              <SwitchMark on={twoWay} />
            </button>
          </div>
          {twoWay && (
            <label className={styles.relationCreateRelated}>
              <span>{t("propertyTypeConfig:relatedPropertyName")}</span>
              <span className={styles.relationCreateBox}>
                <input
                  ref={relatedRef}
                  value={reciprocalName}
                  placeholder={reciprocalDefault}
                  aria-label={t("propertyTypeConfig:relatedPropertyName")}
                  disabled={busy}
                  maxLength={context.bounds.maxNameCharacters}
                  onChange={(event) => setReciprocalName(event.target.value)}
                  onKeyDown={enterCreates}
                />
              </span>
            </label>
          )}
          <div className={styles.relationCreateActions}>
            <button
              type="button"
              className={styles.relationCreateSubmit}
              disabled={busy || !propertyName.trim()}
              onClick={() => void createRelation()}
            >
              {t("propertyTypeConfig:addRelation")}
            </button>
          </div>
        </>
      )}
    </div>
  );

  return createPortal(
    where.kind === "modal" ? (
      <div className={styles.relationCreateOverlay} style={{ zIndex }}>
        <button
          type="button"
          className={styles.relationCreateBackdrop}
          aria-label={t("propertyTypeConfig:cancelRelationCreation")}
          disabled={busy}
          onClick={onCancel}
        />
        {dialog}
      </div>
    ) : dialog,
    document.body,
  );
}
