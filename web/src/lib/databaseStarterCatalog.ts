import catalog from "../../../internal/app/database_starter_templates.json";
import { i18next } from "@/i18n";
import type { GraphProperty, GraphRow, GraphView } from "@/contentGraph";

export type DatabaseStarterSelection = { templateId: string; features: string[]; views: string[] };
export type DatabaseStarterAuthor = { id: string; name: string };
/** A feature: its properties, and its own icon where Notion's chip shows one rather than its first property's type ("chart-line" for progress). */
export type DatabaseStarterFeature = { key: string; name: string; defaultOn: boolean; properties: string[]; icon?: string };
export type DatabaseStarterViewChoice = { key: string; name: string; defaultOn: boolean; id: string; retainedProperties: string[] };
export type DatabaseStarterTemplate = {
  key: string; name: string; icon: string; tone: string; description: string;
  features: DatabaseStarterFeature[]; viewChoices: DatabaseStarterViewChoice[];
  properties: GraphProperty[]; views: GraphView[]; rows: GraphRow[];
  previewPropertyIds?: string[];
};
// A hidden template (the scoped "Memory" database agents make) isn't offered
// in the add window.
export const databaseStarterTemplates = (catalog.templates as unknown as readonly (DatabaseStarterTemplate & { hidden?: boolean })[])
  .filter((template) => !template.hidden);
// The new database panel's suggestions in Notion's order, meeting notes
// fourth (seen 2026-11-11).
const SUGGESTED = ["tasks", "projects", "docs", "meetings", "brainstorm"];
const SUGGESTED_SHOWN = 3;

/**
 * The panel's suggestions as Notion's (seen 2026-11-11): the templates the
 * page already shows go to the end, then three are shown, four when meeting
 * notes is the fourth (a page holding projects shows task tracker, docs hub
 * and meeting notes; one holding projects and meeting notes shows task
 * tracker, docs hub and brainstorm). A search finds among them all and shows
 * as many.
 */
export function suggestedDatabaseStarters(used: ReadonlySet<string>, shown: (template: DatabaseStarterTemplate) => boolean) {
  const ordered = SUGGESTED.flatMap((key) => databaseStarterTemplates.find((candidate) => candidate.key === key) ?? []);
  const sorted = [...ordered.filter((template) => !used.has(template.key)), ...ordered.filter((template) => used.has(template.key))];
  return sorted.filter(shown).slice(0, SUGGESTED_SHOWN + (sorted[SUGGESTED_SHOWN]?.key === "meetings" ? 1 : 0));
}
export function databaseStarterLabel(value: string) {
  const language = i18next.resolvedLanguage?.split("-")[0] ?? "en";
  const labels = (catalog.translations as Record<string, Record<string, string>>)[language];
  return labels?.[value] ?? value;
}
export function defaultDatabaseStarterSelection(template: DatabaseStarterTemplate): DatabaseStarterSelection {
  return { templateId: template.key, features: template.features.filter(f => f.defaultOn).map(f => f.key),
    views: template.viewChoices.filter(v => v.defaultOn).map(v => v.key) };
}
/**
 * The selection a template is made with at once, with no customize window:
 * one with nothing to choose, as Notion's meeting notes is made from the new
 * database panel's suggestion and from its template gallery alike (seen
 * 2026-11-11; task tracker, projects, docs hub and brainstorm open theirs).
 * Null for a template that opens its window.
 */
export function databaseStarterMadeAtOnce(template: DatabaseStarterTemplate): DatabaseStarterSelection | null {
  return template.features.length || template.viewChoices.length > 1 ? null : defaultDatabaseStarterSelection(template);
}
export function databaseStarterProperties(template: DatabaseStarterTemplate, features: readonly string[], viewId?: string) {
  const removed = new Set(template.features.filter(f => !features.includes(f.key)).flatMap(f => f.properties));
  for (const id of template.viewChoices.find(v => v.id === viewId)?.retainedProperties ?? []) removed.delete(id);
  return template.properties.filter(p => !removed.has(p.id));
}

/** Notion's customization preview clears disabled feature examples even when
 * the current view retains the schema. Applying selected views keeps the data. */
export function databaseStarterPreviewRows(template: DatabaseStarterTemplate, features: readonly string[]) {
  const removed = new Set(template.features.filter(f => !features.includes(f.key)).flatMap(f => f.properties));
  return template.rows.map(row => ({ ...row, values: Object.fromEntries(Object.entries(row.values).filter(([id]) => !removed.has(id))) }));
}

/** Project an exported view onto the current preview schema. */
export function databaseStarterPreviewView(view: GraphView | undefined, properties: readonly GraphProperty[], template: DatabaseStarterTemplate) {
  if (!view) return view;
  const removed = new Set(template.properties.filter(p => !properties.some(visible => visible.id === p.id)).map(p => p.id));
  function prune(value: unknown): unknown {
    if (Array.isArray(value)) return value.filter(item => {
      if (typeof item === "string") return !removed.has(item);
      if (item && typeof item === "object") {
        const ref = item as { propertyId?: string; id?: string };
        return !removed.has(ref.propertyId ?? "") && !removed.has(ref.id ?? "");
      }
      return true;
    }).map(prune);
    if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).filter(([key, item]) =>
      !removed.has(key) && !(typeof item === "string" && removed.has(item))).map(([key, item]) => [key, prune(item)]));
    return value;
  }
  return { ...view, config: prune(view.config) as GraphView["config"] };
}
