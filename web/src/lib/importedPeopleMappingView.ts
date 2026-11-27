import {
  productErrorMessage,
  type ProductErrorContext,
} from "@/lib/productErrorPresentation";

/** The part of Settings › People that holds the imported people, as an address names it
 * (`/settings?section=people&part=imported-people`, the Notion migration report's link). */
export const IMPORTED_PEOPLE_PART = "imported-people";

export interface ImportedPersonEntry {
  sourceId: string;
  displayName: string | null;
  email: string | null;
  propertyValueCount: number;
  mentionCount: number;
  suggestedUserId: string | null;
  suggestedDisplayName: string | null;
}

export interface ImportedPeopleMember {
  userId: string;
  displayName: string | null;
  email: string | null;
  role: string | null;
}

export interface ImportedPeopleResult {
  people: ImportedPersonEntry[];
  members: ImportedPeopleMember[];
  nextCursor: string;
  hasMore: boolean;
}

export interface ImportedPeopleApplySummary {
  mappedPeople: number;
  changedPages: number;
  changedBlocks: number;
  skippedPages: number;
}

export interface ImportedPeopleMappingService {
  listPeople(
    workspaceId: string,
    cursor?: string,
  ): Promise<ImportedPeopleResult>;
  applyMappings(
    workspaceId: string,
    mappings: Record<string, string>,
  ): Promise<ImportedPeopleApplySummary>;
}

export interface ImportedPeopleMappingBounds {
  maxPeople: number;
  maxMembers: number;
  maxScalarCharacters: number;
  maxTotalCharacters: number;
}

type StringBudget = { characters: number };

function assertBounds(bounds: ImportedPeopleMappingBounds) {
  for (const [name, value] of Object.entries(bounds)) {
    if (!Number.isSafeInteger(value) || value < 0) {
      throw new RangeError(`Imported people ${name} must be a non-negative safe integer.`);
    }
  }
}

function reserveString(
  value: string | null,
  bounds: ImportedPeopleMappingBounds,
  budget: StringBudget,
) {
  if (value === null) return;
  if (value.length > bounds.maxScalarCharacters) {
    throw new RangeError("Imported people scalar bound exceeded.");
  }
  budget.characters += value.length;
  if (!Number.isSafeInteger(budget.characters)
    || budget.characters > bounds.maxTotalCharacters) {
    throw new RangeError("Imported people total string bound exceeded.");
  }
}

function assertCount(value: number) {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new RangeError("Imported people usage count is invalid.");
  }
}

export function admitImportedPeopleResult(
  result: ImportedPeopleResult,
  bounds: ImportedPeopleMappingBounds,
) {
  assertBounds(bounds);
  if (result.people.length > bounds.maxPeople) {
    throw new RangeError("Imported people bound exceeded.");
  }
  if (result.members.length > bounds.maxMembers) {
    throw new RangeError("Imported people members bound exceeded.");
  }
  const budget = { characters: 0 };
  for (const person of result.people) {
    reserveString(person.sourceId, bounds, budget);
    reserveString(person.displayName, bounds, budget);
    reserveString(person.email, bounds, budget);
    reserveString(person.suggestedUserId, bounds, budget);
    reserveString(person.suggestedDisplayName, bounds, budget);
    assertCount(person.propertyValueCount);
    assertCount(person.mentionCount);
  }
  for (const member of result.members) {
    reserveString(member.userId, bounds, budget);
    reserveString(member.displayName, bounds, budget);
    reserveString(member.email, bounds, budget);
    reserveString(member.role, bounds, budget);
  }
  return result;
}

export function importedPeopleSuggestedSelection(
  result: ImportedPeopleResult,
  bounds: ImportedPeopleMappingBounds,
) {
  admitImportedPeopleResult(result, bounds);
  const suggested: Record<string, string> = {};
  for (const person of result.people) {
    if (person.suggestedUserId) {
      suggested[person.sourceId] = person.suggestedUserId;
    }
  }
  return suggested;
}

export function importedPeopleSelectedMappings(
  selection: Record<string, string>,
  bounds: ImportedPeopleMappingBounds,
) {
  assertBounds(bounds);
  const entries = Object.entries(selection);
  if (entries.length > bounds.maxPeople) {
    throw new RangeError("Imported people mapping bound exceeded.");
  }
  const budget = { characters: 0 };
  const mappings: Record<string, string> = {};
  for (const [sourceId, userId] of entries) {
    reserveString(sourceId, bounds, budget);
    reserveString(userId, bounds, budget);
    if (userId) mappings[sourceId] = userId;
  }
  return mappings;
}

export const IMPORTED_PEOPLE_FAILURE_STAGES = [
  "scan",
  "apply",
  "refresh",
] as const;

export type ImportedPeopleFailureStage =
  typeof IMPORTED_PEOPLE_FAILURE_STAGES[number];

const importedPeopleFailureContexts = {
  scan: "importedPeople.scan",
  apply: "importedPeople.apply",
  refresh: "importedPeople.refresh",
} as const satisfies Record<ImportedPeopleFailureStage, ProductErrorContext>;

export function importedPeopleFailureMessage(
  error: unknown,
  stage: ImportedPeopleFailureStage,
) {
  return productErrorMessage(error, importedPeopleFailureContexts[stage]);
}
