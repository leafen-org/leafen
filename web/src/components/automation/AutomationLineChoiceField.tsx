import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown } from "@/icons/leafen";
import type { AutomationEditorSurface } from "./automationActionModel";
import type { AutomationValueExpression, DbProperty, EditPropertyAutomationAction, Page } from "@/lib/types";
import { AutomationChoicePopover } from "./AutomationChoicePopover";
import { AutomationEditPropertyEditor, AutomationValuePopover, OPTION_TYPES, ValueChips } from "./AutomationEditProperty";
import { AUTOMATION_VALUE_MODES, automationValueModeOf, type AutomationValueMode } from "./AutomationPersonValue";
import type { AutomationVariableList } from "./automationFormulaNames";
import styles from "./automationActionEditor.module.css";
import lineStyles from "./AutomationAddPage.module.css";

/** The properties a page line chooses its value for in a window: options, a checkbox, people and a relation's pages. */
export const AUTOMATION_LINE_CHOICE_TYPES = new Set([...OPTION_TYPES, "checkbox", "person", "relation"]);

/**
 * A page line's value chosen in a window, as Notion's "페이지 추가 위치" and
 * "페이지 편집 위치" lines (seen 2026-11-05, 2026-11-10): a 28px "선택… ⌄"
 * field — a select's pills, a checkbox's box once set — under which the value
 * window opens: a select's options with no head, a checkbox's "체크 표시되지
 * 않음"·"체크 표시됨" under "속성 설정", each closing it as it is picked,
 * people under "속성 설정" (AutomationPersonValue) and a relation's pages so too
 * (AutomationRelationValue — the card's window, its line not seen), a multi-select's options
 * by their boxes (AutomationOptionValues), and "Σ 사용자 지정 수식 작성 ›".
 * A button's own row asks it with its full head.
 */
export function AutomationLineChoiceField({ id, propertyId, value, number, properties, pages, surface, variables, head, align, modes = false, onChange }: {
  /** Whose value it is, for its draft. */
  id: string;
  propertyId: string;
  value: AutomationValueExpression;
  number: number;
  properties: DbProperty[];
  pages: Page[];
  surface?: AutomationEditorSurface;
  variables: AutomationVariableList;
  /** "… 속성 설정: 완료" over the value, as a button's row asks it; otherwise its kind's own. */
  head?: "full";
  align?: "end";
  /** People or a multi-select's options met with what the page holds: "새로운 값 ⌄" before the field, as a page edit's line
   * (Notion's, seen 2026-11-11). */
  modes?: boolean;
  onChange(value: AutomationValueExpression): void;
}) {
  const { t } = useTranslation("databaseView");
  const [anchor, setAnchor] = useState<DOMRect | null>(null);
  const property = properties.find((candidate) => candidate.id === propertyId);
  const draft: EditPropertyAutomationAction = { id, type: "edit_property", target: "trigger_page", propertyId, value };
  const literal = value.kind === "literal" ? value.value : null;
  const checkbox = property?.type === "checkbox";
  // A checkbox's and people's window says "속성 설정" over them (Notion's, seen 2026-11-10, 2026-11-11), a relation's as people's.
  const headed = checkbox || property?.type === "person" || property?.type === "relation";
  // A checkbox set either way shows its box, unchecked too; "선택…" only while nothing is set (Notion's, seen 2026-11-10).
  const empty = checkbox ? typeof literal !== "boolean"
    : literal === null || literal === undefined || literal === "" || (Array.isArray(literal) && !literal.length);
  const field = <>
    <button type="button" className={styles.changeValue} aria-label={t("valueForAction", { number })} aria-haspopup="dialog" aria-expanded={Boolean(anchor)}
      data-automation-change-value onClick={(event) => setAnchor(event.currentTarget.getBoundingClientRect())}>
      <span>{empty || !property ? <span className={styles.changePlaceholder}>{t("automationChooseValue")}</span>
        : checkbox ? <input type="checkbox" checked={literal === true} readOnly tabIndex={-1} aria-hidden="true" />
        : <ValueChips action={draft} property={property} surface={surface} />}</span>
      <ChevronDown size="tiny" aria-hidden="true" />
    </button>
    {anchor ? <AutomationValuePopover anchor={anchor} align={align} line onClose={() => setAnchor(null)}>
      <AutomationEditPropertyEditor action={draft} number={number} properties={properties} pages={pages} surface={surface ?? "page_button"} variables={variables}
        headless={head !== "full" && !headed} lineHead={head !== "full" && headed}
        onChange={(next) => { if (next.type === "edit_property") onChange(next.value); }}
        onDone={() => setAnchor(null)} />
    </AutomationValuePopover> : null}
  </>;
  if (!modes || (property?.type !== "person" && property?.type !== "multi_select" && property?.type !== "relation") || value.kind !== "literal") return field;
  return <span className={lineStyles.people}>
    <LineModeField value={value} onChange={onChange} />
    {field}
  </span>;
}

/**
 * How a page edit's people or options meet what the page holds, as Notion's
 * line (seen 2026-11-11): a 120px ringed "새로운 값 ⌄" whose menu — 새로운
 * 값, 추가, 제거, 토글, the one set ticked — opens right under it at its end,
 * its first row lit.
 */
function LineModeField({ value, onChange }: {
  value: Extract<AutomationValueExpression, { kind: "literal" }>; onChange(value: AutomationValueExpression): void;
}) {
  const { t } = useTranslation("databaseView");
  const [open, setOpen] = useState(false);
  const button = useRef<HTMLButtonElement>(null);
  const mode = automationValueModeOf(value);
  return <>
    <button ref={button} type="button" className={lineStyles.mode} aria-haspopup="menu" aria-expanded={open} data-automation-change-mode={mode}
      onClick={() => setOpen((current) => !current)}>
      <span>{t(`lineModes.${mode}`)}</span><ChevronDown size="small" aria-hidden="true" />
    </button>
    {open ? <AutomationChoicePopover label={t(`lineModes.${mode}`)} width={250} align="end" flush trigger={button.current}
      choices={AUTOMATION_VALUE_MODES.map((key) => ({ key, text: t(`lineModes.${key}`), chosen: key === mode }))}
      onPick={(key) => {
        const next = { ...value };
        delete next.mode;
        if (key !== "replace") next.mode = key as Exclude<AutomationValueMode, "replace">;
        onChange(next);
      }}
      onClose={() => setOpen(false)} /> : null}
  </>;
}
