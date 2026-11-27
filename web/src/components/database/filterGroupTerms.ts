import type { FilterGroup, FilterGroupTerm, ViewFilter } from "@/lib/types";

/**
 * How deep an advanced filter's groups go: a group in a group and no deeper,
 * as Notion's (seen 2026-11-03, and 2026-11-19 in Chrome: the third level's
 * "필터 규칙 추가" has no group to add and its rules' "⋯" no "그룹으로 전환"),
 * on the desktop and the phone alike; the server's maxDatabaseQueryFilterDepth
 * counts the advanced filter's own as the first of those three levels. A
 * conditional color's conditions go as deep, in its card (FilterGroupEditor)
 * and its phone sheet alike — Notion's card holds one condition and adds no
 * group at all (seen 2026-11-19 in Chrome), so its groups are the advanced
 * filter's.
 */
export const FILTER_GROUP_MAX_DEPTH = 2;

/** How many levels of groups a group holds under it. */
export function filterGroupDepth(group: FilterGroup): number {
  return (group.groups ?? []).reduce((deepest, child) => Math.max(deepest, 1 + filterGroupDepth(child)), 0);
}

/**
 * A filter group's rules and groups in the order they show, as Notion's: a
 * rule turned into a group stays in its place (seen 2026-10-21). The group
 * keeps that order in `order`, the n-th "filter" being `filters[n]` and the
 * n-th "group" `groups[n]`; without it, or where it no longer counts them
 * all, the rules come first and what is left follows. And and or don't
 * depend on it, so querying reads `filters` and `groups` as they are.
 */
export type FilterTerm =
  | { kind: "filter"; filter: ViewFilter; index: number }
  | { kind: "group"; group: FilterGroup; index: number };

export function filterGroupTerms(group: FilterGroup): FilterTerm[] {
  const filters = group.filters, groups = group.groups ?? [];
  const terms: FilterTerm[] = [];
  let nextFilter = 0, nextGroup = 0;
  for (const token of group.order ?? []) {
    if (token === "filter" && nextFilter < filters.length) {
      terms.push({ kind: "filter", filter: filters[nextFilter], index: nextFilter });
      nextFilter += 1;
    } else if (token === "group" && nextGroup < groups.length) {
      terms.push({ kind: "group", group: groups[nextGroup], index: nextGroup });
      nextGroup += 1;
    }
  }
  for (; nextFilter < filters.length; nextFilter += 1) terms.push({ kind: "filter", filter: filters[nextFilter], index: nextFilter });
  for (; nextGroup < groups.length; nextGroup += 1) terms.push({ kind: "group", group: groups[nextGroup], index: nextGroup });
  return terms;
}

type Term = { kind: "filter"; filter: ViewFilter } | { kind: "group"; group: FilterGroup };

/** A group holding these terms in this order; `order` only where it isn't rules first. */
function withTerms(group: FilterGroup, terms: Term[]): FilterGroup {
  const filters: ViewFilter[] = [], groups: FilterGroup[] = [], order: FilterGroupTerm[] = [];
  for (const term of terms) {
    order.push(term.kind);
    if (term.kind === "filter") filters.push(term.filter);
    else groups.push(term.group);
  }
  const plain = order.every((token, at) => token === (at < filters.length ? "filter" : "group"));
  const { order: _old, ...rest } = group;
  return plain ? { ...rest, filters, groups } : { ...rest, filters, groups, order };
}

function termsOf(group: FilterGroup): Term[] {
  return filterGroupTerms(group).map((term) => term.kind === "filter"
    ? { kind: "filter", filter: term.filter } : { kind: "group", group: term.group });
}

/** Where the n-th rule or group stands among the terms. */
function placeOf(group: FilterGroup, kind: FilterTerm["kind"], index: number): number {
  return filterGroupTerms(group).findIndex((term) => term.kind === kind && term.index === index);
}

function changeAt(group: FilterGroup, kind: FilterTerm["kind"], index: number, replace: (term: Term) => Term[]): FilterGroup {
  const at = placeOf(group, kind, index);
  if (at < 0) return group;
  const terms = termsOf(group);
  return withTerms(group, [...terms.slice(0, at), ...replace(terms[at]), ...terms.slice(at + 1)]);
}

/** A rule added at the end. */
export function withFilterAdded(group: FilterGroup, filter: ViewFilter): FilterGroup {
  return withTerms(group, [...termsOf(group), { kind: "filter", filter }]);
}

/** A group added at the end. */
export function withGroupAdded(group: FilterGroup, child: FilterGroup): FilterGroup {
  return withTerms(group, [...termsOf(group), { kind: "group", group: child }]);
}

export function withFilterRemoved(group: FilterGroup, index: number): FilterGroup {
  return changeAt(group, "filter", index, () => []);
}

export function withGroupRemoved(group: FilterGroup, index: number): FilterGroup {
  return changeAt(group, "group", index, () => []);
}

/** A rule's copy just after it. */
export function withFilterDuplicated(group: FilterGroup, index: number): FilterGroup {
  return changeAt(group, "filter", index, (term) => term.kind === "filter" ? [term, { kind: "filter", filter: { ...term.filter } }] : [term]);
}

/** A group's copy just after it. */
export function withGroupDuplicated(group: FilterGroup, index: number): FilterGroup {
  return changeAt(group, "group", index, (term) => term.kind === "group" ? [term, { kind: "group", group: structuredClone(term.group) }] : [term]);
}

/** A rule turned into a group of it, in its place. */
export function withFilterAsGroup(group: FilterGroup, index: number): FilterGroup {
  return changeAt(group, "filter", index, (term) => term.kind === "filter"
    ? [{ kind: "group", group: { conjunction: "and", filters: [term.filter] } }] : [term]);
}

/** A group of one rule turned into that rule, in its place. */
export function withGroupAsFilter(group: FilterGroup, index: number): FilterGroup {
  return changeAt(group, "group", index, (term) => term.kind === "group" && term.group.filters.length === 1 && !(term.group.groups ?? []).length
    ? [{ kind: "filter", filter: term.group.filters[0] }] : [term]);
}

/** A group wrapped in a new group, in its place. */
export function withGroupWrapped(group: FilterGroup, index: number): FilterGroup {
  return changeAt(group, "group", index, (term) => term.kind === "group"
    ? [{ kind: "group", group: { conjunction: "and", filters: [], groups: [term.group] } }] : [term]);
}
