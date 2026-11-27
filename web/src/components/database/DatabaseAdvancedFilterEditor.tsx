import { Fragment, useRef, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";
import { Copy, CountChevronDown, Plus, RepeatIcon, StackPlusIcon, Trash } from "@/icons/leafen";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { DbProperty, FilterGroup, FilterOperator, ViewFilter } from "@/lib/types";
import { DatabaseFilterValueInput } from "./DatabaseFilterValueInput";
import { databasePropertyOptions, filterOperatorChange } from "./DatabaseFilterEditors";
import { NotionSelect, NotionSelectCaret } from "./NotionSelect";
import { NO_VALUE_FILTERS, dateFilterLabels, rollupMatchChoices, rollupMatchPatch } from "./databaseFilterLabels";
import {
  dateFilterRelativeValue, defaultOperator, defaultValue, effectiveFilterOperator, filterOperatorLabel, filterOperatorsFor,
  filterAsksRollupMatch, filterPropertyForComparison,
} from "./databaseFilterPolicy";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";
import { filterGroupAt, toolbarListFit, updateFilterGroupAt } from "./databaseToolbarModel";
import { databaseViewLabels } from "./databaseViewLabels";
import { dateFilterShowsPart, isDateFilterProperty } from "./DateFilterEditor";
import { FilterActionMenu, type FilterAction } from "./FilterActionMenu";
import { shownFilterGroupTerms } from "./rowTimestampFilters";
import { useFilterWindowKeys } from "./useFilterWindowKeys";
import {
  FILTER_GROUP_MAX_DEPTH as MAX_GROUP_DEPTH, filterGroupDepth as groupDepth,
  withFilterAdded, withFilterAsGroup, withFilterDuplicated, withFilterRemoved, withGroupAdded, withGroupAsFilter,
  withGroupDuplicated, withGroupRemoved, withGroupWrapped,
} from "./filterGroupTerms";
import styles from "./DatabaseAdvancedFilterEditor.module.css";

/** A rule's property list, as Notion's (seen 2026-11-16, 1440 dark): 280 wide right under its button, 480 high at
 *  most and slid up to fit, unticked, each property by its icon; "속성을 검색하세요" over it, focused, from seven
 *  properties on (six had none, seven and eight had it), "결과 없음" when nothing answers. */
const RULE_PROPERTY_MENU = { width: 280, maxHeight: 480, gap: 0 };
const RULE_PROPERTY_SEARCH_FROM = 7;
/** Every choice of a rule draws Notion's thin ⌄ (arrowChevronSingleDownSmall) 14 tall and as wide as its 8.64 of ink,
 *  as Notion's join, property, date part, condition and value choices (seen 2026-11-22 at 1440 dark, dpr 2). */
const RULE_CARET = <CountChevronDown size="tiny" trim aria-hidden="true" />;

function firstRule(property: DbProperty, properties: DbProperty[]): ViewFilter {
  return { propertyId: property.id, operator: defaultOperator(property, properties), value: defaultValue(property, properties) };
}

/**
 * A view's advanced filter, as Notion's window from its "규칙 N개" pill (seen
 * 2026-11-03, 1280px): each group is a grid of a 60px column for how a term
 * joins the ones above (nothing on the first, "및 ⌄"/"또는 ⌄" on the second,
 * the same word on the rest), the property (a date's with its part, "시작일
 * ⌄"), the condition with its value, and a 32px "⋯"; a group sits over the property and condition columns in a faint
 * box with its own "+ 필터 규칙 추가". Groups go two deep. The window is as
 * wide as its rows: a typed value's field is 170px and up (552px for one text
 * rule), a chosen value a button, and each level of groups adds about 126px. "+ 필터 규칙 추가 ⌄" offers a rule or,
 * while a group can go deeper, a group; "필터 제거" under a line ends it.
 */
export function DatabaseAdvancedFilterEditor({ root, properties, snapshot, onCommit, onRemoveAll }: {
  root: FilterGroup;
  properties: DbProperty[];
  snapshot: DatabaseFilterSnapshot;
  onCommit(next: FilterGroup): void;
  onRemoveAll(): void;
}) {
  const labels = databaseViewLabels();
  return (
    <div className={styles.editor} data-advanced-filter-editor>
      <NotionSelectCaret.Provider value={RULE_CARET}>
        <GroupTerms root={root} path={[]} properties={properties} snapshot={snapshot} onCommit={onCommit} />
        <AddTerm root={root} path={[]} properties={properties} onCommit={onCommit} />
      </NotionSelectCaret.Provider>
      <div className={styles.divider} />
      <button type="button" className={styles.removeAll} data-filter-remove-all onClick={onRemoveAll}>
        <Trash size="regular" aria-hidden="true" />
        <span>{labels.removeFilter}</span>
      </button>
    </div>
  );
}

/**
 * The window the editor shows in: 4px under its "규칙 N개" pill at the pill's
 * left, over the page rather than in it and moved in to stay 12px inside the page's window, as
 * Notion's from the filter bar and from the settings' filter list (seen
 * 2026-11-03); with many rules as tall as the toolbar's lists (toolbarListFit),
 * four fifths of the window, moved up over the pill to show whole 8px over the
 * window's foot (seen 2026-11-17 from the filter bar, 14 rules: 578.4 high at
 * 137.4..715.8 in a 723 window, 418.4 at 97.4..515.8 in a 523 one, the pill
 * at 183.4..207.4 — its sort pill's the same). A press outside closes it, and the press goes on to
 * what it pressed, as Notion's has no shade (seen 2026-11-12: the settings
 * button opened its settings, a cell beside a docked settings panel its
 * editor, and the page under it answered hover all the while). It takes the
 * arrows, Enter, Escape and Tab as it opens, the focus staying where it was, and
 * Escape never closes it, as Notion's (useFilterWindowKeys).
 */
export function AdvancedFilterWindow({ anchor, label, onClose, children }: {
  anchor: RefObject<HTMLElement | null>;
  label: string;
  onClose(): void;
  children: ReactNode;
}) {
  const windowRef = useRef<HTMLDivElement>(null);
  const keys = useFilterWindowKeys(windowRef, anchor);
  const place = usePopoverPlacement(windowRef, (size, viewport) => placeAround(
    popoverRectOf(anchor.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport,
    { gap: 4, align: "start", marginY: 8, ...toolbarListFit(viewport.height) }),
  [anchor], undefined, { kind: "popover", onEscape: () => keys.escape(), onOutsidePress: onClose, inside: () => [anchor.current] });
  return createPortal(
    <div ref={windowRef} role="dialog" aria-label={label} className={styles.window} style={popoverPlacementStyle(place)}
      data-quick-advanced-filter-editor>
      {children}
    </div>,
    document.body,
  );
}

type NodeProps = {
  root: FilterGroup;
  path: number[];
  properties: DbProperty[];
  snapshot: DatabaseFilterSnapshot;
  onCommit(next: FilterGroup): void;
};

function GroupTerms({ root, path, properties, snapshot, onCommit }: NodeProps) {
  const labels = databaseViewLabels();
  const group = filterGroupAt(root, path);
  if (!group) return null;
  const depth = path.length;
  const edit = (change: (current: FilterGroup) => FilterGroup) => onCommit(updateFilterGroupAt(root, path, change));
  const conjunction = group.conjunction === "or" ? "or" : "and";
  const joinWord = conjunction === "or" ? labels.sheetOr : labels.sheetAnd;
  const join = (place: number) => place === 0 ? null : place === 1 ? (
    <NotionSelect className={styles.joinSelect} buttonClassName={styles.choice} ariaLabel={labels.sheetConjunction} value={conjunction} unmarked
      options={[{ value: "and", label: labels.sheetAnd, description: labels.sheetAndHint }, { value: "or", label: labels.sheetOr, description: labels.sheetOrHint }]}
      onChange={(next) => edit((current) => ({ ...current, conjunction: next === "or" ? "or" : "and" }))} />
  ) : <span className={styles.joinWord}>{joinWord}</span>;
  return (
    <div className={styles.grid} data-filter-group-depth={depth}>
      {shownFilterGroupTerms(group).map((term, place) => (
        <Fragment key={`${term.kind}-${term.index}`}>
          <div className={styles.join} data-filter-conjunction={place === 1 ? conjunction : undefined}>{join(place)}</div>
          {term.kind === "filter" ? (
            <RuleCells filter={term.filter} properties={properties} snapshot={snapshot}
              onChange={(patch) => edit((current) => ({ ...current, filters: current.filters.map((filter, index) => index === term.index ? { ...filter, ...patch } : filter) }))}
              actions={[
                { id: "remove", label: labels.sheetRemove, icon: <Trash size="regular" />, danger: true, run: () => edit((current) => withFilterRemoved(current, term.index)) },
                { id: "duplicate", label: labels.duplicate, icon: <Copy size="regular" />, run: () => edit((current) => withFilterDuplicated(current, term.index)) },
                ...(depth < MAX_GROUP_DEPTH ? [{ id: "toGroup", label: labels.sheetTurnIntoGroup, icon: <RepeatIcon size="regular" />,
                  run: () => edit((current) => withFilterAsGroup(current, term.index)) }] : []),
              ]} />
          ) : (
            <>
              <div className={styles.group} data-filter-group={term.index}>
                <GroupTerms root={root} path={[...path, term.index]} properties={properties} snapshot={snapshot} onCommit={onCommit} />
                <AddTerm root={root} path={[...path, term.index]} properties={properties} onCommit={onCommit} />
              </div>
              <FilterActionMenu label={labels.filterActions} className={styles.more} actions={[
                { id: "remove", label: labels.sheetRemove, icon: <Trash size="regular" />, danger: true, run: () => edit((current) => withGroupRemoved(current, term.index)) },
                { id: "duplicate", label: labels.duplicate, icon: <Copy size="regular" />, run: () => edit((current) => withGroupDuplicated(current, term.index)) },
                ...(term.group.filters.length === 1 && !(term.group.groups ?? []).length ? [{ id: "toFilter", label: labels.sheetTurnIntoFilter,
                  icon: <RepeatIcon size="regular" />, run: () => edit((current) => withGroupAsFilter(current, term.index)) }] : []),
                // Wrapped, it and what it holds go one level deeper.
                ...(depth + 2 + groupDepth(term.group) <= MAX_GROUP_DEPTH ? [{ id: "wrap", label: labels.sheetWrapInGroup,
                  description: labels.sheetWrapInGroupHint, icon: <RepeatIcon size="regular" />, run: () => edit((current) => withGroupWrapped(current, term.index)) }] : []),
              ] satisfies FilterAction[]} />
            </>
          )}
        </Fragment>
      ))}
    </div>
  );
}

/**
 * "+ 필터 규칙 추가 ⌄" under a group's terms: a rule, or a group that starts
 * with a copy of the last rule (or a first rule), where a group can go; at
 * the deepest level it adds a rule at once, without the ⌄.
 */
function AddTerm({ root, path, properties, onCommit }: Omit<NodeProps, "snapshot">) {
  const labels = databaseViewLabels();
  const property = properties[0];
  const edit = (change: (current: FilterGroup) => FilterGroup) => onCommit(updateFilterGroupAt(root, path, change));
  const addRule = () => { if (property) edit((current) => withFilterAdded(current, firstRule(property, properties))); };
  const addGroup = () => edit((current) => {
    const copied = current.filters.at(-1);
    const first = copied ? { ...copied } : property ? firstRule(property, properties) : undefined;
    return withGroupAdded(current, { conjunction: "and", filters: first ? [first] : [] });
  });
  const content: ReactNode = <>
    <span className={styles.addIcon} aria-hidden="true"><Plus size="regular" /></span>
    <span>{labels.sheetAddFilterRule}</span>
  </>;
  if (path.length >= MAX_GROUP_DEPTH) {
    return <button type="button" className={styles.add} data-filter-add-rule onClick={addRule}>{content}</button>;
  }
  return (
    <FilterActionMenu label={labels.sheetAddFilterRule} className={styles.add} data={{ "data-filter-add": "" }}
      trigger={<>{content}<CountChevronDown size="tiny" className={styles.addCaret} aria-hidden="true" /></>}
      actions={[
        { id: "rule", label: labels.sheetAddFilterRule, icon: <Plus size="regular" />, run: addRule },
        { id: "group", label: labels.addFilterGroup, description: labels.sheetFilterGroupHint, icon: <StackPlusIcon size="regular" />, run: addGroup },
      ]} />
  );
}

/** A rule's property, its condition and value, and its "⋯", each in its column. */
function RuleCells({ filter, properties, snapshot, onChange, actions }: {
  filter: ViewFilter;
  properties: DbProperty[];
  snapshot: DatabaseFilterSnapshot;
  onChange(patch: Partial<ViewFilter>): void;
  actions: FilterAction[];
}) {
  const labels = databaseViewLabels();
  const selected = properties.find((candidate) => candidate.id === filter.propertyId) ?? properties[0];
  if (!selected) return null;
  const prop = filterPropertyForComparison(selected, properties);
  const operator = effectiveFilterOperator(prop, filter.operator);
  const relative = operator === "relative_to_today";
  const rollupMatch = filterAsksRollupMatch(selected);
  const datePart = !rollupMatch && isDateFilterProperty(prop) && dateFilterShowsPart(prop, operator);
  const options = databasePropertyOptions(properties);
  const property = (
    <NotionSelect className={styles.property} buttonClassName={styles.choice} ariaLabel={labels.filterProperty} value={prop.id}
      options={options} propertyChoice unmarked menuSize={RULE_PROPERTY_MENU}
      search={options.length >= RULE_PROPERTY_SEARCH_FROM
        ? { ariaLabel: labels.searchProperties, placeholder: labels.searchProperties, noResultsLabel: labels.noSearchResults } : undefined}
      onChange={(value) => {
        const next = filterPropertyForComparison(properties.find((candidate) => candidate.id === value) ?? selected, properties);
        onChange({ propertyId: next.id, operator: defaultOperator(next), value: defaultValue(next), rollupMatch: undefined });
      }} />
  );
  return (
    <div className={styles.rule} data-filter-row>
      {/* A date property names its part beside it in the property's column, as Notion's "날짜 ⌄ 시작일 ⌄" (seen 2026-11-12). */}
      {datePart ? (
        <div className={styles.propertyCell} data-filter-date-part>
          {property}
          <NotionSelect className={styles.datePart} buttonClassName={styles.choice} ariaLabel={dateFilterLabels().datePartFor(prop.name)}
            value={filter.datePart === "end" ? "end" : "start"} unmarked
            options={(["start", "end"] as const).map((part) => ({ value: part, label: dateFilterLabels().parts[part] }))}
            onChange={(part) => onChange({ datePart: part === "end" ? "end" : undefined })} />
        </div>
      ) : property}
      <div className={styles.condition}>
        {/* A rollup asks which of its values first, at the condition's start, as Notion's "최소 하나의 옵션 ⌄" (seen 2026-11-12). */}
        {rollupMatch ? (
          <NotionSelect className={styles.operator} buttonClassName={styles.choice} ariaLabel={labels.filterRollupMatch}
            value={filter.rollupMatch ?? "any"} unmarked options={rollupMatchChoices().map(({ key, label }) => ({ value: key, label }))}
            onChange={(key) => onChange(rollupMatchPatch(key))} />
        ) : null}
        {/* Its conditions as a pill's (Notion's, seen 2026-11-22 at 1440 dark in two tabs): a list 190 wide right
            under it, 28px rows a pixel apart 4px in, the chosen one lit and unticked. */}
        <NotionSelect className={styles.operator} buttonClassName={styles.choice} ariaLabel={labels.filterCondition} value={operator}
          plain menuMinWidth={190}
          options={filterOperatorsFor(prop).map((value) => ({ value, label: filterOperatorLabel(value, prop) }))}
          onChange={(value) => onChange(filterOperatorChange(filter, prop, value as FilterOperator, true))} />
        {!NO_VALUE_FILTERS.has(operator) ? (
          <div className={styles.value} data-relative={relative ? "true" : undefined} data-filter-value-cell>
            <DatabaseFilterValueInput prop={prop} operator={operator}
              value={relative && isDateFilterProperty(prop) ? dateFilterRelativeValue(filter) : filter.value}
              databaseProperties={properties} snapshot={snapshot} placeholder={labels.sheetValue} onChange={(value) => onChange({ operator, value })} />
          </div>
        ) : null}
      </div>
      <FilterActionMenu label={labels.filterActions} className={styles.more} actions={actions} />
    </div>
  );
}
