import { distinctPropertyName, type GraphProperty } from "@/contentGraph";
import { rewriteFormulaPropertyReferences } from "@/components/database/formulaReferences";

// A formula means the property it names: when a data source's properties are
// renamed or reordered, the server rewrites the source's formulas and
// database-button formula values that would otherwise read another property
// or none, naming a property by its ID while another property shares its
// name. The loaded graph takes the same rewrite, so it shows the formulas the
// server now holds and later edits start from them.

type ReferenceNames = {
  first: Map<string, string>;
  count: Map<string, number>;
  names: Map<string, string>;
};

function byPosition(left: GraphProperty, right: GraphProperty) {
  if (left.position === right.position) return left.id < right.id ? -1 : left.id > right.id ? 1 : 0;
  return left.position < right.position ? -1 : 1;
}

// The server keeps a property's name without surrounding spaces.
function referenceNames(properties: readonly GraphProperty[]): ReferenceNames {
  const names: ReferenceNames = { first: new Map(), count: new Map(), names: new Map() };
  for (const property of [...properties].sort(byPosition)) {
    const name = property.name.trim();
    if (!names.first.has(name)) names.first.set(name, property.id);
    names.count.set(name, (names.count.get(name) ?? 0) + 1);
    names.names.set(property.id, name);
  }
  return names;
}

function resolve(names: ReferenceNames, reference: string) {
  return names.first.get(reference) ?? (names.names.has(reference) ? reference : undefined);
}

function canonical(names: ReferenceNames, id: string) {
  const name = names.names.get(id);
  if (name === undefined) return undefined;
  return names.count.get(name) === 1 ? name : id;
}

function replacement(authored: boolean, before: ReferenceNames, after: ReferenceNames) {
  return (reference: string) => {
    const current = resolve(after, reference);
    const target = authored && current !== undefined ? current : resolve(before, reference);
    if (target === undefined) return undefined;
    const next = canonical(after, target);
    if (next === undefined || (current === target && reference !== target)) return undefined;
    return next;
  };
}

function rewriteActionFormulas(value: unknown, replace: (reference: string) => string | undefined): unknown {
  if (Array.isArray(value)) {
    let changed = false;
    const next = value.map((item) => {
      const rewritten = rewriteActionFormulas(item, replace);
      if (rewritten !== item) changed = true;
      return rewritten;
    });
    return changed ? next : value;
  }
  if (!value || typeof value !== "object") return value;
  const record = value as Record<string, unknown>;
  let next: Record<string, unknown> | null = null;
  if (record.kind === "formula" && typeof record.expression === "string") {
    const rewritten = rewriteFormulaPropertyReferences(record.expression, replace);
    if (rewritten.replacements > 0) next = { ...record, expression: rewritten.expression };
  }
  for (const [key, child] of Object.entries(record)) {
    if (key === "expression" && next) continue;
    const rewritten = rewriteActionFormulas(child, replace);
    if (rewritten !== child) next = { ...(next ?? record), [key]: rewritten };
  }
  return next ?? value;
}

function rewrittenConfig(
  property: GraphProperty,
  previous: GraphProperty | undefined,
  before: ReferenceNames,
  after: ReferenceNames,
) {
  const keys = property.type === "formula"
    ? ["formula", "expression"]
    : property.type === "button" ? ["button"] : [];
  let config: Record<string, unknown> | null = null;
  for (const key of keys) {
    const value = property.config[key];
    if (value === undefined) continue;
    const authored = !previous || JSON.stringify(previous.config[key]) !== JSON.stringify(value);
    const replace = replacement(authored, before, after);
    if (property.type === "formula") {
      if (typeof value !== "string") continue;
      const rewritten = rewriteFormulaPropertyReferences(value, replace);
      if (rewritten.replacements > 0) config = { ...(config ?? property.config), [key]: rewritten.expression };
      continue;
    }
    const rewritten = rewriteActionFormulas(value, replace);
    if (rewritten !== value) config = { ...(config ?? property.config), [key]: rewritten };
  }
  return config;
}

/**
 * The loaded properties once `changed` is saved: each changed property takes
 * its place, a new one comes after them, and the formulas of every data
 * source whose property names or order changed follow the properties they
 * read.
 */
export function graphPropertiesAfterCommit(
  current: readonly GraphProperty[],
  changed: readonly GraphProperty[],
): GraphProperty[] {
  const saved = new Map(changed.map((property) => [property.id, property]));
  const previous = new Map(current.map((property) => [property.id, property]));
  const next = [
    ...current.map((property) => saved.get(property.id) ?? property),
    ...changed.filter((property) => !previous.has(property.id)),
  ];
  const sources = new Set<string>();
  for (const property of changed) {
    const stored = previous.get(property.id);
    if (!stored) continue;
    if (stored.name.trim() !== property.name.trim() || stored.position !== property.position || stored.dataSourceId !== property.dataSourceId) {
      sources.add(property.dataSourceId);
      sources.add(stored.dataSourceId);
    }
  }
  if (sources.size === 0) return next;
  const names = new Map([...sources].map((sourceId) => [sourceId, {
    before: referenceNames(current.filter((property) => property.dataSourceId === sourceId)),
    after: referenceNames(next.filter((property) => property.dataSourceId === sourceId)),
  }]));
  return next.map((property) => {
    const source = names.get(property.dataSourceId);
    if (!source) return property;
    const config = rewrittenConfig(property, previous.get(property.id), source.before, source.after);
    return config ? { ...property, config } : property;
  });
}

/**
 * The properties to save with their names told apart, as the server tells
 * them apart: a property made, renamed or moved with a name another property
 * of its source has takes the first free "<name> 1", "<name> 2", …, a renamed
 * property's own current name counting as taken.
 */
export function distinctGraphPropertyNames(
  current: readonly GraphProperty[],
  changed: readonly GraphProperty[],
): GraphProperty[] {
  const previous = new Map(current.map((property) => [property.id, property]));
  const moved = new Set(changed.filter((property) => {
    const stored = previous.get(property.id);
    return !stored || stored.name.trim() !== property.name.trim() || stored.dataSourceId !== property.dataSourceId;
  }).map((property) => property.id));
  const saved = new Map(changed.map((property) => [property.id, property]));
  const taken = new Map<string, Set<string>>();
  const take = (sourceId: string, name: string) => {
    const names = taken.get(sourceId) ?? new Set<string>();
    names.add(name);
    taken.set(sourceId, names);
    return names;
  };
  for (const property of [...current, ...changed.filter((candidate) => !previous.has(candidate.id))]) {
    const final = saved.get(property.id) ?? property;
    if (!moved.has(final.id)) take(final.dataSourceId, final.name.trim());
  }
  return changed.map((property) => {
    if (!moved.has(property.id)) return property;
    const names = taken.get(property.dataSourceId) ?? take(property.dataSourceId, "");
    const own = previous.get(property.id)?.name.trim();
    const name = distinctPropertyName(property.name.trim(), own && own !== property.name.trim() ? new Set([...names, own]) : names);
    names.add(name);
    return name === property.name ? property : { ...property, name };
  });
}
