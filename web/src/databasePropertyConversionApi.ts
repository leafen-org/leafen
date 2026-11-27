import type { Session } from "./types";
import {
  databasePropertyConversionOperations,
  type DatabasePropertyConversionResponse,
} from "./databasePropertyConversionWire.generated";

// A property type change converts existing cells on the server after the
// write returns. The write waits for those conversions so the editor rereads
// converted values instead of showing the old ones.

type JsonRequest = <T>(
  path: string,
  init?: RequestInit,
  session?: Session | null,
) => Promise<T>;

const conversionWaitLimitMs = 60_000;

function conversionPath(path: string, pageId: string, propertyId: string) {
  return path
    .replace("{pageId}", encodeURIComponent(pageId))
    .replace("{propertyId}", encodeURIComponent(propertyId));
}

function abortError() {
  return new DOMException("Database property conversion wait was aborted.", "AbortError");
}

function waitForNextConversionTurn(delayMs: number, signal?: AbortSignal) {
  if (signal?.aborted) return Promise.reject(abortError());
  return new Promise<void>((resolve, reject) => {
    const abort = () => {
      window.clearTimeout(timer);
      reject(abortError());
    };
    const timer = window.setTimeout(() => {
      signal?.removeEventListener("abort", abort);
      resolve();
    }, delayMs);
    signal?.addEventListener("abort", abort, { once: true });
  });
}

export function readDatabasePropertyConversion(
  request: JsonRequest, session: Session, pageId: string, propertyId: string, signal?: AbortSignal,
) {
  return request<DatabasePropertyConversionResponse>(
    conversionPath(databasePropertyConversionOperations.read.path, pageId, propertyId),
    { method: databasePropertyConversionOperations.read.method, signal },
    session,
  );
}

export function restoreDatabasePropertyConversion(
  request: JsonRequest, session: Session, pageId: string, propertyId: string, conversionId: string,
  signal?: AbortSignal,
) {
  return request<DatabasePropertyConversionResponse>(
    conversionPath(databasePropertyConversionOperations.restore.path, pageId, propertyId),
    {
      method: databasePropertyConversionOperations.restore.method,
      body: JSON.stringify({ conversionId }),
      signal,
    },
    session,
  );
}

// drainDatabasePropertyConversions waits until each listed property has no
// running conversion, giving up quietly after the wait limit.
export async function drainDatabasePropertyConversions(
  request: JsonRequest, session: Session, pageId: string, propertyIds: readonly string[], signal?: AbortSignal,
) {
  const deadline = Date.now() + conversionWaitLimitMs;
  let delayMs = 25;
  for (const propertyId of propertyIds) {
    for (;;) {
      const { conversion } = await readDatabasePropertyConversion(request, session, pageId, propertyId, signal);
      if (conversion?.status !== "pending" || Date.now() > deadline) break;
      await waitForNextConversionTurn(delayMs, signal);
      delayMs = Math.min(delayMs * 2, 500);
    }
  }
}

export function createDatabasePropertyConversionApi(request: JsonRequest) {
  return {
    readDatabasePropertyConversion: (session: Session, pageId: string, propertyId: string, signal?: AbortSignal) => (
      readDatabasePropertyConversion(request, session, pageId, propertyId, signal)
    ),
    restoreDatabasePropertyConversion: (
      session: Session, pageId: string, propertyId: string, conversionId: string, signal?: AbortSignal,
    ) => restoreDatabasePropertyConversion(request, session, pageId, propertyId, conversionId, signal),
    drainDatabasePropertyConversions: (
      session: Session, pageId: string, propertyIds: readonly string[], signal?: AbortSignal,
    ) => drainDatabasePropertyConversions(request, session, pageId, propertyIds, signal),
  };
}
