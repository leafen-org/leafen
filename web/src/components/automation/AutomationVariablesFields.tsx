import { Fragment, useContext, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactElement } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ChevronDown, Copy, FormulaIcon, Pencil, Trash } from "@/icons/leafen";
import { WarningFillIcon } from "@/icons/leafen/automationIcons";
import { TextPropertyIcon } from "@/icons/leafen/databaseIcons";
import type { AutomationAction, DbProperty, Page, TextSpan } from "@/lib/types";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { FormulaEditorDialog } from "@/components/database/FormulaEditorDialog";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { placeAround, popoverAnchorOf, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import { PhoneSheetMenu, PhoneSheetRow } from "../shell/PhoneSheet";
import { AutomationPhoneSheets } from "./AutomationActionMenus";
import { VALUE_FORMULA_BOUNDS, automationValueFormulaWords } from "./automationFormulaFault";
import { AutomationFormulaField, AutomationWordsField } from "./AutomationWordsField";
import { automationFormulaWords } from "./automationFormulaWords";
import { freshAutomationVariableName } from "./automationVariableNames";
import { messageFormulaOf } from "./AutomationMessageField";
import { automationFormulaNames, automationVariablesKey, type AutomationVariableList } from "./automationFormulaNames";
import { AutomationVariableMark, automationVariableKind, automationVariableKindWords } from "./automationVariableKind";
import type { AutomationEditorSurface } from "./automationActionModel";
import fieldStyles from "./AutomationWebhookVariables.module.css";
import styles from "./AutomationVariableRow.module.css";
import lineStyles from "./AutomationAddPage.module.css";
import { pagesByIdOf } from "@/lib/pageIndex";

type VariablesAction = Extract<AutomationAction, { type: "define_variables" }>;
type Variable = VariablesAction["variables"][number];
// A choice that asks first ("텍스트로 편집" of a formula) keeps the menu open behind its question, as Notion's (seen 2026-11-21).
type Choice = { key: "text" | "formula" | "duplicate" | "delete"; label: string; icon: ReactElement; danger?: boolean; asks?: boolean };

/**
 * Words cleared — erased, or by "변수 지우기" — are no value, of a type that
 * can't be told, where a new variable's empty words are text (Notion's, seen
 * 2026-11-21: "?" and "유형: 알 수 없음").
 */
function clearedWords(value: string, spans?: TextSpan[]): Variable["value"] {
  return !value && !spans ? { kind: "literal", value: null } : { kind: "literal", value, ...(spans ? { spans } : {}) };
}

/** A text variable's words, with any mentions in them. */
function wordsOf(variable: Variable): TextSpan[] {
  if (variable.value.kind !== "literal") return [];
  if (variable.value.spans?.length) return variable.value.spans;
  return typeof variable.value.value === "string" && variable.value.value ? [{ text: variable.value.value }] : [];
}

/**
 * A "변수 정의" card's variables, as Notion's (seen 2026-10-24 at 1280px):
 * each a line of its kind's mark, its name and "⌄" over its value — words
 * in a soft field, "@" putting mentions in them as a message's do (seen
 * 2026-10-25), or a formula in the same field with a blue Σ, "텍스트로 편집"
 * (seen 2026-11-21) — and "다른 변수 정의..." under them. The line's menu
 * names the variable and turns it to words or a formula, copies or deletes
 * it, and says its kind at its foot.
 */
export function AutomationVariablesFields({ action, number, surface, properties, pages, variables, variableNames, onChange }: {
  action: VariablesAction;
  number: number;
  surface: AutomationEditorSurface;
  properties: DbProperty[];
  pages: Page[];
  variables: AutomationVariableList;
  /** Every variable name the automation's actions define, which a new one's number comes after (freshAutomationVariableName). */
  variableNames: readonly string[];
  onChange(action: VariablesAction): void;
}) {
  const { t } = useTranslation(["databaseView", "formulaEditor"]);
  const [menu, setMenu] = useState<number | null>(null);
  const [formula, setFormula] = useState<number | null>(null);
  // The variable whose formula "전환" just made words, from its menu or its field's Σ: its field takes the caret at its end
  // (Notion's, seen 2026-11-21 twice from the menu, a letter typed after the press; from Σ too).
  const [converted, setConverted] = useState<number | null>(null);
  const triggers = useRef<Array<HTMLButtonElement | null>>([]);
  const names = useMemo(() => automationFormulaNames(t, { surface, variables, properties }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [t, surface, automationVariablesKey(variables), properties]);
  const pagesById = pagesByIdOf(pages);
  const nameOf = (count: number) => t("variableDefaultName", { number: count });
  const set = (index: number, next: Variable) => onChange({ ...action, variables: action.variables.map((variable, at) => at === index ? next : variable) });
  const editing = formula === null ? undefined : action.variables[formula];
  // Σ makes the words written so far a formula, as Notion's does.
  const expression = !editing ? "" : editing.value.kind === "formula" ? editing.value.expression : messageFormulaOf(wordsOf(editing));
  const formulaProp = useMemo((): DbProperty => ({ id: `automation-variable-${action.id}-${formula ?? 0}`, databaseId: properties[0]?.databaseId ?? "",
    name: editing?.name ?? "", type: "formula", config: { formula: expression }, position: Number.MAX_SAFE_INTEGER }),
  [action.id, formula, properties, editing?.name, expression]);
  // Σ from words opens with them as its formula, which closing — "완료", Escape or a press outside — keeps unchanged too
  // (FormulaEditorDialog's `fresh`; Notion's, seen 2026-11-21 twice: "b" became `"b"` with a blue Σ). With nothing
  // written, or a formula saved empty, the variable has no value, "?", and its words take the caret (Notion's, seen
  // 2026-11-21 twice: a letter typed after "완료" went in them).
  const savedAs = useRef<string | null>(null);
  const valueOf = (variable: Variable, next: string): Variable => ({ ...variable,
    value: next.trim() ? { kind: "formula", expression: next.trim() } : clearedWords("") });
  const closeFormula = (index: number, variable: Variable) => {
    const words = savedAs.current === null ? variable.value.kind !== "formula" : !savedAs.current;
    if (savedAs.current === null && words && !wordsOf(variable).length) set(index, valueOf(variable, ""));
    if (words) setConverted(index);
    savedAs.current = null;
    setFormula(null);
  };
  // "전환" makes a formula words again, once asked under the words' mark, as Σ and "텍스트로 편집" do in Notion (seen
  // 2026-11-21: "1" gives "1", a variable it names a chip in them; the question in Leafen's own words).
  const toText = async (index: number, expression: string) => {
    const convert = await confirmAction({ icon: <TextPropertyIcon size="large" aria-hidden="true" />, message: t("messageToTextConfirm"),
      detail: t("messageToTextDetail"), confirmLabel: t("messageToTextAction") });
    const current = action.variables[index];
    const words = automationFormulaWords(expression, names, properties, t);
    if (convert && current) set(index, { ...current, value: { kind: "literal", value: words.value, ...(words.spans ? { spans: words.spans } : {}) } });
    return convert;
  };
  return (
    <div className={fieldStyles.variables}>
      {action.variables.map((variable, index) => {
        // A formula its editor names a problem in is kept, as Notion's "저장" keeps it, and says the problem under
        // it without where, its mark a red warning and its name red, until it is fixed (seen 2026-11-21: "1.", "foo + 1").
        const problem = automationValueFormulaWords(t, variable.value, properties, names);
        const held = automationVariableKind(variable.value, properties, names, Boolean(problem));
        const choose = (key: Choice["key"]): Promise<boolean> | void => {
          // On words "텍스트로 편집" keeps them, as Notion's (seen 2026-11-21: "abc" stays); a formula's asks with the menu
          // open behind, which "전환" closes, its words taking the caret at their end, and "취소" leaves open.
          if (key === "text" && variable.value.kind === "formula") {
            return toText(index, variable.value.expression).then((done) => {
              if (done) { setConverted(index); setMenu(null); }
              return done;
            });
          }
          if (key === "formula") setFormula(index);
          // "복제" puts the copy last among the action's variables, not after its original (Notion's, seen 2026-11-21 twice:
          // "변수 1"'s copy "변수 3" after "변수 2").
          if (key === "duplicate") {
            const copy = { ...variable, name: freshAutomationVariableName(variableNames, nameOf) };
            onChange({ ...action, variables: [...action.variables, copy] });
          }
          if (key === "delete") {
            // An action keeps one variable at least: its last's "변수 지우기" keeps its name and clears its value (Notion's,
            // seen 2026-11-21: "변수 1q" kept, its words gone, "?").
            onChange({ ...action, variables: action.variables.length > 1 ? action.variables.filter((_, at) => at !== index)
              : [{ ...variable, value: clearedWords("") }] });
          }
        };
        return (
          <div key={index} className={styles.variable} data-automation-variable={index}>
            <span className={styles.variableHeadWrap}>
              <button ref={(element) => { triggers.current[index] = element; }} type="button" className={styles.variableHead}
                aria-haspopup="dialog" aria-expanded={menu === index} data-warn={problem ? "" : undefined}
                aria-label={t("variableMenuFor", { name: variable.name })} onClick={() => { setConverted(null); setMenu(menu === index ? null : index); }}>
                <AutomationVariableMark kind={held} warn={Boolean(problem)} />
                <span>{variable.name}</span>
                <ChevronDown size="small" aria-hidden="true" />
              </button>
              {menu === index ? (
                <VariableMenu name={variable.name} kind={automationVariableKindWords(t, held)} problem={problem} trigger={triggers.current[index] ?? null}
                  formula={variable.value.kind === "formula"} lone={action.variables.length === 1}
                  onRename={(name) => set(index, { ...variable, name })} onChoose={choose} onClose={() => setMenu(null)} />
              ) : null}
            </span>
            {variable.value.kind === "formula" ? (
              <AutomationFormulaField expression={variable.value.expression} label={t("variableFormulaFor", { number, name: variable.name })}
                names={names} properties={properties} invalid={Boolean(problem)} data={{ "data-automation-variable-formula": "" }}
                onEdit={() => setFormula(index)} onText={() => {
                  if (variable.value.kind === "formula") void toText(index, variable.value.expression).then((done) => { if (done) setConverted(index); });
                }} />
            ) : (
              <AutomationWordsField words={wordsOf(variable)} label={t("variableValueFor", { number, name: variable.name })}
                names={names} properties={properties} onFormula={() => { setConverted(null); setFormula(index); }} autoFocus={converted === index}
                onChange={(value, spans) => set(index, { ...variable, value: clearedWords(value, spans) })} />
            )}
            {problem ? <span className={lineStyles.problem} data-automation-warning="formula">{problem}</span> : null}
          </div>
        );
      })}
      <button type="button" className={`${fieldStyles.textButton} ${fieldStyles.variableAdd}`} data-automation-variable-add
        onClick={() => onChange({ ...action, variables: [...action.variables,
          { name: freshAutomationVariableName(variableNames, nameOf), value: { kind: "literal", value: "" } }] })}>
        {t("defineAnotherVariable")}
      </button>
      {formula !== null && editing ? (
        <FormulaEditorDialog prop={formulaProp} props={properties} pagesById={pagesById} bounds={VALUE_FORMULA_BOUNDS} names={names}
          fresh={editing.value.kind !== "formula"} onSave={(next) => { savedAs.current = next.trim(); set(formula, valueOf(editing, next)); }}
          onClose={() => closeFormula(formula, editing)} />
      ) : null}
    </div>
  );
}

/**
 * A variable's menu: its name field over its choices, its kind at its foot,
 * as Notion's — "유형: 숫자", or while its formula has a problem the warning
 * and "유형: 알 수 없음" in red over what is wrong (seen 2026-11-21).
 */
function VariableMenu({ name, kind, problem, trigger, formula, lone, onRename, onChoose, onClose }: {
  name: string; kind: string; problem: string | null; trigger: HTMLButtonElement | null;
  /** Its value is a formula, which "텍스트로 편집" asks before making words. */
  formula: boolean;
  /** It is its action's only variable: its last row clears it, "변수 지우기"; one of several, "삭제" (Notion's, seen 2026-11-21). */
  lone: boolean;
  onRename(name: string): void; onChoose(key: Choice["key"]): Promise<boolean> | void; onClose(): void;
}) {
  const { t } = useTranslation("databaseView");
  const phone = useContext(AutomationPhoneSheets);
  // "텍스트로 편집" is marked by Notion's pencil over a line (pencilLine, seen 2026-11-21), as a menu's rename.
  const choices: Choice[] = [
    { key: "text", label: t("variableEditAsText"), icon: <Pencil size="regular" aria-hidden="true" />, asks: formula },
    { key: "formula", label: t("variableEditAsFormula"), icon: <FormulaIcon size="regular" aria-hidden="true" /> },
    { key: "duplicate", label: t("duplicate"), icon: <Copy size="regular" aria-hidden="true" /> },
    { key: "delete", label: t(lone ? "variableClear" : "variableDelete"), icon: <Trash size="regular" aria-hidden="true" />, danger: true },
  ];
  const run = (choice: Choice) => { onClose(); onChoose(choice.key); };
  // On a desktop a choice that asks keeps the menu open behind its question; "취소" leaves the focus in the menu, off its
  // name, where a letter typed goes nowhere (Notion's, seen 2026-11-21 twice, read after the press: on its row).
  const ask = (choice: Choice, menu: HTMLElement | null) => {
    void Promise.resolve(onChoose(choice.key)).then((done) => {
      if (done || !menu) return;
      requestAnimationFrame(() => { menu.tabIndex = -1; menu.focus({ preventScroll: true }); });
    });
  };
  const field = <input className={styles.menuName} aria-label={t("variableName")} value={name} onChange={(event) => onRename(event.target.value)} />;
  const foot = problem ? (
    <div className={styles.menuFootWarn} data-automation-variable-kind="warn">
      <span className={styles.menuFootKind}><WarningFillIcon size="regular" aria-hidden="true" /><span>{kind}</span></span>
      <p className={styles.menuFootProblem}>{problem}</p>
    </div>
  ) : <p className={styles.menuFoot} data-automation-variable-kind>{kind}</p>;
  if (phone) {
    return (
      <PhoneSheetMenu label={t("variableMenuFor", { name })} onClose={onClose}>
        <div className={styles.phoneMenuName}>{field}</div>
        {choices.map((choice) => <PhoneSheetRow key={choice.key} label={choice.label} icon={choice.icon} danger={choice.danger} more={false} indent
          data={{ "data-automation-variable-choice": choice.key }} onPress={() => run(choice)} />)}
        {foot}
      </PhoneSheetMenu>
    );
  }
  return <DesktopVariableMenu field={field} foot={foot} choices={choices} trigger={trigger} onRun={run} onAsk={ask} onClose={onClose}
    label={t("variableMenuFor", { name })} />;
}

/**
 * A desktop's variable menu floats over the page, as Notion's: drawn in the
 * card, the automation window's scrolling cut off the menu of a variable near
 * its foot. Right under its "⌄", its start on the line's, and over it where
 * the window has no room for it under (Notion's, seen 2026-11-21 in Chrome at
 * 1440: 0 under a line at 395..423, its foot on a line at 563..591 with 188
 * under it).
 */
function DesktopVariableMenu({ field, foot, choices, trigger, label, onRun, onAsk, onClose }: {
  field: ReactElement; foot: ReactElement; choices: Choice[]; trigger: HTMLButtonElement | null; label: string;
  onRun(choice: Choice): void; onAsk(choice: Choice, menu: HTMLElement | null): void; onClose(): void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const nameRef = useRef<HTMLInputElement>(null);
  const layer = useOverlayLayer(true, { kind: "menu", ref, onEscape: onClose, onOutsidePress: onClose, inside: () => [trigger],
    returnFocusRef: { current: trigger } });
  const place = usePopoverPlacement(ref, trigger ? (size, viewport) => placeAround(popoverAnchorOf(trigger), size, viewport, { gap: 0 }) : null,
    [trigger], undefined, false);
  // The name keeps focus, as Notion's; arrows move the rows' highlight. Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440:
  // a "변수 정의" variable's ⌄ pressed, "z" went in after its name ("변수 1z"; Leafen by code, not seen).
  const list = useNativeMenu({ items: choices, open: true, menuRef: listRef, label, focusOnOpen: false, focusOwnerRef: nameRef, ownerKeepsTab: true, ownerSearches: false,
    keyOf: (choice) => choice.key, textOf: (choice) => choice.label,
    onAction: (choice) => choice.asks ? onAsk(choice, ref.current) : onRun(choice) });
  const highlighted = list.ownerProps["aria-activedescendant"];
  useLayoutEffect(() => {
    const input = ref.current?.querySelector<HTMLInputElement>("input");
    nameRef.current = input ?? null;
    if (!input) return;
    if (highlighted) input.setAttribute("aria-activedescendant", highlighted); else input.removeAttribute("aria-activedescendant");
    input.setAttribute("aria-controls", list.ownerProps["aria-controls"] ?? "");
  }, [highlighted, list.ownerProps]);
  useLayoutEffect(() => { ref.current?.querySelector<HTMLInputElement>("input")?.focus({ preventScroll: true }); }, []);
  const keyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.target === nameRef.current && list.ownerProps.onKeyDown(event)) return;
    layer.onKeyDown(event);
  };
  if (typeof document === "undefined") return null;
  return createPortal(
    <div ref={ref} role="dialog" aria-label={label} className={styles.menu} style={{ ...popoverPlacementStyle(place), zIndex: layer.zIndex }}
      data-automation-variable-menu
      onKeyDown={keyDown} onKeyUp={(event) => { if (event.target === nameRef.current) list.ownerProps.onKeyUp(event); }}>
      <div className={styles.menuNameRow}>{field}</div>
      <div className={styles.menuRule} />
      <div {...list.menuProps} ref={listRef} className={styles.menuList}>
        {choices.map((choice) => (
          <Fragment key={choice.key}>
            {choice.danger ? <div className={styles.menuRule} /> : null}
            <NativeMenuItem state={list.state} itemKey={choice.key} className={styles.menuItem} data-danger={choice.danger || undefined}
              data-automation-variable-choice={choice.key}>
              {({ labelProps }) => <>{choice.icon}<span {...labelProps}>{choice.label}</span></>}
            </NativeMenuItem>
          </Fragment>
        ))}
      </div>
      <div className={styles.menuRule} />
      {foot}
    </div>,
    document.body,
  );
}
