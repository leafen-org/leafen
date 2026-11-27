import { useEffect, useMemo, useRef } from "react";
import { Copy, Plus, RepeatIcon, Trash, X } from "@/icons/leafen";
import type {
  DbProperty,
  FilterGroup,
  FilterOperator,
  RelativeDateFilterValue,
  ViewFilter,
} from "@/lib/types";
import { isRelativeDateFilterValue } from "@/shared/database/queryRelativeDate.mjs";
import { DatabaseFilterValueInput } from "./DatabaseFilterValueInput";
import { NotionSelect, type NotionSelectOption } from "./NotionSelect";
import { PropertyIcon } from "./PropertyIcon";
import { NO_VALUE_FILTERS } from "./databaseFilterLabels";
import { databaseViewLabels } from "./databaseViewLabels";
import { dateFilterOperatorChange, isDateFilterProperty } from "./DateFilterEditor";
import {
  dateFilterRelativeValue,
  defaultOperator,
  defaultValue,
  effectiveFilterOperator,
  filterOperatorLabel,
  filterOperatorsFor,
  filterPropertyForComparison,
} from "./databaseFilterPolicy";
import {
  createDatabaseFilterSingleFlight,
  databaseFilterReadContext,
  databaseFilterReadRequest,
  type DatabaseFilterReadBoundary,
  type DatabaseFilterSnapshot,
} from "./databaseFilterSnapshot";
import styles from "./databaseSheet";
import filterStyles from "./filterGroups.module.css";
import { disambiguatePropertyLabels } from "./propertyOptionLabels";
import { FilterActionMenu, type FilterAction } from "./FilterActionMenu";
import {
  FILTER_GROUP_MAX_DEPTH, filterGroupDepth, filterGroupTerms, withFilterAsGroup, withFilterDuplicated, withGroupAsFilter, withGroupDuplicated,
  withGroupWrapped,
} from "./filterGroupTerms";

export { defaultOperator, defaultValue } from "./databaseFilterPolicy";
export { FilterRow as DatabaseFilterRow };
export type {
  DatabaseFilterReadBoundary,
  DatabaseFilterSnapshot,
} from "./databaseFilterSnapshot";

export const DEFAULT_RELATIVE_DATE_FILTER_VALUE: RelativeDateFilterValue = {
  direction: "past",
  count: 1,
  unit: "week",
};

/**
 * What a filter's new operator changes: a date's keeps its part and value as
 * it can, a rule's (`rule`) day starting "오늘" (dateFilterOperatorChange);
 * "오늘 기준" starts a week back; leaving it, the value starts anew.
 */
export function filterOperatorChange(filter: ViewFilter, prop: DbProperty, next: FilterOperator, rule = false): Partial<ViewFilter> {
  if (isDateFilterProperty(prop)) return dateFilterOperatorChange(filter, next, rule);
  const operator = effectiveFilterOperator(prop, filter.operator);
  return {
    operator: next,
    ...(next === "relative_to_today"
      ? { value: isRelativeDateFilterValue(filter.value) ? filter.value : DEFAULT_RELATIVE_DATE_FILTER_VALUE }
      : operator === "relative_to_today" && !NO_VALUE_FILTERS.has(next) ? { value: undefined } : {}),
  };
}

type FilterGroupEditorProps = {
  group: FilterGroup;
  path: number[];
  props: DbProperty[];
  snapshot: DatabaseFilterSnapshot;
  read?: DatabaseFilterReadBoundary;
  onSetConjunction: (path: number[], next: "and" | "or") => void;
  onUpdateFilter: (path: number[], index: number, patch: Partial<ViewFilter>) => void;
  onRemoveFilter: (path: number[], index: number) => void;
  onRemoveGroup: (parentPath: number[], index: number) => void;
  onAddFilter: (path: number[]) => void;
  onAddGroup: (path: number[]) => void;
  /**
   * Changes the group at a path as a whole: with it, each rule and group has
   * Notion's "⋯" — 제거, 복제, 그룹으로 전환, a group's 필터로 전환 and 상위 그룹
   * 생성 (seen 2026-10-23) — as the phone's sheet does; without it, a "×".
   */
  onEdit?: (path: number[], change: (group: FilterGroup) => FilterGroup) => void;
  propertyOptions?: NotionSelectOption[];
};

export function databasePropertyOptions(props: DbProperty[]): NotionSelectOption[] {
  const labels = disambiguatePropertyLabels(props, databaseViewLabels().untitled);
  return props.map((property, index) => ({
    value: property.id,
    label: labels[index],
    icon: <PropertyIcon property={property} size="small" />,
  }));
}

export function FilterGroupEditor(props: FilterGroupEditorProps) {
  const {
    group,
    props: dbProps,
    snapshot,
    read,
    propertyOptions,
  } = props;
  const request = useMemo(
    () => databaseFilterReadRequest(group, dbProps, snapshot),
    [dbProps, group, snapshot],
  );
  const singleFlight = useRef(createDatabaseFilterSingleFlight()).current;

  useEffect(() => {
    if (!read || read.signal.aborted) return;
    if (request.schemaDatabaseIds.length === 0 && request.rowDatabaseIds.length === 0) return;
    const context = databaseFilterReadContext(
      read.contextIdentity,
      read.requestGeneration,
      snapshot,
      read.signal,
    );
    if (!read.isCurrent(context)) return;
    const key = JSON.stringify({ context, signal: undefined, request });
    void singleFlight(key, () => read.ensureDatabases(context, request)).catch((error) => {
      if (!context.signal.aborted && read.isCurrent(context)) read.onError(error);
    });
  }, [read, request, singleFlight, snapshot]);

  return (
    <FilterGroupEditorNode
      {...props}
      propertyOptions={propertyOptions ?? databasePropertyOptions(dbProps)}
    />
  );
}

function FilterGroupEditorNode({
  group,
  path,
  props: dbProps,
  snapshot,
  onSetConjunction,
  onUpdateFilter,
  onRemoveFilter,
  onRemoveGroup,
  onAddFilter,
  onAddGroup,
  onEdit,
  propertyOptions = databasePropertyOptions(dbProps),
}: FilterGroupEditorProps) {
  const labels = databaseViewLabels();
  const edit = onEdit ? (change: (group: FilterGroup) => FilterGroup) => onEdit(path, change) : undefined;
  const conjunction = group.conjunction === "or" ? "or" : "and";
  // Groups go as deep as the advanced filter's and no deeper (FILTER_GROUP_MAX_DEPTH): a group, a rule turned into
  // one or a group wrapped in one only where it fits. A group stored deeper still shows, and colors, as it was made.
  const depth = path.length;

  function connector(termIndex: number) {
    if (termIndex === 0) return null;
    return (
      <div className={filterStyles.conjunctionRow}>
        {termIndex === 1 ? (
          <div className={filterStyles.conjunctionToggle}>
            {(["and", "or"] as const).map((candidate) => (
              <button
                key={candidate}
                type="button"
                data-active={conjunction === candidate ? "true" : undefined}
                onClick={() => onSetConjunction(path, candidate)}
              >
                {candidate === "and" ? databaseViewLabels().and : databaseViewLabels().or}
              </button>
            ))}
          </div>
        ) : (
          <span className={filterStyles.conjunctionLabel}>
            {conjunction === "and" ? databaseViewLabels().and : databaseViewLabels().or}
          </span>
        )}
      </div>
    );
  }

  // Rules and groups in the order they show, a rule turned into a group in its place (filterGroupTerms).
  return (
    <div className={filterStyles.group}>
      {filterGroupTerms(group).map((term, termIndex) => term.kind === "filter" ? (
        <div key={`leaf-${term.index}`}>
          {connector(termIndex)}
          <FilterRow
            filter={term.filter}
            props={dbProps}
            snapshot={snapshot}
            propertyOptions={propertyOptions}
            onChange={(patch) => onUpdateFilter(path, term.index, patch)}
            onRemove={() => onRemoveFilter(path, term.index)}
            actions={edit && [
              { id: "remove", label: labels.sheetRemove, icon: <Trash size="regular" />, danger: true, run: () => onRemoveFilter(path, term.index) },
              { id: "duplicate", label: labels.duplicate, icon: <Copy size="regular" />, run: () => edit((current) => withFilterDuplicated(current, term.index)) },
              ...(depth < FILTER_GROUP_MAX_DEPTH ? [{ id: "toGroup", label: labels.sheetTurnIntoGroup, icon: <RepeatIcon size="regular" />,
                run: () => edit((current) => withFilterAsGroup(current, term.index)) }] : []),
            ]}
          />
        </div>
      ) : (
          <div key={`group-${term.index}`}>
            {connector(termIndex)}
            <div className={filterStyles.subgroup} data-filter-group={term.index}>
              <div className={filterStyles.subgroupHead}>
                <span className={filterStyles.conjunctionLabel}>{labels.filterGroup}</span>
                {edit ? (
                  <FilterActionMenu label={labels.filterActions} className={filterStyles.groupRemove} actions={[
                    { id: "remove", label: labels.sheetRemove, icon: <Trash size="regular" />, danger: true, run: () => onRemoveGroup(path, term.index) },
                    { id: "duplicate", label: labels.duplicate, icon: <Copy size="regular" />, run: () => edit((current) => withGroupDuplicated(current, term.index)) },
                    ...(term.group.filters.length === 1 && !(term.group.groups ?? []).length ? [{ id: "toFilter", label: labels.sheetTurnIntoFilter,
                      icon: <RepeatIcon size="regular" />, run: () => edit((current) => withGroupAsFilter(current, term.index)) }] : []),
                    ...(depth + 2 + filterGroupDepth(term.group) <= FILTER_GROUP_MAX_DEPTH ? [{ id: "wrap", label: labels.sheetWrapInGroup,
                      description: labels.sheetWrapInGroupHint, icon: <RepeatIcon size="regular" />, run: () => edit((current) => withGroupWrapped(current, term.index)) }] : []),
                  ]} />
                ) : (
                  <button
                    type="button"
                    className={filterStyles.groupRemove}
                    aria-label={labels.removeFilterGroup}
                    onClick={() => onRemoveGroup(path, term.index)}
                  >
                    <X size="small" />
                  </button>
                )}
              </div>
              <FilterGroupEditorNode
                group={term.group}
                path={[...path, term.index]}
                props={dbProps}
                snapshot={snapshot}
                onSetConjunction={onSetConjunction}
                onUpdateFilter={onUpdateFilter}
                onRemoveFilter={onRemoveFilter}
                onRemoveGroup={onRemoveGroup}
                onAddFilter={onAddFilter}
                onAddGroup={onAddGroup}
                onEdit={onEdit}
                propertyOptions={propertyOptions}
              />
            </div>
          </div>
      ))}
      <div className={filterStyles.addRow}>
        <button type="button" className={filterStyles.addBtn} onClick={() => onAddFilter(path)}>
          <Plus size="small" /> {databaseViewLabels().addFilter}
        </button>
        {depth < FILTER_GROUP_MAX_DEPTH ? (
          <button type="button" className={filterStyles.addBtn} onClick={() => onAddGroup(path)}>
            <Plus size="small" /> {databaseViewLabels().addFilterGroup}
          </button>
        ) : null}
      </div>
    </div>
  );
}

function FilterRow({
  filter,
  props,
  snapshot,
  propertyOptions,
  onChange,
  onRemove,
  actions,
}: {
  filter: ViewFilter;
  props: DbProperty[];
  snapshot: DatabaseFilterSnapshot;
  propertyOptions: NotionSelectOption[];
  onChange: (patch: Partial<ViewFilter>) => void;
  onRemove: () => void;
  /** Its "⋯" menu in place of the "×". */
  actions?: FilterAction[];
}) {
  const selected = props.find((candidate) => candidate.id === filter.propertyId) ?? props[0];
  if (!selected) return null;
  const prop = filterPropertyForComparison(selected, props);
  const operators = filterOperatorsFor(prop);
  const operator = effectiveFilterOperator(prop, filter.operator);
  const isRelativeDate = operator === "relative_to_today";
  const operatorOptions = operators.map((value) => ({ value, label: filterOperatorLabel(value, prop) }));

  return (
    <div
      className={`${styles.ruleRow} ${isRelativeDate ? styles.relativeDateRuleRow : ""}`}
      data-filter-row
    >
      <NotionSelect
        className={styles.ruleProperty}
        ariaLabel={databaseViewLabels().filterProperty}
        value={prop.id}
        options={propertyOptions}
        onChange={(value) => {
          const nextProp = filterPropertyForComparison(props.find((candidate) => candidate.id === value) ?? selected, props);
          onChange({
            propertyId: nextProp.id,
            operator: defaultOperator(nextProp),
            value: defaultValue(nextProp),
          });
        }}
      />
      <NotionSelect
        className={styles.ruleCondition}
        ariaLabel={databaseViewLabels().filterCondition}
        value={operator}
        options={operatorOptions}
        onChange={(value) => onChange(filterOperatorChange(filter, prop, value as FilterOperator))}
      />
      {!NO_VALUE_FILTERS.has(operator) ? (
        <DatabaseFilterValueInput
          prop={prop}
          operator={operator}
          value={isRelativeDate && isDateFilterProperty(prop) ? dateFilterRelativeValue(filter) : filter.value}
          databaseProperties={props}
          snapshot={snapshot}
          onChange={(value) => onChange({ operator, value })}
        />
      ) : (
        <span className={styles.ruleSpacer} />
      )}
      {actions ? (
        <FilterActionMenu label={databaseViewLabels().filterActions} className={styles.ruleRemove} actions={actions} />
      ) : (
        <button
          type="button"
          className={styles.ruleRemove}
          aria-label={databaseViewLabels().removeFilter}
          onClick={onRemove}
        >
          <X size="small" />
        </button>
      )}
    </div>
  );
}
