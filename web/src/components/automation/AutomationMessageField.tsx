import { useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { DbProperty, Page, TextSpan } from "@/lib/types";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { FormulaEditorDialog } from "@/components/database/FormulaEditorDialog";
import { formulaPieces, formulaPropertyChoices } from "@/components/database/formulaEditorModel";
import { formulaPageField, resolveFormulaPropertyReference } from "@/components/database/formulaReferences";
import type { FormulaBounds } from "@/components/database/formulaBudget";
import { PropertyTypeIcon } from "@/components/database/PropertyTypeIcon";
import { AtIcon, FormulaIcon } from "@/icons/leafen";
import { RichTextCellEditor, type RichTextCellHandle } from "@/components/database/RichTextCellEditor";
import { useCommentMentionSnapshot } from "@/components/comments/commentMentionContext";
import codeStyles from "@/components/database/FormulaCode.module.css";
import { FORMULA_CODE, formulaTokenClass } from "@/components/database/formulaTokenTheme";
import { AutomationMentionMenu } from "./AutomationMentionMenu";
import { automationFormulaNames, automationVariablesKey, type AutomationVariableList } from "./automationFormulaNames";
import { automationValueFormulaWords } from "./automationFormulaFault";
import type { AutomationEditorSurface } from "./automationActionModel";
import styles from "./automationMessageField.module.css";
import { pagesByIdOf } from "@/lib/pageIndex";

type MessageAction = { id: string; message: string; messageFormula?: string; messageSpans?: TextSpan[] };

const UNBOUNDED = Number.MAX_SAFE_INTEGER;

// A message's formula is computed on the server as it runs; the editor's
// own reading of it stays within these.
const MESSAGE_FORMULA_BOUNDS: FormulaBounds = {
  maxExpressionCharacters: 4_096, maxProperties: UNBOUNDED, maxOptions: UNBOUNDED, maxPages: UNBOUNDED, maxEvaluations: 10_000,
  maxValueNodes: UNBOUNDED, maxValueDepth: 10, maxStringCharacters: 1_000_000,
  rollup: { maxDatabases: UNBOUNDED, maxProperties: UNBOUNDED, maxPages: UNBOUNDED, maxValueNodes: UNBOUNDED, maxValueDepth: 10,
    maxValuePieces: UNBOUNDED, maxOptions: UNBOUNDED, maxFileAttachments: UNBOUNDED, maxPersonNodes: UNBOUNDED, maxPersonDepth: 8,
    maxPeopleEntries: UNBOUNDED, maxStringCharacters: 1_000_000 },
};

/**
 * The formula written words with mentions make, as Notion's Σ turns a
 * message or a text variable into one (seen 2026-10-23, 25): each run of words a text and each of
 * the run's mentions its formula, joined with "+"; a person's, a page's or a
 * date's mention is its words.
 */
export function messageFormulaOf(spans: readonly TextSpan[]) {
  return spans.filter((span) => span.text || span.automation)
    .map((span) => span.mention === "automation" && span.automation ? span.automation : JSON.stringify(span.text)).join(" + ");
}

/**
 * Whether written words hold more than their text: a mention, or a mark —
 * bold, italic, underline, strikethrough, code, a link — as Notion's message
 * box keeps those typed or pasted (seen 2026-11-11); their spans keep them.
 */
export function automationMessageShaped(spans: readonly TextSpan[] | undefined): spans is TextSpan[] {
  return Boolean(spans?.some((span) => span.mention || span.bold || span.italic || span.underline || span.strikethrough
    || span.code || span.link));
}

/**
 * An action's words, as Notion's message box (seen 2026-10-22, 23): written
 * out with "@" mentions in them — a person, a page, a date, or what the run
 * gives as a chip (AutomationMentionMenu) — or with Σ one formula of the
 * trigger page, who ran it, its page's maker, when, and the variables, made
 * from the words written so far; either is computed as the action runs
 * (interactive_automation_message.go). A formula shows in the box with a
 * blue Σ, which makes it words again, those it was made from, once asked;
 * its tokens in its editor's colours, 14px mono (Notion's, seen 2026-11-15).
 */
export function AutomationMessageField<T extends MessageAction>({
  action, label, placeholder, properties, pages, surface, variables = [], onChange,
}: {
  action: T;
  label: string;
  placeholder?: string;
  /** The trigger database's properties, none for a page's button. */
  properties: DbProperty[];
  /** The pages known here, whose trigger rows show the formula's result. */
  pages: Page[];
  surface?: AutomationEditorSurface;
  /** The variables the actions before this one define. */
  variables?: AutomationVariableList;
  onChange(action: T): void;
}) {
  const { t } = useTranslation(["databaseView", "formulaEditor"]);
  // The formula its editor opens with, from Σ the words' formula (`words` beside it) or the formula pressed.
  const [opened, setOpened] = useState<{ start: string; words?: TextSpan[] } | null>(null);
  const fieldRef = useRef<RichTextCellHandle>(null);
  const snapshot = useCommentMentionSnapshot();
  const formula = action.messageFormula ?? "";
  const databaseId = properties[0]?.databaseId ?? "";
  const start = opened?.start ?? formula;
  const prop = useMemo((): DbProperty => ({ id: `automation-message-${action.id}`, databaseId, name: label, type: "formula",
    config: { formula: start }, position: UNBOUNDED }), [action.id, databaseId, label, start]);
  const pagesById = pagesByIdOf(pages);
  const variableKey = automationVariablesKey(variables);
  const names = useMemo(() => automationFormulaNames(t, { surface, variables, properties }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [t, surface, variableKey, properties]);
  const choices = useMemo(() => formulaPropertyChoices(properties, prop), [properties, prop]);
  const pieces = useMemo(() => formulaPieces(formula, (name) => {
    const property = formulaPageField(properties, name) ?? resolveFormulaPropertyReference(properties, name);
    return property ? { name: property.name, type: property.type } : undefined;
  }, names), [formula, properties, names]);
  // A formula its editor names a problem in is kept and rings red with the problem under it, as Notion's (seen 2026-11-21).
  const problem = useMemo(() => automationValueFormulaWords(t, formula ? { kind: "formula", expression: formula } : undefined, properties, names),
    [t, formula, properties, names]);
  const spans = useMemo(() => action.messageSpans ?? (action.message ? [{ text: action.message }] : []), [action.messageSpans, action.message]);
  // The words' text: a formula's words come back without it (the server keeps their spans alone).
  const written = spans.map((span) => span.text).join("");
  // The words as last typed here, over the words they were typed on: they reach `action` once its owner draws it
  // again, which a page's button does a moment after (its actions live in its block).
  const typed = useRef<{ over: TextSpan[]; words: TextSpan[] } | null>(null);

  function openFormula() {
    // A syllable still composing is typed first.
    fieldRef.current?.publish();
    const words = typed.current?.over === spans ? typed.current.words : spans;
    // The words written so far become the formula, as Notion's Σ makes them one, its editor showing them at once
    // (seen 2026-11-21 twice); closing it keeps them a formula, its words beside it to come back when it is made
    // words again (FormulaEditorDialog's `fresh`).
    setOpened({ start: messageFormulaOf(words), words });
  }

  async function toText() {
    const convert = await confirmAction({ message: t("databaseView:messageToTextConfirm"), detail: t("databaseView:messageToTextDetail"),
      confirmLabel: t("databaseView:messageToTextAction") });
    if (convert) onChange({ ...action, message: written, messageFormula: undefined });
  }

  return (<>
    {/* Notion's box (seen 2026-11-21 twice): 2px in, its words over the whole width 2px 4px in on 20px lines, in a part
        4.5em high (a confirmation's 6em) that scrolls past that, "@" and Σ on their own row under them at its end. */}
    <div className={styles.message} data-automation-message-box data-invalid={problem ? "" : undefined}>
      {formula ? (
        <div className={styles.messageArea}>
          <button type="button" className={styles.messageFormula} aria-label={label} data-automation-message-formula
            data-invalid={problem ? "" : undefined} aria-invalid={problem ? true : undefined} onClick={() => setOpened({ start: formula })}>
            <code className={FORMULA_CODE}>
              {pieces.map((piece, index) => piece.chip ? (
                <span key={index} className={codeStyles.chip}>
                  <PropertyTypeIcon size="tiny" type={piece.chip.type as DbProperty["type"]} />{piece.chip.name}
                </span>
              ) : <span key={index} data-token={piece.kind} className={piece.kind ? formulaTokenClass(piece.kind) : undefined}>{piece.text}</span>)}
            </code>
          </button>
        </div>
      ) : (
        <div className={styles.messageArea}>
          <RichTextCellEditor key={action.id} spans={spans} value={written} multiline ariaLabel={label} placeholder={placeholder}
            className={styles.messageText} handleRef={fieldRef} dataAttributes={{ "data-automation-message": "" }}
            onChange={(change) => {
              typed.current = { over: spans, words: change.spans ?? (change.value ? [{ text: change.value }] : []) };
              onChange({ ...action, message: change.value, messageSpans: automationMessageShaped(change.spans) ? change.spans : undefined });
            }}
            renderMentionMenu={(menu) => <AutomationMentionMenu menu={menu} names={names} properties={choices} snapshot={snapshot} />} />
        </div>
      )}
      {/* Notion's 20px buttons with its 16px marks, named "멘션" and "수식" whether a formula is set or not (seen 2026-11-21). */}
      <span className={styles.messageButtons}>
        {formula ? null : (
          <button type="button" className={styles.messageMention} aria-label={t("databaseView:messageMention")}
            title={t("databaseView:messageMention")} data-automation-message-mention onClick={() => fieldRef.current?.mention()}>
            <AtIcon size="small" aria-hidden="true" />
          </button>
        )}
        <button type="button" className={styles.messageSigma} data-formula={formula ? "" : undefined}
          aria-label={t("databaseView:messageFormulaButton")}
          title={formula ? t("databaseView:messageToText") : t("databaseView:messageFormula")} data-automation-message-sigma
          onClick={() => (formula ? void toText() : openFormula())}><FormulaIcon size="small" aria-hidden="true" /></button>
      </span>
      {opened ? (
        <FormulaEditorDialog prop={prop} props={properties} pagesById={pagesById} bounds={MESSAGE_FORMULA_BOUNDS} names={names}
          fresh={Boolean(opened.words)} onSave={(next) => onChange({ ...action, messageFormula: next.trim() || undefined,
            ...(opened.words ? { message: opened.words.map((span) => span.text).join(""), messageSpans: opened.words } : {}) })}
          onClose={() => setOpened(null)} />
      ) : null}
    </div>
    {problem ? <span className={styles.messageProblem} data-automation-warning="formula">{problem}</span> : null}
  </>);
}
