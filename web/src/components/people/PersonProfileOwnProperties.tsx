import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { emptyContentGraph, type ContentGraph, type GraphDataSource, type GraphProperty, type GraphRow } from "@/contentGraph";
import { PropertyCell } from "@/components/database/PropertyCell";
import { PropertyTypeIcon } from "@/components/database/PropertyTypeIcon";
import { tableRowPage } from "@/components/database/tableGridRowCells";
import { useNativePropertyCells } from "@/components/database/useNativePropertyCells";
import { readPeopleDatabase, peopleRowPerson, type PeopleDatabase } from "@/lib/peopleDatabase";
import { usePeopleRevision } from "@/lib/peopleProfiles";
import type { DbProperty, PropertyConfig } from "@/lib/types";
import type { Session, Workspace } from "@/types";
import { usePeopleDatabaseWrites } from "./usePeopleDatabaseWrites";
import styles from "./PeopleView.module.css";

const NO_SOURCES: readonly GraphDataSource[] = [];

/** Whether a row shows nothing for a property, as an empty cell does. */
function rowValueEmpty(row: GraphRow, property: GraphProperty) {
  const value = property.type === "created_time" ? row.createdAt
    : property.type === "last_edited_time" ? row.updatedAt
      : property.type === "last_edited_by" ? row.lastEditedBy : row.values[property.id]?.value;
  return value === null || value === undefined || value === "" || (Array.isArray(value) && value.length === 0);
}

/**
 * The properties a workspace's owner added to People, on a person's profile
 * after "Membership Type", each with its kind's icon and the person's value
 * in their People row (people_database.go), as Notion's profile shows them
 * (seen 2026-11-15): its owner writes the value there, everyone else reads
 * it; one without a value says so as "About" does. They are read again as
 * People changes.
 */
export function PersonProfileOwnProperties({ session, workspace, userId }: {
  session: Session;
  workspace: Workspace;
  userId: string;
}) {
  const { t } = useTranslation("peopleProfiles");
  const [database, setDatabase] = useState<PeopleDatabase | null>(null);
  const [graph, setGraph] = useState<ContentGraph>(emptyContentGraph);
  const revision = usePeopleRevision(workspace.id);
  const read = useCallback(async (signal?: AbortSignal) => {
    const answer = await readPeopleDatabase(session, workspace.id, signal);
    setDatabase(answer);
    setGraph(answer.graph);
  }, [session, workspace.id]);
  useEffect(() => {
    const controller = new AbortController();
    read(controller.signal).catch(() => {});
    return () => controller.abort();
  }, [read, revision]);
  const reread = useCallback(() => read().then(() => true, () => false), [read]);
  const writes = useMemo(() => database?.editable ? { session, workspaceId: workspace.id, pageId: database.pageId } : null,
    [database, session, workspace.id]);
  const adapters = usePeopleDatabaseWrites({ input: writes, graph, setGraph, onReread: reread });
  const ids = database?.propertyIds;
  const sourceId = database?.dataSourceId ?? "";
  const own = useMemo(() => new Set(ids ? [ids.name, ids.email, ids.about, ids.membership, ids.person] : []), [ids]);
  const properties = useMemo(() => graph.properties.filter((property) => property.dataSourceId === sourceId && !own.has(property.id)),
    [graph.properties, own, sourceId]);
  const row = useMemo(() => ids ? graph.rows.find((candidate) => peopleRowPerson(candidate, ids) === userId) : undefined, [graph.rows, ids, userId]);
  const source = useMemo<GraphDataSource>(() => ({ id: sourceId, pageId: database?.pageId, title: "" }), [database?.pageId, sourceId]);
  const sources = useMemo(() => [source], [source]);
  const rows = useMemo(() => row ? [row] : [], [row]);
  const filterGraph = useMemo(() => ({ properties, rows }), [properties, rows]);
  const adapter = adapters.tableAdapter;
  const { propertyCellEnvironment } = useNativePropertyCells({
    adapter, readOnly: !adapter, contextIdentity: `${sourceId}:${userId}`, currentUserId: session.user.id,
    dataSource: source, dataSources: sources.length ? sources : NO_SOURCES, filterGraph, pageId: database?.pageId ?? "",
    properties, requestGeneration: 0, rows, workspaceId: workspace.id,
  });
  if (!row || !properties.length || !propertyCellEnvironment) return null;
  const page = tableRowPage(row, properties, workspace.id);
  return (
    <>
      {properties.map((property, position) => (
        <div key={property.id} className={styles.property} data-person-own-property={property.id}>
          <div className={styles.propertyLabel}><PropertyTypeIcon type={property.type} />{property.name}</div>
          <div className={`${styles.propertyValue} ${styles.ownValue}`}>
            {/* One without a value says so, as "About" does, until it is written in. */}
            {property.type !== "checkbox" && rowValueEmpty(row, property) ? <span className={styles.ownEmpty} aria-hidden="true">{t("empty")}</span> : null}
            <PropertyCell row={page} presentation="rowDetail" readOnly={!adapter} {...propertyCellEnvironment}
              prop={{ id: property.id, databaseId: sourceId, name: property.name, type: property.type,
                config: property.config as PropertyConfig, position } as DbProperty} />
          </div>
        </div>
      ))}
    </>
  );
}
