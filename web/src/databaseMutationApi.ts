import type { GraphMutationInput, GraphMutationReceipt, GraphOperation } from "./contentGraph";
import type { Session } from "./types";
import { databaseDataSourceOperations } from "./databaseDataSourceWire.generated";
import { drainDatabasePropertyConversions } from "./databasePropertyConversionApi";
import { graphMutationOnTheWire } from "./lib/graphRowWire";
import {
  databaseMutationErrorCodes,
  databaseMutationOperations,
  databaseMutationPhases,
  databaseMutationStatuses,
  databaseMutationTargetKinds,
  type DatabaseMutationInput,
  type DatabaseMutationResponse,
  type DatabaseMutationTargetKind,
} from "./databaseMutationWire.generated";

const databaseMutationPhaseOrder: Record<DatabaseMutationTargetKind, string[]> = {
  [databaseMutationTargetKinds.source]: [
    databaseMutationPhases.sourceCells,
    databaseMutationPhases.sourceFiles,
    databaseMutationPhases.sourceRows,
    databaseMutationPhases.sourceViews,
    databaseMutationPhases.sourceProperties,
    databaseMutationPhases.sourceFinal,
    databaseMutationPhases.completed,
  ],
  [databaseMutationTargetKinds.property]: [
    databaseMutationPhases.propertyCells,
    databaseMutationPhases.propertyFiles,
    databaseMutationPhases.propertyFinal,
    databaseMutationPhases.completed,
  ],
  [databaseMutationTargetKinds.row]: [
    databaseMutationPhases.rowIncoming,
    databaseMutationPhases.rowCells,
    databaseMutationPhases.rowFiles,
    databaseMutationPhases.rowFinal,
    databaseMutationPhases.completed,
  ],
};

type JsonRequest = <T>(
  path: string,
  init?: RequestInit,
  session?: Session | null,
) => Promise<T>;

function destructiveTarget(operation: GraphOperation): {
  kind: DatabaseMutationTargetKind;
  id: string;
} | null {
  if (operation.kind === "deleteDataSource") {
    return { kind: databaseMutationTargetKinds.source, id: operation.dataSourceDeletion.id };
  }
  if (operation.kind === "deleteProperty") {
    return { kind: databaseMutationTargetKinds.property, id: operation.propertyDeletion.id };
  }
  if (operation.kind === "deleteRow") {
    return { kind: databaseMutationTargetKinds.row, id: operation.rowDeletion.id };
  }
  return null;
}

function relatedPropertyOf(operation: GraphOperation | undefined) {
  const related = operation?.kind === "deleteProperty" ? operation.propertyDeletion.relatedProperty : undefined;
  return related ? { relatedProperty: related } : {};
}

function databaseMutationPath(template: string, pageId: string, jobId = "") {
  return template
    .replace("{pageId}", encodeURIComponent(pageId))
    .replace("{jobId}", encodeURIComponent(jobId));
}

function abortError() {
  return new DOMException("Database mutation was aborted.", "AbortError");
}

function waitForNextDatabaseMutationTurn(signal?: AbortSignal) {
  if (signal && signal.aborted) return Promise.reject(abortError());
  return new Promise<void>((resolve, reject) => {
    const abort = () => {
      window.clearTimeout(timer);
      reject(abortError());
    };
    const timer = window.setTimeout(() => {
      signal?.removeEventListener("abort", abort);
      resolve();
    }, 25);
    signal?.addEventListener("abort", abort, { once: true });
  });
}

export function createDatabaseMutationApi(request: JsonRequest) {
  async function drainDatabaseMutation(
    session: Session,
    pageId: string,
    mutationId: string,
    targetKind: DatabaseMutationTargetKind,
    targetId: string,
    signal?: AbortSignal,
  ): Promise<GraphMutationReceipt> {
    if (signal && signal.aborted) throw abortError();
    const input: DatabaseMutationInput = {
      requestId: mutationId,
      mutationId,
      targetKind,
      targetId,
    };
    let response = await request<DatabaseMutationResponse>(
      databaseMutationPath(databaseMutationOperations.start.path, pageId),
      { method: databaseMutationOperations.start.method, body: JSON.stringify(input), signal },
      session,
    );
    const jobId = response.jobId;
    let revision = 0;
    let processed = 0;
    let phase = -1;
    for (;;) {
      if (signal && signal.aborted) throw abortError();
      const nextPhase = databaseMutationPhaseOrder[targetKind].indexOf(response.phase);
      if (
        !jobId || response.jobId !== jobId || response.pageId !== pageId ||
        response.mutationId !== mutationId || response.targetKind !== targetKind ||
        response.targetId !== targetId ||
        (response.status !== databaseMutationStatuses.pending &&
          response.status !== databaseMutationStatuses.completed) ||
        !Number.isSafeInteger(response.progress.revision) || response.progress.revision < 1 ||
        !Number.isSafeInteger(response.progress.processed) || response.progress.processed < 0 ||
        response.progress.revision < revision || response.progress.processed < processed ||
        nextPhase < phase || nextPhase < 0 ||
        (response.status === databaseMutationStatuses.completed) !==
          (response.phase === databaseMutationPhases.completed)
      ) {
        throw new Error(databaseMutationErrorCodes.unavailable);
      }
      revision = response.progress.revision;
      processed = response.progress.processed;
      phase = nextPhase;
      if (response.status === databaseMutationStatuses.completed) {
        if (!response.receipt || response.receipt.mutationId !== mutationId) {
          throw new Error(databaseMutationErrorCodes.unavailable);
        }
        return response.receipt;
      }
      await waitForNextDatabaseMutationTurn(signal);
      response = await request<DatabaseMutationResponse>(
        databaseMutationPath(databaseMutationOperations.read.path, pageId, jobId),
        { method: databaseMutationOperations.read.method, signal },
        session,
      );
    }
  }

  return {
    waitForPageTextStored: (session: Session, pageId: string, state: string, signal?: AbortSignal) =>
      request<{ stored: boolean }>(`/pages/${encodeURIComponent(pageId)}/text-stored`, {
        method: "POST", body: JSON.stringify({ state }), signal,
      }, session),
    mutateContentGraph: (
      session: Session,
      pageId: string,
      input: GraphMutationInput,
      signal?: AbortSignal,
    ) => {
      const target = input.operations.length === 1 ? destructiveTarget(input.operations[0]) : null;
      // A deleted property goes to the database's Deleted properties.
      if (target?.kind === databaseMutationTargetKinds.property) {
        return request<GraphMutationReceipt>(
          databaseDataSourceOperations.trashProperty.path
            .replace("{pageId}", encodeURIComponent(pageId))
            .replace("{propertyId}", encodeURIComponent(target.id)),
          {
            method: databaseDataSourceOperations.trashProperty.method,
            // A two-way relation's other side goes along or keeps its values, as chosen.
            body: JSON.stringify({ requestId: input.mutationId, ...relatedPropertyOf(input.operations[0]) }),
            signal,
          },
          session,
        );
      }
      if (target) {
        return drainDatabaseMutation(
          session, pageId, input.mutationId, target.kind, target.id, signal,
        );
      }
      return request<GraphMutationReceipt>(
        `/pages/${encodeURIComponent(pageId)}/content-graph/mutations`,
        { method: "POST", body: JSON.stringify(graphMutationOnTheWire(input)), signal },
        session,
      ).then(async (receipt) => {
        if (receipt.propertyConversions?.length) {
          await drainDatabasePropertyConversions(request, session, pageId, receipt.propertyConversions, signal);
        }
        return receipt;
      });
    },
  };
}
