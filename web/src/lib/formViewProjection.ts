import { i18next } from "@/i18n";
import type {
  DbProperty,
  FormViewConfig,
} from "./types";
import {
  createFormQuestion,
  formAskedQuestions,
  isFormQuestionOffered,
  isFormViewProperty,
} from "./formViewModel";

export function createDefaultFormViewConfig(
  properties: DbProperty[],
  overrides: Partial<Pick<FormViewConfig, "title" | "description" | "icon" | "cover">> = {},
): FormViewConfig {
  const questions = properties
    .filter(isFormQuestionOffered)
    .sort((a, b) => a.position - b.position || a.id.localeCompare(b.id))
    .map(createFormQuestion);
  return {
    version: 1,
    title: overrides.title ?? "",
    description: overrides.description ?? "",
    icon: overrides.icon ?? "",
    cover: overrides.cover ?? "",
    questions,
    // A new form's button text and thanks are empty, showing Notion's words
    // in the reader's language ("제출", a thanks heading, a line that the answer went in),
    // and its thanks offer no "다른 응답 제출하기" (seen 2026-11-08).
    submit: {
      buttonLabel: "",
      confirmationTitle: "",
      confirmationBody: "",
      buttonColor: "blue",
      showSubmitAnother: false,
    },
  };
}

// Forms made before kept these English words as their defaults; they show in
// the reader's language, as the defaults now do.
const OLD_DEFAULT_BUTTON_LABEL = "Submit";
const OLD_DEFAULT_CONFIRMATION_TITLE = "Thanks for your response";

/** The submit button's label as it shows: an empty one is the default word. */
export function formSubmitButtonLabel(label: string) {
  return label === OLD_DEFAULT_BUTTON_LABEL || !label.trim() ? i18next.t("formView:defaults.submit") : label;
}

/** A sent form's thanks as it shows: an empty title or body is the default words. */
export function formConfirmationShown(confirmation: { confirmationTitle: string; confirmationBody: string }) {
  const old = confirmation.confirmationTitle === OLD_DEFAULT_CONFIRMATION_TITLE;
  return {
    confirmationTitle: old || !confirmation.confirmationTitle.trim() ? i18next.t("formView:defaults.thanks") : confirmation.confirmationTitle,
    confirmationBody: confirmation.confirmationBody.trim() ? confirmation.confirmationBody : i18next.t("formView:defaults.submitted"),
  };
}

export function formRequiresWorkspaceAudience(
  form: FormViewConfig,
  properties: Array<Pick<DbProperty, "id" | "type">>,
) {
  const byId = new Map(properties.map((property) => [property.id, property]));
  return form.questions.some((question) => {
    const type = byId.get(question.propertyId)?.type;
    return type === "person" || type === "relation";
  });
}

function normalizedText(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}

export function buildFormSubmissionAnswers(
  form: FormViewConfig,
  properties: Array<Pick<DbProperty, "id" | "type">>,
  values: Record<string, unknown>,
): Record<string, unknown> {
  const propertiesById = new Map(properties.map((property) => [property.id, property]));
  const answers: Record<string, unknown> = {};
  for (const question of formAskedQuestions(form, values)) {
    const property = propertiesById.get(question.propertyId);
    if (!property || !isFormViewProperty(property)) continue;
    const raw = values[property.id];
    if (property.type === "date" && raw && typeof raw === "object") {
      answers[property.id] = raw;
      continue;
    }
    if (property.type === "checkbox") {
      if (typeof raw === "boolean") answers[property.id] = raw;
      continue;
    }
    if (property.type === "files") {
      // The files sent for it, by id; the server makes them the page's.
      if (Array.isArray(raw)) {
        const ids = raw.flatMap((item) => item && typeof item === "object" && typeof (item as { fileId?: unknown }).fileId === "string"
          ? [(item as { fileId: string }).fileId] : []);
        if (ids.length) answers[property.id] = ids;
      }
      continue;
    }
    if (property.type === "place") {
      // A point, with the name and address typed for it.
      const place = raw && typeof raw === "object" ? raw as { lat?: unknown; lon?: unknown; name?: unknown; address?: unknown } : null;
      if (place && typeof place.lat === "number" && typeof place.lon === "number") {
        answers[property.id] = {
          lat: place.lat, lon: place.lon,
          ...(typeof place.name === "string" && place.name.trim() ? { name: place.name.trim() } : {}),
          ...(typeof place.address === "string" && place.address.trim() ? { address: place.address.trim() } : {}),
        };
      }
      continue;
    }
    if (property.type === "number") {
      if (raw === "" || raw === undefined || raw === null) continue;
      const number = typeof raw === "number" ? raw : Number(normalizedText(raw));
      if (Number.isFinite(number)) answers[property.id] = number;
      continue;
    }
    if (property.type === "multi_select" || property.type === "person" || property.type === "relation") {
      if (!Array.isArray(raw)) continue;
      // As many as the question takes: its own most, if it sets one.
      const selectionLimit = question.maxSelections === undefined
        ? Number.MAX_SAFE_INTEGER
        : Math.max(0, Math.floor(question.maxSelections));
      if (raw.length > selectionLimit) {
        throw new Error("Form selection exceeds its authoritative bound.");
      }
      const ids = raw
        .filter((value): value is string => typeof value === "string")
        .map((value) => value.trim())
        .filter((value, index, all) => value.length > 0 && all.indexOf(value) === index)
        .slice(0, selectionLimit);
      if (ids.length > 0) answers[property.id] = ids;
      continue;
    }
    const text = normalizedText(raw);
    if (text) answers[property.id] = text;
  }
  return answers;
}
