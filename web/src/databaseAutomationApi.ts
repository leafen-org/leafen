import { newId } from "@/lib/ids";
import type { AutomationAction, DatabaseAutomationDefinition } from "@/lib/types";
import type { Session } from "@/types";
import {
  databaseAutomationOperations,
  type DatabaseAutomationDefinitionInput,
  type DatabaseAutomationDefinitionWire,
  type DatabaseAutomationListResponse,
  type DatabaseAutomationMutationInput,
  type DatabaseAutomationMutationResponse,
  type DatabaseAutomationScope,
  type DatabaseAutomationTrigger,
} from "./databaseAutomationWire.generated";

type JsonRequest = <T>(
  path: string,
  init?: RequestInit,
  session?: Session | null,
) => Promise<T>;

function automationPath(
  template: string,
  pageId: string,
  databaseId: string,
  automationId = "",
) {
  return template
    .replace("{pageId}", encodeURIComponent(pageId))
    .replace("{databaseId}", encodeURIComponent(databaseId))
    .replace("{automationId}", encodeURIComponent(automationId));
}

export function createDatabaseAutomationApi(request: JsonRequest) {
  return {
    listDatabaseAutomations: (
      session: Session,
      pageId: string,
      databaseId: string,
      signal?: AbortSignal,
    ) => request<DatabaseAutomationListResponse>(
      automationPath(databaseAutomationOperations.list.path, pageId, databaseId),
      { method: databaseAutomationOperations.list.method, signal },
      session,
    ),
    createDatabaseAutomation: (
      session: Session,
      pageId: string,
      databaseId: string,
      input: DatabaseAutomationDefinitionInput,
      signal?: AbortSignal,
    ) => request<DatabaseAutomationMutationResponse>(
      automationPath(databaseAutomationOperations.create.path, pageId, databaseId),
      { method: databaseAutomationOperations.create.method, body: JSON.stringify(input), signal },
      session,
    ),
    updateDatabaseAutomation: (
      session: Session,
      pageId: string,
      databaseId: string,
      automationId: string,
      input: DatabaseAutomationDefinitionInput,
      signal?: AbortSignal,
    ) => request<DatabaseAutomationMutationResponse>(
      automationPath(databaseAutomationOperations.update.path, pageId, databaseId, automationId),
      { method: databaseAutomationOperations.update.method, body: JSON.stringify(input), signal },
      session,
    ),
    deleteDatabaseAutomation: (
      session: Session,
      pageId: string,
      databaseId: string,
      automationId: string,
      input: DatabaseAutomationMutationInput,
      signal?: AbortSignal,
    ) => request<DatabaseAutomationMutationResponse>(
      automationPath(databaseAutomationOperations.delete.path, pageId, databaseId, automationId),
      { method: databaseAutomationOperations.delete.method, body: JSON.stringify(input), signal },
      session,
    ),
    setDatabaseAutomationEnabled: (
      session: Session,
      pageId: string,
      databaseId: string,
      automationId: string,
      input: DatabaseAutomationMutationInput,
      signal?: AbortSignal,
    ) => request<DatabaseAutomationMutationResponse>(
      automationPath(databaseAutomationOperations.enabled.path, pageId, databaseId, automationId),
      { method: databaseAutomationOperations.enabled.method, body: JSON.stringify(input), signal },
      session,
    ),
    resumeDatabaseAutomation: (
      session: Session,
      pageId: string,
      databaseId: string,
      automationId: string,
      input: DatabaseAutomationMutationInput,
      signal?: AbortSignal,
    ) => request<DatabaseAutomationMutationResponse>(
      automationPath(databaseAutomationOperations.resume.path, pageId, databaseId, automationId),
      { method: databaseAutomationOperations.resume.method, body: JSON.stringify(input), signal },
      session,
    ),
  };
}

export type DatabaseAutomationApi = ReturnType<typeof createDatabaseAutomationApi>;

export type DatabaseAutomationDraft = {
  name: string;
  scope: DatabaseAutomationScope;
  trigger: DatabaseAutomationTrigger;
  actionDocument: { version: 1; label: string; actions: AutomationAction[] };
};

function assertDefinitionIdentity(
  wire: DatabaseAutomationDefinitionWire,
  pageId: string,
  databaseId: string,
  automationId?: string,
) {
  if (
    wire.pageId !== pageId
    || wire.databaseId !== databaseId
    || (automationId !== undefined && wire.id !== automationId)
  ) {
    throw new Error("Invalid database automation response identity.");
  }
}

function mapDefinition(
  wire: DatabaseAutomationDefinitionWire,
  pageId: string,
  databaseId: string,
): DatabaseAutomationDefinition {
  assertDefinitionIdentity(wire, pageId, databaseId);
  if (wire.status === "deleted") {
    throw new Error("Invalid database automation list status.");
  }
  return {
    id: wire.id,
    workspaceId: wire.workspaceId,
    databaseId: wire.databaseId,
    name: wire.name,
    enabled: wire.status !== "disabled",
    scopeType: wire.scope.type,
    ...(wire.scope.viewId ? { viewId: wire.scope.viewId } : {}),
    triggerType: wire.triggerType,
    trigger: structuredClone(wire.trigger),
    actionDocument: structuredClone(wire.actionDocument),
    ...(wire.nextRunAt ? { nextRunAt: wire.nextRunAt } : {}),
    status: wire.status,
    revision: wire.revision,
    ...(wire.pausedReason ? { pausedReason: wire.pausedReason } : {}),
    ...(wire.createdBy ? { createdBy: wire.createdBy } : {}),
    createdAt: new Date(wire.createdAt).toISOString(),
    updatedAt: new Date(wire.updatedAt).toISOString(),
  };
}

function definitionInput(
  mutationId: string,
  draft: DatabaseAutomationDraft,
  expectedRevision?: number,
): DatabaseAutomationDefinitionInput {
  return {
    mutationId,
    ...(expectedRevision === undefined ? {} : { expectedRevision }),
    name: draft.name,
    scope: structuredClone(draft.scope),
    trigger: structuredClone(draft.trigger),
    actionDocument: structuredClone(draft.actionDocument),
  };
}

export type DatabaseAutomationService = {
  identity: string;
  /** The page holding the database, whose room tells of its automations' changes (lib/automationRevisionEvents.ts). */
  pageId?: string;
  list(signal: AbortSignal): Promise<DatabaseAutomationDefinition[]>;
  create(draft: DatabaseAutomationDraft, signal?: AbortSignal): Promise<DatabaseAutomationDefinition>;
  update(current: DatabaseAutomationDefinition, draft: DatabaseAutomationDraft, signal?: AbortSignal): Promise<DatabaseAutomationDefinition>;
  remove(current: DatabaseAutomationDefinition, signal?: AbortSignal): Promise<void>;
  setEnabled(current: DatabaseAutomationDefinition, enabled: boolean, signal?: AbortSignal): Promise<DatabaseAutomationDefinition>;
  resume(current: DatabaseAutomationDefinition, signal?: AbortSignal): Promise<DatabaseAutomationDefinition>;
};

export function createDatabaseAutomationService(
  api: DatabaseAutomationApi,
  session: Session,
  pageId: string,
  databaseId: string,
  createMutationId: () => string = newId,
): DatabaseAutomationService {
  const mapMutation = (
    response: DatabaseAutomationMutationResponse,
    automationId?: string,
  ) => {
    assertDefinitionIdentity(response.definition, pageId, databaseId, automationId);
    return mapDefinition(response.definition, pageId, databaseId);
  };
  return {
    identity: JSON.stringify([session.user.id, session.sessionId ?? "", session.token, pageId, databaseId]),
    pageId,
    async list(signal) {
      const response = await api.listDatabaseAutomations(session, pageId, databaseId, signal);
      if (response.nextCursor !== "") {
        throw new Error("Invalid database automation list continuation.");
      }
      try {
        return response.items.map((item) => mapDefinition(item, pageId, databaseId));
      } catch (cause) {
        throw new Error("Invalid database automation list response.", { cause });
      }
    },
    async create(draft, signal) {
      const response = await api.createDatabaseAutomation(
        session, pageId, databaseId, definitionInput(createMutationId(), draft), signal,
      );
      return mapMutation(response);
    },
    async update(current, draft, signal) {
      const response = await api.updateDatabaseAutomation(
        session, pageId, databaseId, current.id,
        definitionInput(createMutationId(), draft, current.revision), signal,
      );
      return mapMutation(response, current.id);
    },
    async remove(current, signal) {
      const response = await api.deleteDatabaseAutomation(
        session, pageId, databaseId, current.id,
        { mutationId: createMutationId(), expectedRevision: current.revision }, signal,
      );
      assertDefinitionIdentity(response.definition, pageId, databaseId, current.id);
      if (response.definition.status !== "deleted") {
        throw new Error("Invalid database automation delete response.");
      }
    },
    async setEnabled(current, enabled, signal) {
      const response = await api.setDatabaseAutomationEnabled(
        session, pageId, databaseId, current.id,
        { mutationId: createMutationId(), expectedRevision: current.revision, enabled }, signal,
      );
      return mapMutation(response, current.id);
    },
    async resume(current, signal) {
      const response = await api.resumeDatabaseAutomation(
        session, pageId, databaseId, current.id,
        { mutationId: createMutationId(), expectedRevision: current.revision }, signal,
      );
      return mapMutation(response, current.id);
    },
  };
}
