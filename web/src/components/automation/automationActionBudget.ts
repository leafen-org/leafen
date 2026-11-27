import type {
  AutomationAction,
  DbProperty,
  DbView,
  Page,
} from "@/lib/types";

// Every option a property holds (PROPERTY_OPTION_CAPACITY in
// database/queryBudget.ts; this module stays import-free).
const PRESERVED_MAX_OPTIONS = 32_768;
const PRESERVED_MAX_RECIPIENTS = 20;

export type AutomationEditorBounds = {
  maxActions: number;
  maxProperties: number;
  maxPages: number;
  maxViews: number;
  maxOptionsPerProperty: number;
  maxVariablesPerAction: number;
  maxBlocksPerAction: number;
  maxChangesPerAction: number;
  maxRecipientsPerAction: number;
  maxWebhookHeaders: number;
  maxWebhookProperties: number;
  maxValueNodes: number;
  maxValueDepth: number;
  maxContainerEntries: number;
  maxScalarCharacters: number;
  maxTotalCharacters: number;
};

type AutomationEditorBudget = {
  nodes: number;
  entries: number;
  characters: number;
  seen: WeakSet<object>;
};

function assertBounds(bounds: AutomationEditorBounds) {
  for (const [name, value] of Object.entries(bounds)) {
    if (!Number.isSafeInteger(value) || value < 0) {
      throw new RangeError(`Automation editor bound ${name} must be a non-negative safe integer.`);
    }
  }
  if (
    bounds.maxOptionsPerProperty < PRESERVED_MAX_OPTIONS
    || bounds.maxRecipientsPerAction < PRESERVED_MAX_RECIPIENTS
  ) {
    throw new RangeError("Automation editor bounds cannot narrow preserved product limits.");
  }
  if (
    bounds.maxVariablesPerAction < 1
    || bounds.maxBlocksPerAction < 1
    || bounds.maxChangesPerAction < 1
    || bounds.maxValueNodes < 1
    || bounds.maxContainerEntries < 1
    || bounds.maxScalarCharacters < 1
    || bounds.maxTotalCharacters < 1
  ) {
    throw new RangeError("Automation editor structural bounds must admit one valid value.");
  }
}

function createBudget(bounds: AutomationEditorBounds): AutomationEditorBudget {
  assertBounds(bounds);
  return { nodes: 0, entries: 0, characters: 0, seen: new WeakSet() };
}

function reserveString(
  value: unknown,
  bounds: AutomationEditorBounds,
  budget: AutomationEditorBudget,
) {
  if (typeof value !== "string") return;
  if (value.length > bounds.maxScalarCharacters) {
    throw new RangeError("Automation editor scalar exceeds its caller-owned bound.");
  }
  budget.characters += value.length;
  if (budget.characters > bounds.maxTotalCharacters) {
    throw new RangeError("Automation editor text exceeds its caller-owned aggregate bound.");
  }
}

function admitValue(
  value: unknown,
  bounds: AutomationEditorBounds,
  budget: AutomationEditorBudget,
) {
  const stack: Array<{ value: unknown; depth: number }> = [{ value, depth: 0 }];
  while (stack.length > 0) {
    const current = stack.pop()!;
    if (current.depth > bounds.maxValueDepth) {
      throw new RangeError("Automation editor value exceeds its caller-owned depth bound.");
    }
    budget.nodes += 1;
    const nodes = budget.nodes;
    if (nodes > bounds.maxValueNodes) {
      throw new RangeError("Automation editor value exceeds its caller-owned node bound.");
    }
    reserveString(current.value, bounds, budget);
    if (!current.value || typeof current.value !== "object") continue;
    if (budget.seen.has(current.value)) continue;
    budget.seen.add(current.value);

    if (Array.isArray(current.value)) {
      budget.entries += current.value.length;
      const entries = budget.entries;
      if (
        current.value.length > bounds.maxContainerEntries
        || entries > bounds.maxContainerEntries
      ) {
        throw new RangeError("Automation editor arrays exceed their caller-owned entry bound.");
      }
      for (let index = current.value.length - 1; index >= 0; index -= 1) {
        stack.push({ value: current.value[index], depth: current.depth + 1 });
      }
      continue;
    }

    const record = current.value as Record<string, unknown>;
    let ownEntries = 0;
    for (const key in record) {
      if (!Object.prototype.hasOwnProperty.call(record, key)) continue;
      ownEntries += 1;
      budget.entries += 1;
      const entries = budget.entries;
      if (
        ownEntries > bounds.maxContainerEntries
        || entries > bounds.maxContainerEntries
      ) {
        throw new RangeError("Automation editor objects exceed their caller-owned entry bound.");
      }
      reserveString(key, bounds, budget);
      stack.push({ value: record[key], depth: current.depth + 1 });
    }
  }
}

function admitActionCollections(
  actions: readonly AutomationAction[],
  bounds: AutomationEditorBounds,
) {
  for (const action of actions) {
    if (action.type === "define_variables" && action.variables.length > bounds.maxVariablesPerAction) {
      throw new RangeError("Automation variables exceed their caller-owned bound.");
    }
    if (action.type === "insert_blocks" && (action.blocks?.length ?? 0) > bounds.maxBlocksPerAction) {
      throw new RangeError("Automation blocks exceed their caller-owned bound.");
    }
    if (action.type === "edit_pages" && action.changes.length > bounds.maxChangesPerAction) {
      throw new RangeError("Automation changes exceed their caller-owned bound.");
    }
    if (
      action.type === "send_notification"
      && (
        action.recipientIds.length > bounds.maxRecipientsPerAction
        || action.recipientIds.length > PRESERVED_MAX_RECIPIENTS
      )
    ) {
      throw new RangeError("Automation recipients exceed their caller-owned bound.");
    }
    if (action.type === "send_webhook") {
      if (action.headers.length > bounds.maxWebhookHeaders) {
        throw new RangeError("Automation webhook headers exceed their caller-owned bound.");
      }
      if (action.propertyIds.length > bounds.maxWebhookProperties) {
        throw new RangeError("Automation webhook properties exceed their caller-owned bound.");
      }
    }
  }
}

export function admitAutomationEditorInputs(
  actions: readonly AutomationAction[],
  properties: readonly DbProperty[],
  pages: readonly Page[],
  views: readonly DbView[],
  bounds: AutomationEditorBounds,
) {
  const budget = createBudget(bounds);
  if (actions.length > bounds.maxActions) {
    throw new RangeError("Automation actions exceed their caller-owned bound.");
  }
  if (properties.length > bounds.maxProperties) {
    throw new RangeError("Automation properties exceed their caller-owned bound.");
  }
  if (pages.length > bounds.maxPages) {
    throw new RangeError("Automation pages exceed their caller-owned bound.");
  }
  if (views.length > bounds.maxViews) {
    throw new RangeError("Automation views exceed their caller-owned bound.");
  }

  admitValue(actions, bounds, budget);
  admitActionCollections(actions, bounds);
  for (const property of properties) {
    reserveString(property.id, bounds, budget);
    reserveString(property.name, bounds, budget);
    reserveString(property.description, bounds, budget);
    const options = property.config?.options ?? [];
    if (
      options.length > bounds.maxOptionsPerProperty
      || options.length > PRESERVED_MAX_OPTIONS
    ) {
      throw new RangeError("Automation property options exceed their caller-owned bound.");
    }
    for (const option of options) {
      reserveString(option.id, bounds, budget);
      reserveString(option.name, bounds, budget);
    }
  }
  for (const page of pages) {
    reserveString(page.id, bounds, budget);
    reserveString(page.title, bounds, budget);
  }
  for (const view of views) {
    reserveString(view.id, bounds, budget);
    reserveString(view.name, bounds, budget);
  }
}

export function admitAutomationDocument(
  value: unknown,
  bounds: AutomationEditorBounds,
) {
  const budget = createBudget(bounds);
  admitValue(value, bounds, budget);
  const actions = (value as { actions?: unknown } | null)?.actions;
  if (
    Array.isArray(actions)
    && actions.length > bounds.maxActions
  ) {
    throw new RangeError("Automation document actions exceed their caller-owned bound.");
  }
}
