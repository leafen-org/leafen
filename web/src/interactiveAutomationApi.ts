import {
  interactiveAutomationOperations,
  type InteractiveAutomationExecuteInput,
  type InteractiveAutomationExecuteResponse,
} from "@/interactiveAutomationWire.generated";
import type { GraphProperty } from "@/contentGraph";
import type { Session } from "@/types";
import { continueInteractiveAutomation } from "./interactiveAutomationContinuation";

/** A database an action may add to or edit, wherever its page is. */
export interface InteractiveAutomationTargetDatabase {
  dataSourceId: string;
  pageId: string;
  title: string;
  /** The database's own icon, as its sources list shows it. */
  icon?: string;
  iconType?: string;
  properties: GraphProperty[];
  /** A task database: an edit of all of it reads "작업 전체". */
  tasks?: boolean;
  /** What its rows without a name are called: its default template's name. */
  untitledRowName?: string;
  templates?: { id: string; name: string }[];
}

type JsonRequest = <T>(
  path: string,
  init?: RequestInit,
  session?: Session | null,
) => Promise<T>;

function executionPath(template: string, pageId: string, blockId: string) {
  return template
    .replace("{pageId}", encodeURIComponent(pageId))
    .replace("{blockId}", encodeURIComponent(blockId));
}

export function createInteractiveAutomationApi(request: JsonRequest) {
  return {
    // A database button runs for one row, on the page that holds its data source.
    executeDatabaseButton: (
      session: Session,
      target: { pageId: string; dataSourceId: string; rowId: string; propertyId: string },
      input: InteractiveAutomationExecuteInput,
      signal?: AbortSignal,
    ) => {
      const operation = interactiveAutomationOperations.databaseButtonExecute;
      const path = operation.path
        .replace("{pageId}", encodeURIComponent(target.pageId))
        .replace("{databaseId}", encodeURIComponent(target.dataSourceId))
        .replace("{rowId}", encodeURIComponent(target.rowId))
        .replace("{propertyId}", encodeURIComponent(target.propertyId));
      return continueInteractiveAutomation(next => request<InteractiveAutomationExecuteResponse>(
        path,
        { method: operation.method, body: JSON.stringify(next), signal },
        session,
      ), input, signal);
    },
    // A page's button block runs on the page that holds it.
    executePageButton: (
      session: Session,
      pageId: string,
      blockId: string,
      input: InteractiveAutomationExecuteInput,
      signal?: AbortSignal,
      onProgress?: (response: InteractiveAutomationExecuteResponse) => Promise<void>,
    ) => {
      const operation = interactiveAutomationOperations.pageButtonExecute;
      return continueInteractiveAutomation(next => request<InteractiveAutomationExecuteResponse>(
        executionPath(operation.path, pageId, blockId),
        { method: operation.method, body: JSON.stringify(next), signal },
        session,
      ), input, signal, onProgress);
    },
    readAutomationTargetDatabase: (
      session: Session,
      workspaceId: string,
      dataSourceId: string,
      signal?: AbortSignal,
    ) => {
      const operation = interactiveAutomationOperations.targetDatabaseRead;
      return request<InteractiveAutomationTargetDatabase>(
        operation.path
          .replace("{workspaceId}", encodeURIComponent(workspaceId))
          .replace("{dataSourceId}", encodeURIComponent(dataSourceId)),
        { method: operation.method, signal },
        session,
      );
    },
    executeLegacyTemplate: (
      session: Session,
      pageId: string,
      blockId: string,
      input: InteractiveAutomationExecuteInput,
      signal?: AbortSignal,
    ) => {
      const operation = interactiveAutomationOperations.legacyTemplateExecute;
      return request<InteractiveAutomationExecuteResponse>(
        executionPath(operation.path, pageId, blockId),
        { method: operation.method, body: JSON.stringify(input), signal },
        session,
      );
    },
  };
}
