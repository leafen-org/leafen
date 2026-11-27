import { PersonAvatar } from "@/components/people/PersonAvatar";
import type { PlaceSearchResponse } from "@/administratorWire.generated";
import type { Page } from "@/types";
import { storedIconType } from "../page/nativeIcons";
import { PageCover } from "../page/PageCover";
import { PageIconGlyph } from "../page/PageIcon";
"use client";

import { useTranslation } from "react-i18next";
import type {
  DbProperty,
  FormPublicProperty,
  FormReferenceOption,
  FormViewConfig,
  FormViewQuestion,
} from "@/lib/types";
import { formAskedQuestions, formQuestionLabel, formQuestionMostSelections } from "@/lib/formViewModel";
import {
  assertFormHeader,
  assertFormSubmitButton,
  projectFormQuestionFields,
  type FormSurfaceBounds,
} from "./formSurfaceBudget";
import { formSubmitButtonLabel } from "@/lib/formViewProjection";
import styles from "./FormSurface.module.css";
import { FormDropdown } from "./FormDropdown";
import { FormReferencePicker, type FormReferenceTarget } from "./FormReferencePicker";
import { FormPlaceField } from "./FormPlaceField";
import { FormFileField, type FormFileAnswer } from "./FormFileField";
import { FormDateField } from "./FormDateField";

type FormProperty = DbProperty | FormPublicProperty;

/** A form's icon as a page's icon reads it: an emoji, a built-in icon's "name:color" or a picture's address. */
export function formIconSource(icon: string) {
  return { icon, iconType: storedIconType(icon) as Page["iconType"] };
}

/** A form's cover as a page's cover component reads it. */
export function formCoverPage(form: Pick<FormViewConfig, "cover" | "coverPosition">, pageId: string, workspaceId: string) {
  return { id: pageId, workspaceId, cover: form.cover, coverPosition: form.coverPosition ?? 50 } as Page;
}

/**
 * A form's cover across the top of its page, over the page's own margin, as
 * a page's cover is, the form's column under it (Notion's respondent page
 * and preview, seen 2026-11-08: 30% of the window high, the full width).
 */
export function FormCover({ form }: { form: FormViewConfig }) {
  if (!form.cover) return null;
  return (
    <div className={styles.cover} data-form-cover>
      <PageCover page={formCoverPage(form, "", "")} readOnly onChange={() => false} />
    </div>
  );
}

export function FormHeader({
  form,
  bounds,
}: {
  form: FormViewConfig;
  bounds: FormSurfaceBounds;
}) {
  assertFormHeader(form, bounds);
  // A form with no head of its own starts at its questions, as Notion's member page (seen 2026-11-03).
  if (!form.icon && !form.title && !form.description) return null;
  return (
    <header className={styles.header}>
      <div className={styles.headerBody}>
        {/* Its icon as a page's, 78 square, 8 in, 42 up into a cover (Notion's, seen 2026-11-08). */}
        {form.icon ? <span className={styles.icon} aria-hidden="true">
          <PageIconGlyph page={formIconSource(form.icon)} size={78} box={78} fallback="none" />
        </span> : null}
        {form.title ? <h1 className={styles.title}>{form.title}</h1> : null}
        {form.description && <p className={styles.description}>{form.description}</p>}
      </div>
    </header>
  );
}

/** "다음 이름으로 응답 제출 [나] 〈이름〉": who a signed-in respondent answers as, as Notion's form shows (seen 2026-10-08). */
export function FormRespondent({ respondent, anonymous = false }: { respondent: { id: string; name: string }; anonymous?: boolean }) {
  const { t } = useTranslation("formView");
  // An anonymous form says so in place of the name (Notion's, seen 2026-11-05).
  if (anonymous) return <p className={styles.respondent} data-form-respondent="anonymous"><span>{t("formView:respondingAnonymously")}</span></p>;
  return (
    <p className={styles.respondent} data-form-respondent>
      <span>{t("formView:respondingAs")}</span>
      <PersonAvatar userId={respondent.id} name={respondent.name} size={20} />
      <span className={styles.respondentName}>{respondent.name}</span>
    </p>
  );
}

function answerEmpty(value: unknown) {
  return value === undefined || value === null || value === false
    || (typeof value === "string" && value.trim() === "") || (Array.isArray(value) && value.length === 0);
}

/** The shown required questions still unanswered, by property, as Notion checks them on "보내기" (seen 2026-11-03). */
export function unansweredRequiredQuestions(form: FormViewConfig, properties: readonly FormProperty[], values: Record<string, unknown>) {
  const present = new Set(properties.map(({ id }) => id));
  return formAskedQuestions(form, values)
    .filter((question) => question.required && present.has(question.propertyId) && answerEmpty(values[question.propertyId]))
    .map((question) => question.propertyId);
}

/** A typed answer Notion turns back at "보내기", under its question (seen 2026-11-04). */
export type FormAnswerProblem = "email" | "number" | "url" | "phone";

// An email as the server takes one (form_submission_values.go validFormEmail).
function validEmail(text: string) {
  const at = text.lastIndexOf("@");
  return at > 0 && at < text.length - 3 && text.slice(at + 1).includes(".") && !/\s/.test(text);
}

// A web address the server keeps: http or https, with a host.
function validUrl(text: string) {
  try {
    const url = new URL(text);
    return (url.protocol === "http:" || url.protocol === "https:") && url.host !== "";
  } catch {
    return false;
  }
}

/**
 * The shown questions whose typed answer is not an email, a number, a web
 * address or a phone number as its property asks, by property: each says so
 * under its answer until it is one ("유효하지 않은 이메일" and the like).
 */
export function invalidAnswers(form: FormViewConfig, properties: readonly FormProperty[], values: Record<string, unknown>) {
  const types = new Map(properties.map(({ id, type }) => [id, type]));
  const problems: Record<string, FormAnswerProblem> = {};
  for (const question of formAskedQuestions(form, values)) {
    const raw = values[question.propertyId];
    const text = typeof raw === "string" ? raw.trim() : typeof raw === "number" ? String(raw) : "";
    if (!text) continue;
    const type = types.get(question.propertyId);
    if (type === "email" && !validEmail(text)) problems[question.propertyId] = "email";
    else if (type === "number" && !Number.isFinite(Number(text))) problems[question.propertyId] = "number";
    else if (type === "url" && !validUrl(text)) problems[question.propertyId] = "url";
    else if (type === "phone" && !(/^[\d\s()+.\-]+$/.test(text) && /\d/.test(text))) problems[question.propertyId] = "phone";
  }
  return problems;
}

function optionList(property: FormProperty) {
  return Array.isArray(property.config?.options) ? property.config.options : [];
}

function textField(
  property: FormProperty,
  question: FormViewQuestion,
  value: unknown,
  disabled: boolean,
  placeholder: string,
  onChange: (value: unknown) => void,
) {
  const common = {
    id: `form-answer-${property.id}`,
    name: property.id,
    disabled,
    placeholder,
    required: question.required,
    value: typeof value === "string" || typeof value === "number" ? value : "",
    onChange: (event: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => onChange(event.target.value),
  };
  if ((property.type === "rich_text" || property.type === "title") && question.longAnswer) {
    return <textarea {...common} className={styles.textarea} rows={3} />;
  }
  // A number is typed as text, so a wrong one says so as Notion's does.
  const type = property.type === "email"
      ? "email"
      : property.type === "phone"
        ? "tel"
        : property.type === "url"
          ? "url"
          : "text";
  return <input {...common} className={styles.input} type={type} inputMode={property.type === "number" ? "decimal" : undefined} />;
}

function choiceField(
  property: FormProperty,
  question: FormViewQuestion,
  value: unknown,
  disabled: boolean,
  onChange: (value: unknown) => void,
  label: string,
  placeholder: string,
) {
  const options = optionList(property);
  const multi = property.type === "multi_select";
  const selected = multi && Array.isArray(value)
    ? value.filter((item): item is string => typeof item === "string")
    : [];
  if (question.optionsDisplay === "dropdown") {
    return (
      <span className={styles.dropdown} data-form-answer={property.id}>
        {/* Several at most one go as one, as the list draws them round. */}
        <FormDropdown label={label} placeholder={placeholder} options={options} multiple={multi && question.maxSelections !== 1}
          max={question.maxSelections} disabled={disabled}
          values={multi ? selected : typeof value === "string" && value ? [value] : []}
          onChange={(next) => onChange(multi ? next : next[0] ?? "")} />
        {/* The browser stops an unanswered required dropdown as it did the select it replaced. */}
        {question.required && <input className={styles.dropdownRequired} tabIndex={-1} aria-hidden="true" required disabled={disabled}
          value={multi ? selected.join(",") : typeof value === "string" ? value : ""} onChange={() => undefined} />}
      </span>
    );
  }
  // A choice of at most one is round, as the builder and Notion draw it.
  const single = !multi || question.maxSelections === 1;
  return (
    <div className={styles.choiceList} role={single ? "radiogroup" : "group"} aria-required={question.required}>
      {options.map((option) => {
        const checked = multi ? selected.includes(option.id) : value === option.id;
        return (
          <label key={option.id} className={styles.choice}>
            <input
              className={styles.choiceInput}
              type={single ? "radio" : "checkbox"}
              name={property.id}
              value={option.id}
              checked={checked}
              disabled={disabled || (multi && !single && !checked && selected.length >= (question.maxSelections ?? Number.MAX_SAFE_INTEGER))}
              required={single && question.required}
              onChange={(event) => {
                if (!multi) {
                  onChange(option.id);
                  return;
                }
                if (single) {
                  onChange([option.id]);
                  return;
                }
                const next = event.currentTarget.checked
                  ? [...selected, option.id].slice(0, question.maxSelections ?? Number.MAX_SAFE_INTEGER)
                  : selected.filter((id) => id !== option.id);
                onChange(next);
              }}
            />
            <span className={styles.choiceMarker} data-radio={single || undefined} aria-hidden="true" />
            <span>{option.name}</span>
          </label>
        );
      })}
    </div>
  );
}

export function FormQuestionFields({
  form,
  bounds,
  properties,
  values,
  disabled = false,
  referenceOptions = {},
  referenceLoading = {},
  referenceErrors = {},
  referenceHasMore = {},
  referenceTaskDatabase = {},
  referenceTarget = {},
  respondentId,
  onRequestReferenceOptions,
  uploadFile,
  searchPlaces,
  missing = [],
  invalid = {},
  onChange,
}: {
  form: FormViewConfig;
  bounds: FormSurfaceBounds;
  properties: FormProperty[];
  values: Record<string, unknown>;
  /** Required questions left unanswered at "보내기": each says so under its answer, its "*" red. */
  missing?: readonly string[];
  /** Answers not of their kind, by property, once "보내기" was pressed (invalidAnswers). */
  invalid?: Readonly<Record<string, FormAnswerProblem>>;
  disabled?: boolean;
  referenceOptions?: Record<string, FormReferenceOption[] | undefined>;
  referenceLoading?: Record<string, boolean>;
  referenceErrors?: Record<string, string | undefined>;
  referenceHasMore?: Record<string, boolean>;
  /** The questions whose pages are a task database's, an untitled one "새 작업". */
  referenceTaskDatabase?: Record<string, boolean>;
  /** The database each relation's list reads, which it names. */
  referenceTarget?: Record<string, FormReferenceTarget>;
  /** Who answers: first in a person's list, as "〈이름〉(나)". */
  respondentId?: string;
  onRequestReferenceOptions?: (propertyId: string, append: boolean, query: string, pageFilter?: "all" | "verified" | "archived") => void;
  /** Sends a file for a file question; none where nothing can be sent, as in the builder's preview. */
  uploadFile?: (propertyId: string, file: File, requestId: string) => Promise<FormFileAnswer>;
  /** Searches places by name for a place question; none where the form can't, as in the builder's preview. */
  searchPlaces?: (propertyId: string, query: string, signal: AbortSignal) => Promise<PlaceSearchResponse>;
  onChange: (propertyId: string, value: unknown) => void;
}) {
  const { t } = useTranslation("formView");
  const { propertiesById, visibleQuestions } =
    projectFormQuestionFields({
      form,
      properties,
      values,
      referenceOptions,
      referenceErrors,
      bounds,
    });
  return (
    <div className={styles.questions}>
      {visibleQuestions.map((question) => {
        const property = propertiesById.get(question.propertyId);
        if (!property) return null;
        const label = formQuestionLabel(question, property);
        const value = values[property.id];
        // As many as the question takes: its own most, if it sets one.
        const referenceMax = formQuestionMostSelections(question, property) ?? Number.MAX_SAFE_INTEGER;
        const field = property.type === "person" || property.type === "relation"
          ? (
            <span className={styles.dropdown} data-form-answer={property.id}>
              <FormReferencePicker
                id={`form-answer-${property.id}`}
                kind={property.type}
                label={label || property.name}
                placeholder={t("formView:typedAnswerPlaceholder")}
                value={value}
                disabled={disabled}
                options={referenceOptions[property.id]}
                loading={referenceLoading[property.id] === true}
                error={referenceErrors[property.id]}
                hasMore={referenceHasMore[property.id] === true}
                taskDatabase={referenceTaskDatabase[property.id] === true}
                target={referenceTarget[property.id]}
                respondentId={respondentId}
                max={referenceMax}
                onRequest={(append, query, pageFilter) => onRequestReferenceOptions?.(property.id, append, query, pageFilter)}
                onChange={(next) => onChange(property.id, next)}
              />
              {question.required && <input className={styles.dropdownRequired} tabIndex={-1} aria-hidden="true" required disabled={disabled}
                value={Array.isArray(value) && value.length ? "1" : ""} onChange={() => undefined} />}
            </span>
          )
          : property.type === "select" || property.type === "status" || property.type === "multi_select"
          ? choiceField(property, question, value, disabled, (next) => onChange(property.id, next), label || property.name, t("formView:typedAnswerPlaceholder"))
          : property.type === "checkbox"
            ? (
              // Its label stands after its box and checks it too, as Notion's (seen 2026-11-17: 18px box, the words 12px after it).
              <label className={styles.checkboxAnswer}>
                <input
                  className={styles.choiceInput}
                  id={`form-answer-${property.id}`}
                  type="checkbox"
                  name={property.id}
                  disabled={disabled}
                  required={question.required}
                  checked={value === true}
                  onChange={(event) => onChange(property.id, event.currentTarget.checked)}
                />
                <span className={styles.choiceMarker} aria-hidden="true" />
                {question.checkboxLabel ? <span className={styles.checkboxLabel}>{question.checkboxLabel}</span> : null}
              </label>
            )
            : property.type === "date"
              ? <FormDateField property={property} question={question} value={value} disabled={disabled} label={label || property.name}
                placeholder={t("formView:typedAnswerPlaceholder")} clearLabel={t("common:actions.delete")} onChange={(next) => onChange(property.id, next)} />
              : property.type === "files"
                ? (
                  <>
                    <FormFileField id={property.id} label={label || property.name} value={value} disabled={disabled}
                      upload={uploadFile ? (file, requestId) => uploadFile(property.id, file, requestId) : undefined}
                      onChange={(next) => onChange(property.id, next)} />
                    {question.required && <input className={styles.dropdownRequired} tabIndex={-1} aria-hidden="true" required disabled={disabled}
                      value={Array.isArray(value) && value.length ? "1" : ""} onChange={() => undefined} />}
                  </>
                )
              : property.type === "place"
                ? (
                  <span className={styles.dropdown} data-form-answer={property.id}>
                    <FormPlaceField id={property.id} label={label || property.name} value={value} disabled={disabled}
                      search={searchPlaces ? (query, signal) => searchPlaces(property.id, query, signal) : undefined}
                      placeholder={t("formView:typedAnswerPlaceholder")} onChange={(next) => onChange(property.id, next)} />
                    {question.required && <input className={styles.dropdownRequired} tabIndex={-1} aria-hidden="true" required disabled={disabled}
                      value={value ? "1" : ""} onChange={() => undefined} />}
                  </span>
                )
              : textField(property, question, value, disabled, t("formView:typedAnswerPlaceholder"), (next) => onChange(property.id, next));
        return (
          <section className={styles.question} key={question.id} data-form-question={property.id}
            data-missing={missing.includes(property.id) || undefined}>
            <label className={styles.questionLabel} htmlFor={`form-answer-${property.id}`}>
              {label || property.name}
              {question.required && <span className={styles.required} aria-hidden="true">*</span>}
            </label>
            {question.description && <p className={styles.questionDescription}>{question.description}</p>}
            {/* How many a person's or a page's question takes, as Notion's says to its respondent (seen 2026-11-06). */}
            {property.type === "person" || property.type === "relation" ? <span className={styles.questionHint}>
              {formQuestionMostSelections(question, property) === undefined ? t("formView:pickAnyHint")
                : t("formView:pickMostHint", { count: formQuestionMostSelections(question, property) })}</span> : null}
            {/* A one-answer choice can be taken back, as Notion's (seen 2026-11-05). */}
            {question.optionsDisplay !== "dropdown" && !disabled && (property.type === "select" || property.type === "status"
              || (property.type === "multi_select" && question.maxSelections === 1))
              && (Array.isArray(value) ? value.length > 0 : Boolean(value)) ? (
              <button type="button" className={styles.clearChoice} data-form-clear-choice
                onClick={() => onChange(property.id, property.type === "multi_select" ? [] : "")}>{t("formView:clearChoice")}</button>
            ) : null}
            {field}
            {missing.includes(property.id) ? <p className={styles.missing} role="alert">{t("formView:requiredAnswer")}</p>
              : invalid[property.id] ? <p className={styles.missing} role="alert">{t(`formView:invalidAnswer.${invalid[property.id]}`)}</p> : null}
          </section>
        );
      })}
    </div>
  );
}

export function FormSubmitButton({
  form,
  bounds,
  disabled,
  label,
}: {
  form: FormViewConfig;
  bounds: FormSurfaceBounds;
  disabled?: boolean;
  label?: string;
}) {
  assertFormSubmitButton(form, label, bounds);
  return (
    <button
      type="submit"
      className={styles.submit}
      data-color={form.submit.buttonColor === "blue" ? undefined : form.submit.buttonColor}
      disabled={disabled}
    >
      {label ?? formSubmitButtonLabel(form.submit.buttonLabel)}
    </button>
  );
}
