import { filterAwaitsValue, filterMatches } from "./queryFilterPolicy";
import type {
  QueryAdapters,
  QueryFilter,
  QueryFilterGroup,
  QueryPage,
  QueryProperty,
  QueryViewConfig,
} from "./queryTypes";

export function matchesFilterGroup(
  row: QueryPage,
  group: QueryFilterGroup,
  adapters: QueryAdapters,
  byId: Map<string, QueryProperty>,
): boolean {
  const results = new Map<QueryFilterGroup, boolean>();
  const stack: Array<{ group: QueryFilterGroup; visited: boolean }> = [
    { group, visited: false },
  ];
  while (stack.length > 0) {
    const current = stack.pop()!;
    if (!current.visited) {
      stack.push({ group: current.group, visited: true });
      const children = current.group.groups ?? [];
      for (let index = children.length - 1; index >= 0; index -= 1) {
        stack.push({ group: children[index], visited: false });
      }
      continue;
    }
    const terms: boolean[] = [];
    for (const filter of current.group.filters) {
      const property = byId.get(filter.propertyId);
      if (property) terms.push(filterMatches(row, property, filter, adapters));
    }
    for (const child of current.group.groups ?? []) terms.push(results.get(child) ?? true);
    results.set(
      current.group,
      terms.length === 0
        ? true
        : current.group.conjunction === "or"
          ? terms.some(Boolean)
          : terms.every(Boolean),
    );
  }
  return results.get(group) ?? true;
}

function knownFilterTerm(
  term: QueryFilter | QueryFilterGroup,
  byId: Map<string, QueryProperty>,
) {
  const stack: Array<QueryFilter | QueryFilterGroup> = [term];
  while (stack.length > 0) {
    const current = stack.pop()!;
    if (!("conjunction" in current)) {
      if (byId.has(current.propertyId)) return true;
      continue;
    }
    stack.push(...(current.filters ?? []), ...(current.groups ?? []));
  }
  return false;
}

function filterGroupFromTerms(
  terms: Array<QueryFilter | QueryFilterGroup>,
): QueryFilterGroup | undefined {
  const filters: QueryFilter[] = [];
  const groups: QueryFilterGroup[] = [];
  for (const term of terms) {
    if ("conjunction" in term) groups.push(term);
    else filters.push(term);
  }
  return filters.length || groups.length
    ? { conjunction: "and", filters, groups }
    : undefined;
}

function filterGroupHasTerms(
  group: QueryFilterGroup | undefined,
): group is QueryFilterGroup {
  return !!group && (group.filters.length > 0 || (group.groups?.length ?? 0) > 0);
}

/**
 * The group without the filters still waiting for a value, and without the
 * groups that leaves empty: such a filter doesn't filter yet, and such a
 * conditional color condition colors no page yet (conditionalColorModel.ts).
 */
export function withoutAwaitingFilters(group: QueryFilterGroup): QueryFilterGroup | undefined {
  const filters = group.filters.filter((filter) => !filterAwaitsValue(filter));
  const groups = (group.groups ?? []).flatMap((child) => withoutAwaitingFilters(child) ?? []);
  return filters.length || groups.length ? { ...group, filters, groups } : undefined;
}

export function effectiveFilterGroup(
  config: QueryViewConfig | undefined,
  byId?: Map<string, QueryProperty>,
): QueryFilterGroup | undefined {
  const groups: QueryFilterGroup[] = [];
  const terms: Array<QueryFilter | QueryFilterGroup> = [];
  const storedFilterGroup = filterGroupHasTerms(config?.filterGroup)
    ? withoutAwaitingFilters(config.filterGroup)
    : undefined;
  if (storedFilterGroup) groups.push(storedFilterGroup);
  if (!filterGroupHasTerms(config?.filterGroup) && Array.isArray(config?.filters) && config.filters.length > 0) {
    const legacy = withoutAwaitingFilters({
      conjunction: config.filterConjunction === "or" ? "or" : "and",
      filters: config.filters,
      groups: [],
    });
    if (legacy) groups.push(legacy);
  }
  for (const term of config?.quickFilters ?? []) {
    if (byId && !knownFilterTerm(term, byId)) continue;
    const effective = "conjunction" in term ? withoutAwaitingFilters(term) : filterAwaitsValue(term) ? undefined : term;
    if (effective) terms.push(effective);
  }
  const quickGroup = filterGroupFromTerms(terms);
  if (quickGroup) groups.push(quickGroup);
  if (groups.length === 0) return undefined;
  if (groups.length === 1) return groups[0];
  return { conjunction: "and", filters: [], groups };
}
