import { useCallback, useMemo, useRef, useState } from "react";
import { compareGraphOrder, type GraphOperation, type GraphProperty } from "@/contentGraph";
import { plainTitleOf } from "@/lib/closedMentions";
import { sendContentGraphChange } from "@/lib/contentGraphSend";
import { graphPositionBetween } from "@/lib/nativeBoardProductAdapter";
import { propertyDeletion } from "@/lib/relatedPropertyDeletion";
import type { RelationPropertyCommand } from "@/lib/relationPropertyCreationModel";
import type { PropertyType } from "@/lib/types";
import type { Session } from "@/types";
import type { DatabasePropertyEditing } from "./DatabasePropertyEditPanel";
import type { PageLayoutTabEnvironment } from "./pageLayoutTabEnvironment";
import { newRowPageProperty } from "./rowPagePropertyAdd";

// A relation made in the builder stands in the draft under this until it is made.
const DRAFT_RELATION = "draft-relation-";

/**
 * The layout builder's changes to its database's properties, which wait in
 * the draft until it is applied, as Notion's (seen 2026-10-25: a visibility
 * chosen or a property added is gone on 취소; 2026-11-12: a relation made
 * there is not in the database before, and gone on 취소, nor a tab's
 * property renamed from its column's head): changed and new properties, of
 * the database or a tab's source, relations — each a stand-in property the
 * draft names until its relation is made — and the properties to delete.
 * A source a tab reads from its own page (sourcePages) has its properties'
 * changes written there, the database's writers not reaching it yet.
 */
export function usePageLayoutPropertyDraft(editing: DatabasePropertyEditing | undefined, properties: readonly GraphProperty[], dataSourceId: string) {
  const [pending, setPending] = useState<readonly GraphProperty[]>([]);
  const [relations, setRelations] = useState<readonly { property: GraphProperty; command: RelationPropertyCommand }[]>([]);
  // Properties "레이아웃 및 데이터베이스에서 제거" takes out, deleted as the layout is applied.
  const [removing, setRemoving] = useState<readonly string[]>([]);
  // Where each property of a source read from its own page is written, by the property.
  const elsewhere = useRef(new Map<string, { session: Session; pageId: string }>());
  const base = editing?.properties ?? properties;
  const shownProperties = useMemo(() => withChanges([...base, ...relations.map(({ property }) => property)],
    pending.filter((property) => property.dataSourceId === dataSourceId))
    .filter((property) => !removing.includes(property.id)), [base, dataSourceId, relations, pending, removing]);
  const relationCreation = editing?.relationCreation;
  // A relation asked for here (the tab menu's, a phone's window, a property's) is a draft's stand-in.
  const draftRelationCreation = useMemo(() => relationCreation && { ...relationCreation, context: { ...relationCreation.context,
    commit: async (command: RelationPropertyCommand) => {
      const property: GraphProperty = { ...newRowPageProperty("relation", dataSourceId, shownProperties),
        id: `${DRAFT_RELATION}${command.mutationId}`, name: command.name, config: command.propertyConfig };
      setRelations((current) => [...current, { property, command }]);
      return property;
    } } }, [dataSourceId, relationCreation, shownProperties]);
  // A property changed here waits, a stand-in's change staying with its relation.
  const stage = (changed: readonly GraphProperty[]) => {
    const own = (property: GraphProperty) => property.id.startsWith(DRAFT_RELATION);
    setRelations((current) => current.map((item) => ({ ...item, property: changed.find((property) => property.id === item.property.id) ?? item.property })));
    setPending((current) => withChanges(current, changed.filter((property) => !own(property))));
  };
  // A property deleted here goes as the layout is applied; one made here just goes.
  const drop = (propertyId: string, known: boolean) => {
    if (propertyId.startsWith(DRAFT_RELATION)) setRelations((current) => current.filter(({ property }) => property.id !== propertyId));
    else if (!known) setPending((current) => current.filter((property) => property.id !== propertyId));
    else setRemoving((current) => current.includes(propertyId) ? current : [...current, propertyId]);
  };
  const draftEditing = useMemo<DatabasePropertyEditing | undefined>(() => editing && {
    ...editing, properties: shownProperties, onCommitProperties: (changed) => setPending((current) => withChanges(current, changed)),
    relationCreation: draftRelationCreation,
    onDeleteProperty: (propertyId) => drop(propertyId, base.some((property) => property.id === propertyId)),
  }, [base, draftRelationCreation, editing, shownProperties]);
  // The tabs show their sources as the draft has them: a new two-way
  // relation's other side among its source's columns (Notion's, seen
  // 2026-11-12: "w556 과제" in the new tab's table before it is applied),
  // and a column's property changed from its head waiting with the layout.
  const sourceName = plainTitleOf(editing?.dataSource);
  const draftTabEnvironment = useCallback((given: PageLayoutTabEnvironment): PageLayoutTabEnvironment => {
    if (!editing) return given;
    const known = given.filterGraph.properties;
    const others = withChanges(known.filter((property) => property.dataSourceId !== dataSourceId),
      pending.filter((property) => property.dataSourceId !== dataSourceId)).filter((property) => !removing.includes(property.id));
    const drafted = [...shownProperties, ...others];
    const pairs = relations.flatMap(({ property }) => relatedStandIn(property, dataSourceId, sourceName, drafted));
    const place = (properties: readonly GraphProperty[]) => {
      for (const property of properties) {
        const pageId = given.sourcePages?.[property.dataSourceId];
        if (pageId && given.session) elsewhere.current.set(property.id, { session: given.session, pageId });
      }
    };
    const tableAdapter = given.tableAdapter && { ...given.tableAdapter,
      commitProperties: async (changed: readonly GraphProperty[]) => { place(changed); stage(changed); },
      deleteProperties: async (propertyIds: readonly string[]) => {
        place(known.filter((property) => propertyIds.includes(property.id)));
        for (const id of propertyIds) drop(id, known.some((property) => property.id === id));
      },
    };
    return { ...given, tableAdapter, filterGraph: { ...given.filterGraph, properties: [...drafted, ...pairs] } };
  // eslint-disable-next-line react-hooks/exhaustive-deps -- stage and drop only set this hook's state.
  }, [dataSourceId, editing, pending, relations, removing, shownProperties, sourceName]);
  // A new property goes where it was asked for, named as typed and with the icon picked, as the row page's "속성 추가" makes it.
  const addProperty = editing ? (type: PropertyType, name?: string, icon?: string) => {
    const property = newRowPageProperty(type, dataSourceId, shownProperties, name, icon);
    setPending((current) => [...current, property]);
    return property.id;
  } : undefined;
  /**
   * Makes the draft's relations, then its properties, the layout taking
   * each relation's property in place of its stand-in; the layout to apply.
   * Each relation made is the database's from then on: `made` hands the
   * caller the layout naming it, and the draft's changes name it too, so a
   * step refused after it leaves the builder showing the relation that was
   * made, not an empty stand-in, and applying again does not make it twice.
   */
  async function commit<T>(layout: T, made?: (layout: T) => void): Promise<T> {
    let changes = pending;
    for (const { property, command } of relations) {
      const relation = relationCreation ? await relationCreation.context.commit({ ...command, name: property.name, propertyConfig: property.config },
        new AbortController().signal) : null;
      if (!relation) throw new Error("relation not made");
      [layout, changes] = [renamedId(layout, property.id, relation.id), renamedId(changes, property.id, relation.id)];
      setRelations((current) => current.filter((item) => item.property.id !== property.id));
      setPending((current) => renamedId(current, property.id, relation.id));
      made?.(layout);
    }
    const here = changes.filter((property) => !elsewhere.current.has(property.id));
    if (here.length > 0 && editing) await editing.onCommitProperties(here);
    await writeElsewhere(changes.flatMap((property) => elsewhere.current.has(property.id)
      ? [{ propertyId: property.id, operation: { kind: "upsertProperty", property } as GraphOperation }] : []));
    setPending([]);
    return layout;
  }
  /** The properties taken out of the database too, after the layout is applied. */
  function deleteRemoved() {
    for (const propertyId of removing) if (!elsewhere.current.has(propertyId)) editing?.onDeleteProperty?.(propertyId);
    void writeElsewhere(removing.flatMap((propertyId) => elsewhere.current.has(propertyId)
      ? [{ propertyId, operation: { kind: "deleteProperty", propertyDeletion: propertyDeletion(propertyId) } as GraphOperation }] : []));
    setRemoving([]);
  }
  // Each page's writes at once, in the order made.
  async function writeElsewhere(writes: readonly { propertyId: string; operation: GraphOperation }[]) {
    const runs = new Map<string, { session: Session; operations: GraphOperation[] }>();
    for (const { propertyId, operation } of writes) {
      const { session, pageId } = elsewhere.current.get(propertyId)!;
      const run = runs.get(pageId) ?? { session, operations: [] };
      runs.set(pageId, { ...run, operations: [...run.operations, operation] });
    }
    for (const [pageId, { session, operations }] of runs) await sendContentGraphChange(session, pageId, operations);
  }
  return {
    shownProperties, draftEditing, relationCreation, draftRelationCreation, addProperty, commit, deleteRemoved, draftTabEnvironment,
    remove: (propertyId: string) => setRemoving((current) => [...current, propertyId]),
    changed: pending.length > 0 || removing.length > 0 || relations.length > 0,
    pending,
  };
}

/**
 * A two-way relation's other side while its relation waits: on the related
 * source, after its properties, named as the relation's settings ask or as
 * the server names it then — this database's name (database_relation_pairs.go).
 */
function relatedStandIn(relation: GraphProperty, dataSourceId: string, sourceName: string,
  properties: readonly GraphProperty[]): GraphProperty[] {
  const { relatedPropertyId: id, relationDatabaseId: target, reciprocalName } = relation.config;
  if (typeof id !== "string" || typeof target !== "string" || properties.some((property) => property.id === id)) return [];
  const last = properties.filter((property) => property.dataSourceId === target).sort(compareGraphOrder).at(-1);
  return [{
    id, dataSourceId: target, type: "relation", position: graphPositionBetween(last?.position, undefined),
    name: typeof reciprocalName === "string" && reciprocalName.trim() ? reciprocalName.trim() : sourceName || "Related",
    config: { relationDatabaseId: dataSourceId, relatedPropertyId: relation.id },
  }];
}

/** The properties with the draft's changes: each changed one in its place, a new one after. */
function withChanges(properties: readonly GraphProperty[], changed: readonly GraphProperty[]): readonly GraphProperty[] {
  if (changed.length === 0) return properties;
  const byId = new Map(changed.map((property) => [property.id, property]));
  const known = new Set(properties.map((property) => property.id));
  return [...properties.map((property) => byId.get(property.id) ?? property), ...changed.filter((property) => !known.has(property.id))];
}

// The draft with a stand-in's ID turned to the property made for it.
function renamedId<T>(value: T, from: string, to: string): T {
  return JSON.parse(JSON.stringify(value).split(JSON.stringify(from).slice(1, -1)).join(JSON.stringify(to).slice(1, -1))) as T;
}
