import { rowPagePropertyOrder } from "./rowPropertiesModel";
import { useMemo, useRef } from "react";
import type { GraphProperty, GraphRow } from "@/contentGraph";
import type { TextSpan } from "@/lib/types";
import { graphFilePropertyValue, normalizeFileAttachments } from "./files";
import { galleryPresentationPage } from "./galleryViewModel";
import type { PropertyValueReadContext } from "./PropValue";
import type { PropertyCellEnvironment } from "./propertyCellTypes";

// The properties a database page made from a template takes a value for; the
// others are its title, which the template names, or computed for the page.
// A formula and a rollup are computed from the template's own values, its
// title among them, as Notion's template shows them (seen 2026-10-30: "Dep
// points 0"; 2026-11-08: "Tmpl W71 fx" from the title "Tmpl W71").
const unsetTypes = new Set<string>([
  "title", "created_time", "last_edited_time", "created_by", "last_edited_by",
  "unique_id", "button", "location", "last_visited_time",
]);

/** A database's properties as its template's page shows them: its title and those it takes a value for. */
export function templatePageProperties(properties: readonly GraphProperty[]) {
  return properties.filter((property) => property.type === "title" || !unsetTypes.has(property.type));
}

/** The cells a database template's property values edit with, and the values they read. */
export interface DatabaseTemplatePropertyCells {
  environment?: PropertyCellEnvironment;
  valueContext: PropertyValueReadContext;
}

/**
 * The cells a database template's values edit with, and the template as the
 * page they show on: its values the template's own, a date's "오늘" and a
 * person's "나" among them, each change going to onChange.
 */
export function useDatabaseTemplateCells({ templateId, workspaceId, title, properties, values, spans, cells, readOnly, onChange }: {
  templateId: string;
  workspaceId: string;
  /** The template's title, which its formulas read as a page's. */
  title: string;
  properties: readonly GraphProperty[];
  values: Readonly<Record<string, unknown>>;
  spans: Readonly<Record<string, readonly TextSpan[]>>;
  cells: DatabaseTemplatePropertyCells;
  readOnly: boolean;
  onChange(propertyId: string, value: unknown, spans?: TextSpan[]): void;
}) {
  // In the order the pages made from it list them, as Notion's (seen 2026-11-05).
  const shown = useMemo(() => rowPagePropertyOrder(properties.filter((property) => !unsetTypes.has(property.type)).map((property) => ({ property })))
    .map(({ property }) => property), [properties]);
  const latestValues = useRef(values);
  latestValues.current = values;
  const { row, graphValues } = useMemo(() => {
    const graphRow: GraphRow = {
      id: templateId,
      dataSourceId: shown[0]?.dataSourceId ?? "",
      subitemParentId: null,
      position: "a0",
      values: Object.fromEntries(shown.flatMap((property) => property.id in values
        ? [[property.id, {
          type: property.type, value: values[property.id],
          ...(spans[property.id]?.length ? { spans: [...spans[property.id]] } : {}),
        }]]
        : [])) as GraphRow["values"],
    };
    return { row: { ...galleryPresentationPage({ row: graphRow, workspaceId, position: 0 }), title }, graphValues: graphRow.values };
  }, [shown, spans, templateId, title, values, workspaceId]);
  const environment = useMemo<PropertyCellEnvironment | undefined>(() => {
    const base = cells.environment;
    if (!base || readOnly) return undefined;
    const set = (propertyId: string, value: unknown, textSpans?: TextSpan[]) => {
      const empty = value === null || value === undefined || value === "" || (Array.isArray(value) && value.length === 0);
      onChange(propertyId, empty ? null : value, empty ? undefined : textSpans);
    };
    return {
      snapshot: { ...base.snapshot, templateValues: true },
      context: base.context,
      operations: {
        ...base.operations,
        setValue: (_rowId, property, value) => set(property.id, property.type === "files" ? graphFilePropertyValue(value) : value),
        setTextValue: (_rowId, property, value, _context, textSpans) => set(property.id, value, textSpans),
        setRelation: (_rowId, property, relatedRowIds) => set(property.id, [...relatedRowIds]),
        getCurrentFiles: (_rowId, propertyId) => normalizeFileAttachments(latestValues.current[propertyId]),
      },
    };
  }, [cells.environment, onChange, readOnly]);
  // Its values as a row page reads a row's, by property.
  return { shown, row, values: graphValues, environment };
}
