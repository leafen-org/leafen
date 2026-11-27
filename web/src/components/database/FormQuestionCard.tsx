import { SwitchMark } from "@/components/Switch";
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { AlignLeftIcon, ArrowLeft, ArrowsDownUpIcon, ArrowsLeftRight, BulletedListIcon, CalendarIcon, CheckIcon, ChevronDown, ChevronRight, ChevronUpDown, ClockIcon, Copy, DatabaseCylinderIcon, DotsHorizontal, HashIcon, MoreAppsIcon, SyncIcon, Trash, X } from "@/icons/leafen";
import type { GraphProperty } from "@/contentGraph";
import { FileUploadMark, FORM_FILE_LIMIT } from "@/components/forms/FormFileField";
import { formQuestionLabel, formQuestionMostFixed, formQuestionMostSelections, formQuestionTakesCondition } from "@/lib/formViewModel";
import { scrollCardFootIntoView } from "@/lib/openerScroll";
import { placeAround, placeBeside, placeBesideRow, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { PopoverLayer } from "@/lib/popoverLayer";
import type { DbProperty, FormViewQuestion, SelectOption } from "@/lib/types";
import { DatabasePropertyEditPanel, type DatabasePropertyEditing } from "./DatabasePropertyEditPanel";
import { formQuestionTypeMadeHere, NEW_QUESTION_TYPES, type NewQuestionType } from "./FormAddQuestionMenu";
import { PropertyTypeIcon } from "./PropertyTypeIcon";
import { FormQuestionOptions } from "./FormQuestionOptions";
import { localizedPropertyTypeLabel } from "./propertyTypes";
import formStyles from "./FormView.module.css";
import styles from "./FormQuestionCard.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

/**
 * A question of the form as Notion's builder shows it (seen 2026-10-04,
 * 2026-10-06): a card with the question and a "응답자의 답변" field; pointing
 * at it shows "질문 편집" (…) with the question's options — required, a
 * description, a long answer, its type and property, keeping the property's
 * name, moving, duplicating and removing.
 */

function MarkOf({ d }: { d: string }) {
  return <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.25" strokeLinecap="round" aria-hidden="true"><path d={d} /></svg>;
}
const REQUIRED_MARK = "M10 3.5v13M4.4 6.75l11.2 6.5M15.6 6.75 4.4 13.25";
const CONDITION_MARK = "M6 4.5v6.5a3 3 0 0 0 3 3h3.5M6 4.5a1.25 1.25 0 1 0 0-.01M15 14a1.25 1.25 0 1 0-.01 0";

/** Where "질문 옮기기" sends a question: a step either way, or the group's first or last place. */
export type FormQuestionMove = -1 | 1 | "first" | "last";
/** What "질문 유형" turns a question to: a kind the form makes, or back to the title while no question asks it. */
export type FormQuestionKind = NewQuestionType | "title";
export function FormQuestionCard({
  question, property, index, count, canDuplicate, focusTitle = false, editing, graphProperty, onChange, onOptionsChange, onRename, onMove, onDuplicate, onRemove,
  onAddCondition, onLeaveGroup, onChangeType, titleFree = false, menuOpenAtStart = false,
}: {
  /** Renames the question's property, which a question kept in step with it does as its title is typed; false when another property has the name. */
  onRename?: (name: string) => boolean;
  question: FormViewQuestion;
  property: DbProperty;
  index: number;
  count: number;
  canDuplicate: boolean;
  /** A question just made: its options open and its title is chosen for typing. */
  focusTitle?: boolean;
  onChange: (patch: Partial<FormViewQuestion>) => void;
  /** Changes the property's options, which a select question's card edits. */
  onOptionsChange?: (options: SelectOption[]) => void;
  onMove: (to: FormQuestionMove) => void;
  onDuplicate: () => void;
  onRemove: () => void;
  /** "연결된 속성 보기": its property's settings in the menu, as Notion's (seen 2026-11-05). */
  editing?: DatabasePropertyEditing;
  graphProperty?: GraphProperty;
  /** "조건부 로직 추가", on a question out of any group whose answer can be read. */
  onAddCondition?: () => void;
  /** "조건부 그룹에서 제거", in place of moving it, for a question in a group. */
  onLeaveGroup?: () => void;
  /** "질문 유형": turns the question to another kind at once, as Notion's (seen 2026-11-05). */
  onChangeType?: (type: FormQuestionKind) => void;
  /** No question asks the title, so "질문 유형" offers it back, second, as Notion's (seen 2026-11-08). */
  titleFree?: boolean;
  /** Its options open as it comes, for a question "질문 유형" just moved to a new property. */
  menuOpenAtStart?: boolean;
}) {
  const { t } = useTranslation(["formView", "common"]);
  const [menuOpen, setMenuOpen] = useState(menuOpenAtStart);
  const [describing, setDescribing] = useState(false);
  // "다음으로 옵션 표시" opens its two ways in the menu, and "질문 옮기기" its
  // four beside it, as Notion's (seen 2026-11-04).
  const [displayOpen, setDisplayOpen] = useState(false);
  const [moveOpen, setMoveOpen] = useState(false);
  const [propertyOpen, setPropertyOpen] = useState(false);
  const [typesOpen, setTypesOpen] = useState(false);
  const [mostOpen, setMostOpen] = useState(false);
  const mostRow = useRef<HTMLButtonElement>(null), mostMenu = useRef<HTMLDivElement>(null);
  const typeRow = useRef<HTMLButtonElement>(null), typeMenu = useRef<HTMLDivElement>(null);
  const menuButton = useRef<HTMLButtonElement>(null), menu = useRef<HTMLDivElement>(null), title = useRef<HTMLInputElement>(null);
  const moveRow = useRef<HTMLButtonElement>(null), moveMenu = useRef<HTMLDivElement>(null), card = useRef<HTMLElement>(null);
  // A question just made opens its options with its title chosen, as Notion's
  // (measured 2026-11-15 in Chrome only, made by the "+" under the questions
  // and a type clicked, no script in the page: "질문 옵션" open beside, the
  // title's words chosen, and the next letter typed took their place).
  useEffect(() => {
    if (!focusTitle) return;
    setMenuOpen(true);
    return focusOpenedField(() => title.current, { select: true });
  }, [focusTitle]);
  // The options open right of the card, 24 from it, where they fit, else
  // left (Notion's at 1920 and 1280, seen 2026-11-08), 8 under the card's top
  // and slid up to fit the window; a card whose foot is under the window's
  // comes up first, with the "+" under it.
  useLayoutEffect(() => { if (menuOpen) scrollCardFootIntoView(card.current); }, [menuOpen]);
  const place = usePopoverPlacement(menu, menuOpen ? (size, viewport) => placeBeside(
    popoverRectOf(card.current)!, size, viewport, { prefer: "right", gap: 24, offsetTop: 8 }) : null,
  [menuOpen], undefined, { kind: "picker", onEscape: () => setMenuOpen(false), onOutsidePress: () => setMenuOpen(false),
    inside: () => [menuButton.current], returnFocusRef: menuButton });
  const movePlace = usePopoverPlacement(moveMenu, menuOpen && moveOpen ? (size, viewport) => placeBesideRow(moveRow.current, size, viewport) : null,
    [menuOpen, moveOpen], undefined, { kind: "picker", restoreFocus: false, onEscape: () => setMoveOpen(false), onOutsidePress: () => setMoveOpen(false), inside: () => [moveRow.current] });
  const typePlace = usePopoverPlacement(typeMenu, menuOpen && typesOpen ? (size, viewport) => placeAround(
    popoverRectOf(typeRow.current)!, size, viewport, { gap: 0, prefer: "above" }) : null,
    [menuOpen, typesOpen], undefined, { kind: "picker", restoreFocus: false, onEscape: () => setTypesOpen(false), onOutsidePress: () => setTypesOpen(false), inside: () => [typeRow.current] });
  const mostPlace = usePopoverPlacement(mostMenu, menuOpen && mostOpen ? (size, viewport) => placeAround(
    popoverRectOf(mostRow.current)!, size, viewport, { gap: 0, prefer: "above" }) : null,
    [menuOpen, mostOpen], undefined, { kind: "picker", restoreFocus: false, onEscape: () => setMostOpen(false), onOutsidePress: () => setMostOpen(false), inside: () => [mostRow.current] });
  // What is being typed shows as typed, before its property's new name comes back.
  const [typing, setTyping] = useState<string | null>(null);
  const label = typing ?? formQuestionLabel(question, property);
  const typeName = property.type === "select" || property.type === "multi_select" ? t("formView:newQuestionTypes.choice")
    : property.type === "title" ? t("formView:titleKind") : property.type === "relation" ? t("formView:relationKind")
      : localizedPropertyTypeLabel(property.type);
  const choice = property.type === "select" || property.type === "status" || property.type === "multi_select";
  const multiple = property.type === "multi_select" || property.type === "person" || property.type === "relation";
  // No most unless the question sets one.
  const most = formQuestionMostSelections(question, property), mostFixed = formQuestionMostFixed(property);
  const selectionLimit = most ?? Number.MAX_SAFE_INTEGER;
  const options = property.config?.options ?? [];
  // A status keeps its options in its groups, which its settings edit.
  const editsOptions = Boolean(onOptionsChange) && (property.type === "select" || property.type === "multi_select");
  const describes = describing || question.description !== "";
  const kinds: readonly (readonly [FormQuestionKind, string])[] = titleFree && property.type !== "title"
    ? [NEW_QUESTION_TYPES[0], ["title", "title"], ...NEW_QUESTION_TYPES.slice(1)] : NEW_QUESTION_TYPES;
  const toggle = (mark: ReactNode, name: string, on: boolean, change: (on: boolean) => void, data: string) => (
    <button type="button" role="switch" aria-checked={on} className={styles.questionOption} data-form-question-option={data}
      onClick={() => change(!on)}>
      {mark}<span className={styles.questionOptionName}>{name}</span><SwitchMark on={on} />
    </button>
  );
  return (
    <section ref={card} className={styles.questionCard} data-form-question-editor={property.id} data-menu-open={menuOpen ? "true" : undefined}>
      <div className={styles.questionTitleRow}>
        {/* A press beside the name, in its 10px either side or on its "*", puts the caret at that end of it, as Notion's
            (seen 2026-11-12); further off it does not. */}
        <span className={styles.questionTitleField} onMouseDown={(event) => {
          const input = title.current;
          if (!input || event.target === input) return;
          event.preventDefault();
          const box = input.getBoundingClientRect();
          const end = getComputedStyle(input).direction === "rtl" ? event.clientX < box.left : event.clientX > box.right;
          const at = end ? input.value.length : 0;
          input.focus();
          input.setSelectionRange(at, at);
        }}>
          <input ref={title} className={styles.questionTitle} value={label} aria-label={t("formView:label")}
            onBlur={() => setTyping(null)}
            onChange={(event) => {
              const typed = event.target.value;
              setTyping(typed);
              // In step with its property, as a new question starts: the property takes the title too, as
              // Notion's (seen 2026-11-07); a title cleared, another property's name or no way to rename goes its own way.
              if (question.syncWithPropertyName && onRename && typed.trim() && onRename(typed)) onChange({ label: typed });
              else onChange({ label: typed, syncWithPropertyName: false });
            }} />
          {question.required ? <span className={styles.questionRequired} aria-label={t("formView:answerRequired")}>*</span> : null}
        </span>
        <button ref={menuButton} type="button" className={styles.questionMenuButton} aria-label={t("formView:editQuestion")}
          title={t("formView:editQuestion")} aria-haspopup="dialog" aria-expanded={menuOpen} onClick={() => setMenuOpen((open) => !open)}>
          <DotsHorizontal size="small" aria-hidden="true" />
        </button>
      </div>
      {describes ? (
        <textarea className={styles.questionDescription} rows={1} value={question.description}
          placeholder={t("formView:addDescription")} aria-label={t("formView:questionDescription")}
          onChange={(event) => onChange({ description: event.target.value })} />
      ) : null}
      {choice && multiple && question.maxSelections !== undefined
        ? <p className={styles.questionSelectionHint}>{t("formView:selectionHint", { count: selectionLimit })}</p> : null}
      {/* A person's or a page's question says how many its respondent may pick (Notion's, seen 2026-11-04). */}
      {!choice && multiple ? (
        <p className={styles.questionSelectionHint}>
          {most === undefined ? t("formView:selectionHintAny") : t("formView:selectionHint", { count: selectionLimit })}
        </p>
      ) : null}
      {/* A dropdown's card says so above its options, which it lists without marks (Notion's, seen 2026-10-21). */}
      {choice && question.optionsDisplay === "dropdown" ? (
        <div className={styles.questionDropdownHint} aria-hidden="true" data-form-dropdown-hint>
          <span>{t("formView:dropdownHint")}</span><ChevronUpDown size="small" />
        </div>
      ) : null}
      {choice && (options.length > 0 || editsOptions) ? (
        <FormQuestionOptions options={options} multiple={multiple && selectionLimit > 1} marks={question.optionsDisplay !== "dropdown"}
          onChange={editsOptions ? onOptionsChange : undefined} />
      ) : choice && question.optionsDisplay === "dropdown" ? null : property.type === "files" ? (
        // A file question's bar, "[📄 업로드]" and its limits, as Notion's builder (seen 2026-11-24, no ground there).
        <div className={styles.questionFiles} aria-hidden="true" data-kind="files">
          <span className={styles.questionFilesButton}><FileUploadMark />{t("formView:uploadFiles")}</span>
          <span>{t("formView:fileLimits", { size: 100, count: FORM_FILE_LIMIT })}</span>
        </div>
      ) : property.type === "checkbox" ? (
        // A checkbox's bar holds its box and the words its respondent reads beside it, asked for as
        // "체크박스 라벨(선택 사항)", as Notion's builder (seen 2026-11-17: an 18px box 10px in, the words 12px after).
        <div className={styles.questionAnswer} data-kind="checkbox">
          <span className={styles.questionCheckbox} aria-hidden="true" />
          <textarea className={styles.questionCheckboxLabel} rows={1} value={question.checkboxLabel ?? ""}
            placeholder={t("formView:checkboxLabelPlaceholder")} aria-label={t("formView:checkboxLabel")}
            onChange={(event) => onChange({ checkboxLabel: event.target.value })} />
        </div>
      ) : <div className={styles.questionAnswer} aria-hidden="true" data-kind={property.type} data-long={question.longAnswer === true || undefined}>
        {choice ? <>{t("formView:choose")}<ChevronDown size="small" /></> : <>{t("formView:answerPlaceholder")}
          {/* A date's and a place's field ends in ⌃⌄, as Notion's builder (seen 2026-11-02). */}
          {property.type === "date" || property.type === "place" ? <ChevronUpDown size="small" /> : null}</>}
      </div>}
      {menuOpen ? <PopoverLayer>
        <div ref={menu} className={styles.questionMenu} role="dialog" aria-label={t("formView:questionOptions")}
          style={popoverPlacementStyle(place)} data-form-question-menu>
          {propertyOpen && editing && graphProperty ? <>
            <div className={formStyles.addMenuHeader}>
              <button type="button" aria-label={t("common:actions.back")} data-form-property-back onClick={() => setPropertyOpen(false)}><ArrowLeft size="small" aria-hidden="true" /></button>
              <span className={formStyles.addMenuHeaderTitle}>{t("formView:propertyEdit")}</span>
              <button type="button" aria-label={t("common:actions.close")} onClick={() => { setPropertyOpen(false); setMenuOpen(false); }}><X size="tiny" aria-hidden="true" /></button>
            </div>
            <DatabasePropertyEditPanel property={graphProperty} editing={editing} layoutCard onClose={() => setPropertyOpen(false)} />
          </> : <>
          <div className={formStyles.addMenuHeader}>
            <span>{t("formView:questionOptions")}</span>
            <button type="button" aria-label={t("common:actions.close")} onClick={() => setMenuOpen(false)}><X size="tiny" aria-hidden="true" /></button>
          </div>
          {toggle(<MarkOf d={REQUIRED_MARK} />, t("formView:answerRequired"), question.required, (on) => onChange({ required: on }), "required")}
          {toggle(<AlignLeftIcon aria-hidden="true" />, t("formView:description"), describes, (on) => {
            setDescribing(on);
            if (!on) onChange({ description: "" });
          }, "description")}
          {property.type === "rich_text" || property.type === "title"
            ? toggle(<ArrowsLeftRight aria-hidden="true" />, t("formView:longAnswer"), question.longAnswer === true, (on) => onChange({ longAnswer: on }), "long")
            : null}
          {property.type === "date" ? <>
            {toggle(<CalendarIcon aria-hidden="true" />, t("formView:includeEndDate"), question.includeEndDate === true, (on) => onChange({ includeEndDate: on || undefined }), "end-date")}
            {toggle(<ClockIcon aria-hidden="true" />, t("formView:includeTime"), question.includeTime === true, (on) => onChange({ includeTime: on || undefined }), "time")}
          </> : null}
          {choice ? (
            <button type="button" className={styles.questionOption} aria-expanded={displayOpen} data-form-question-option="display"
              onClick={() => setDisplayOpen((open) => !open)}>
              <BulletedListIcon aria-hidden="true" />
              <span className={styles.questionOptionName}>{t("formView:optionsDisplay")}</span>
              <span className={styles.questionOptionValue}>
                {t(question.optionsDisplay === "dropdown" ? "formView:optionsDropdown" : "formView:optionsList")}
                {displayOpen ? <ChevronDown size="small" aria-hidden="true" /> : <ChevronRight size="small" aria-hidden="true" />}
              </span>
            </button>
          ) : null}
          {choice && displayOpen ? (["list", "dropdown"] as const).map((display) => (
            <button key={display} type="button" role="menuitemradio" aria-checked={(question.optionsDisplay ?? "list") === display}
              className={`${styles.questionOption} ${styles.questionSubOption}`} data-form-options-display={display}
              onClick={() => onChange({ optionsDisplay: display })}>
              <span>{t(display === "dropdown" ? "formView:optionsDropdown" : "formView:optionsList")}</span>
              {(question.optionsDisplay ?? "list") === display ? <CheckIcon size="small" aria-hidden="true" /> : null}
            </button>
          )) : null}
          {multiple ? (
            <button ref={mostRow} type="button" className={styles.questionOption} aria-haspopup="menu" aria-expanded={mostOpen}
              data-form-question-option="most" disabled={mostFixed} onClick={() => setMostOpen((open) => !open)}>
              <HashIcon aria-hidden="true" />
              <span className={styles.questionOptionName}>{t("formView:maxSelections")}</span>
              <span className={styles.questionOptionValue}>{most ?? t("formView:noSelectionLimit")}
                {mostFixed ? null : mostOpen ? <ChevronDown size="small" aria-hidden="true" /> : <ChevronRight size="small" aria-hidden="true" />}</span>
            </button>
          ) : null}
          {mostOpen ? (
            <div ref={mostMenu} className={styles.questionMoveMenu} role="menu" aria-label={t("formView:maxSelections")}
              style={popoverPlacementStyle(mostPlace)} data-form-question-most>
              {[undefined, 1, 2, 3, 4, 5, 10].map((most) => (
                <button key={most ?? "any"} type="button" role="menuitemradio" aria-checked={question.maxSelections === most} className={styles.questionOption}
                  onClick={() => { setMostOpen(false); onChange({ maxSelections: most }); }}>
                  <span className={styles.questionOptionName}>{most ?? t("formView:noSelectionLimit")}</span>
                  {question.maxSelections === most ? <CheckIcon size="small" aria-hidden="true" /> : null}
                </button>
              ))}
              <input className={styles.questionMostCustom} type="number" min={1} placeholder={t("formView:customSelectionLimit")} aria-label={t("formView:customSelectionLimit")}
                defaultValue={question.maxSelections !== undefined && ![1, 2, 3, 4, 5, 10].includes(question.maxSelections) ? question.maxSelections : ""}
                onKeyDown={(event) => {
                  if (!fieldEnter(event)) return;
                  const value = Math.floor(Number(event.currentTarget.value));
                  if (value >= 1) { setMostOpen(false); onChange({ maxSelections: value }); }
                }} />
            </div>
          ) : null}
          <div className={formStyles.questionMenuDivider} />
          {/* A select's question is "객관식", as Notion names it (seen 2026-11-04); the rest by their property's type.
              A title's and a relation's open too, with nothing ticked (Notion's, seen 2026-11-08). */}
          {onChangeType ? (
            <button ref={typeRow} type="button" className={styles.questionOption} aria-haspopup="menu" aria-expanded={typesOpen}
              data-form-question-option="type" onClick={() => setTypesOpen((open) => !open)}>
              <MoreAppsIcon aria-hidden="true" /><span className={styles.questionOptionName}>{t("formView:questionType")}</span>
              <span className={styles.questionOptionValue}>{typeName}{typesOpen ? <ChevronDown size="small" aria-hidden="true" /> : <ChevronRight size="small" aria-hidden="true" />}</span>
            </button>
          ) : (
            // Where the question's kind can't change here, it reads greyed.
            <button type="button" className={styles.questionOption} disabled data-form-question-option="type">
              <MoreAppsIcon aria-hidden="true" /><span className={styles.questionOptionName}>{t("formView:questionType")}</span>
              <span className={styles.questionOptionValue}>{typeName}</span>
            </button>
          )}
          {typesOpen ? (
            <div ref={typeMenu} className={styles.questionMoveMenu} role="menu" aria-label={t("formView:questionType")}
              style={popoverPlacementStyle(typePlace)} data-form-question-types>
              {kinds.map(([type, kind]) => {
                const chosen = type === property.type || (kind === "choice" && (property.type === "select" || property.type === "multi_select"));
                return (
                  <button key={type} type="button" role="menuitemradio" aria-checked={chosen} className={styles.questionOption} data-form-question-type={type}
                    onClick={() => { setTypesOpen(false); if (!chosen) onChangeType?.(type); }}>
                    {/* "제목" offered back reads with the text's lines, not "Aa", as Notion's list (seen 2026-11-09). */}
                    <PropertyTypeIcon type={type === "title" ? "rich_text" : type} /><span className={styles.questionOptionName}>{kind === "title" ? t("formView:titleKind") : t(`formView:newQuestionTypes.${kind}`)}</span>
                    {chosen ? <CheckIcon size="small" aria-hidden="true" /> : null}
                  </button>
                );
              })}
            </div>
          ) : null}
          {editing && graphProperty ? (
            <button type="button" className={styles.questionOption} aria-haspopup="dialog" data-form-question-option="property" onClick={() => setPropertyOpen(true)}>
              <DatabaseCylinderIcon aria-hidden="true" /><span className={styles.questionOptionName}>{t("formView:linkedProperty")}</span><ChevronRight size="small" aria-hidden="true" />
            </button>
          ) : <div className={styles.questionFact}><DatabaseCylinderIcon aria-hidden="true" /><span className={styles.questionOptionName}>{t("formView:linkedProperty")}</span><span className={styles.questionOptionValue}>{property.name}</span></div>}
          {toggle(<SyncIcon aria-hidden="true" />, t("formView:syncPropertyName"), question.syncWithPropertyName,
            (on) => onChange({ syncWithPropertyName: on, label: on && !question.ownLabel ? property.name : label }), "sync")}
          <div className={formStyles.questionMenuDivider} />
          {onLeaveGroup ? (
            <button type="button" className={styles.questionOption} data-form-question-option="leave-group" onClick={() => { setMenuOpen(false); onLeaveGroup(); }}>
              <ArrowsLeftRight aria-hidden="true" /><span className={styles.questionOptionName}>{t("formView:conditionLeave")}</span>
            </button>
          ) : (
            <button ref={moveRow} type="button" className={styles.questionOption} aria-haspopup="menu" aria-expanded={moveOpen}
              disabled={count < 2} data-form-question-option="move" onClick={() => setMoveOpen((open) => !open)}>
              <ArrowsDownUpIcon aria-hidden="true" /><span className={styles.questionOptionName}>{t("formView:moveQuestion")}</span><ChevronRight size="small" aria-hidden="true" />
            </button>
          )}
          {moveOpen ? (
            <div ref={moveMenu} className={styles.questionMoveMenu} role="menu" aria-label={t("formView:moveQuestion")}
              style={popoverPlacementStyle(movePlace)} data-form-question-move>
              {([[-1, "moveUp", index === 0], [1, "moveDown", index === count - 1],
                ["first", "moveToGroupTop", index === 0], ["last", "moveToGroupBottom", index === count - 1]] as const).map(([to, key, off]) => (
                <button key={key} type="button" role="menuitem" className={styles.questionOption} disabled={off}
                  onClick={() => { setMoveOpen(false); onMove(to); }}>{t(`formView:${key}`)}</button>
              ))}
            </div>
          ) : null}
          {onAddCondition && formQuestionTakesCondition(property.type) && (property.type === "checkbox" || options.length > 0) ? (
            <button type="button" className={styles.questionOption} data-form-question-option="condition" onClick={() => { setMenuOpen(false); onAddCondition(); }}>
              <MarkOf d={CONDITION_MARK} /><span className={styles.questionOptionName}>{t("formView:conditionAdd")}</span>
            </button>
          ) : null}
          {formQuestionTypeMadeHere(property.type) ? (
            <button type="button" className={styles.questionOption} disabled={!canDuplicate} onClick={() => { setMenuOpen(false); onDuplicate(); }}>
              <Copy aria-hidden="true" /><span className={styles.questionOptionName}>{t("formView:duplicateQuestion")}</span>
            </button>
          ) : null}
          <button type="button" className={styles.questionOption} onClick={() => { setMenuOpen(false); onRemove(); }}>
            <Trash aria-hidden="true" /><span className={styles.questionOptionName}>{t("formView:removeQuestion")}</span>
          </button>
          </>}
        </div></PopoverLayer> : null}
    </section>
  );
}
