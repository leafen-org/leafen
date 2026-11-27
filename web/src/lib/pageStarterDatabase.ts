import { createGraphProperty, createInlineDatabase, type GraphOperation, type GraphView, type GraphProperty } from "@/contentGraph";
import { i18next } from "@/i18n";
import { createDefaultFormViewConfig } from "@/lib/formViewProjection";
import type { DbProperty, PropertyConfig } from "@/lib/types";
import { newId } from "@/lib/ids";
import { viewWrapsAtFirst } from "@/lib/viewFirstWrap";

/** A new form starts with Notion's two questions and response metadata. */
export function createPageStarterForm(pageId: string, title: string) {
  const label = (key: string, count?: number) => i18next.t(`editor:starter.formDefaults.${key}`, { count });
  const created = createInlineDatabase([], title, pageId, { name: label("question", 1) },
    { type: "form", name: label("builder") });
  const properties: GraphProperty[] = [...created.properties];
  const operations: GraphOperation[] = [...created.operations];
  // Its responses read question 1, respondent, submission time, then the
  // second question, with questions added later after them, as the form
  // Notion starts on an empty page (seen 2026-11-08).
  for (const [type, name, config] of [
    ["created_by", label("respondent"), {}],
    ["created_time", label("submittedAt"), {}],
    ["multi_select", label("question", 2), { options: ["blue", "yellow", "red"].map((color, index) => ({
      id: newId(), name: label("option", index + 1), color,
    })) }],
  ] as const) {
    const next = createGraphProperty(properties, created.dataSource.id, type, name, config);
    properties.push(next.property);
    operations.push(...next.operations);
  }
  const form = createDefaultFormViewConfig(properties.map((property, position): DbProperty => ({
    id: property.id, databaseId: property.dataSourceId, type: property.type, name: property.name,
    config: property.config as PropertyConfig, position,
  })));
  // Notion's starter choice question permits one answer even though the
  // response column is multi-select; later edits keep their saved limits.
  for (const question of form.questions) {
    if (properties.find(property => property.id === question.propertyId)?.type === "multi_select") question.maxSelections = 1;
  }
  // A new form is open to the workspace's members, as Notion's starter says (seen 2026-11-05).
  const first: GraphView = { ...created.views[0], config: { leafenForm: form, leafenFormAudience: "workspace" } };
  // Its responses table wraps its content at first, as Notion's form (seen 2026-11-05).
  const responses = { ...created.views[0], id: newId(), type: "table" as const, name: label("responses"),
    position: "a1", config: viewWrapsAtFirst("table") ? { wrap: true } : {} };
  return { formViewId: first.id, operations: operations.map(operation => operation.kind === "upsertView"
    ? { kind: "upsertView" as const, view: first } : operation).concat({ kind: "upsertView", view: responses }) };
}
