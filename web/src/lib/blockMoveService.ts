import { api } from "@/api";
import {
  blockMoveBounds,
  blockMoveOperationValues,
  blockMoveStatuses,
  type BlockMoveInput,
  type BlockMoveOperation,
  type BlockMoveResponse,
} from "@/blockMoveWire.generated";
import { createDurableJobMemory } from "@/lib/durableJobMemory";
import { waitForDurableJob } from "@/lib/durableJobPolling";
import { newId } from "@/lib/ids";
import { pageTextStored } from "@/lib/pageTextStored";
import type { Session } from "@/types";

type RememberedBlockMoveIntent = {
  destinationPageId: string;
  operation: BlockMoveOperation;
  requestId: string;
  withoutContent?: boolean;
};

type BlockMoveIntentMemory = {
  clear: (identity: readonly string[], requestId: string) => boolean;
  read: (identity: readonly string[]) => RememberedBlockMoveIntent | null;
  remember: (identity: readonly string[], intent: RememberedBlockMoveIntent) => boolean;
};

type BlockMoveTransport = {
  start: (
    session: Session,
    sourcePageId: string,
    blockId: string,
    input: BlockMoveInput,
    signal: AbortSignal,
  ) => Promise<BlockMoveResponse>;
  read: (session: Session, jobId: string, signal: AbortSignal) => Promise<BlockMoveResponse>;
};

type BlockMoveWait = (options: {
  initial: BlockMoveResponse;
  isSameJob: (next: BlockMoveResponse) => boolean;
  isTerminal: (next: BlockMoveResponse) => boolean;
  onProgress: (next: BlockMoveResponse) => void;
  read: () => Promise<BlockMoveResponse>;
  signal: AbortSignal;
}) => Promise<BlockMoveResponse>;

type BlockMoveServiceDependencies = {
  createRequestId: () => string;
  memory: BlockMoveIntentMemory;
  transport: BlockMoveTransport;
  waitForJob: BlockMoveWait;
};

export type BlockMoveRequest = {
  blockId: string;
  destinationPageId: string;
  // A copied database brings its properties, views and settings but no rows.
  withoutContent?: boolean;
  // A copy goes right after this block, under its parent, while it stands there; at the page's end otherwise.
  placeAfter?: string;
  placeParent?: string;
  onProgress?: (progress: BlockMoveResponse) => void;
  signal?: AbortSignal;
  sourcePageId: string;
};

// A move of a few blocks is done on the server a moment after it starts;
// these looks come before the job's regular ones, which start 250 ms in
// (2026-11-11: a database turned into a page waited for that first look).
const EARLY_LOOKS_MS = [20, 40, 80];

/** Waits for the move's job, looking early first, then as every durable job is waited for. */
const waitForBlockMove: BlockMoveWait = async (options) => {
  let current = options.initial;
  for (const delay of EARLY_LOOKS_MS) {
    if (options.isTerminal(current)) return current;
    await new Promise((resolve) => { globalThis.setTimeout(resolve, delay); });
    if (options.signal.aborted) throw new DOMException("Aborted", "AbortError");
    const next = await options.read().catch(() => null);
    if (!next) break;
    if (!options.isSameJob(next)) break;
    current = next;
    options.onProgress(current);
  }
  return waitForDurableJob({ ...options, initial: current });
};

const volatileIntents = new Map<string, RememberedBlockMoveIntent>();
const VOLATILE_INTENT_LIMIT = 32;
const durableIntents = createDurableJobMemory<RememberedBlockMoveIntent>({
  bounds: {
    maxIdentityCharacters: 1_024,
    maxSerializedCharacters: 1_024,
    maxStorageKeyCharacters: 2_400,
  },
  decode(value) {
    const parsed: unknown = JSON.parse(value);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return null;
    const intent = parsed as Record<string, unknown>;
    return typeof intent.destinationPageId === "string"
      && intent.destinationPageId.length > 0
      && intent.destinationPageId.length <= 512
      && typeof intent.requestId === "string"
      && intent.requestId.length > 0
      && Array.from(intent.requestId).length <= blockMoveBounds.requestIdRunes
      ? {
          destinationPageId: intent.destinationPageId,
          operation: intent.operation === blockMoveOperationValues.copy
            ? blockMoveOperationValues.copy
            : blockMoveOperationValues.move,
          requestId: intent.requestId,
          ...(intent.withoutContent === true ? { withoutContent: true } : {}),
        }
      : null;
  },
  encode(value) {
    return value.destinationPageId.length > 0
      && value.destinationPageId.length <= 512
      && Object.values(blockMoveOperationValues).includes(value.operation)
      && value.requestId.length > 0
      && Array.from(value.requestId).length <= blockMoveBounds.requestIdRunes
      ? JSON.stringify(value)
      : null;
  },
  identityPartCount: 4,
  storagePrefix: "leafen:block-move-start",
});

function intentKey(identity: readonly string[]) {
  return durableIntents.storageKey(identity);
}

const blockMoveIntentMemory: BlockMoveIntentMemory = {
  clear(identity, requestId) {
    const key = intentKey(identity);
    const current = this.read(identity);
    if (!key || current?.requestId !== requestId) return false;
    volatileIntents.delete(key);
    durableIntents.clear(identity);
    return true;
  },
  read(identity) {
    const key = intentKey(identity);
    if (!key) return null;
    const durable = durableIntents.read(identity);
    if (durable) {
      volatileIntents.delete(key);
      return durable;
    }
    return volatileIntents.get(key) ?? null;
  },
  remember(identity, intent) {
    const key = intentKey(identity);
    if (!key) return false;
    if (durableIntents.remember(identity, intent)) {
      volatileIntents.delete(key);
      return true;
    }
    volatileIntents.delete(key);
    volatileIntents.set(key, { ...intent });
    while (volatileIntents.size > VOLATILE_INTENT_LIMIT) {
      const oldest = volatileIntents.keys().next().value as string | undefined;
      if (!oldest) break;
      volatileIntents.delete(oldest);
    }
    return true;
  },
};

const defaultTransport: BlockMoveTransport = {
  start: (session, sourcePageId, blockId, input, signal) => (
    api.startBlockMove(session, sourcePageId, blockId, input, signal)
  ),
  read: (session, jobId, signal) => api.readBlockMove(session, jobId, signal),
};

// The message is diagnostic only: every caller shows its own translated
// notice for a failed move or copy.
function blockMoveError(code: string, message: string) {
  return Object.assign(new Error(message), { code });
}

function aborted(signal: AbortSignal) {
  if (signal.aborted) throw new DOMException("Aborted", "AbortError");
}

function exactResponse(
  response: BlockMoveResponse,
  request: BlockMoveRequest,
  operation: BlockMoveOperation,
  jobId?: string,
) {
  return response.sourcePageId === request.sourcePageId
    && response.destinationPageId === request.destinationPageId
    && response.blockId === request.blockId
    && response.operation === operation
    && response.targetBlockId.length > 0
    && (operation === blockMoveOperationValues.copy || response.targetBlockId === request.blockId)
    && (!jobId || response.jobId === jobId);
}

export function createBlockMoveService(
  session: Session,
  overrides: Partial<BlockMoveServiceDependencies> = {},
) {
  const dependencies: BlockMoveServiceDependencies = {
    createRequestId: newId,
    memory: blockMoveIntentMemory,
    transport: defaultTransport,
    waitForJob: waitForBlockMove,
    ...overrides,
  };
  const active = new Map<string, {
    controller: AbortController;
    promise: Promise<BlockMoveResponse>;
  }>();

  function transfer(
    operation: BlockMoveOperation,
    request: BlockMoveRequest,
  ): Promise<BlockMoveResponse> {
    const identity = [
      session.user.id,
      session.sessionId ?? "sessionless",
      request.sourcePageId,
      request.blockId,
    ];
    const key = JSON.stringify([...identity, request.destinationPageId, operation]);
    const current = active.get(key);
    if (current) return current.promise;
    const controller = new AbortController();
    const abort = () => controller.abort();
    request.signal?.addEventListener("abort", abort, { once: true });

    const run = async () => {
      const retryIntent = dependencies.memory.read(identity);
      const withoutContent = request.withoutContent === true;
      if (retryIntent && (
        retryIntent.destinationPageId !== request.destinationPageId
        || retryIntent.operation !== operation
        || (retryIntent.withoutContent === true) !== withoutContent
      )) {
        throw blockMoveError(
          "LEAFEN_BLOCK_MOVE_INTENT_PENDING",
          "The previous move of this block is still unresolved.",
        );
      }
      const requestId = retryIntent?.requestId ?? dependencies.createRequestId();
      // The intent is the fence: noted before waiting for the page's text, so
      // another destination for this block is refused all the while; a wait
      // that ends without the text sent nothing, and lets the intent go.
      dependencies.memory.remember(identity, {
        destinationPageId: request.destinationPageId,
        operation,
        requestId,
        ...(withoutContent ? { withoutContent } : {}),
      });
      if (!retryIntent) {
        let stored = false;
        try {
          stored = await pageTextStored(session, request.sourcePageId, controller.signal);
          aborted(controller.signal);
        } catch (error) {
          dependencies.memory.clear(identity, requestId);
          throw error;
        }
        if (!stored) {
          dependencies.memory.clear(identity, requestId);
          throw blockMoveError("LEAFEN_BLOCK_MOVE_TEXT_UNSAVED", "The page's text has not reached the server yet.");
        }
      }
      aborted(controller.signal);
      const initial = await dependencies.transport.start(
        session,
        request.sourcePageId,
        request.blockId,
        { destinationPageId: request.destinationPageId, operation, requestId, withoutContent,
          placeAfter: request.placeAfter ?? "", placeParent: request.placeParent ?? "" },
        controller.signal,
      );
      aborted(controller.signal);
      if (!exactResponse(initial, request, operation)) {
        throw blockMoveError("LEAFEN_BLOCK_MOVE_RESPONSE_INVALID", "The block move response does not match the request.");
      }
      const terminal = await dependencies.waitForJob({
        initial,
        isSameJob: (next) => exactResponse(next, request, operation, initial.jobId),
        isTerminal: (next) => next.status !== blockMoveStatuses.pending,
        onProgress: (next) => {
          if (!controller.signal.aborted) request.onProgress?.(next);
        },
        read: async () => {
          const next = await dependencies.transport.read(session, initial.jobId, controller.signal);
          aborted(controller.signal);
          return next;
        },
        signal: controller.signal,
      });
      aborted(controller.signal);
      dependencies.memory.clear(identity, requestId);
      if (terminal.status === blockMoveStatuses.completed) return terminal;
      throw blockMoveError(
        terminal.errorCode || "LEAFEN_BLOCK_MOVE_UNAVAILABLE",
        "The block could not be moved or copied.",
      );
    };

    let promise: Promise<BlockMoveResponse>;
    promise = run().finally(() => {
      request.signal?.removeEventListener("abort", abort);
      if (active.get(key)?.promise === promise) active.delete(key);
    });
    active.set(key, { controller, promise });
    return promise;
  }

  return {
    cancel() {
      const current = [...active.values()];
      active.clear();
      for (const operation of current) operation.controller.abort();
    },
    copy(request: BlockMoveRequest) {
      return transfer(blockMoveOperationValues.copy, request);
    },
    move(request: BlockMoveRequest) {
      return transfer(blockMoveOperationValues.move, request);
    },
  };
}

export type BlockMoveService = ReturnType<typeof createBlockMoveService>;

/** Moves blocks to another page one after another, so they keep their order there. */
export async function moveBlocksInOrder(service: BlockMoveService, request: {
  blockIds: readonly string[];
  destinationPageId: string;
  sourcePageId: string;
  signal?: AbortSignal;
}) {
  for (const blockId of request.blockIds) {
    await service.move({
      blockId, destinationPageId: request.destinationPageId, sourcePageId: request.sourcePageId, signal: request.signal,
    });
  }
}
