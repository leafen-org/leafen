import { useState } from "react";
import { Copy, Plus, RepeatIcon, StackPlusIcon, Trash } from "@/icons/leafen";
import type { DbProperty, FilterGroup, ViewFilter } from "@/lib/types";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetHeadButton, PhoneSheetRow, phoneSheetStyles as sheetStyles } from "../shell/PhoneSheet";
import { PhoneChoiceSheet } from "./DatabasePhonePickers";
import { NO_VALUE_FILTERS } from "./databaseFilterLabels";
import { defaultOperator, defaultValue, effectiveFilterOperator, filterOperatorLabel, filterPropertyForComparison } from "./databaseFilterPolicy";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";
import { filterValueText } from "./databaseFilterValueText";
import { databaseViewLabels } from "./databaseViewLabels";
import { filterGroupAt, filterRuleCount, updateFilterGroupAt } from "./databaseToolbarModel";
import {
  FILTER_GROUP_MAX_DEPTH, filterGroupDepth,
  withFilterAdded, withFilterAsGroup, withFilterDuplicated, withFilterRemoved, withGroupAdded, withGroupAsFilter,
  withGroupDuplicated, withGroupRemoved, withGroupWrapped,
} from "./filterGroupTerms";
import { shownFilterGroupTerms } from "./rowTimestampFilters";
import { PhoneFilterRuleRows } from "./PhoneFilterRuleRows";
import styles from "./DatabasePhoneAdvancedFilter.module.css";

type Picking = { kind: "group"; index: number } | { kind: "conjunction" } | null;

function firstRule(property: DbProperty): ViewFilter {
  return { propertyId: property.id, operator: defaultOperator(property), value: defaultValue(property) };
}

/** A group's rules in a line, as Notion's group row ('Name 값을 포함하는 데이터 "ab"'). */
function groupSummary(group: FilterGroup, properties: DbProperty[], join: string): string {
  return shownFilterGroupTerms(group).map((term) => {
    if (term.kind === "group") return `(${groupSummary(term.group, properties, join)})`;
    const selected = properties.find(({ id }) => id === term.filter.propertyId);
    if (!selected) return "";
    const property = filterPropertyForComparison(selected, properties);
    const operator = effectiveFilterOperator(property, term.filter.operator);
    const value = NO_VALUE_FILTERS.has(operator) ? "" : filterValueText(term.filter.value, property);
    return [selected.name, filterOperatorLabel(operator, property), value ? `"${value}"` : ""].filter(Boolean).join(" ");
  }).filter(Boolean).join(` ${join} `);
}

/**
 * A view's advanced filter on a phone, as Notion's "고급 필터" screen (seen
 * 2026-10-21 at 375px): each rule under its grey "필터 1" in a white group —
 * its property ("Aa Name ›", picked on a screen of properties), its condition
 * ("값을 포함하는 데이터 ›", picked in a half sheet), its value (typed in the
 * row with "편집", or "옵션 선택 ›" on a screen of options), red "제거",
 * "복제" and "그룹으로 전환"; each group under "필터 그룹 2" as a line of its
 * rules and their count ("›" opens it on a screen of its own with
 * "뒤로가기"), "제거", "복제", "필터로 전환" for a group of one rule and
 * "상위 그룹 생성"; "및" or "또는" between them in a group of its own, chosen
 * in a half sheet; then "+ 필터 규칙 추가" and "필터 그룹 추가", and on the
 * first screen "필터 제거". A group's rules come before its groups, as the
 * desktop's.
 */
export function PhoneAdvancedFilter({ root, path = [], properties, snapshot, onCommit, onRemoveAll }: {
  root: FilterGroup;
  path?: number[];
  properties: DbProperty[];
  snapshot: DatabaseFilterSnapshot;
  onCommit(next: FilterGroup): void;
  /** "필터 제거", on the first screen only. */
  onRemoveAll?(): void;
}) {
  const labels = databaseViewLabels();
  const [picking, setPicking] = useState<Picking>(null);
  const group = filterGroupAt(root, path);
  if (!group) return null;
  const edit = (change: (group: FilterGroup) => FilterGroup) => onCommit(updateFilterGroupAt(root, path, change));
  const groups = group.groups ?? [];
  const conjunction = group.conjunction === "or" ? "or" : "and";
  const joinWord = conjunction === "or" ? labels.sheetOr : labels.sheetAnd;
  const updateRule = (index: number, patch: Partial<ViewFilter>) =>
    edit((current) => ({ ...current, filters: current.filters.map((filter, at) => at === index ? { ...filter, ...patch } : filter) }));
  const close = () => setPicking(null);
  // Groups go two levels under the advanced filter, as on the desktop (FILTER_GROUP_MAX_DEPTH).
  const depth = path.length;

  // Each rule or group is named by where it stands ("필터 그룹 1" then "필터 2"), as Notion's.
  const rule = (filter: ViewFilter, index: number, place: number) => (
    <PhoneSheetGroup key={`rule-${index}`} label={labels.sheetFilterNumber(place + 1)}>
      <PhoneFilterRuleRows filter={filter} properties={properties} snapshot={snapshot} onChange={(patch) => updateRule(index, patch)} />
      <PhoneSheetRow icon={<Trash size="regular" />} label={labels.sheetRemove} danger more={false} data={{ "data-filter-rule-remove": "" }}
        onPress={() => edit((current) => withFilterRemoved(current, index))} />
      <PhoneSheetRow icon={<Copy size="regular" />} label={labels.duplicate} more={false} data={{ "data-filter-rule-duplicate": "" }}
        onPress={() => edit((current) => withFilterDuplicated(current, index))} />
      {depth < FILTER_GROUP_MAX_DEPTH ? (
        <PhoneSheetRow icon={<RepeatIcon size="regular" />} label={labels.sheetTurnIntoGroup} more={false} data={{ "data-filter-rule-to-group": "" }}
          onPress={() => edit((current) => withFilterAsGroup(current, index))} />
      ) : null}
    </PhoneSheetGroup>
  );

  const subgroup = (child: FilterGroup, index: number, place: number) => {
    const single = child.filters.length === 1 && !(child.groups ?? []).length;
    return (
      <PhoneSheetGroup key={`group-${index}`} label={labels.sheetFilterGroupNumber(place + 1)}>
        <PhoneSheetRow label={groupSummary(child, properties, child.conjunction === "or" ? labels.sheetOr : labels.sheetAnd)}
          value={<span className={styles.count}>{filterRuleCount(child)}</span>} onPress={() => setPicking({ kind: "group", index })}
          data={{ "data-filter-group-open": String(index) }} />
        <PhoneSheetRow icon={<Trash size="regular" />} label={labels.sheetRemove} danger more={false} data={{ "data-filter-group-remove": "" }}
          onPress={() => edit((current) => withGroupRemoved(current, index))} />
        <PhoneSheetRow icon={<Copy size="regular" />} label={labels.duplicate} more={false} data={{ "data-filter-group-duplicate": "" }}
          onPress={() => edit((current) => withGroupDuplicated(current, index))} />
        {single ? (
          <PhoneSheetRow icon={<RepeatIcon size="regular" />} label={labels.sheetTurnIntoFilter} more={false} data={{ "data-filter-group-to-rule": "" }}
            onPress={() => edit((current) => withGroupAsFilter(current, index))} />
        ) : null}
        {depth + 2 + filterGroupDepth(child) <= FILTER_GROUP_MAX_DEPTH ? (
          <PhoneSheetRow icon={<RepeatIcon size="regular" />} label={labels.sheetWrapInGroup} description={labels.sheetWrapInGroupHint} more={false}
            data={{ "data-filter-group-wrap": "" }}
            onPress={() => edit((current) => withGroupWrapped(current, index))} />
        ) : null}
      </PhoneSheetGroup>
    );
  };

  const ordered = shownFilterGroupTerms(group);
  const terms = ordered.map((term, place) => term.kind === "filter" ? rule(term.filter, term.index, place) : subgroup(term.group, term.index, place));
  const picked = picking?.kind === "group" ? picking : null;
  return (
    <div className={sheetStyles.stack} data-phone-advanced-filter={path.join(".")}>
      {terms.flatMap((term, at) => at === 0 ? [term] : [
        <PhoneSheetGroup key={`join-${at}`}>
          <PhoneSheetRow indent label={joinWord} onPress={() => setPicking({ kind: "conjunction" })} data={{ "data-filter-conjunction": conjunction }} />
        </PhoneSheetGroup>,
        term,
      ])}
      <PhoneSheetGroup>
        <PhoneSheetRow icon={<Plus size="regular" />} label={labels.sheetAddFilterRule} more={false} data={{ "data-filter-add-rule": "" }}
          onPress={() => { const property = properties[0]; if (property) edit((current) => withFilterAdded(current, firstRule(property))); }} />
        {/* A new group starts with a copy of the last rule, or a first rule, as the desktop's. */}
        {depth < FILTER_GROUP_MAX_DEPTH ? (
          <PhoneSheetRow icon={<StackPlusIcon size="regular" />} label={labels.addFilterGroup} description={labels.sheetFilterGroupHint} more={false}
            data={{ "data-filter-add-group": "" }}
            onPress={() => edit((current) => {
              const copied = current.filters.at(-1), property = properties[0];
              const first = copied ? { ...copied } : property ? firstRule(property) : undefined;
              return withGroupAdded(current, { conjunction: "and", filters: first ? [first] : [] });
            })} />
        ) : null}
      </PhoneSheetGroup>
      {onRemoveAll ? (
        <PhoneSheetGroup>
          <PhoneSheetRow icon={<Trash size="regular" />} label={labels.removeFilter} more={false} onPress={onRemoveAll} data={{ "data-filter-remove-all": "" }} />
        </PhoneSheetGroup>
      ) : null}
      {picking?.kind === "conjunction" ? (
        <PhoneChoiceSheet title={labels.sheetConjunction} onClose={close} data={{ "data-filter-conjunctions": "" }}
          choices={[{ key: "and", label: labels.sheetAnd, description: labels.sheetAndHint }, { key: "or", label: labels.sheetOr, description: labels.sheetOrHint }]}
          onPick={(key) => edit((current) => ({ ...current, conjunction: key }))} />
      ) : null}
      {picked && groups[picked.index] ? (
        <PhoneSheet size="screen" title={labels.sheetFilterGroupNumber(ordered.findIndex((term) => term.kind === "group" && term.index === picked.index) + 1)} onClose={close}
          start={<PhoneSheetHeadButton label={labels.sheetBack} onPress={close} />} data={{ "data-filter-group-screen": "" }}>
          <PhoneAdvancedFilter root={root} path={[...path, picked.index]} properties={properties} snapshot={snapshot} onCommit={onCommit} />
        </PhoneSheet>
      ) : null}
    </div>
  );
}
