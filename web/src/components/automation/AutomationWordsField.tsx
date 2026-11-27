import { useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { FormulaIcon } from "@/icons/leafen";
import type { AutomationValueExpression, DbProperty, TextSpan } from "@/lib/types";
import { formulaPieces, formulaPropertyChoices, type FormulaScopeName } from "@/components/database/formulaEditorModel";
import { PropertyTypeIcon } from "@/components/database/PropertyTypeIcon";
import { RichTextCellEditor, type RichTextCellHandle } from "@/components/database/RichTextCellEditor";
import { useCommentMentionSnapshot } from "@/components/comments/commentMentionContext";
import codeStyles from "@/components/database/FormulaCode.module.css";
import { FORMULA_CODE, formulaTokenClass } from "@/components/database/formulaTokenTheme";
import { AutomationMentionMenu } from "./AutomationMentionMenu";
import { messageFormulaOf } from "./AutomationMessageField";
import styles from "./AutomationWebhookVariables.module.css";

/** Σ's press: the field it is in, which a formula's editor hangs from. */
export type AutomationFormulaOpen = (from?: HTMLElement) => void;

/** The properties whose value is words, which take "@" and Σ. */
export const AUTOMATION_WORDS_TYPES = new Set(["title", "rich_text", "url", "email", "phone"]);

/** A value's words, with any mentions in them; none for a formula. */
export function automationWordsOf(value: AutomationValueExpression | string | undefined): TextSpan[] {
  if (typeof value === "string") return value ? [{ text: value }] : [];
  if (!value || value.kind !== "literal") return [];
  if (value.spans?.length) return value.spans;
  return typeof value.value === "string" && value.value ? [{ text: value.value }] : [];
}

/** Words typed, as a value keeps them: their mentions beside their text. */
export function automationWordsValue(value: string, spans?: TextSpan[]): AutomationValueExpression {
  return { kind: "literal", value, ...(spans?.some((span) => span.mention) ? { spans } : {}) };
}

/**
 * Words in a soft field with "@" and Σ at its end, as Notion's automation
 * values (seen 2026-10-25 at 1280px — a variable's, a new page's name and
 * properties): the mentions are those a message's "@" puts in
 * (AutomationMentionMenu), computed as the action runs; Σ makes them a
 * formula.
 */
export function AutomationWordsField({ words, label, placeholder, names, properties, autoFocus, onFormula, onChange }: {
  words: TextSpan[]; label: string; placeholder?: string; names: readonly FormulaScopeName[]; properties: DbProperty[];
  /** Takes the caret at its end as it is drawn: a variable's words "전환" just made of its formula. */
  autoFocus?: boolean;
  onFormula: AutomationFormulaOpen; onChange(value: string, spans?: TextSpan[]): void;
}) {
  const { t } = useTranslation("databaseView");
  const fieldRef = useRef<RichTextCellHandle>(null);
  const snapshot = useCommentMentionSnapshot();
  const choices = useMemo(() => formulaPropertyChoices(properties, { id: "", databaseId: properties[0]?.databaseId ?? "" }), [properties]);
  return (
    <span className={styles.softBox} data-automation-words data-automation-field>
      <RichTextCellEditor spans={words} value={words.map((span) => span.text).join("")} ariaLabel={label} className={styles.words}
        placeholder={placeholder} handleRef={fieldRef} autoFocus={autoFocus}
        onChange={(change) => onChange(change.value, change.spans?.some((span) => span.mention) ? change.spans : undefined)}
        renderMentionMenu={(menu) => <AutomationMentionMenu menu={menu} names={names} properties={choices} snapshot={snapshot} />} />
      <span className={styles.softBoxButtons}>
        <button type="button" className={styles.softBoxButton} aria-label={t("messageMention")} title={t("messageMention")}
          data-automation-variable-mention onClick={() => fieldRef.current?.mention()}>@</button>
        <AutomationFormulaButton onFormula={onFormula} />
      </span>
    </span>
  );
}

/** Σ at a soft field's end, which makes its value a formula. */
export function AutomationFormulaButton({ onFormula }: { onFormula: AutomationFormulaOpen }) {
  const { t } = useTranslation("databaseView");
  return (
    <button type="button" className={styles.softBoxButton} aria-label={t("variableEditAsFormula")} title={t("variableEditAsFormula")}
      data-automation-value-sigma onClick={(event) => onFormula(event.currentTarget.closest<HTMLElement>("[data-automation-field]") ?? undefined)}>
      <FormulaIcon size="small" aria-hidden="true" />
    </button>
  );
}

/**
 * A number in a page's line (a new page's, an edit's), as Notion's (seen
 * 2026-11-10): the soft field of words, with Σ alone at its end and nothing
 * said while empty. It takes any words, as Notion's does ("5+5+1abc"); those
 * spelling one number are kept as that number (automationNumberWords).
 */
export function AutomationNumberField({ value, label, onFormula, onChange }: {
  value: unknown; label: string; onFormula: AutomationFormulaOpen; onChange(value: number | string | null): void;
}) {
  const shown = automationNumberText(value);
  // The words as typed, while they read as the value kept ("1." is 1 until the next digit).
  const [typed, setTyped] = useState(shown);
  const text = automationNumberWords(typed) === automationNumberWords(shown) ? typed : shown;
  return (
    <span className={`${styles.softBox} ${styles.numberBox}`} data-automation-number data-automation-field>
      <input type="text" className={styles.number} aria-label={label} value={text}
        onChange={(event) => { setTyped(event.currentTarget.value); onChange(automationNumberWords(event.currentTarget.value)); }} />
      <span className={styles.softBoxButtons}><AutomationFormulaButton onFormula={onFormula} /></span>
    </span>
  );
}

/** A number line's words: a number kept as its digits, words as they are, nothing as nothing. */
export function automationNumberText(value: unknown) {
  return typeof value === "number" ? String(value) : typeof value === "string" ? value : "";
}

/**
 * What a number line keeps of its words: a number when they are its own
 * spelling ("12", "-0.5"), otherwise the words as written ("1,234", "007",
 * "5+5+1abc"), which the run reads as Notion's does (551 for "5+5+1", seen
 * 2026-11-10); nothing while empty.
 */
export function automationNumberWords(words: string): number | string | null {
  const trimmed = words.trim();
  if (!trimmed) return null;
  const number = Number(trimmed);
  return Number.isFinite(number) && String(number) === trimmed ? number : words;
}

/**
 * The formula Σ starts from in a page's line, as Notion's (seen 2026-11-10):
 * the words written, a number's too as words ("12" for 12), which a number
 * then can't take; nothing while empty.
 */
export function automationLineFormulaStart(value: AutomationValueExpression | undefined, propertyType: string | undefined) {
  if (!value) return "";
  if (value.kind === "formula") return value.expression;
  if (propertyType === "number") return value.kind === "literal" && automationNumberText(value.value) ? JSON.stringify(automationNumberText(value.value)) : "";
  return propertyType && AUTOMATION_WORDS_TYPES.has(propertyType) ? messageFormulaOf(automationWordsOf(value)) : "";
}

/**
 * A page line's value written as a formula, as Notion's (seen 2026-11-10):
 * its code in the line's soft field, coloured as its editor colours it, its
 * properties as chips — pressing it opens the editor — and a blue Σ, "텍스트로
 * 편집", which makes it words again once asked. A formula of a type the
 * property can't take rings the field red. A variable's formula stands in
 * the same field (Notion's, seen 2026-11-21).
 */
export function AutomationFormulaField({ expression, label, names, properties, invalid, onEdit, onText, data }: {
  expression: string; label: string; names: readonly FormulaScopeName[]; properties: readonly DbProperty[]; invalid?: boolean;
  onEdit: AutomationFormulaOpen; onText(): void;
  /** What marks the field where it stands; a page's line by default. */
  data?: Record<string, string>;
}) {
  const { t } = useTranslation("databaseView");
  const pieces = useMemo(() => formulaPieces(expression, (name) => {
    const property = properties.find((candidate) => candidate.name === name);
    return property ? { name: property.name, type: property.type } : undefined;
  }, names), [expression, names, properties]);
  return (
    <span className={styles.softBox} data-automation-field {...(data ?? { "data-automation-line-formula": "" })} data-invalid={invalid || undefined}>
      <button type="button" className={styles.formulaCode} aria-label={label}
        onClick={(event) => onEdit(event.currentTarget.closest<HTMLElement>("[data-automation-field]") ?? undefined)}>
        <code className={FORMULA_CODE}>
          {pieces.map((piece, index) => piece.chip ? (
            <span key={index} className={codeStyles.chip}>
              <PropertyTypeIcon size="tiny" type={piece.chip.type as DbProperty["type"]} />{piece.chip.name}
            </span>
          ) : <span key={index} data-token={piece.kind} className={piece.kind ? formulaTokenClass(piece.kind) : undefined}>{piece.text}</span>)}
        </code>
      </button>
      <span className={styles.softBoxButtons}>
        <button type="button" className={styles.softBoxButton} aria-pressed="true" aria-label={t("messageToText")} title={t("messageToText")}
          data-automation-value-to-text onClick={onText}>
          <FormulaIcon size="small" aria-hidden="true" />
        </button>
      </span>
    </span>
  );
}
