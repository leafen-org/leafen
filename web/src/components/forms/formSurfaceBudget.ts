import type {
  DbProperty,
  FormPublicProperty,
  FormReferenceOption,
  FormViewConfig,
  FormViewQuestion,
} from "@/lib/types";
import { formAskedQuestions } from "@/lib/formViewModel";

type FormProperty = DbProperty | FormPublicProperty;

export type FormSurfaceBounds = {
  maxProperties: number;
  maxQuestions: number;
  maxOptionsPerProperty: number;
  maxReferenceOptionsPerProperty: number;
  maxSelectionsPerProperty: number;
  maxReferenceSelections: number;
  maxScalarCharacters: number;
  maxTotalCharacters: number;
};

type FormSurfaceBudget = {
  characters: number;
};

function assertBounds(bounds: FormSurfaceBounds) {
  for (const [name, value] of Object.entries(bounds)) {
    if (!Number.isSafeInteger(value) || value < 0) {
      throw new RangeError(`Form surface bound ${name} must be a non-negative safe integer.`);
    }
  }
}

function reserveString(
  value: unknown,
  bounds: FormSurfaceBounds,
  budget: FormSurfaceBudget,
) {
  if (typeof value !== "string") return;
  if (value.length > bounds.maxScalarCharacters) {
    throw new RangeError("Form surface scalar exceeds its caller-owned bound.");
  }
  budget.characters += value.length;
  if (budget.characters > bounds.maxTotalCharacters) {
    throw new RangeError("Form surface text exceeds its caller-owned aggregate bound.");
  }
}

function createBudget(bounds: FormSurfaceBounds) {
  assertBounds(bounds);
  return { characters: 0 } satisfies FormSurfaceBudget;
}

export function assertFormHeader(
  form: FormViewConfig,
  bounds: FormSurfaceBounds,
) {
  const budget = createBudget(bounds);
  reserveString(form.title, bounds, budget);
  reserveString(form.description, bounds, budget);
  reserveString(form.icon, bounds, budget);
  reserveString(form.cover, bounds, budget);
}

export function assertFormSubmitButton(
  form: FormViewConfig,
  label: string | undefined,
  bounds: FormSurfaceBounds,
) {
  const budget = createBudget(bounds);
  reserveString(label ?? form.submit.buttonLabel, bounds, budget);
  reserveString(form.submit.buttonColor, bounds, budget);
}

export function projectFormQuestionFields({
  form,
  properties,
  values,
  referenceOptions,
  referenceErrors,
  bounds,
}: {
  form: FormViewConfig;
  properties: FormProperty[];
  values: Record<string, unknown>;
  referenceOptions: Record<string, FormReferenceOption[] | undefined>;
  referenceErrors: Record<string, string | undefined>;
  bounds: FormSurfaceBounds;
}): {
  propertiesById: Map<string, FormProperty>;
  referenceSelectionCount: number;
  visibleQuestions: FormViewQuestion[];
} {
  const budget = createBudget(bounds);
  if (properties.length > bounds.maxProperties) {
    throw new RangeError("Form surface properties exceed their caller-owned bound.");
  }
  if (form.questions.length > bounds.maxQuestions) {
    throw new RangeError("Form surface questions exceed their caller-owned bound.");
  }

  const asked = new Set(formAskedQuestions(form, values));
  const visibleQuestions: FormViewQuestion[] = [];
  for (const question of form.questions) {
    reserveString(question.id, bounds, budget);
    reserveString(question.propertyId, bounds, budget);
    reserveString(question.label, bounds, budget);
    reserveString(question.description, bounds, budget);
    if (
      question.maxSelections !== undefined
      && (
        !Number.isSafeInteger(question.maxSelections)
        || question.maxSelections < 0
        || question.maxSelections > bounds.maxSelectionsPerProperty
      )
    ) {
      throw new RangeError("Form question selections exceed their caller-owned bound.");
    }
    if (asked.has(question)) visibleQuestions.push(question);
  }

  const propertiesById = new Map<string, FormProperty>();
  let referenceSelectionCount = 0;
  for (const property of properties) {
    reserveString(property.id, bounds, budget);
    reserveString(property.name, bounds, budget);
    reserveString(property.description, bounds, budget);
    propertiesById.set(property.id, property);

    const options = Array.isArray(property.config?.options) ? property.config.options : [];
    if (options.length > bounds.maxOptionsPerProperty) {
      throw new RangeError("Form property options exceed their caller-owned bound.");
    }
    for (const option of options) {
      reserveString(option.id, bounds, budget);
      reserveString(option.name, bounds, budget);
    }

    const value = values[property.id];
    if (Array.isArray(value)) {
      if (value.length > bounds.maxSelectionsPerProperty) {
        throw new RangeError("Form answer selections exceed their caller-owned bound.");
      }
      for (const selected of value) reserveString(selected, bounds, budget);
      if (property.type === "person" || property.type === "relation") {
        referenceSelectionCount += value.length;
      }
    } else {
      reserveString(value, bounds, budget);
    }

    const references = referenceOptions[property.id];
    if (references) {
      if (references.length > bounds.maxReferenceOptionsPerProperty) {
        throw new RangeError("Form reference options exceed their caller-owned bound.");
      }
      // Each choice's text is bounded; the list isn't sent, so it adds
      // nothing to what an answer may hold.
      for (const option of references) {
        for (const text of [option.id, option.name, option.avatar]) {
          if (typeof text === "string" && text.length > bounds.maxScalarCharacters) {
            throw new RangeError("Form surface scalar exceeds its caller-owned bound.");
          }
        }
      }
    }
    reserveString(referenceErrors[property.id], bounds, budget);
  }

  if (referenceSelectionCount > bounds.maxReferenceSelections) {
    throw new RangeError("Form reference selections exceed their caller-owned bound.");
  }
  return { propertiesById, referenceSelectionCount, visibleQuestions };
}
