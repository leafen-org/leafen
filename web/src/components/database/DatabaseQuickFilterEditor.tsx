import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { CheckIcon, DotsHorizontal, FilterIcon, Settings, SidebarChevronDown, Trash } from "@/icons/leafen";
import { SearchClearButton } from "@/components/PageActionSearch";
import { i18next } from "@/i18n";
import type { DbProperty, FilterOperator, ViewFilter } from "@/lib/types";
import { DatabaseFilterValueInput, pagePicks, personPicks, relationFilterTarget } from "./DatabaseFilterValueInput";
import { useRelationRowsInOrder } from "./relationPageOrder";
import { useFilterThisPage } from "./filterThisPage";
import { FilterPicks } from "./DatabaseFilterPicks";
import {
  effectiveFilterOperator,
  filterOperatorLabel,
  filterOperatorsFor,
  filterAsksRollupMatch,
  filterPropertyForComparison,
  filterUsesNumberSigns,
} from "./databaseFilterPolicy";
import type { DatabaseFilterSnapshot } from "./databaseFilterSnapshot";
import { NO_VALUE_FILTERS, dateFilterLabels, rollupMatchChoices, rollupMatchPatch } from "./databaseFilterLabels";
import { DateFilterValue, dateFilterField, dateFilterShowsPart, isDateFilterProperty } from "./DateFilterEditor";
import { databaseViewLabels } from "./databaseViewLabels";
import { filterOperatorChange } from "./DatabaseFilterEditors";
import { useMultiSourceEdit } from "./dashboardMultiSourceEdit";
import { NotionSelect } from "./NotionSelect";
import { OptionFilterChoices } from "./DatabaseOptionFilterChoices";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { placeBeside, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { mergeStyleModules } from "@/lib/styleModules";
import barStyles from "./DatabaseQuickFilterBar.module.css";
import editorStyles from "./DatabaseQuickFilterEditor.module.css";
import { usePageNames } from "@/lib/pageNames";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

// The pill's window rules stay with the bar; its editor's own parts have their own sheet.
const styles = mergeStyleModules(barStyles, editorStyles);
// Its head's choices draw Notion's filled ⌄ (arrowChevronSingleDownFillSmall) at 12, 7.6 by 4.38 of ink in its 16-unit
// view, as Notion's "시작일 ⌄", "오늘 기준 ⌄" and "값과 동일한 데이터 ⌄" (seen 2026-11-22 at 1440 dark, dpr 2).
const HEAD_CARET = <SidebarChevronDown size="micro" aria-hidden="true" />;

/**
 * A quick filter's editor under its pill, as Notion's (seen 2026-10-01): a
 * head of the property's name (a date property's date part, "시작일 ˅"), its
 * operator (a number's as a sign, "="), and "⋯" ("필터 제거", "고급 필터에
 * 추가"); under it the value — a field ("값 입력", after the sign for a
 * number), a select's options to tick any of, a checkbox's two states,
 * choosing one closing it, or a date's day, range or relative date with its
 * calendar. A dashboard filter's "⋯" holds "⚙ 다중 출처 필터링" in place of
 * "고급 필터에 추가" (MultiSourceEditContext).
 */
export function QuickFilterEditor({ filter, property: sourceProperty, properties, snapshot, onChange, onRemove, onToAdvanced, onClose }: {
  filter: ViewFilter;
  property: DbProperty;
  properties: DbProperty[];
  snapshot: DatabaseFilterSnapshot;
  onChange(patch: Partial<ViewFilter>): void;
  onRemove(): void;
  onToAdvanced?(): void;
  onClose(): void;
}) {
  const labels = databaseViewLabels();
  const property = filterPropertyForComparison(sourceProperty, properties);
  const rollupMatch = filterAsksRollupMatch(sourceProperty);
  const [more, setMore] = useState(false);
  const operator = effectiveFilterOperator(property, filter.operator);
  const dates = isDateFilterProperty(property);
  const setOperator = (next: FilterOperator) => onChange(filterOperatorChange(filter, property, next));
  // A date's day field, or a range's start, takes the focus as a new
  // condition brings it in; a condition that keeps a day's field, or shows
  // none, leaves the focus on the condition (Notion's, seen 2026-11-12: into
  // "값과 동일한 데이터" from "오늘 기준", "범위 내" or "비어 있음", and into
  // "범위 내"; not from "값과 동일한 데이터" to "~이후(당일 포함)").
  const headRef = useRef<HTMLDivElement>(null);
  const field = dates ? dateFilterField(operator) : null;
  const fieldShown = useRef(field);
  useEffect(() => {
    const brought = field !== null && field !== fieldShown.current;
    fieldShown.current = field;
    if (!brought) return;
    // After the condition's list hands the focus back to its button, what is typed from the pick going in.
    return focusOpenedField(() => headRef.current?.parentElement?.querySelector<HTMLElement>("[data-date-filter-value] input"));
  }, [field]);
  // "⋯" holds the keys while open; Escape returns to its button.
  const moreButtonRef = useRef<HTMLButtonElement>(null), moreMenuRef = useRef<HTMLSpanElement>(null);
  const moreLayer = useOverlayLayer(more, { kind: "picker", ref: moreMenuRef, onEscape: () => setMore(false), onOutsidePress: () => setMore(false),
    inside: () => [moreButtonRef.current], returnFocusRef: moreButtonRef });
  // A dashboard's filter goes on to "⚙ 다중 출처 필터링" in its place (Notion's, seen 2026-11-17).
  const multiSource = useMultiSourceEdit();
  const moreActions = [
    { key: "remove", label: labels.removeFilter, icon: <Trash size="regular" />, data: { "data-quick-filter-remove": "" }, run: onRemove },
    ...(multiSource ? [{ key: "multiSource", label: i18next.t("databaseView:multiSourceFiltering"), icon: <Settings size="regular" />,
      data: { "data-quick-filter-multi-source": "" }, run: multiSource }]
      : onToAdvanced ? [{ key: "advanced", label: labels.addToAdvancedFilter, icon: <FilterIcon size="regular" />,
        data: { "data-quick-filter-to-advanced": "" }, run: onToAdvanced }] : []),
  ];
  const moreMenu = useNativeMenu({ items: moreActions, open: more, menuRef: moreMenuRef, label: labels.filterMoreActions,
    keyOf: (action) => action.key, textOf: (action) => action.label, onAction: (action) => action.run() });
  // Its menu stands apart beside "⋯", 2px to its right with their tops even,
  // over whatever the pill's window holds, as Notion's (seen 2026-11-07: the
  // window's value field and its edge stay whole under it; 2026-11-15 in
  // Chrome at 1440: "⋯" 562–582 at 219.4, its menu at 584, 219.4).
  const morePlace = usePopoverPlacement(moreMenuRef, more
    ? (size, viewport) => placeBeside(popoverRectOf(moreButtonRef.current)!, size, viewport, { gap: 2 }) : null, [], undefined, false);
  return (
    <>
      <div ref={headRef} className={styles.editorHead}>
        {/* A rollup of the related values asks which of them first, as Notion's "최소 하나의 옵션 ⌄" (seen 2026-11-05). */}
        {rollupMatch ? (
          <NotionSelect ariaLabel={labels.filterRollupMatch} className={styles.editorOperator}
            buttonClassName={styles.editorOperatorButton} caret={HEAD_CARET} value={filter.rollupMatch ?? "any"} showCheck={false}
            options={rollupMatchChoices().map(({ key, label }) => ({ value: key, label }))}
            onChange={(key) => onChange(rollupMatchPatch(key))} />
        ) : dates && dateFilterShowsPart(property, operator) ? (
          <NotionSelect ariaLabel={dateFilterLabels().datePartFor(property.name)} className={styles.editorOperator}
            buttonClassName={styles.editorOperatorButton} caret={HEAD_CARET} value={filter.datePart === "end" ? "end" : "start"} showCheck={false}
            options={(["start", "end"] as const).map((part) => ({ value: part, label: dateFilterLabels().parts[part] }))}
            onChange={(part) => onChange({ datePart: part === "end" ? "end" : undefined })} />
        ) : <span className={styles.editorName}>{property.name}</span>}
        {/* Its conditions as Notion's (seen 2026-11-05): a list 190 wide (2026-11-15), 4px in, 28px rows a pixel apart in 14px, unticked;
            a status's too, "값과 동일한 데이터 ⌄" (2026-11-08). */}
        <NotionSelect ariaLabel={labels.filterCondition} className={`${styles.editorOperator} ${styles.editorCondition}`}
          buttonClassName={styles.editorOperatorButton} caret={HEAD_CARET}
          value={operator} plain menuMinWidth={190} menuClassName={styles.operatorMenu}
          options={filterOperatorsFor(property).map((candidate) => ({ value: candidate, label: filterOperatorLabel(candidate, property) }))}
          onChange={(next) => setOperator(next as FilterOperator)} />
        <span className={styles.editorMoreWrap}>
          <button ref={moreButtonRef} type="button" className={styles.editorMore} aria-label={labels.filterMoreActions} aria-haspopup="menu"
            aria-expanded={more} data-quick-filter-more onClick={() => setMore(!more)}>
            <DotsHorizontal size="small" aria-hidden="true" />
          </button>
          {more && createPortal(
            <span {...moreMenu.menuProps} ref={moreMenuRef} className={styles.editorMenu}
              style={{ position: "fixed", zIndex: moreLayer.zIndex, ...popoverPlacementStyle(morePlace) }}>
              {moreActions.map((action) => (
                <NativeMenuItem key={action.key} state={moreMenu.state} itemKey={action.key} className={styles.menuItem} {...action.data}>
                  {({ labelProps }) => <>
                    <span className={styles.menuItemIcon} aria-hidden="true">{action.icon}</span><span {...labelProps}>{action.label}</span>
                  </>}
                </NativeMenuItem>
              ))}
            </span>, document.body,
          )}
        </span>
      </div>
      <QuickFilterValue filter={filter} property={sourceProperty} properties={properties} snapshot={snapshot} onChange={onChange} onClose={onClose} closeOnClear />
    </>
  );
}

const PEOPLE = new Set(["person", "created_by", "last_edited_by"]);

/**
 * A filter's value as Notion's pill editor shows it under its head (above):
 * a field (after the sign for a number), a select's options to tick any of,
 * a status's groups and options, a checkbox's two states — choosing one
 * closes it — or a date's day, range or relative date. A conditional
 * color's card shows the same (ConditionalColorCard).
 */
export function QuickFilterValue({ filter, property: sourceProperty, properties, snapshot, onChange, onClose, closeOnClear = false }: {
  filter: ViewFilter;
  property: DbProperty;
  properties: DbProperty[];
  snapshot: DatabaseFilterSnapshot;
  onChange(patch: Partial<ViewFilter>): void;
  onClose(): void;
  /** A pill's editor closes as people, pages or options are cleared with "ⓧ" (Notion's, seen 2026-11-12, 2026-11-16). */
  closeOnClear?: boolean;
}) {
  const labels = databaseViewLabels();
  // Its pages named as they are named everywhere now, followed (lib/pageNames.ts).
  const thisPage = useFilterThisPage(), pageNames = usePageNames();
  const property = filterPropertyForComparison(sourceProperty, properties);
  const operator = effectiveFilterOperator(property, filter.operator);
  // A relation's pages in the relation picker's order (useRelationRowsInOrder).
  const relationRows = useRelationRowsInOrder(snapshot.rowsByDatabase[relationFilterTarget(property)], snapshot.workspaceId,
    property.type === "relation");
  const signs = filterUsesNumberSigns(property);
  const dates = isDateFilterProperty(property);
  // A checkbox's two states are a menu under the head, its first row lit and
  // the chosen one ticked, with "지우기" under a line while one is chosen;
  // choosing closes it (Notion's, seen 2026-11-12).
  const checkboxRef = useRef<HTMLDivElement>(null);
  const checkbox = !dates && !NO_VALUE_FILTERS.has(operator) && property.type === "checkbox";
  const chosen = filter.value === true || filter.value === "true" ? true : filter.value === false || filter.value === "false" ? false : undefined;
  const checkboxItems: (boolean | "clear")[] = checkbox ? [false, true, ...(chosen === undefined ? [] : ["clear" as const])] : [];
  const checkboxMenu = useNativeMenu({ items: checkboxItems, open: checkbox, menuRef: checkboxRef, highlightOnOpen: true,
    label: labels.filterValueFor(property.name), keyOf: String,
    textOf: (state) => state === "clear" ? labels.clearValue : state ? labels.checked : labels.unchecked,
    onAction: (state) => { onChange({ operator, value: state === "clear" ? undefined : state }); onClose(); } });
  let value = null;
  if (dates) value = <DateFilterValue filter={filter} property={property} onChange={onChange} />;
  else if (NO_VALUE_FILTERS.has(operator)) value = null;
  else if (property.type === "checkbox") {
    value = (
      <div {...checkboxMenu.menuProps} ref={checkboxRef} className={styles.editorChoices} data-quick-filter-states>
        {[false, true].map((state) => (
          <NativeMenuItem key={String(state)} state={checkboxMenu.state} itemKey={String(state)} role="menuitemradio"
            className={styles.editorChoice} aria-checked={chosen === state} data-quick-filter-checkbox={String(state)}>
            {({ labelProps }) => <>
              <span {...labelProps}>{state ? labels.checked : labels.unchecked}</span>
              {chosen === state ? <CheckIcon size="small" aria-hidden="true" /> : null}
            </>}
          </NativeMenuItem>
        ))}
        {chosen === undefined ? null : <>
          <div role="separator" className={styles.editorChoicesRule} />
          <NativeMenuItem state={checkboxMenu.state} itemKey="clear" className={styles.editorChoice} data-quick-filter-checkbox-clear>
            {({ labelProps }) => <span {...labelProps}>{labels.clearValue}</span>}
          </NativeMenuItem>
        </>}
      </div>
    );
  } else if (property.type === "status" || property.type === "select" || property.type === "multi_select") {
    value = <OptionFilterChoices property={property} value={filter.value} onChoose={(next) => onChange({ operator, value: next })}
      onCleared={closeOnClear ? onClose : undefined} />;
  } else if (property.type === "relation" || PEOPLE.has(property.type)) {
    // As many people or pages as wanted, as Notion's (seen 2026-11-04).
    const { leads, picks } = property.type === "relation" ? pagePicks(property, snapshot, thisPage, filter.value, relationRows, pageNames) : personPicks(snapshot);
    value = <FilterPicks label={labels.filterValueFor(property.name)} leads={leads} picks={picks} value={filter.value} pages={property.type === "relation"}
      placeholder={property.type === "relation" ? labels.sheetSearchPages : labels.sheetSearchPeople}
      onChange={(next) => onChange({ operator, value: next })} onCleared={closeOnClear ? onClose : undefined} />;
  } else {
    value = (
      <div className={styles.editorValue}>
        {signs ? <span className={styles.editorSign} aria-hidden="true">{filterOperatorLabel(operator, property)}</span> : null}
        <DatabaseFilterValueInput prop={property} operator={operator} value={filter.value} databaseProperties={properties}
          snapshot={snapshot} onChange={(next) => onChange({ operator, value: next })} />
        {/* A typed value's "ⓧ" empties it, a number's too: Notion's shared "입력 값 지우기" 2px from the field's end
            (seen 2026-11-12, 2026-11-17); the focus goes with it, so what is typed next goes nowhere and Escape
            closes (Notion's, seen 2026-11-12). */}
        <SearchClearButton value={(typeof filter.value === "string" && filter.value !== "") || typeof filter.value === "number"}
          className={styles.editorValueClear} tabIndex={-1} onClear={() => onChange({ operator, value: "" })} />
      </div>
    );
  }
  return value;
}
