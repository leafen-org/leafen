import { type RefObject, useId, useLayoutEffect, useMemo, useRef, useState } from "react";
import { CaretPickerOption, useCaretPicker } from "@/lib/caretPicker";
import { useTranslation } from "react-i18next";
import type { DbProperty, PropertyType } from "@/lib/types";
import { copyText } from "@/lib/copyText";
import { Copy } from "@/components/icons";
import { ReturnKeyIcon } from "@/icons/leafen";
import { QuestionCircleIcon } from "@/icons/leafen/automationIcons";
import { PhoneSheet, PhoneSheetGroup } from "@/components/shell/PhoneSheet";
import { PropertyIcon } from "./PropertyIcon";
import { PropertyTypeIcon } from "./PropertyTypeIcon";
import { evaluateFormulaExpressionResult, FormulaDate, FormulaRichText } from "./formulaCore";
import { formulaDateText } from "./formulaDateText";
import { formulaLocale } from "./formulaLocales";
import { formulaTimeZone } from "./formulaDates";
import { formatFormulaValue } from "./formulaValues";
import { FORMULA_EXAMPLE_SAMPLES, FORMULA_SIGNATURES, type FormulaCatalogEntry, type FormulaReturnType } from "./formulaFunctionCatalog";
import { FORMULA_PROPERTY_HELP_KINDS, formulaPieces, formulaPropertyExamples, type FormulaScopeName, type FormulaSuggestionGroup } from "./formulaEditorModel";
import { localizedPropertyTypeLabel } from "./propertyTypes";
import codeStyles from "./FormulaCode.module.css";
import { FORMULA_CODE, formulaTokenClass } from "./formulaTokenTheme";
import styles from "./FormulaEditor.module.css";
import phoneStyles from "./FormulaEditorPhone.module.css";

// The formula editor's help, as Notion's: the suggestions for the name being
// typed by group, each marked with the type it gives, and for the chosen one
// what it does and examples with their results.

export type FormulaHelperItem =
  | { kind: "name"; name: FormulaScopeName }
  | { kind: "property"; property: DbProperty }
  | { kind: "entry"; entry: FormulaCatalogEntry };

function formulaHelperItems(groups: FormulaSuggestionGroup[]): FormulaHelperItem[] {
  return groups.flatMap((group): FormulaHelperItem[] => ("names" in group
    ? group.names.map((name) => ({ kind: "name", name }))
    : "properties" in group
    ? group.properties.map((property) => ({ kind: "property", property }))
    : group.entries.map((entry) => ({ kind: "entry", entry }))));
}

function formulaHelperItemKey(item: FormulaHelperItem) {
  return item.kind === "name" ? `name:${item.name.name}`
    : item.kind === "property" ? `property:${item.property.id}` : `entry:${item.entry.group}:${item.entry.key}`;
}

const helperItemLabel = (item: FormulaHelperItem) =>
  item.kind === "name" ? item.name.label : item.kind === "property" ? item.property.name : item.entry.label;

const RETURN_ICON: Record<FormulaReturnType, PropertyType> = {
  number: "number", text: "rich_text", boolean: "checkbox", date: "date", list: "multi_select", person: "person", any: "formula",
};

export function FormulaEditorHelper({ groups, names = [], edit, ownerRef, phone, onCaretReady, onInsert }: {
  groups: FormulaSuggestionGroup[];
  /**
   * On a phone the suggestions alone, across the sheet, each with a "?" at its end that opens its help as a sheet
   * of its own, "정의" with a blue "완료" (Notion's, seen 2026-11-14 at 402pt): flat on the sheet's grey where the
   * editor is a sheet of its own, in white 12px-round groups 16px in where it opened over another sheet.
   */
  phone?: "flat" | "grouped";
  /** The names the formula runs with, written as chips in their examples. */
  names?: readonly FormulaScopeName[];
  // edit changes with every edit and caret move; as Notion's, the first
  // suggestion is chosen again then.
  edit: string; ownerRef: RefObject<HTMLElement | null>;
  onCaretReady(handler: ((event: KeyboardEvent) => void) | null): void;
  onInsert: (item: FormulaHelperItem) => void;
}) {
  const { t } = useTranslation(["formulaEditor", "common"]);
  const items = useMemo(() => formulaHelperItems(groups), [groups]), listRef = useRef<HTMLDivElement>(null), id = useId();
  const picker = useCaretPicker({ items, query: edit, id, label: t("helperToggle"), listRef, ownerRef,
    keyOf: formulaHelperItemKey, textOf: helperItemLabel, pointerTakesAtOnce: true,
    onPick: onInsert, onClose: () => {} });
  const selected = items[picker.active] ?? null;
  const [defined, setDefined] = useState<FormulaHelperItem | null>(null);
  useLayoutEffect(() => { onCaretReady(picker.onCaretKeyDown); return () => onCaretReady(null); }, [picker.onCaretKeyDown, onCaretReady]);
  if (phone) return (
    <div {...picker.listBoxProps} ref={listRef} className={phoneStyles.list} data-formula-phone-list={phone}>
      {groups.map((group, index) => (
        <div key={group.group} role="group" aria-label={t(`groups.${group.group}`)} className={phoneStyles.section}>
          {group.group === "properties" && groups[index - 1]?.group === "names" ? null
            : <div className={phoneStyles.groupLabel}>{t(`groups.${group.group}`)}</div>}
          <div className={phoneStyles.group}>
            {formulaHelperItems([group]).map((item) => {
              const key = formulaHelperItemKey(item);
              return (
                <div key={key} className={phoneStyles.row}>
                  <CaretPickerOption state={picker.state} itemKey={key} className={phoneStyles.rowPick}>
                    {({ labelProps }) => <>
                    <span className={phoneStyles.rowIcon} aria-hidden="true">
                      {item.kind === "property" ? <PropertyIcon size="small" property={item.property} />
                        : <PropertyTypeIcon size="small" type={item.kind === "name" ? item.name.type : RETURN_ICON[item.entry.returns]} />}
                    </span>
                    <span {...labelProps} className={phoneStyles.rowLabel}>{helperItemLabel(item)}</span>
                    </>}
                  </CaretPickerOption>
                  {/* A place has no help in Notion's (seen 2026-11-03), so no "?" either. */}
                  {item.kind === "property" && item.property.type === "place" ? null : (
                    <button type="button" className={phoneStyles.rowHelp} aria-label={t("showDefinition")} aria-haspopup="dialog"
                      onMouseDown={(event) => event.preventDefault()} onClick={() => setDefined(item)}>
                      <QuestionCircleIcon aria-hidden="true" />
                    </button>
                  )}
                </div>
              );
            })}
          </div>
        </div>
      ))}
      {defined ? (
        <PhoneSheet title={t("definition")} onClose={() => setDefined(null)}
          action={{ label: t("common:actions.done"), onPress: () => setDefined(null) }}>
          <PhoneSheetGroup><div className={phoneStyles.definition}><FormulaHelperDetail item={defined} names={names} /></div></PhoneSheetGroup>
        </PhoneSheet>
      ) : null}
    </div>
  );
  return (
    <div className={styles.helper}>
      <div {...picker.listBoxProps} ref={listRef} className={styles.helperList}>
        {groups.map((group, index) => (
          <div key={group.group} role="group" aria-label={t(`groups.${group.group}`)}>
            {/* The names and the properties under one "Properties", as Notion lists an automation's. */}
            {group.group === "properties" && groups[index - 1]?.group === "names" ? null
              : <div className={styles.helperGroup}>{t(`groups.${group.group}`)}</div>}
            {formulaHelperItems([group]).map((item) => {
              const key = formulaHelperItemKey(item);
              return (
                <CaretPickerOption key={key} state={picker.state} itemKey={key} className={styles.helperItem}>
                  {({ active, labelProps }) => <>
                  <span className={styles.helperIcon} aria-hidden="true">
                    {item.kind === "property" ? <PropertyIcon size="small" property={item.property} />
                      : <PropertyTypeIcon size="small" type={item.kind === "name" ? item.name.type : RETURN_ICON[item.entry.returns]} />}
                  </span>
                  <span {...labelProps} className={styles.helperLabel}>{helperItemLabel(item)}</span>
                  {active ? <ReturnKeyIcon size="tiny" className={styles.helperEnter} aria-hidden="true" /> : null}
                  </>}
                </CaretPickerOption>
              );
            })}
          </div>
        ))}
      </div>
      <div className={styles.helperDetail}>{selected ? <FormulaHelperDetail item={selected} names={names} /> : null}</div>
    </div>
  );
}

function FormulaHelperDetail({ item, names }: { item: FormulaHelperItem; names: readonly FormulaScopeName[] }) {
  const { t } = useTranslation("formulaEditor");
  if (item.kind === "name") {
    const { name } = item;
    return (
      <>
        <div className={styles.detailTitle}>
          <PropertyTypeIcon size="small" type={name.type} /> {name.label}
        </div>
        <p className={styles.detailText}>{name.description}</p>
        <div className={styles.examples}>{(name.examples ?? [name.name]).map((example) => (
          <FormulaExample key={example} expression={example} evaluate={false} chips={NO_CHIPS} names={names} />
        ))}</div>
      </>
    );
  }
  if (item.kind === "property") {
    const { property } = item;
    // A place has no help in Notion's (seen 2026-11-03).
    if (property.type === "place") return null;
    // The property shows with its kind's mark; an example's sample names as plain chips.
    const chips = (name: string) => ({ name, type: name === property.name ? property.type : "" as const });
    return (
      <>
        <div className={styles.detailTitle}>
          <PropertyIcon size="small" property={property} /> {property.name}
        </div>
        <p className={styles.detailText}>{FORMULA_PROPERTY_HELP_KINDS.has(property.type)
          ? t(`propertyHelp.${property.type}`) : t("propertyDescription", { type: localizedPropertyTypeLabel(property.type) })}</p>
        <div className={styles.examples}>{[`prop(${JSON.stringify(property.name)})`, ...formulaPropertyExamples(property)].map((example) => (
          <FormulaExample key={example} expression={example} evaluate={false} chips={chips} />
        ))}</div>
      </>
    );
  }
  const { entry } = item;
  const description = entry.group === "builtins" ? t(`operators.${entry.key}`) : t(`functions.${entry.key}`);
  const title = entry.group === "builtins" ? entry.label : `${entry.key}(${FORMULA_SIGNATURES[entry.key] ?? ""})`;
  return (
    <>
      <div className={styles.detailTitle}>
        <PropertyTypeIcon size="small" type={RETURN_ICON[entry.returns]} /> {title}
      </div>
      {/* Keyed by place: a description's blank lines (formatNumber's) are alike. */}
      {description.split("\n").map((line, index) => <p key={index} className={styles.detailText}>{line}</p>)}
      <div className={styles.examples}>{entry.examples.map((example) => (
        <FormulaExample key={example} expression={example} evaluate={!entry.bare} chips={SAMPLE_CHIPS} />
      ))}</div>
    </>
  );
}

const NO_CHIPS = () => undefined;
// An example's properties are plain chips, as Notion's help writes them.
const SAMPLE_CHIPS = (name: string) => ({ name, type: "" as const });

function sampleProperty(name: string) {
  if (!(name in FORMULA_EXAMPLE_SAMPLES)) throw new Error(`No sample for ${name}`);
  return FORMULA_EXAMPLE_SAMPLES[name];
}

function FormulaExample({ expression, evaluate, chips, names }: {
  expression: string; evaluate: boolean; chips?: (name: string) => { name: string; type: PropertyType | "" } | undefined;
  names?: readonly FormulaScopeName[];
}) {
  const { t } = useTranslation("formulaEditor");
  const result = useMemo(() => {
    if (!evaluate) return "";
    const outcome = evaluateFormulaExpressionResult(expression, sampleProperty);
    if (outcome.error) return "";
    const viewer = { timeZone: formulaTimeZone(undefined), locale: formulaLocale() };
    // As Notion's help writes a result: text quoted, a date as a mention, a
    // list in brackets, a number whole (sqrt(7) = 2.6457513110645907),
    // nothing as "empty".
    const shown = (value: typeof outcome.value): string => value === null || value === "" ? "empty"
      : typeof value === "number" && Number.isFinite(value) ? String(value)
      : value instanceof FormulaDate ? `@${formulaDateText(value, viewer)}`
      : Array.isArray(value) ? `[${value.map(shown).join(", ")}]`
      : typeof value === "string" || value instanceof FormulaRichText ? JSON.stringify(formatFormulaValue(value))
      : formatFormulaValue(value);
    const result = shown(outcome.value);
    // A result that only repeats the example is left out (true).
    return result === expression ? "" : result;
  }, [evaluate, expression]);
  const pieces = useMemo(() => (chips ? formulaPieces(expression, chips, names) : [{ text: expression }]), [chips, expression, names]);
  return (
    <div className={styles.example}>
      <code className={FORMULA_CODE}>
        {pieces.map((piece, index) => piece.chip ? (
          <span key={index} className={codeStyles.chip}>
            {piece.chip.type ? <PropertyTypeIcon size="tiny" type={piece.chip.type as PropertyType} /> : null}{piece.chip.name}
          </span>
        ) : piece.kind ? <span key={index} className={formulaTokenClass(piece.kind)}>{piece.text}</span> : piece.text)}
        {result ? `\n= ${result}` : null}
      </code>
      <button type="button" className={styles.exampleCopy} aria-label={t("copy")} onClick={() => void copyText(expression)}>
        <Copy size="tiny" aria-hidden="true" />
      </button>
    </div>
  );
}
