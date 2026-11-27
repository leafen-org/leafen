import { useContext, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { GraphDataSource, GraphProperty, GraphView } from "@/contentGraph";
import { ChevronRight, PaintBrushIcon, Plus, RestoreIcon, Trash, X } from "@/icons/leafen";
import { confirmAction } from "../ConfirmActionDialog";
import { SearchClearButton } from "../PageActionSearch";
import { useInSettingsPanel, useOpenSettingsScreen, useSettingsScreenHead } from "./settingsScreenHead";
import type { DatabaseDeletedProperty } from "@/databaseDataSourceWire.generated";
import type { DatabaseDeletedPropertiesSurface } from "@/lib/databaseDeletedProperties";
import type { DbProperty, Page } from "@/lib/types";
import { RELATION_CREATION_BOUNDS, relationDatabaseOptions, type RelationDatabaseOption } from "@/lib/relationPropertyCreationModel";
import { placeBesideRow, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { PropertyType } from "@/lib/types";
import { configForType, localizedPropertyTypeLabel } from "./propertyTypes";
import { TablePropertyTypeList, useTablePropertyTypeChoice } from "./TablePropertyTypeChoices";
import tableStyles from "./TableAddProperty.module.css";
import { databaseViewLabels } from "./databaseViewLabels";
import { PropertyIcon } from "./PropertyIcon";
import { RelationPropertyCreationDialog } from "./RelationPropertyCreationDialog";
import { DatabasePropertyVisibilityList } from "./DatabasePropertyVisibilityList";
import { DatabaseTimelineVisibilityTabs } from "./DatabaseTimelineVisibilityTabs";
import { DatabasePropertyEditPanel } from "./DatabasePropertyEditPanel";
import { DatabaseTaskSettingsHeader } from "./DatabaseTaskSettingsHeader";
import { orderViewProperties } from "./viewPropertyOrder";
import styles from "./databaseSheet";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { DatabaseNewPropertyScreen, type NewPropertyChoice } from "./DatabaseNewPropertyScreen";
import { DatabaseDataSourcesContext, surfaceDataSourceName } from "./databaseDataSourcesContext";
import { PhoneSettingsListContext } from "./PropertySettingRow";

type PropertiesMenuProps = {
  contextIdentity: string;
  dataSource: GraphDataSource;
  dataSources: readonly GraphDataSource[];
  properties: readonly GraphProperty[];
  snapshotIdentity: string;
  view: GraphView;
  writable: boolean;
  onCommitProperties(properties: readonly GraphProperty[]): void;
  onCreateProperty?(type: GraphProperty["type"], name: string, config?: Record<string, unknown>): Promise<GraphProperty | null>;
  onUpdateView(view: GraphView): void;
  searchRelationDatabases?: (query: string, signal: AbortSignal) => Promise<RelationDatabaseOption[]>;
  /** The database's Deleted properties, as Notion lists them under its properties. */
  deletedProperties?: DatabaseDeletedPropertiesSurface;
  /** The row pages a formula's editor previews its result for. */
  pagesById?: Record<string, Page>;
  /** The properties of the databases its relations reach, for a rollup's target. */
  propertiesByDataSourceId?: Readonly<Record<string, readonly DbProperty[]>>;
  /**
   * Which of Notion's two property lists this is: the view's, which shows,
   * hides and orders its properties, or the data source's, which opens each
   * property's settings and adds, restores and deletes them; or a
   * timeline's "표 속성": its table's list, each row's "›" opening the
   * property's settings, and "새 속성" (Notion's, seen 2026-11-12).
   */
  mode?: "visibility" | "edit" | "timelineTable";
  /** The property whose settings it opens on. */
  initialDetailId?: string;
  onClose?(): void;
  /** Moves a property to the deleted properties. */
  onDeleteProperty?(propertyId: string): void;
  /**
   * In a phone's settings sheet the list heads the sheet, "속성", back to the
   * settings, and a property's settings head it "속성 편집", back to the
   * list, as Notion's (seen 2026-10-23).
   */
  phoneBack?(): void;
};

export function DatabasePropertiesMenu({
  contextIdentity,
  dataSource,
  dataSources,
  properties,
  snapshotIdentity,
  view,
  writable,
  onCommitProperties,
  onCreateProperty,
  onUpdateView,
  searchRelationDatabases,
  deletedProperties,
  pagesById,
  propertiesByDataSourceId,
  mode = "visibility",
  initialDetailId,
  onClose,
  onDeleteProperty,
  phoneBack,
}: PropertiesMenuProps) {
  const labels = databaseViewLabels();
  const openScreen = useOpenSettingsScreen(), sourcesSurface = useContext(DatabaseDataSourcesContext);
  const { t } = useTranslation(["rowProperties", "propertyTypes", "databaseView"]);
  const [search, setSearch] = useState("");
  const searchRef = useRef<HTMLInputElement>(null);
  const phoneSettings = useContext(PhoneSettingsListContext);
  const [visibilityTab, setVisibilityTab] = useState<"bars" | "table">("bars");
  const [detailId, setDetailId] = useState<string | null>(initialDetailId ?? null);
  // Opened at a property (a cell's "속성 편집", a relation just made), its
  // panel's own "←" is the way back, as Notion's "← 속성 편집 ×" (seen
  // 2026-11-02); from the list, a row leads back to it.
  const [openedAtDetail, setOpenedAtDetail] = useState(Boolean(initialDetailId));
  // A new relation opens in the settings' place, from its "새 속성".
  const [relationOpen, setRelationOpen] = useState<{ anchor: HTMLElement | null; name?: string } | null>(null);
  // In the view settings panel "새 속성" turns the panel into its own screen,
  // "← 〈database〉의 새 속성" (DatabaseNewPropertyScreen), as Notion's (seen
  // 2026-11-16, 2026-11-17); the property made there opens its settings, whose
  // "←" comes back to a fresh one. Elsewhere (a phone's sheet) it lists the
  // types beside the list, as the table's "+" does, a press outside both
  // closing the list's host too (closesHost). A relation is one of the types
  // and asks which database first. Notion's list has "새 속성" alone
  // (2026-10-16: a gray "관계형" row under it read as a heading).
  const panelScreen = useInSettingsPanel() && !phoneBack;
  const [newOpen, setNewOpen] = useState(false);
  const [madeFromNew, setMadeFromNew] = useState(false);
  const screenRef = useRef<HTMLDivElement>(null);
  const [typesOpen, setTypesOpen] = useState(false);
  const newRef = useRef<HTMLButtonElement>(null);
  const typesRef = useRef<HTMLDivElement>(null);
  const typeChoice = useTablePropertyTypeChoice(undefined,
    { dataSourceId: dataSource.id, has: properties.some((property) => property.type === "unique_id") }, typesOpen);
  const typesPlace = usePopoverPlacement(typesRef, typesOpen ? (size, viewport) => placeBesideRow(newRef.current, size, viewport) : null,
    [typesOpen], undefined, { kind: "submenu", onEscape: () => setTypesOpen(false), onOutsidePress: () => setTypesOpen(false),
      inside: () => [newRef.current] });
  async function chooseType(type: PropertyType | null) {
    setTypesOpen(false);
    if (!type || !onCreateProperty) return;
    if (type === "relation") { setRelationOpen({ anchor: newRef.current }); return; }
    const made = await onCreateProperty(type, localizedPropertyTypeLabel(type));
    if (made) setDetailId(made.id);
  }
  // From the panel's screen: named as typed or for its type, with the icon picked and its type's settings.
  async function makeNew({ type, name, icon }: NewPropertyChoice) {
    if (!onCreateProperty) return;
    if (type === "relation") { setRelationOpen({ anchor: screenRef.current, name }); return; }
    const made = await onCreateProperty(type, name.trim() || localizedPropertyTypeLabel(type),
      { ...(configForType(type, undefined, dataSource.id) ?? {}), ...(icon ? { icon } : {}) });
    if (!made) return;
    setNewOpen(false);
    setMadeFromNew(true);
    setDetailId(made.id);
  }
  const [deleted, setDeleted] = useState<DatabaseDeletedProperty[]>([]);
  const [deletedCursor, setDeletedCursor] = useState<string>();
  const [deletedBusy, setDeletedBusy] = useState(false);
  // "삭제한 속성 N ›" opens its own screen, as Notion's (seen 2026-11-02).
  const [deletedOpen, setDeletedOpen] = useState(false);
  // Deleted properties are read again whenever the schema changes.
  useEffect(() => {
    if (!deletedProperties || mode !== "edit") return;
    const controller = new AbortController();
    deletedProperties.load(controller.signal).then((page) => {
      setDeleted(page.items);
      setDeletedCursor(page.nextCursor);
    }, () => {});
    return () => controller.abort();
  }, [deletedProperties, mode, properties]);
  // However many there are, the next ones load when asked for.
  async function loadMoreDeleted() {
    if (!deletedProperties || !deletedCursor) return;
    setDeletedBusy(true);
    try {
      const page = await deletedProperties.load(undefined, deletedCursor);
      setDeleted((current) => [...current, ...page.items.filter((item) => !current.some(({ id }) => id === item.id))]);
      setDeletedCursor(page.nextCursor);
    } catch {
      // The button stays for another try.
    } finally {
      setDeletedBusy(false);
    }
  }
  async function changeDeleted(action: () => Promise<void>, propertyId: string) {
    setDeletedBusy(true);
    try {
      await action();
      setDeleted((current) => current.filter(({ id }) => id !== propertyId));
      return true;
    } catch {
      // The list stays as it was; the next open reads it again.
      return false;
    } finally {
      setDeletedBusy(false);
    }
  }
  // A restored property opens on its settings, as Notion's (seen 2026-11-02).
  async function restoreDeleted(propertyId: string) {
    if (!deletedProperties || !await changeDeleted(() => deletedProperties.restore(propertyId), propertyId)) return;
    setDeletedOpen(false);
    setDetailId(propertyId);
  }
  // Asked first, as Notion's, since it can't be undone.
  async function deleteForever(propertyId: string) {
    if (!deletedProperties) return;
    const confirmed = await confirmAction({ message: labels.deletePropertyForeverConfirm, detail: labels.cannotUndo,
      confirmLabel: labels.deletePropertyForever, danger: true });
    if (confirmed) await changeDeleted(() => deletedProperties.deletePermanently(propertyId), propertyId);
  }
  async function deleteAllForever() {
    if (!deletedProperties) return;
    const confirmed = await confirmAction({ message: labels.deleteAllPropertiesForeverConfirm, detail: labels.cannotUndo,
      confirmLabel: labels.deletePropertyForever, danger: true });
    if (!confirmed) return;
    setDeletedBusy(true);
    try {
      let cursor: string | undefined;
      let items = deleted;
      do {
        for (const property of items) await deletedProperties.deletePermanently(property.id);
        if (!cursor && !deletedCursor) break;
        const page = await deletedProperties.load(undefined, cursor ?? deletedCursor);
        items = page.items;
        cursor = page.nextCursor;
      } while (items.length);
      setDeleted([]);
      setDeletedCursor(undefined);
      setDeletedOpen(false);
    } catch {
      // What is left shows when the screen reads them again.
    } finally {
      setDeletedBusy(false);
    }
  }
  // In the view's order, as Notion's "속성" lists them (seen 2026-11-12: a view keeping none lists them by name).
  const ordered = orderViewProperties(properties, Array.isArray(view.config.propertyOrder)
    ? view.config.propertyOrder.filter((id): id is string => typeof id === "string") : undefined);
  const visible = ordered.filter((property) => property.name.toLocaleLowerCase().includes(search.toLocaleLowerCase()));
  const detail = properties.find((property) => property.id === detailId);
  // The panel's heading, as Notion's (seen 2026-11-02): "속성" over the list,
  // "← 삭제한 속성" over the deleted ones and "← 속성 편집" over a property
  // opened from the list, each "←" back to the list.
  const leaveDetail = () => { setDetailId(null); if (madeFromNew) { setMadeFromNew(false); setNewOpen(true); } };
  useSettingsScreenHead(phoneBack || mode === "visibility" ? null
    : detail ? (openedAtDetail ? null : { title: labels.editProperties, onBack: leaveDetail })
    : newOpen ? { title: t("databaseView:newPropertyIn", { name: surfaceDataSourceName(sourcesSurface, dataSource) }), onBack: () => setNewOpen(false) }
    : mode === "timelineTable" ? null
    : deletedOpen ? { title: labels.deletedProperties, onBack: () => setDeletedOpen(false) }
    : { title: labels.properties });
  // A relation asks which database first, in the settings' place; its settings follow, as Notion's "속성 편집".
  const relationDialog = (open: { anchor: HTMLElement | null; name?: string }) => onCreateProperty ? (
    <RelationPropertyCreationDialog anchor={open.anchor} initialName={open.name} databaseId={dataSource.id}
      snapshot={{ databases: relationDatabaseOptions(dataSources), snapshotIdentity }}
      context={{
        bounds: RELATION_CREATION_BOUNDS,
        identity: contextIdentity,
        notifyFailure: () => {},
        commit: (command) => onCreateProperty("relation", command.name, command.propertyConfig),
        searchDatabases: searchRelationDatabases,
      }}
      onCancel={() => setRelationOpen(null)}
      onCreated={(property) => {
        setRelationOpen(null);
        if (newOpen) { setNewOpen(false); setMadeFromNew(true); }
        setDetailId(property.id);
      }} />
  ) : null;
  if (detail) {
    return (
      <>
        {phoneBack ? <DatabaseTaskSettingsHeader title={labels.editProperties} backLabel={labels.backToProperties}
          onBack={() => { setOpenedAtDetail(false); setDetailId(null); }} /> : null}
        <DatabasePropertyEditPanel property={detail} focusName={madeFromNew ? "select" : undefined}
          onClose={() => { setOpenedAtDetail(false); setDetailId(null); }} editing={{
          contextIdentity, dataSource, dataSources, properties, snapshotIdentity, view, writable, pagesById,
          propertiesByDataSourceId, onCommitProperties, onUpdateView, onDeleteProperty,
        }} />
      </>
    );
  }

  if (newOpen && onCreateProperty) {
    return (
      <div ref={screenRef} className={styles.sourcePropertiesMenu} data-property-edit="new" data-new-property-screen>
        <DatabaseNewPropertyScreen onMake={(choice) => void makeNew(choice)}
          idSource={{ dataSourceId: dataSource.id, has: properties.some((property) => property.type === "unique_id") }} />
        {relationOpen ? relationDialog(relationOpen) : null}
      </div>
    );
  }

  if (deletedOpen && mode === "edit" && deletedProperties) {
    // Each with its icon and name, "되돌리기" and "삭제", then "모두 영구 삭제하기" (Notion's, seen 2026-11-02).
    return (
      <div className={styles.sourcePropertiesMenu} data-deleted-properties>
        {phoneBack && <DatabaseTaskSettingsHeader title={labels.deletedProperties} backLabel={labels.properties} onBack={() => setDeletedOpen(false)} />}
        <div className={styles.sourcePropertyList}>
          {deleted.map((property) => (
            <div key={property.id} className={styles.sourcePropertyRow} data-deleted-property={property.id}>
              <span className={styles.propertyToggle}>
                <PropertyIcon property={property} size="small" />
                <span>{property.name}</span>
              </span>
              <button type="button" disabled={!writable || deletedBusy} data-restore-property
                aria-label={labels.restorePropertyNamed(property.name)} onClick={() => void restoreDeleted(property.id)}>
                <RestoreIcon size="small" aria-hidden="true" />
              </button>
              <button type="button" disabled={!writable || deletedBusy} data-delete-property-forever
                aria-label={labels.deletePropertyForeverNamed(property.name)} onClick={() => void deleteForever(property.id)}>
                <Trash size="small" aria-hidden="true" />
              </button>
            </div>
          ))}
          {deletedCursor ? (
            <button type="button" className={styles.sourcePropertyRow} disabled={deletedBusy} data-deleted-properties-more
              onClick={() => void loadMoreDeleted()}>
              <span className={styles.propertyToggle}>{t("databaseView:loadMore")}</span>
            </button>
          ) : null}
        </div>
        {deleted.length ? (
          <button type="button" className={styles.sourcePropertyDeleteAll} disabled={!writable || deletedBusy} data-delete-properties-forever
            onClick={() => void deleteAllForever()}>
            <Trash size="regular" aria-hidden="true" />{labels.deleteAllPropertiesForever}
          </button>
        ) : null}
      </div>
    );
  }

  return (
    <div className={styles.sourcePropertiesMenu} data-closable={onClose ? "true" : undefined}>
      {phoneBack && <DatabaseTaskSettingsHeader title={labels.properties} backLabel={labels.backToViewSettings} onBack={phoneBack} />}
      <label className={styles.sourcePropertySearch}>
        {/* As in Notion, the view's property list and "속성 편집" open with their search focused (seen 2026-11-06). */}
        <input ref={searchRef} value={search} placeholder={labels.searchProperties} aria-label={labels.searchProperties}
          autoFocus={(mode === "visibility" || !phoneBack) && fieldFocusOnOpen()} onChange={(event) => setSearch(event.currentTarget.value)} />
        {/* A phone's sheet clears it with "입력 값 지우기", as Notion's and the library's (seen 2026-11-25). */}
        {phoneBack || phoneSettings ? <SearchClearButton value={search} inputRef={searchRef} className={styles.sourcePropertySearchClear} onClear={() => setSearch("")} /> : null}
      </label>
      {mode === "visibility" ? (<>
        {view.type === "timeline" ? <DatabaseTimelineVisibilityTabs view={view} tab={visibilityTab} onTab={setVisibilityTab} writable={writable}
          onUpdateView={onUpdateView} /> : null}
        {visibilityTab === "bars" || view.type !== "timeline" || view.config?.timelineShowTable === true ? (
          <DatabasePropertyVisibilityList view={view} properties={properties} search={search} writable={writable}
            onUpdateView={onUpdateView} timelineTable={view.type === "timeline" && visibilityTab === "table"} />
        ) : null}
        {/* Cards laid out "작게" say which properties take a line of their own from here too, as Notion's (seen 2026-11-05). */}
        {openScreen && view.config?.cardLayout === "compact" && (view.type === "gallery" || view.type === "board") && (
          <button type="button" className={styles.sourcePropertyMore} data-card-property-style onClick={() => openScreen("compactCards")}>
            <PaintBrushIcon size="regular" aria-hidden="true" />
            <span>{labels.changePropertyStyle}</span>
          </button>
        )}
      </>) : mode === "timelineTable" ? (
        <DatabasePropertyVisibilityList view={view} properties={properties} search={search} writable={writable}
          onUpdateView={onUpdateView} timelineTable onOpenProperty={setDetailId} />
      ) : (
        <div className={styles.sourcePropertyList}>
          {visible.map((property) => (
            <div key={property.id} className={styles.sourcePropertyRow} data-source-property={property.id}>
              <button type="button" className={styles.propertyToggle} aria-label={t("propertyOptions", { name: property.name })}
                onClick={() => setDetailId(property.id)}>
                <PropertyIcon property={property} size="regular" />
                <span>{property.name}</span>
              </button>
              <button type="button" tabIndex={-1} aria-hidden="true" onClick={() => setDetailId(property.id)}>
                <ChevronRight size="tiny" aria-hidden="true" />
              </button>
            </div>
          ))}
        </div>
      )}
      {mode !== "visibility" && writable && onCreateProperty && (
        <div className={styles.sourcePropertyActions}>
          <button ref={newRef} type="button" aria-haspopup="dialog" aria-expanded={typesOpen} data-new-source-property
            onClick={() => {
              if (panelScreen) { setNewOpen(true); return; }
              if (typesOpen) { setTypesOpen(false); return; } typeChoice.reset(); setTypesOpen(true);
            }}>
            <Plus size="small" /> {labels.newProperty}
          </button>
        </div>
      )}
      {typesOpen && typeof document !== "undefined" ? createPortal(
        <div ref={typesRef} className={tableStyles.menu} role="dialog" aria-label={t("rowProperties:newPropertyType")}
          data-new-source-property-types style={popoverPlacementStyle(typesPlace)}>
          <TablePropertyTypeList choice={typeChoice} onFinish={(type) => void chooseType(type)} />
        </div>,
        document.body,
      ) : null}
      {relationOpen ? relationDialog(relationOpen) : null}
      {mode === "edit" && !visible.length && <div className={styles.sourcePropertyEmpty}>{labels.noSearchResults}</div>}
      {mode === "edit" && deletedProperties && deleted.length > 0 && (
        <button type="button" className={styles.sourcePropertyMore} data-deleted-properties-open onClick={() => setDeletedOpen(true)}>
          <Trash size="regular" aria-hidden="true" />
          <span>{labels.deletedProperties}</span>
          <small>{deletedCursor ? `${deleted.length}+` : deleted.length}</small>
          <ChevronRight size="tiny" aria-hidden="true" />
        </button>
      )}
      {onClose && (
        <button type="button" className={styles.sourcePropertyClose} aria-label={labels.closeProperties} onClick={() => onClose?.()}>
          <X size="small" />
        </button>
      )}
    </div>
  );
}
