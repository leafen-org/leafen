import { useContext, useMemo, useRef, useState } from "react";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { FormulaEditorDialog } from "@/components/database/FormulaEditorDialog";
import { VALUE_FORMULA_BOUNDS } from "./AutomationEditProperty";
import { AUTOMATION_LINE_CHOICE_TYPES, AutomationLineChoiceField } from "./AutomationLineChoiceField";
import { AutomationFormulaField, automationLineFormulaStart, automationNumberWords, automationWordsValue } from "./AutomationWordsField";
import { automationFormulaWords } from "./automationFormulaWords";
import { automationFormulaNames, automationVariablesKey, type AutomationVariableList } from "./automationFormulaNames";
import { AutomationPageLine } from "./AutomationPageLine";
import { automationValueTypeWords, useAutomationResultProblem } from "./automationValueTypes";
import { automationValueFormulaWords } from "./automationFormulaFault";
import type { AutomationEditorSurface } from "./automationActionModel";
import { useTranslation } from "react-i18next";
import { advancedFilterParts } from "@/components/database/DatabaseAdvancedFilter";
import { nativeDatabaseFilterSnapshot, type DatabaseFilterSnapshot } from "@/components/database/databaseFilterSnapshot";
import { isDatabaseAutomationEditableProperty } from "@/components/database/databaseAutomationPanelModel";
import { disambiguatePropertyLabels } from "@/components/database/propertyOptionLabels";
import { CellMenuPortal } from "@/components/database/PropertyCellMenu";
import type { NativeEditorGraphAdapter } from "@/components/editor/editorRuntimeTypes";
import type { ArchivePagesAutomationAction, AutomationAction, DbProperty, EditPagesAutomationAction, FilterGroup, Page } from "@/lib/types";
import { DatabaseTaskRowsContext } from "@/components/database/untitledRowName";
import { automationPropertyOrder, defaultLineValue } from "./automationActionModel";
import { AutomationPropertyValueField } from "./AutomationPropertyValueField";
import { useAutomationRelationRows } from "./automationRelationRows";
import type { useAutomationTargetDatabases } from "./automationTargetDatabases";
import styles from "./automationActionEditor.module.css";
import chipStyles from "./automationHeadChip.module.css";
import filterStyles from "./automationPagesFilter.module.css";
import lineStyles from "./AutomationAddPage.module.css";
import { ChevronDown } from "@/icons/leafen";
import { AutomationChoicePopover, AutomationPropertyMark } from "./AutomationChoicePopover";
import { pagesByIdOf } from "@/lib/pageIndex";

const emptyFilter = (): FilterGroup => ({ conjunction: "and", filters: [], groups: [] });

/** An action aimed at pages: an edit of them, or their archive, which names them alike (Notion's, seen 2026-11-15). */
export type PagesAutomationAction = EditPagesAutomationAction | ArchivePagesAutomationAction;

// An edit's changes start over with a new aim; an archive sets none.
const freshChanges = <A extends PagesAutomationAction>(action: A) => action.type === "edit_pages" ? { changes: [] } : {};

/** Edits aimed at another database start over: no filter and no changes. */
export function retargetEditPages<A extends PagesAutomationAction>(action: A, databaseId: string): A {
  if (databaseId === action.target.databaseId && action.target.type === "database") return action;
  return { ...action, target: { type: "database", databaseId, filter: emptyFilter() as unknown as Record<string, unknown>,
    timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone }, ...freshChanges(action) };
}

/**
 * The row the button is on, as Notion's "↗ 이 페이지 ⌄ 편집" (seen 2026-11-02),
 * or the page that set off an automation, its "보관 [↗ 페이지 실행]" (seen
 * 2026-11-15); a new aim starts over too.
 */
export function aimEditPagesAtThisPage<A extends PagesAutomationAction>(action: A, databaseId: string): A {
  if (action.target.type === "this_page") return action;
  return { ...action, target: { type: "this_page", databaseId, filter: {} }, ...freshChanges(action) };
}

/**
 * Which of the database's pages an edit changes, in its card's heading as
 * Notion's "[작업 전체 ⌄] 편집 [T ⌄] 에서" (seen 2026-11-03): all of them, or
 * those its filter matches, set in the filter window it opens.
 */
export function AutomationEditPagesScope({ action, number, properties: own, pages, targets, context, onChange }: {
  action: PagesAutomationAction; number: number; properties: DbProperty[]; pages: Page[];
  targets?: ReturnType<typeof useAutomationTargetDatabases>; context?: NativeEditorGraphAdapter;
  onChange(action: AutomationAction): void;
}) {
  const foreign = targets?.properties;
  const properties = useMemo(() => foreign?.length ? [...own, ...foreign] : own, [foreign, own]);
  const { t } = useTranslation("databaseView");
  const ownTasks = useContext(DatabaseTaskRowsContext);
  // A task database's edit reads "작업 전체", as Notion's (seen 2026-11-05).
  const tasks = targets?.targets[action.target.databaseId]?.tasks ?? (ownTasks && own.some((property) => property.databaseId === action.target.databaseId));
  const [filterOpen, setFilterOpen] = useState(false);
  const filterButton = useRef<HTMLButtonElement>(null);
  const relationRows = useAutomationRelationRows();
  const targetProperties = properties.filter(property => property.databaseId === action.target.databaseId);
  const snapshot = useMemo<DatabaseFilterSnapshot>(() => {
    if (context) return nativeDatabaseFilterSnapshot({ currentUserId: context.session?.user.id ?? "", graph: context.graph,
      snapshotIdentity: `${context.pageId}/${context.generation}`, workspaceId: context.workspaceId });
    const byDatabase: Record<string, DbProperty[]> = {}, rows: Record<string, Page[]> = {};
    for (const property of properties) (byDatabase[property.databaseId] ??= []).push(property);
    for (const databaseId of Object.keys(byDatabase)) rows[databaseId] = relationRows?.(databaseId) ?? pages.filter(page => page.parentId === databaseId && page.kind === "page");
    return { snapshotIdentity: action.id, currentUserId: "", organizationProfiles: [], workspaceMembers: [],
      propertiesByDatabase: byDatabase, rowsByDatabase: rows, pagesById: Object.fromEntries(Object.values(rows).flat().map(page => [page.id, page])) };
  }, [context, properties, pages, relationRows, action.id]);
  const legacyIds = Array.isArray(action.target.filter.rowIds) ? action.target.filter.rowIds : null;
  const group = Array.isArray(action.target.filter.filters) ? action.target.filter as unknown as FilterGroup : emptyFilter();
  const commitFilter = (filter: FilterGroup) => onChange({ ...action, target: { ...action.target,
    filter: filter as unknown as Record<string, unknown>, limit: undefined, timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone } });
  const filter = advancedFilterParts({ view: { config: { filterGroup: group } }, properties: targetProperties, snapshot,
    onUpdateView: view => commitFilter(view.config.filterGroup) });
  if (legacyIds) return <span className={styles.actionTitleText} data-head-word>{t("automationSelectedPages", { count: legacyIds.length })}
    <button type="button" onClick={() => commitFilter(emptyFilter())}>{t("automationUseFilter")}</button></span>;
  return <>
    <button ref={filterButton} type="button" className={`${styles.targetButton} ${chipStyles.chip}`} aria-label={t("filterForAction", { number })}
      disabled={!targetProperties.length} aria-haspopup="dialog" aria-expanded={filterOpen} data-automation-edit-scope
      onClick={() => { if (!filter.count) filter.add(); setFilterOpen(current => !current); }}>
      <span>{filter.count ? t("automationFilteredPages", { count: filter.count }) : t(tasks ? "automationAllTasks" : "automationAllPages")}</span>
      <ChevronDown size="tiny" aria-hidden="true" />
    </button>
    {filterOpen && <CellMenuPortal triggerRef={filterButton} ariaLabel={t("filterForAction", { number })}
      className={filterStyles.window} width={0} fitWidth align="center" gap={0} onClose={() => setFilterOpen(false)}>
      {filter.editor}
      <button type="button" className={filterStyles.remove} data-automation-remove-filter
        onClick={() => { commitFilter(emptyFilter()); setFilterOpen(false); }}>{t("automationRemoveFilter")}</button>
    </CellMenuPortal>}
  </>;
}

export function AutomationEditPagesFields({ action, number, surface, properties: own, pages, targets, variables = [], onChange }: {
  action: EditPagesAutomationAction; number: number; surface?: AutomationEditorSurface; properties: DbProperty[]; pages: Page[]; databases: Page[];
  targets?: ReturnType<typeof useAutomationTargetDatabases>; context?: NativeEditorGraphAdapter;
  /** The variables the actions before this one define, which its words may name. */
  variables?: AutomationVariableList;
  onChange(action: AutomationAction): void;
}) {
  // A database on another page brings its own properties, read for it.
  const foreign = targets?.properties;
  const properties = useMemo(() => foreign?.length ? [...own, ...foreign] : own, [foreign, own]);
  const { t } = useTranslation(["databaseView", "formulaEditor"]);
  // A change's words take "@" and Σ, as a new page's do (AutomationWordsField).
  const names = useMemo(() => automationFormulaNames(t, { surface, variables, properties: own }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [t, surface, automationVariablesKey(variables), own]);
  // The line whose formula is being written, and the field its editor hangs from.
  const [formulaAt, setFormulaAt] = useState<{ index: number; anchor: DOMRect | null } | null>(null);
  const [picking, setPicking] = useState(false);
  const addButton = useRef<HTMLButtonElement>(null);
  const targetProperties = properties.filter(property => property.databaseId === action.target.databaseId);
  // Listed as "속성 편집..." lists them (automationPropertyOrder).
  const writable = targetProperties.filter(isDatabaseAutomationEditableProperty).sort(automationPropertyOrder);
  const labels = disambiguatePropertyLabels(writable, t("untitled"));
  const labelOf = Object.fromEntries(writable.map((property, index) => [property.id, labels[index]]));
  const left = writable.map((property, index) => ({ property, label: labels[index] }))
    .filter(({ property }) => !action.changes.some(change => change.propertyId === property.id));
  const changed = (propertyId: string) => writable.find(property => property.id === propertyId);
  const setChange = (index: number, change: EditPagesAutomationAction["changes"][number]) =>
    onChange({ ...action, changes: action.changes.map((current, at) => at === index ? change : current) });
  const openFormula = (index: number) => (from?: HTMLElement) => setFormulaAt({ index, anchor: from?.getBoundingClientRect() ?? null });
  // "전환" makes a formula words again, once asked (Notion's, seen 2026-11-10: `"12"` gives 12, "5+5+1" its own words).
  const toText = async (index: number, expression: string, property: DbProperty | undefined) => {
    const convert = await confirmAction({ message: t("messageToTextConfirm"), detail: t("messageToTextDetail"), confirmLabel: t("messageToTextAction") });
    if (!convert) return;
    const words = automationFormulaWords(expression, names, own, t);
    setChange(index, { propertyId: action.changes[index]!.propertyId,
      value: property?.type === "number" ? { kind: "literal", value: automationNumberWords(words.value) } : automationWordsValue(words.value, words.spans) });
  };
  return <div className={lineStyles.lines}>
    {action.changes.map((change, index) => {
      const property = changed(change.propertyId);
      const gone = targetProperties.length > 0 && !property;
      // What its formula's editor names first, else a type its property can't take, under the value (Notion's, seen 2026-11-21).
      const problem = automationValueFormulaWords(t, change.value, own, names) ?? automationValueTypeWords(t, change.value, property, own);
      return <AutomationPageLine key={index} property={gone ? null : property ?? null} name={gone ? t("automationGoneProperty") : labelOf[change.propertyId] ?? ""}
        left={left.map(({ property }) => property)} labels={labelOf} problem={problem} data={{ "data-automation-change-line": String(index) }}
        onPick={(next) => setChange(index, { propertyId: next.id, value: defaultLineValue(next) })}
        onRemove={() => onChange({ ...action, changes: action.changes.filter((_, at) => at !== index) })}>
        {gone ? undefined : change.value.kind === "literal" && AUTOMATION_LINE_CHOICE_TYPES.has(property?.type ?? "") ? <AutomationLineChoiceField id={`${action.id}-${index}`}
          propertyId={change.propertyId} value={change.value} number={number} properties={writable} pages={pages} surface={surface} variables={variables}
          head={action.target.type === "this_page" ? "full" : undefined} align={action.target.type === "this_page" ? undefined : "end"} modes
          onChange={(value) => setChange(index, { propertyId: change.propertyId, value })} />
        : change.value.kind === "formula" ? <AutomationFormulaField expression={change.value.expression} label={t("valueForAction", { number })} names={names}
          properties={own} invalid={Boolean(problem)} onEdit={openFormula(index)}
          onText={() => void toText(index, change.value.kind === "formula" ? change.value.expression : "", property)} />
        : <AutomationPropertyValueField action={{ id: action.id, type: "edit_property", target: "trigger_page", ...change }}
          number={number} properties={writable} words={{ names, properties: own, onFormula: openFormula(index) }} line
          onChange={next => { if (next.type === "edit_property") setChange(index, { propertyId: next.propertyId, value: next.value }); }} />}
      </AutomationPageLine>;
    })}
    {/* "속성 편집 ⌄" picks the property first, as Notion's (seen 2026-11-03), and its row follows; it lists those
        the card doesn't set yet, each with its mark, and goes once it sets them all — a new page's list (Notion's, seen 2026-11-10). */}
    {writable.length && !left.length ? null : <button ref={addButton} type="button" disabled={!writable.length} aria-haspopup="dialog" aria-expanded={picking}
      data-automation-add-change onClick={() => setPicking(open => !open)}>{t("automationAddChange")} <ChevronDown size="tiny" aria-hidden="true" /></button>}
    {picking && left.length ? <AutomationChoicePopover label={t("automationAddChange")} searchPlaceholder={t("searchProperties")} width={280} align="center" flush properties
      trigger={addButton.current} choices={left.map(({ property, label }) => ({ key: property.id, text: label, icon: <AutomationPropertyMark property={property} />, wideIcon: true }))}
      onPick={(propertyId) => { const property = writable.find(candidate => candidate.id === propertyId); setPicking(false);
        if (property) onChange({ ...action, changes: [...action.changes, { propertyId: property.id, value: defaultLineValue(property) }] }); }}
      onClose={() => setPicking(false)} /> : null}
    {formulaAt !== null && action.changes[formulaAt.index] ? <ChangeFormula action={action} index={formulaAt.index} anchor={formulaAt.anchor} properties={writable} own={own} pages={pages}
      names={names} onChange={onChange} onClose={() => setFormulaAt(null)} /> : null}
  </div>;
}

/**
 * A change's value as a formula: Σ makes its words one — a number's too, as
 * words — and its editor says what its property can't take, as Notion's
 * (seen 2026-11-10).
 */
function ChangeFormula({ action, index, anchor, properties, own, pages, names, onChange, onClose }: {
  action: EditPagesAutomationAction; index: number; anchor: DOMRect | null; properties: DbProperty[]; own: DbProperty[]; pages: Page[];
  names: ReturnType<typeof automationFormulaNames>; onChange(action: AutomationAction): void; onClose(): void;
}) {
  const change = action.changes[index]!;
  const property = properties.find((candidate) => candidate.id === change.propertyId);
  const expression = automationLineFormulaStart(change.value, property?.type);
  const pagesById = pagesByIdOf(pages);
  const prop = useMemo((): DbProperty => ({ id: `automation-change-${action.id}-${index}`, databaseId: property?.databaseId ?? "",
    name: property?.name ?? "", type: "formula", config: { formula: expression }, position: Number.MAX_SAFE_INTEGER }),
  [action.id, index, property?.databaseId, property?.name, expression]);
  const resultProblem = useAutomationResultProblem(property, own, names);
  return <FormulaEditorDialog prop={prop} props={own} pagesById={pagesById} bounds={VALUE_FORMULA_BOUNDS} names={names} anchor={anchor}
    fresh={change.value.kind !== "formula"} resultProblem={resultProblem}
    onSave={(next) => onChange({ ...action, changes: action.changes.map((current, at) => at === index
      ? { ...current, value: next.trim() ? { kind: "formula", expression: next.trim() } : { kind: "literal", value: property?.type === "number" ? null : "" } } : current) })}
    onClose={onClose} />;
}
