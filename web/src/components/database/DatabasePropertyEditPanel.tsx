import { useEffect, useMemo, useRef } from "react";
import type { GraphDataSource, GraphProperty, GraphView } from "@/contentGraph";
import type { DbProperty, Page, PropertyConfig } from "@/lib/types";
import { DatabaseButtonActionsEditor } from "./DatabaseButtonActionsEditor";
import { relationTargetConfig, relationTwoWayConfig } from "@/lib/relationPairConfig";
import { nativeBoardQueryBounds } from "./boardViewModel";
import { DatabasePropertyEditScreen } from "./DatabasePropertyEditScreen";
import { DatabasePropertyPhoneEditScreen } from "./DatabasePropertyPhoneEditScreen";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PropertyTypeConfig } from "./PropertyTypeConfig";
import type { TableRelationCreation } from "./TablePropertyMenu";
import { propertiesAfterDuplicate } from "./rowPropertiesModel";
import { tableConfigAfterDuplicate } from "./tablePropertyMenuModel";
import { confirmPropertyDeletion } from "./confirmPropertyDeletion";
import { inlineDatabasePlaceholderTitle } from "../editor/databaseTitles";
import { plainTitleOf } from "@/lib/closedMentions";
import { dataSourcePage } from "@/lib/dataSourcePage";

/** What changing a database's properties needs, where they can be changed. */
export type DatabasePropertyEditing = {
  contextIdentity: string;
  dataSource: GraphDataSource;
  dataSources: readonly GraphDataSource[];
  properties: readonly GraphProperty[];
  snapshotIdentity: string;
  /** The view it is edited from; a database page opened by its address has none. */
  view?: GraphView;
  writable: boolean;
  /** The row pages a formula's editor previews its result for. */
  pagesById?: Record<string, Page>;
  /** The properties of the databases its relations reach, which a rollup's settings list as its target. */
  propertiesByDataSourceId?: Readonly<Record<string, readonly DbProperty[]>>;
  /** Reads the databases its relations reach as a property's menu opens: a row page away from its database holds none till then. */
  readRelated?(): void;
  /** Saves them; what it answers settles as they're saved, for one that waits (the layout builder's apply). */
  onCommitProperties(properties: readonly GraphProperty[]): void | Promise<void>;
  onUpdateView?(view: GraphView): void;
  /** Moves a property to the deleted properties. */
  onDeleteProperty?(propertyId: string): void;
  /** Makes a new relation, to a database the page shows or one found by name. */
  relationCreation?: TableRelationCreation;
};

function dbProperty(property: GraphProperty, index: number): DbProperty {
  return {
    id: property.id,
    databaseId: property.dataSourceId,
    name: property.name,
    type: property.type,
    position: index,
    config: property.config as PropertyConfig,
    sprintManaged: property.sprintManaged,
  };
}

function graphProperty(property: DbProperty, current: GraphProperty): GraphProperty {
  return {
    ...current,
    name: property.name,
    type: property.type,
    config: property.config as Record<string, unknown> ?? {},
  };
}

/**
 * One property's settings, as Notion's "속성 편집" (seen 2026-09-30): the view
 * settings' property list and a database page's property menu open it.
 */
export function DatabasePropertyEditPanel({ property, editing, focusName = false, layoutCard = false, rowPage = false, rollupForm = false, onClose }: {
  property: GraphProperty;
  editing: DatabasePropertyEditing;
  /** A rollup's small form alone, as its value opens it ("관계형", "속성", "계산"). */
  rollupForm?: boolean;
  /** A page layout's property card: its moves follow in place of copying and deleting, as Notion's (seen 2026-11-01). */
  layoutCard?: boolean;
  /** From a database page's property menu, which shows no view's column settings, as Notion's (seen 2026-11-06). */
  rowPage?: boolean;
  /** Puts the caret in the name, as the page's "이름 바꾸기" does, or "select" all of a new one's. */
  focusName?: boolean | "select";
  onClose(): void;
}) {
  const {
    contextIdentity, dataSource, dataSources, properties, snapshotIdentity, view, writable, pagesById,
    propertiesByDataSourceId, onCommitProperties, onUpdateView, onDeleteProperty,
  } = editing;
  const dbProperties = useMemo(() => properties.map(dbProperty), [properties]);
  const readRelated = editing.readRelated;
  useEffect(() => readRelated?.(), [readRelated]);
  // On a phone they are Notion's sheet of white groups (DatabasePropertyPhoneEditScreen).
  const phone = useSidebarLayout(true).phone;
  // A relation's settings name the database it reaches, on this page or another.
  const pages = useMemo(() => [
    ...dataSources.map((source) => dataSourcePage(source, "", 0, { parentId: null })),
    ...Object.values(pagesById ?? {}).filter((page) => page.kind === "database" && !dataSources.some(({ id }) => id === page.id)),
  ], [dataSources, pagesById]);

  function updateDbProperty(propertyId: string, patch: Partial<DbProperty>) {
    const currentIndex = properties.findIndex((candidate) => candidate.id === propertyId);
    const current = properties[currentIndex];
    if (!current) return;
    const projected = { ...dbProperty(current, currentIndex), ...patch } as DbProperty;
    onCommitProperties(properties.map((candidate) => candidate.id === propertyId
      ? graphProperty(projected, current)
      : candidate));
  }

  const projected = dbProperty(property, properties.indexOf(property));
  // A button's actions, kept as the document the server runs, edit here as
  // from its column's menu, saved onto the property as it is by then.
  const live = useRef(properties);
  live.current = properties;
  const typeSettings = property.type === "button" ? (
    <DatabaseButtonActionsEditor key={property.id} property={property} properties={dbProperties} pages={[dataSourcePage(dataSource, "", 0, { parentId: null })]}
      current={() => live.current.find(({ id }) => id === property.id)}
      onSave={(next) => onCommitProperties([next])} />
  ) : (
    <PropertyTypeConfig
      prop={projected}
      snapshot={{
        databases: pages,
        formulaBounds: nativeBoardQueryBounds.formula,
        // Wherever it opens, a rollup reads its related database's properties as the column's menu does.
        propertiesByDatabase: { ...(propertiesByDataSourceId ?? {}), [dataSource.id]: dbProperties },
        snapshotIdentity,
        pagesById,
      }}
      context={{
        contextIdentity,
        ops: {
          loadDatabase: () => {},
          setRelationDatabase: (_context, propertyId, targetDatabaseId) => updateDbProperty(propertyId, {
            config: relationTargetConfig(projected.config ?? {}, dataSource.id, targetDatabaseId),
          }),
          setRelationTwoWay: (_context, propertyId, enabled, reciprocalName) => updateDbProperty(propertyId, {
            config: relationTwoWayConfig(projected.config ?? {}, dataSource.id, enabled, reciprocalName),
          }),
          updateProperty: (_context, propertyId, patch) => updateDbProperty(propertyId, patch),
        },
      }}
      onClose={onClose}
      cellForm={rollupForm}
    />
  );
  if (rollupForm) return typeSettings;
  const Screen = phone ? DatabasePropertyPhoneEditScreen : DatabasePropertyEditScreen;
  return (
    <Screen property={property} properties={properties} view={layoutCard || rowPage ? undefined : view} dataSourceId={dataSource.id}
      writable={writable} manageAccess={Boolean(dataSource.manageAccess)} typeSettings={typeSettings} focusName={focusName} popover={rowPage}
      onUpdate={(next) => onCommitProperties([next])}
      onUpdateView={onUpdateView}
      onDuplicate={writable && !layoutCard ? (duplicate) => {
        // The copy comes right after its property, as the column menu places it, the view's
        // columns or, from a page, the page's lines giving a database without one its page order.
        const config = view ? tableConfigAfterDuplicate(view.config, [...properties, duplicate], property.id, duplicate.id) : null;
        onCommitProperties(propertiesAfterDuplicate(properties, property.id, duplicate, rowPage ? undefined : config?.propertyOrder));
        if (view && config && onUpdateView) onUpdateView({ ...view, config });
      } : undefined}
      // Asked first, as the column's menu asks (Notion's, seen 2026-11-02 from the settings too).
      onDelete={writable && onDeleteProperty && !layoutCard ? (propertyId) => {
        void confirmPropertyDeletion({
          property: { id: property.id, type: property.type, dataSourceId: dataSource.id, config: property.config },
          database: plainTitleOf(dataSource) || inlineDatabasePlaceholderTitle(),
          relatedDatabase: (id) => dataSources.find((source) => source.id === id)?.title ?? pagesById?.[id]?.title,
        }).then((confirmed) => { if (!confirmed) return; onDeleteProperty(propertyId); onClose(); });
      } : undefined} />
  );
}
