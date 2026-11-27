import { Fragment, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown } from "@/icons/leafen";
import type { AddPageAutomationAction, AutomationAction, AutomationPropertyChange, AutomationValueExpression, DbProperty, Page } from "@/lib/types";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { FormulaEditorDialog } from "@/components/database/FormulaEditorDialog";
import { isDatabaseAutomationEditableProperty } from "@/components/database/databaseAutomationPanelModel";
import { AutomationChoicePopover, AutomationPropertyMark } from "./AutomationChoicePopover";
import { VALUE_FORMULA_BOUNDS } from "./AutomationEditProperty";
import { AUTOMATION_LINE_CHOICE_TYPES, AutomationLineChoiceField } from "./AutomationLineChoiceField";
import { AutomationPageLine } from "./AutomationPageLine";
import { AutomationPropertyValueField } from "./AutomationPropertyValueField";
import {
  AUTOMATION_WORDS_TYPES, AutomationFormulaField, AutomationWordsField, automationLineFormulaStart, automationNumberWords, automationWordsOf,
  automationWordsValue,
} from "./AutomationWordsField";
import { automationFormulaWords } from "./automationFormulaWords";
import { automationPropertyOrder, defaultLineValue, type AutomationEditorSurface } from "./automationActionModel";
import { automationFormulaNames, automationVariablesKey, type AutomationVariableList } from "./automationFormulaNames";
import type { useAutomationTargetDatabases } from "./automationTargetDatabases";
import { automationValueTypeWords, useAutomationResultProblem } from "./automationValueTypes";
import { automationValueFormulaWords } from "./automationFormulaFault";
import styles from "./AutomationAddPage.module.css";
import editorStyles from "./automationActionEditor.module.css";
import chipStyles from "./automationHeadChip.module.css";
import { pagesByIdOf } from "@/lib/pageIndex";

const TITLE = "title";

/**
 * A "페이지 추가 위치" card's page, as Notion's (seen 2026-10-25 at 1280px):
 * a line for its name — "Aa 이름 ⌄" over a soft field, "제목 없음" while
 * empty, with "@" and Σ at its end — and one for each property it sets,
 * then "속성 편집 ⌄", which lists the properties left to set. A line's "⌄"
 * swaps its property or takes it away (AutomationPageLine); words take
 * mentions and formulas, the other properties their own fields.
 */
export function AutomationAddPageFields({ action, number, surface, properties: own, pages, targets, variables, onChange }: {
  action: AddPageAutomationAction;
  number: number;
  surface: AutomationEditorSurface;
  properties: DbProperty[];
  pages: Page[];
  targets?: ReturnType<typeof useAutomationTargetDatabases>;
  variables: AutomationVariableList;
  onChange(action: AutomationAction): void;
}) {
  const { t } = useTranslation(["databaseView", "formulaEditor"]);
  const foreign = targets?.properties;
  const properties = useMemo(() => foreign?.length ? [...own, ...foreign] : own, [foreign, own]);
  const writable = useMemo(() => properties.filter((property) => property.databaseId === action.databaseId
    && isDatabaseAutomationEditableProperty(property)).sort(automationPropertyOrder), [action.databaseId, properties]);
  const titleProperty = writable.find((property) => property.type === "title");
  // Until the database's properties come, none of its lines is known to be gone.
  const known = properties.some((property) => property.databaseId === action.databaseId);
  const changes = action.properties ?? [];
  // The name's line goes until the page's name is set again; a name typed keeps it.
  const [titleGone, setTitleGone] = useState(false);
  // Where the name's line sits among the others, as Notion's: first, then where it took another line's place or,
  // added again, last; a property taking its place takes that place (seen 2026-11-10).
  const [titleAt, setTitleAt] = useState(0);
  const title: AutomationValueExpression = typeof action.title === "string" ? { kind: "literal", value: action.title } : action.title;
  const titleShown = !action.templateId && Boolean(titleProperty) && (!titleGone || automationWordsOf(title).length > 0 || title.kind === "formula");
  const names = useMemo(() => automationFormulaNames(t, { surface, variables, properties: own }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [t, surface, automationVariablesKey(variables), own]);
  const pagesById = pagesByIdOf(pages);
  const [adding, setAdding] = useState(false);
  // The line whose formula is being written, and the field its editor hangs from.
  const [formula, setFormula] = useState<{ key: string; anchor: DOMRect | null } | null>(null);
  const addButton = useRef<HTMLButtonElement>(null);
  const titleOf = (value: AutomationValueExpression) => value.kind === "literal" && !value.spans?.length && typeof value.value === "string" ? value.value : value;
  const setTitle = (value: AutomationValueExpression) => onChange({ ...action, title: titleOf(value) });
  const setChange = (propertyId: string, value: AutomationValueExpression) => onChange({ ...action,
    properties: changes.map((change) => change.propertyId === propertyId ? { propertyId, value } : change) });
  const valueOf = (key: string) => key === TITLE ? title : changes.find((change) => change.propertyId === key)?.value;
  const setValue = (key: string, value: AutomationValueExpression) => key === TITLE ? setTitle(value) : setChange(key, value);
  const nameAt = Math.min(titleAt, changes.length);
  const remove = (key: string) => {
    if (key === TITLE) { setTitleGone(true); setTitle({ kind: "literal", value: "" }); return; }
    if (changes.findIndex((change) => change.propertyId === key) < nameAt) setTitleAt(nameAt - 1);
    onChange({ ...action, properties: changes.filter((change) => change.propertyId !== key) });
  };
  // Another property takes a line's place, its value starting over (Notion's, seen 2026-11-10); the name's line goes
  // for another, and comes back for the name in that line's place.
  const replace = (key: string, property: DbProperty) => {
    const fresh: AutomationPropertyChange = { propertyId: property.id, value: defaultLineValue(property) };
    if (key === TITLE) {
      setTitleGone(true);
      onChange({ ...action, title: "", properties: [...changes.slice(0, nameAt), fresh, ...changes.slice(nameAt)] });
      return;
    }
    if (property.type === "title") {
      setTitleGone(false);
      setTitleAt(Math.max(0, changes.findIndex((change) => change.propertyId === key)));
      onChange({ ...action, properties: changes.filter((change) => change.propertyId !== key) });
      return;
    }
    onChange({ ...action, properties: changes.map((change) => change.propertyId === key ? fresh : change) });
  };
  const left = writable.filter((property) => property.type === "title" ? !titleShown : !changes.some((change) => change.propertyId === property.id));
  const add = (propertyId: string) => {
    const property = writable.find((candidate) => candidate.id === propertyId);
    if (!property) return;
    if (property.type === "title") { setTitleGone(false); setTitleAt(changes.length); return; }
    const change: AutomationPropertyChange = { propertyId, value: defaultLineValue(property) };
    onChange({ ...action, properties: [...changes, change] });
  };
  const propertyOf = (key: string | undefined) => key === TITLE ? titleProperty : writable.find((property) => property.id === key);
  const editing = formula === null ? undefined : valueOf(formula.key);
  const formulaProperty = propertyOf(formula?.key);
  const expression = !editing ? "" : automationLineFormulaStart(editing, formulaProperty?.type);
  const formulaProp = useMemo((): DbProperty => ({ id: `automation-add-page-${action.id}-${formula?.key ?? ""}`, databaseId: action.databaseId,
    name: formulaProperty?.name ?? "", type: "formula", config: { formula: expression }, position: Number.MAX_SAFE_INTEGER }),
  [action.databaseId, action.id, expression, formula?.key, formulaProperty?.name]);
  const resultProblem = useAutomationResultProblem(formulaProperty, own, names);
  if (!action.databaseId) return null;
  const openFormula = (key: string) => (from?: HTMLElement) => setFormula({ key, anchor: from?.getBoundingClientRect() ?? null });
  // "전환" makes a formula words again, once asked (Notion's, seen 2026-11-10).
  const toText = async (key: string, expression: string, property: DbProperty) => {
    const convert = await confirmAction({ message: t("messageToTextConfirm"), detail: t("messageToTextDetail"), confirmLabel: t("messageToTextAction") });
    if (!convert) return;
    const words = automationFormulaWords(expression, names, own, t);
    setValue(key, property.type === "number" ? { kind: "literal", value: automationNumberWords(words.value) } : automationWordsValue(words.value, words.spans));
  };

  // A deleted property's line says so, red, until it is taken away.
  const line = (key: string, property: DbProperty | null) => {
    const data = { "data-automation-add-page-line": key === TITLE ? "title" : property?.id ?? key };
    const name = property?.name ?? t("automationGoneProperty");
    if (!property) return <AutomationPageLine key={key} property={null} name={name} left={left} data={data}
      onPick={(next) => replace(key, next)} onRemove={() => remove(key)} />;
    const value = valueOf(key) ?? defaultLineValue(property);
    const words = AUTOMATION_WORDS_TYPES.has(property.type);
    const label = key === TITLE ? t("pageTitleForAction", { number }) : t("valueForAction", { number });
    // What its formula's editor names first, else a type its property can't take, under the value (Notion's, seen 2026-11-21).
    const problem = automationValueFormulaWords(t, value, own, names) ?? automationValueTypeWords(t, value, property, own);
    return (
      <AutomationPageLine key={key} property={property} name={name} left={left} problem={problem} data={data}
        onPick={(next) => replace(key, next)} onRemove={() => remove(key)}>
        {value.kind === "formula" ? (
          <AutomationFormulaField expression={value.expression} label={label} names={names} properties={own} invalid={Boolean(problem)}
            onEdit={openFormula(key)} onText={() => void toText(key, value.expression, property)} />
        ) : AUTOMATION_LINE_CHOICE_TYPES.has(property.type) && value.kind === "literal" ? (
          <AutomationLineChoiceField id={`${action.id}-${key}`} propertyId={property.id} value={value} number={number} properties={writable}
            pages={pages} surface={surface} variables={variables} onChange={(next) => setValue(key, next)} />
        ) : words ? (
          <AutomationWordsField words={automationWordsOf(value)} label={label} names={names} properties={own}
            placeholder={key === TITLE ? t("untitled") : undefined} onFormula={openFormula(key)}
            onChange={(text, spans) => setValue(key, automationWordsValue(text, spans))} />
        ) : (
          <span className={styles.field}>
            <AutomationPropertyValueField action={{ id: action.id, type: "edit_property", target: "trigger_page", propertyId: property.id, value }}
              number={number} properties={writable} words={{ names, properties: own, onFormula: openFormula(key) }} line
              onChange={(next) => { if (next.type === "edit_property") setValue(key, next.value); }} />
          </span>
        )}
      </AutomationPageLine>
    );
  };

  return (
    <div className={styles.lines}>
      {changes.map((change, index) => {
        const property = writable.find((candidate) => candidate.id === change.propertyId);
        return <Fragment key={change.propertyId}>
          {index === nameAt && titleShown && titleProperty ? line(TITLE, titleProperty) : null}
          {property || known ? line(change.propertyId, property ?? null) : null}
        </Fragment>;
      })}
      {nameAt === changes.length && titleShown && titleProperty ? line(TITLE, titleProperty) : null}
      {left.length ? (
        <span className={styles.labelWrap}>
          <button ref={addButton} type="button" className={styles.edit} aria-haspopup="dialog" aria-expanded={adding}
            data-automation-add-page-edit onClick={() => setAdding(!adding)}>
            <span>{t("addPageEditProperties")}</span>
            <ChevronDown size="small" aria-hidden="true" />
          </button>
          {adding ? (
            <AutomationChoicePopover label={t("addPageEditProperties")} width={280} align="center" trigger={addButton.current}
              searchPlaceholder={t("addPagePropertySearch")} flush properties
              choices={left.map((property) => ({ key: property.id, text: property.name, icon: <AutomationPropertyMark property={property} />, wideIcon: true }))}
              onPick={add} onClose={() => setAdding(false)} />
          ) : null}
        </span>
      ) : null}
      {formula !== null && editing ? (
        <FormulaEditorDialog prop={formulaProp} props={own} pagesById={pagesById} bounds={VALUE_FORMULA_BOUNDS} names={names}
          anchor={formula.anchor} fresh={editing.kind !== "formula"} resultProblem={resultProblem}
          onSave={(next) => setValue(formula.key, next.trim() ? { kind: "formula", expression: next.trim() }
            : { kind: "literal", value: formulaProperty?.type === "number" ? null : "" })}
          onClose={() => setFormula(null)} />
      ) : null}
    </div>
  );
}

/**
 * "[비어 있음 ⌄] (으)로" after a card's heading, when its database has
 * templates, as Notion's (seen 2026-11-04): "템플릿 선택" lists them and
 * "비어 있음"; one chosen, the page comes from it and the card keeps only
 * "속성 편집 ⌄" (AutomationAddPageFields).
 */
export function AutomationAddPageTemplate({ action, number, templates, onChange }: {
  action: AddPageAutomationAction; number: number; templates?: readonly { id: string; name: string }[]; onChange(action: AutomationAction): void;
}) {
  const { t } = useTranslation("databaseView");
  const button = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  if (!templates?.length) return null;
  const chosen = templates.find((template) => template.id === action.templateId);
  // "[신규 작업] (으)로", "as [New task]": the word on its side of the chip; a
  // language whose words name no place for it says them after the chip.
  const words = t("automationTemplateTo", { template: CHIP });
  const [before, after] = words.includes(CHIP) ? words.split(CHIP).map((part) => part.trim()) : ["", words];
  return <>
    {before ? <span className={editorStyles.actionTitleText} data-head-word>{before}</span> : null}
    <button ref={button} type="button" className={`${editorStyles.targetButton} ${chipStyles.chip}`} aria-label={`${t("automationTemplatePick")} ${number}`} aria-haspopup="dialog" aria-expanded={open}
      data-automation-add-page-template onClick={() => setOpen((current) => !current)}>
      <span>{chosen ? chosen.name || t("untitled") : t("automationTemplateEmpty")}</span><ChevronDown size="tiny" aria-hidden="true" />
    </button>
    {after ? <span className={editorStyles.actionTitleText} data-head-word>{after}</span> : null}
    {open ? <AutomationChoicePopover label={t("automationTemplatePick")} title={t("automationTemplatePick")} width={240} trigger={button.current}
      choices={[...templates.map((template) => ({ key: template.id, text: template.name || t("untitled"), chosen: template.id === action.templateId })),
        { key: EMPTY_TEMPLATE, text: t("automationTemplateEmpty"), chosen: !chosen, rule: true }]}
      onPick={(key) => { setOpen(false); onChange({ ...action, templateId: key === EMPTY_TEMPLATE ? undefined : key }); }}
      onClose={() => setOpen(false)} /> : null}
  </>;
}
const EMPTY_TEMPLATE = "\u0000empty";
const CHIP = "\u0000";
