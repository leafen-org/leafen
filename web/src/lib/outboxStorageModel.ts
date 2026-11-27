import type { OutboxEntry } from "./outboxOperationTypes";

export interface OutboxStorageBounds {
  maxEntriesPerStep: number;
  maxEntryKeyCharacters: number;
  maxMutationsPerBatch: number;
  maxTabIdCharacters: number;
  maxTabIdsPerStep: number;
}

export interface OutboxEntryPage {
  entries: OutboxEntry[];
  nextAfterSeq: number | null;
}

export interface OutboxTabIdPage {
  nextAfterTabId: string | null;
  tabIds: string[];
}

interface OutboxStorageMutationBase {
  entryKey: string;
  ifUpdatedAt?: number | null;
  tabId: string;
}

export type OutboxStorageMutation = OutboxStorageMutationBase & (
  | {
    kind: "put";
    updatedAt: number;
    value: OutboxEntry["value"];
  }
  | {
    kind: "remove";
  }
);

export interface OutboxStorageMutationResult {
  applied: boolean;
}

export interface OutboxStorageModel {
  applyBatch(mutations: readonly OutboxStorageMutation[]): Promise<OutboxStorageMutationResult[]>;
  claimTabStep(input: {
    afterSeq: number | null;
    fromTabId: string;
    limit: number;
    toTabId: string;
  }): Promise<OutboxEntryPage>;
  clear(): Promise<void>;
  countEntries(): Promise<number>;
  listEntries(input: {
    afterSeq: number | null;
    limit: number;
    tabId?: string;
  }): Promise<OutboxEntryPage>;
  listEntriesByKey(input: {
    afterSeq: number | null;
    entryKey: string;
    limit: number;
  }): Promise<OutboxEntryPage>;
  listTabIds(input: {
    afterTabId: string | null;
    limit: number;
  }): Promise<OutboxTabIdPage>;
  put(entry: {
    entryKey: string;
    tabId: string;
    updatedAt: number;
    value: OutboxEntry["value"];
  }): Promise<void>;
  remove(tabId: string, entryKey: string): Promise<void>;
}

function assertPositiveSafeInteger(value: number, label: string): void {
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new RangeError(`${label} must be a positive safe integer.`);
  }
}

function assertCursor(value: number | null): void {
  if (value !== null && (!Number.isSafeInteger(value) || value < 0)) {
    throw new RangeError("afterSeq must be null or a non-negative safe integer.");
  }
}

function assertBoundedString(value: string, maximum: number, label: string): void {
  if (typeof value !== "string" || value.length > maximum) {
    throw new RangeError(`${label} exceeds its character bound.`);
  }
}

function assertStepLimit(value: number, maximum: number, label: string): void {
  assertPositiveSafeInteger(value, label);
  if (value > maximum) {
    throw new RangeError(`${label} exceeds its configured step bound.`);
  }
}

export function outboxDestinationSupersedesSource(
  destination: OutboxEntry,
  source: OutboxEntry,
): boolean {
  if (destination.updatedAt !== source.updatedAt) {
    return destination.updatedAt > source.updatedAt;
  }
  return destination.seq >= source.seq;
}

export function assertOutboxEntryKey(
  value: string,
  bounds: OutboxStorageBounds,
): void {
  assertBoundedString(value, bounds.maxEntryKeyCharacters, "entryKey");
}

export function assertOutboxTabId(value: string, bounds: OutboxStorageBounds): void {
  assertBoundedString(value, bounds.maxTabIdCharacters, "tabId");
}

export function assertOutboxEntryStep(
  afterSeq: number | null,
  limit: number,
  bounds: OutboxStorageBounds,
): void {
  assertCursor(afterSeq);
  assertStepLimit(limit, bounds.maxEntriesPerStep, "limit");
}

export function assertOutboxTabStep(
  afterTabId: string | null,
  limit: number,
  bounds: OutboxStorageBounds,
): void {
  if (afterTabId !== null) assertOutboxTabId(afterTabId, bounds);
  assertStepLimit(limit, bounds.maxTabIdsPerStep, "limit");
}

export function assertOutboxUpdatedAt(updatedAt: number): void {
  if (!Number.isFinite(updatedAt)) {
    throw new RangeError("updatedAt must be finite.");
  }
}

export function validateOutboxStorageBounds(bounds: OutboxStorageBounds): void {
  assertPositiveSafeInteger(bounds.maxEntriesPerStep, "maxEntriesPerStep");
  assertPositiveSafeInteger(bounds.maxEntryKeyCharacters, "maxEntryKeyCharacters");
  assertPositiveSafeInteger(bounds.maxMutationsPerBatch, "maxMutationsPerBatch");
  assertPositiveSafeInteger(bounds.maxTabIdCharacters, "maxTabIdCharacters");
  assertPositiveSafeInteger(bounds.maxTabIdsPerStep, "maxTabIdsPerStep");
}

export function outboxMutationMatches(
  existing: OutboxEntry | undefined,
  ifUpdatedAt: number | null | undefined,
): boolean {
  if (ifUpdatedAt === undefined) return true;
  if (ifUpdatedAt === null) return existing === undefined;
  return existing?.updatedAt === ifUpdatedAt;
}

export function validateOutboxStorageMutationBatch(
  mutations: readonly OutboxStorageMutation[],
  bounds: OutboxStorageBounds,
): void {
  if (!Array.isArray(mutations) || mutations.length > bounds.maxMutationsPerBatch) {
    throw new RangeError("Outbox storage mutation batch exceeds its configured bound.");
  }
  for (const mutation of mutations) {
    if (!mutation || (mutation.kind !== "put" && mutation.kind !== "remove")) {
      throw new TypeError("Outbox storage mutation is invalid.");
    }
    assertOutboxEntryKey(mutation.entryKey, bounds);
    assertOutboxTabId(mutation.tabId, bounds);
    if (mutation.ifUpdatedAt !== undefined && mutation.ifUpdatedAt !== null) {
      assertOutboxUpdatedAt(mutation.ifUpdatedAt);
    }
    if (mutation.kind === "put") assertOutboxUpdatedAt(mutation.updatedAt);
  }
}

export function outboxCompositeKey(tabId: string, entryKey: string): string {
  return JSON.stringify([tabId, entryKey]);
}
