import { useContext, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ArrowLeft, CalendarViewIcon, CheckboxValueIcon, CheckmarkSquareIcon, ChevronRight, PersonCircleFillIcon, RelationIcon, TextJustifyStartIcon } from "@/icons/leafen";
import type { AutomationAction, DbProperty, EditPropertyAutomationAction, Page } from "@/lib/types";
import { chipStyle } from "@/components/database/colors";
import { FormulaEditorDialog } from "@/components/database/FormulaEditorDialog";
import type { FormulaScopeName } from "@/components/database/formulaEditorModel";
import { PropertyIcon } from "@/components/database/PropertyIcon";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetHeadButton, PhoneSheetRow, phoneSheetStyles as sheetStyles } from "../shell/PhoneSheet";
import { AutomationPhoneSheets } from "./AutomationActionMenus";
import { AutomationPropertyMark } from "./AutomationChoicePopover";
import { automationFormulaNames, automationVariablesKey, type AutomationVariableList } from "./automationFormulaNames";
import type { AutomationEditorSurface } from "./automationActionModel";
import { AutomationDateChip, AutomationDateValue } from "./AutomationDateValue";
import { AutomationPropertyValueField } from "./AutomationPropertyValueField";
import { AutomationCheckboxValues } from "./AutomationCheckboxValues";
import { AutomationPersonValues, PersonValueChips, automationPeopleOf, automationValueModeOf, usePersonCardWords } from "./AutomationPersonValue";
import { AutomationOptionValues, OptionValueChips, automationChosenOptions } from "./AutomationOptionValues";
import { AutomationRelationValues, RelationValueChips, automationRelationItemsOf } from "./AutomationRelationValue";
import { automationValueTypeWrong, useAutomationResultProblem } from "./automationValueTypes";
import { VALUE_FORMULA_BOUNDS, type AutomationFormulaFault } from "./automationFormulaFault";
import type { PopoverRect } from "@/lib/popoverPlacement";
import { messageFormulaOf } from "./AutomationMessageField";
import { useGonePropertyName } from "./AutomationGoneProperty";
import { AUTOMATION_WORDS_TYPES, automationWordsOf } from "./AutomationWordsField";
import styles from "./AutomationEditProperty.module.css";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { pagesByIdOf } from "@/lib/pageIndex";
import { PhoneSheetSearch } from "@/components/shell/PhoneSheetSearch";

export { AutomationValuePopover } from "./AutomationValuePopover";

export const OPTION_TYPES = new Set(["select", "status", "multi_select"]);
const QUOTED_TYPES = new Set(["number", "title", "rich_text", "url", "email", "phone"]);
const UNBOUNDED = Number.MAX_SAFE_INTEGER;
// A value's formula is checked and computed within these (automationFormulaFault.ts).
export { VALUE_FORMULA_BOUNDS };
// Where a sentence's parts sit, each language's word order being its own.
const PROPERTY = "\u0001", VALUE = "\u0002";

// A formula's mark in its chip, by what the property it sets takes (Notion's, seen 2026-11-10): a date its
// calendar, a person, a checkbox and a relation their own; anything else ≡, a number too. These are Notion's
// own chip glyphs, not the property type marks (seen 2026-11-22 at 1440 dark, dpr 1, in the chips' DOM at
// 14px): viewCalendar, personCircleFill, an outlined checkmarkSquare, arrowDiagonalUpRight and textJustifyLeft.
const FORMULA_MARKS: Partial<Record<string, typeof TextJustifyStartIcon>> = {
  date: CalendarViewIcon, person: PersonCircleFillIcon, checkbox: CheckmarkSquareIcon, relation: RelationIcon,
};

/**
 * An edit-property action's value in its sentence: its options' pills, its
 * words in a grey one, or a formula as Notion's blue "[≡] 내 값" (seen
 * 2026-11-10), whatever it says.
 */
export function ValueChips({ action, property, surface }: {
  action: EditPropertyAutomationAction; property: DbProperty;
  /** Whose run it is, naming who runs it (AutomationPersonValue). */
  surface?: AutomationEditorSurface;
}) {
  const { t } = useTranslation("databaseView");
  const plain = (text: string, empty = false) => <span className={styles.valueChip} data-empty={empty || undefined}>{text}</span>;
  const value = action.value;
  if (value.kind === "formula") {
    const Mark = FORMULA_MARKS[property.type] ?? TextJustifyStartIcon;
    return <span className={styles.runChip} data-formula><Mark size="tiny" aria-hidden="true" />{t("editPropertyFormulaValue")}</span>;
  }
  if (value.kind === "execution_time" || value.kind === "execution_date") return <AutomationDateChip value={value} />;
  if (value.kind === "variable") return plain(value.name);
  if (value.kind === "trigger_property") return plain(value.propertyId);
  const literal = value.value;
  // People in a line's field: faces and names, who runs it and the page's creator in blue tags (AutomationPersonValue).
  if (property.type === "person") {
    const items = automationPeopleOf(value);
    return items.length ? <PersonValueChips items={items} surface={surface} small /> : plain(t("editPropertyEmptyValue"), true);
  }
  // Pages in a line's field: each by its icon and name, the page a run starts from in a blue tag (AutomationRelationValue).
  if (property.type === "relation") {
    const items = automationRelationItemsOf(value);
    return items.length ? <RelationValueChips items={items} property={property} small /> : plain(t("editPropertyEmptyValue"), true);
  }
  if (OPTION_TYPES.has(property.type)) {
    const chosen = automationChosenOptions(property, literal);
    if (!chosen.length) return plain(t("editPropertyEmptyValue"), true);
    return <>{chosen.map((option) => <span key={option.id} className={styles.pill} style={chipStyle(option.color)}
      data-status-pill={property.type === "status" ? "" : undefined}>{option.name}</span>)}</>;
  }
  if (literal === null || literal === undefined || literal === "" || (Array.isArray(literal) && !literal.length)) return plain(t("editPropertyEmptyValue"), true);
  // A checkbox's chip carries its box, ticked or not: "☑ checked", "☐ unchecked"
  // (Notion's squareSmall and checkmarkSquareSmall, seen 2026-11-19 in English).
  if (property.type === "checkbox") return <span className={styles.valueChip}>
    <CheckboxValueIcon checked={literal === true} size="tiny" aria-hidden="true" />{t(literal === true ? "trueValue" : "falseValue")}</span>;
  if (property.type === "verification") {
    const state = literal && typeof literal === "object" && "state" in literal ? String((literal as { state?: unknown }).state) : "";
    return plain(t(state === "verified" ? "verified" : "unverified"));
  }
  if (property.type === "date") return <AutomationDateChip value={value} />;
  if (Array.isArray(literal)) return plain(t("editPropertyCount", { count: literal.length }));
  if (typeof literal === "object") {
    const place = literal as { lat?: unknown; lon?: unknown };
    return plain([place.lat, place.lon].filter((part) => typeof part === "number").join(", "));
  }
  // A number or words typed are quoted in the sentence: "Amount 속성을 "99" 값으로 설정" (Notion's, seen 2026-10-24).
  if (typeof literal === "number" || (typeof literal === "string" && QUOTED_TYPES.has(property.type))) return <span>{`"${literal}"`}</span>;
  return plain(String(literal));
}

/**
 * An edit-property card's sentence, as Notion's (seen 2026-10-24 at 1280px
 * and 375px): "Stage 속성을 [Won] 값으로 설정", the property's name in
 * 500, its value's pills between the words, or "Stage 지우기" when it is
 * set to nothing; a property gone, "〈이름〉 속성 삭제됨" in red.
 */
export function EditPropertySentence({ action, property, properties = [], surface, fault }: {
  action: EditPropertyAutomationAction; property: DbProperty | undefined;
  /** What its formula reads, whose result's type the property must take. */
  properties?: readonly DbProperty[];
  surface?: AutomationEditorSurface;
  /** What its formula's editor names first in it (automationActionFormulaFault). */
  fault?: AutomationFormulaFault | null;
}) {
  const { t } = useTranslation("databaseView");
  const cardWords = usePersonCardWords(surface);
  const goneName = useGonePropertyName(action.propertyId);
  // A property deleted reads "〈이름〉 속성 삭제됨" in red, its mark red beside it (Notion's, seen 2026-11-11, which leaves
  // "{propertyName}" unfilled).
  if (!property) {
    return <span className={styles.gone} data-automation-warning="property">
      {goneName === null ? t("automationGoneProperty") : t("automationGonePropertyNamed", { name: goneName })}
    </span>;
  }
  // An empty value clears it: "Stage 지우기" (Notion's, seen 2026-10-24).
  const literal = action.value.kind === "literal" ? action.value.value : 0;
  const clears = literal === null || literal === undefined || literal === "" || (Array.isArray(literal) && !literal.length);
  // People, a relation's pages and a multi-select's options are put in its place, added, taken out or toggled: "담당자
  // 속성에 〈사람〉 및 [실행한 모든 사람] 추가", "태그 속성에서 [빨강] 및 [파랑] 토글" (Notion's, seen 2026-11-11), "관계
  // 속성을 [가] 및 [페이지 실행] 값으로 바꾸기" (2026-11-22). People are put so whatever gives them — a formula's too,
  // "사람 속성을 [내 값] 값으로 바꾸기", a variable's read alike (not seen) — while a relation's, a multi-select's and any other formula
  // is set, "… 값으로 설정" (Notion's, seen 2026-11-22 at 1440 dark: its cards' messages replaceWith and
  // relationProperty.single, the same in English).
  const people = property.type === "person" && action.value.kind === "literal";
  const pages = property.type === "relation" && action.value.kind === "literal" && !clears ? automationRelationItemsOf(action.value) : null;
  const options = property.type === "multi_select" && action.value.kind === "literal" && !clears
    ? automationChosenOptions(property, action.value.value) : null;
  const byMode = property.type === "person" || pages?.length || options;
  const sentence = t(clears ? "editPropertyClearSentence" : byMode ? `editPropertyPeopleSentence.${automationValueModeOf(action.value)}`
    : "editPropertySentence", { property: PROPERTY, value: VALUE });
  const parts = sentence.split(/(\u0001|\u0002)/u).filter(Boolean);
  const chosen = people && !clears ? automationPeopleOf(action.value) : null;
  const words = <span className={styles.sentence} data-valued={clears || people ? undefined : ""}
    data-people={chosen ? chosen.some((item) => typeof item === "string") ? "members" : "" : undefined}
    data-card-words={chosen ? cardWords(property.name, chosen) : undefined}>
    {parts.map((part, index) => part === PROPERTY ? <strong key={index}>{property.name}</strong>
      : part === VALUE ? chosen ? <PersonValueChips key={index} items={chosen} surface={surface} />
        : pages?.length ? <RelationValueChips key={index} items={pages} property={property} />
        : options?.length ? <OptionValueChips key={index} options={options} />
        : <ValueChips key={index} action={action} property={property} />
        : <span key={index}>{part}</span>)}
  </span>;
  // A formula of a type the property can't take says so in red under it (Notion's, seen 2026-11-10), one that
  // can't be read that it can't, and one naming what it doesn't know a type error too (seen 2026-11-21).
  if (!fault && !automationValueTypeWrong(action.value, property, properties)) return words;
  return <span className={styles.sentenceWrong}>{words}
    <span className={styles.typeWrong} data-automation-warning={fault?.parse ? "formula-parse" : "formula-type"}>
      {t(fault?.parse ? "actionProblems.formulaParse" : "actionProblems.formulaType")}</span></span>;
}

/**
 * The properties "속성 편집..." sets, as Notion's (seen 2026-10-24): on a
 * desktop a page of the action list — "← 속성 편집", "속성을 검색하세요." and
 * each property with "›"; on a phone a screen of its own.
 */
export function AutomationPropertyPick({ properties, onPick, onBack, onClose }: {
  properties: DbProperty[];
  onPick(property: DbProperty): void;
  onBack(): void;
  onClose(): void;
}) {
  const { t } = useTranslation(["databaseView", "common"]);
  const phone = useContext(AutomationPhoneSheets);
  const [query, setQuery] = useState("");
  const shown = properties.filter((property) => property.name.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()));
  const search = phone ? <PhoneSheetSearch value={query} autoFocus={fieldFocusOnOpen()} aria-label={t("databaseView:editPropertySearch")}
    placeholder={t("databaseView:editPropertySearch")} onValueChange={setQuery} /> : <input className={styles.search} value={query} autoFocus={fieldFocusOnOpen()}
    aria-label={t("databaseView:editPropertySearch")} placeholder={t("databaseView:editPropertySearch")} onChange={(event) => setQuery(event.currentTarget.value)} />;
  if (phone) {
    return <PhoneSheet size="screen" title={t("databaseView:actionTypes.edit_property")} onClose={onClose}
      start={<PhoneSheetHeadButton label={t("common:actions.back")} onPress={onBack}><ArrowLeft size="regular" aria-hidden="true" /></PhoneSheetHeadButton>}
      action={{ label: t("common:actions.done"), onPress: onClose }} data={{ "data-automation-property-pick": "" }}>
      <div className={sheetStyles.stack}>
        <PhoneSheetGroup>{search}</PhoneSheetGroup>
        <PhoneSheetGroup>
          {shown.map((property) => <PhoneSheetRow key={property.id} icon={<PropertyIcon property={property} size="regular" />} label={property.name}
            data={{ "data-automation-property": property.id }} onPress={() => onPick(property)} />)}
        </PhoneSheetGroup>
      </div>
    </PhoneSheet>;
  }
  return <div className={styles.pick} data-automation-property-pick>
    <button type="button" className={styles.pickBack} onClick={onBack}>
      <ArrowLeft size="small" aria-hidden="true" />{t("databaseView:actionTypes.edit_property")}
    </button>
    {search}
    {shown.map((property) => <button key={property.id} type="button" className={styles.pickRow} data-automation-property={property.id}
      onClick={() => onPick(property)}>
      <span className={styles.pickIcon} aria-hidden="true"><AutomationPropertyMark property={property} /></span>
      <span className={styles.pickName}>{property.name}</span><ChevronRight size="small" aria-hidden="true" />
    </button>)}
  </div>;
}

/**
 * An edit-property action's value written as a formula, in the formula
 * editor: Σ makes the words written so far one, as Notion's does; an empty
 * one sets nothing. Where it says what the property can't take
 * (automationValueTypes.ts).
 */
export function AutomationValueFormula({ action, properties, pages, names, anchor, above, keepOpen, onChange, onClose }: {
  action: EditPropertyAutomationAction; properties: DbProperty[]; pages: Page[];
  /** What it runs with, as its action's (automationFormulaNames). */
  names: readonly FormulaScopeName[];
  /** What it hangs from on a desktop: its card, or the value window (FormulaEditorDialog). */
  anchor?: PopoverRect | null;
  above?: boolean;
  /** "저장" leaves it open, as from Σ in the value window's words (FormulaEditorDialog). */
  keepOpen?: boolean;
  onChange(action: AutomationAction): void;
  onClose(): void;
}) {
  const { t } = useTranslation(["databaseView", "common"]);
  const property = properties.find((candidate) => candidate.id === action.propertyId);
  const title = t("databaseView:editPropertyTitle", { property: property?.name ?? "" });
  const pagesById = pagesByIdOf(pages);
  const expression = action.value.kind === "formula" ? action.value.expression : "";
  const formulaStart = expression || (property && AUTOMATION_WORDS_TYPES.has(property.type) ? messageFormulaOf(automationWordsOf(action.value)) : "");
  const formulaProp = useMemo((): DbProperty => ({ id: `automation-value-${action.id}`, databaseId: property?.databaseId ?? "",
    name: title, type: "formula", config: { formula: formulaStart }, position: UNBOUNDED }), [action.id, property?.databaseId, title, formulaStart]);
  const resultProblem = useAutomationResultProblem(property, properties, names);
  return <FormulaEditorDialog prop={formulaProp} props={properties} pagesById={pagesById} bounds={VALUE_FORMULA_BOUNDS} names={names}
    anchor={anchor} above={above} fresh={action.value.kind !== "formula"} keepOpen={keepOpen} resultProblem={resultProblem} onSave={(next) => onChange({ ...action, value: next.trim() ? { kind: "formula", expression: next.trim() } : { kind: "literal", value: null } })}
    onClose={onClose} />;
}

/**
 * Where an edit-property action's value is set, as Notion's (seen
 * 2026-10-24): "Stage 속성 설정:" and a blue "완료" at its head ("←" back to
 * the properties right after choosing one), the value — a select's options,
 * any other property's own field — then "Σ 사용자 지정 수식 작성 ›", which
 * sets it to a formula. On a desktop it opens under its card; on a phone a
 * screen of its own. Its formula editor closing closes it too, its card then
 * saying "내 값" (Notion's, seen 2026-11-10); a formula's card opens that
 * editor alone (AutomationActionEditor).
 */
export function AutomationEditPropertyEditor({ action, number, properties, pages, surface, variables = [], headless, lineHead, onBack, onChange, onDone }: {
  action: EditPropertyAutomationAction;
  number: number;
  properties: DbProperty[];
  pages: Page[];
  surface: AutomationEditorSurface;
  variables?: AutomationVariableList;
  /** Without "… 속성 설정: 완료" over it, as a page edit's value window (Notion's, seen 2026-11-05). */
  headless?: boolean;
  /** "속성 설정" alone over it, as a page line's checkbox (Notion's, seen 2026-11-10). */
  lineHead?: boolean;
  /** Back to the properties, right after one was chosen. */
  onBack?(): void;
  onChange(action: AutomationAction): void;
  onDone(): void;
}) {
  const { t } = useTranslation(["databaseView", "common"]);
  const phone = useContext(AutomationPhoneSheets);
  // Its formula's editor, open: on a desktop it hangs from the value window — under it from Σ in words, its "저장"
  // leaving it open there, over it from the "Σ … ›" line (Notion's, seen 2026-11-10).
  const [formula, setFormula] = useState<{ anchor: DOMRect | null; above: boolean; keep: boolean } | null>(null);
  const panel = useRef<HTMLDivElement>(null);
  const openFormula = (above: boolean, keep = false) => {
    const held = phone ? null : panel.current?.closest<HTMLElement>("[data-automation-value-popover]") ?? panel.current;
    setFormula({ anchor: held?.getBoundingClientRect() ?? null, above, keep });
  };
  const property = properties.find((candidate) => candidate.id === action.propertyId);
  // A person's, a multi-select's and a relation's head names it to edit, "← 담당자 편집 완료", "← 태그 편집 완료" (Notion's,
  // seen 2026-11-11), "← 관계 편집 완료" (2026-11-22).
  const title = t(property?.type === "person" || property?.type === "multi_select" || property?.type === "relation" ? "databaseView:editPropertyPeopleTitle"
    : "databaseView:editPropertyTitle", { property: property?.name ?? "" });
  const names = useMemo(() => automationFormulaNames(t, { surface, variables, properties }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [t, surface, automationVariablesKey(variables), properties]);
  if (!property) return null;
  const line = Boolean(headless || lineHead);
  const value = OPTION_TYPES.has(property.type) ? <AutomationOptionValues action={action} property={property} phone={phone} line={line}
    onChange={onChange} onDone={onDone} />
    : property.type === "checkbox" ? <AutomationCheckboxValues action={action} label={property.name} phone={phone} onChange={onChange} onDone={onDone} />
    : property.type === "date" && phone ? <AutomationDateValue action={action} onChange={onChange} onDone={onDone} />
    : property.type === "person" ? <AutomationPersonValues action={action} surface={surface} line={line} onChange={onChange} />
    : property.type === "relation" ? <AutomationRelationValues action={action} property={property} surface={surface} line={line} onChange={onChange} />
    : <div className={phone ? styles.phoneValue : styles.field}>
      <AutomationPropertyValueField action={action} number={number} properties={properties} onChange={onChange}
        words={{ names, properties, onFormula: () => openFormula(false, true) }} />
    </div>;
  // Saved or not, the formula's editor closing closes the value with it (Notion's, seen 2026-11-10).
  const dialog = formula ? <AutomationValueFormula action={action} properties={properties} pages={pages} names={names} onChange={onChange}
    anchor={formula.anchor} above={formula.above} keepOpen={formula.keep} onClose={() => { setFormula(null); onDone(); }} /> : null;
  // On a phone a screen of its own, as Notion's (seen 2026-10-24 at 375px): "완료" alone at its head, the value in flat rows,
  // "Σ 사용자 지정 수식 작성 ›" at its foot.
  if (phone) {
    return <PhoneSheet size="screen" title="" label={title} onClose={onDone} action={{ label: t("common:actions.done"), onPress: onDone }}
      data={{ "data-automation-value-editor": action.id }}>
      <div className={`${sheetStyles.stack} ${styles.phoneStack}`}>
        {OPTION_TYPES.has(property.type) ? value : <PhoneSheetGroup>{value}</PhoneSheetGroup>}
      </div>
      <div className={styles.phoneFormula}>
        <PhoneSheetRow icon={<span aria-hidden="true">Σ</span>} label={t("databaseView:editPropertyFormula")} onPress={() => openFormula(false)}
          data={{ "data-automation-value-formula-open": "" }} />
      </div>
      {dialog}
    </PhoneSheet>;
  }
  const formulaRow = <button type="button" className={styles.formulaRow} data-automation-value-formula-open onClick={() => openFormula(true)}>
    <span aria-hidden="true">Σ</span><span>{t("databaseView:editPropertyFormula")}</span><ChevronRight size="small" aria-hidden="true" />
  </button>;
  const head = headless ? null : lineHead
    ? <div className={`${styles.editorHead} ${styles.lineHead}`}><span className={styles.editorTitle}>{t("databaseView:automationValueHead")}</span></div>
    : <div className={styles.editorHead}>
    {onBack ? <button type="button" className={styles.back} aria-label={t("databaseView:editPropertyBack")} onClick={onBack}>
      <ArrowLeft size="small" aria-hidden="true" /></button> : null}
    <span className={styles.editorTitle}>{title}</span>
    <button type="button" className={styles.done} onClick={onDone}>{t("common:actions.done")}</button>
  </div>;
  // A date's values are Notion's list, its formula's line among them and its days a page of their own (AutomationDateValue).
  const body = property.type === "date" ? <AutomationDateValue action={action} head={head} formulaRow={formulaRow}
    onChange={onChange} onDone={onDone} />
    // Words take Σ in their field, with no line of its own (Notion's, seen 2026-11-10 on a title and a text); a checkbox's
    // list is under a line, as a menu's.
    : <>{head}{head && property.type === "checkbox" ? <span className={styles.rule} role="separator" /> : null}{value}
      {AUTOMATION_WORDS_TYPES.has(property.type) ? null : <div className={styles.foot}><span className={styles.rule} role="separator" />{formulaRow}</div>}</>;
  return <div ref={panel} className={styles.editor} data-automation-value-editor={action.id} data-automation-value-window
    onKeyDown={(event) => { if (event.key === "Escape" && !formula) { event.stopPropagation(); onDone(); } }}>
    {body}
    {dialog}
  </div>;
}
