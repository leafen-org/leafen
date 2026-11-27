"use client";

import { LeafenTransportError } from "@/apiTransport";
import {
  fileTransferBounds,
  fileUploadPurposes,
  type FileDownloadCapability,
  type FileTransferFile,
  type FileUploadStateResponse,
} from "@/fileTransferWire.generated";
import { organizationErrorCodes } from "@/organizationWire.generated";
import type { Session } from "@/types";
import { loadSession } from "./authSessionStore";
import { assertSafeWorkspaceUpload } from "./fileSecurity";
import { newId } from "./ids";
import { activePersistentGeneratedLabels } from "./persistentGeneratedLabels";
import {
  nativeWorkspaceFileLocator,
  nativeWorkspaceFileTransport,
  type WorkspaceFileTransport,
} from "./workspaceFileTransport";
import { sha256Hex } from "./browserCrypto";
import { WAITING_NOTE_MS } from "./pendingChoice";
import { onUncertainWritesSettled } from "./uncertainWrites";

const NATIVE_FILE_UPLOAD_RETRY_DELAYS_MS = [100, 400] as const;
// An upload the server doesn't answer waits for it, as other writes do
// (2026-11-07: on a network that stopped, a custom emoji's save showed only
// a dimmed button for almost 100 seconds, then failed). Its request says it
// is waiting once it goes ten seconds unanswered, longer for what its bytes
// take on a slow link, and it goes again — the same request, which the
// server answers once — when the server answers again, or every while.
const UPLOAD_WAITING_BYTES_PER_SECOND = 1024 * 1024;
const UPLOAD_SEND_AGAIN_EVERY_MS = 15_000;
const STORAGE_RECONCILIATION_RETRY_BASE_DELAY_MS = 100;
const STORAGE_RECONCILIATION_MAX_ATTEMPTS = 30;
const STORAGE_RECONCILIATION_MAX_ELAPSED_MS = 25_000;
const MAX_WORKSPACE_FILE_SCOPE_CHARACTERS = 255;
const MAX_WORKSPACE_FILE_TARGET_CHARACTERS = 255;

export interface UploadedWorkspaceFile {
  fileId: string;
  key: string;
  url: string;
  name: string;
  type?: string;
  size?: number;
  sha256?: string;
}

export interface WorkspaceFileTarget {
  workspaceId?: string;
  pageId?: string;
  /** The room the file goes up for, which lets a guest in it send it. */
  roomId?: string;
  blockId?: string;
  databaseId?: string;
  propertyId?: string;
  /** What keeps the file as its icon until it changes (KeptIconGlyph), sent with the workspace alone. */
  iconHolder?: "workspace" | "teamspace" | "group";
}

export type UploadProgressPhase = "preparing" | "uploading" | "finalizing" | "complete";

export interface UploadProgress {
  phase: UploadProgressPhase;
  percent: number;
  loaded?: number;
  total?: number;
  /** The server hasn't answered for a while: the upload waits for it and goes on once it answers. */
  waiting?: boolean;
}

export interface UploadWorkspaceFileOptions {
  onProgress?: (progress: UploadProgress) => void;
  signal?: AbortSignal;
}

export type DeleteWorkspaceFileInput = {
  fileId?: string;
  key?: string;
  uploadId?: string;
  requestId?: string;
  signal?: AbortSignal;
};

export type WorkspaceFileDownloadInput = {
  fileId?: string;
  key?: string;
  uploadId?: string;
  expiresIn?: string;
  signal?: AbortSignal;
};

type WorkspaceFileStorageOptions = {
  transport?: WorkspaceFileTransport;
  currentSession?: () => Session | null;
  createRequestId?: () => string;
  digestPart?: (part: Blob) => Promise<string>;
  fileLocator?: (fileId: string) => string;
  now?: () => number;
  wait?: (delayMs: number, signal?: AbortSignal) => Promise<void>;
};

type RetryContext = {
  assertCurrentSession(): void;
  signal?: AbortSignal;
  wait: (delayMs: number, signal?: AbortSignal) => Promise<void>;
  /** An upload's: it waits for the server rather than failing, and says when it is waiting. */
  untilAnswered?: { bytes: number; onWaiting(waiting: boolean): void };
};

function defaultWait(delayMs: number, signal?: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    if (signal?.aborted) {
      reject(signal.reason);
      return;
    }
    const complete = () => {
      signal?.removeEventListener("abort", abort);
      resolve();
    };
    const timer = setTimeout(complete, delayMs);
    const abort = () => {
      clearTimeout(timer);
      reject(signal?.reason ?? new Error("Workspace file operation was aborted."));
    };
    signal?.addEventListener("abort", abort, { once: true });
  });
}

async function partSha256Hex(part: Blob) {
  return sha256Hex(await part.arrayBuffer());
}

function storageReconciliationPending(error: unknown) {
  return !!error && typeof error === "object" && "code" in error
    && error.code === organizationErrorCodes.storageReconciliationPending;
}

function storageReconciliationExhaustedError(
  cause: unknown,
  attempts: number,
  elapsedMs: number,
) {
  return Object.assign(
    new Error(`Storage reconciliation remained pending after ${attempts} attempt(s) and ${elapsedMs} ms.`),
    {
      code: "LEAFEN_STORAGE_RECONCILIATION_EXHAUSTED" as const,
      cause,
      attempts,
      elapsedMs,
      maxAttempts: STORAGE_RECONCILIATION_MAX_ATTEMPTS,
      maxElapsedMs: STORAGE_RECONCILIATION_MAX_ELAPSED_MS,
    },
  );
}

async function runStorageReconciliation<T>(
  request: () => Promise<T>,
  options: {
    now: () => number;
    signal?: AbortSignal;
    wait: (delayMs: number, signal?: AbortSignal) => Promise<void>;
  },
) {
  const startedAt = options.now();
  for (let attempt = 1; ; attempt += 1) {
    try {
      return await request();
    } catch (error) {
      if (!storageReconciliationPending(error)) throw error;
      const elapsedMs = Math.max(0, options.now() - startedAt);
      if (
        attempt >= STORAGE_RECONCILIATION_MAX_ATTEMPTS
        || elapsedMs >= STORAGE_RECONCILIATION_MAX_ELAPSED_MS
      ) throw storageReconciliationExhaustedError(error, attempt, elapsedMs);
      const delayMs = Math.min(
        1_000,
        STORAGE_RECONCILIATION_RETRY_BASE_DELAY_MS * (2 ** Math.min(attempt - 1, 4)),
        STORAGE_RECONCILIATION_MAX_ELAPSED_MS - elapsedMs,
      );
      await options.wait(delayMs, options.signal);
    }
  }
}

async function runResponseLossRetry<T>(request: () => Promise<T>, context: RetryContext) {
  const until = context.untilAnswered;
  let waiting = false;
  const sayWaiting = () => {
    if (waiting) return;
    waiting = true;
    until?.onWaiting(true);
  };
  const timer = until ? setTimeout(sayWaiting, WAITING_NOTE_MS + (until.bytes / UPLOAD_WAITING_BYTES_PER_SECOND) * 1000) : undefined;
  try {
    for (let attempt = 0; ; attempt += 1) {
      try {
        const value = await request();
        context.assertCurrentSession();
        if (waiting) until?.onWaiting(false);
        return value;
      } catch (error) {
        context.assertCurrentSession();
        if (!(error instanceof LeafenTransportError)) throw error;
        const delayMs = NATIVE_FILE_UPLOAD_RETRY_DELAYS_MS[attempt];
        if (delayMs !== undefined) {
          await context.wait(delayMs, context.signal);
        } else if (until) {
          sayWaiting();
          await serverAnswersOrWhile(context);
        } else {
          throw error;
        }
        context.assertCurrentSession();
      }
    }
  } finally {
    clearTimeout(timer);
  }
}

/** Waits until the server answers again after a write it didn't answer, or a while. */
async function serverAnswersOrWhile(context: RetryContext) {
  let stop = () => {};
  const answered = new Promise<void>((resolve) => { stop = onUncertainWritesSettled(resolve); });
  const ended = new AbortController();
  const forward = () => ended.abort(context.signal?.reason);
  if (context.signal?.aborted) forward();
  else context.signal?.addEventListener("abort", forward, { once: true });
  const awhile = context.wait(UPLOAD_SEND_AGAIN_EVERY_MS, ended.signal);
  awhile.catch(() => undefined);
  try {
    await Promise.race([answered, awhile]);
  } finally {
    stop();
    context.signal?.removeEventListener("abort", forward);
    ended.abort();
  }
}

function boundedIdentity(value: string | undefined, label: string) {
  const normalized = value?.trim() ?? "";
  if (!normalized || normalized.length > MAX_WORKSPACE_FILE_TARGET_CHARACTERS) {
    throw new RangeError(`${label} is required and must stay within its bound.`);
  }
  return normalized;
}

function validateUploadInput(file: File, scope: string, target: WorkspaceFileTarget) {
  assertSafeWorkspaceUpload(file);
  const workspaceId = target.workspaceId?.trim() ?? "";
  if (!workspaceId) throw new RangeError("Workspace id is required for a file upload.");
  if (workspaceId.length > MAX_WORKSPACE_FILE_TARGET_CHARACTERS) {
    throw new RangeError("Workspace id exceeds its configured bound.");
  }
  const normalizedScope = scope.trim();
  if (!normalizedScope || normalizedScope.length > MAX_WORKSPACE_FILE_SCOPE_CHARACTERS) {
    throw new RangeError("Workspace file scope is invalid.");
  }
  for (const [label, value] of Object.entries(target)) {
    if (value !== undefined && (!value.trim() || value.length > MAX_WORKSPACE_FILE_TARGET_CHARACTERS)) {
      throw new RangeError(`Workspace file ${label} is invalid.`);
    }
  }
  if (!Number.isSafeInteger(file.size) || file.size <= 0 || file.size > fileTransferBounds.maxFileBytes) {
    throw new RangeError("Workspace file size is invalid.");
  }
  if (Math.ceil(file.size / fileTransferBounds.partBytes) > fileTransferBounds.maxParts) {
    throw new RangeError("Workspace file requires too many parts.");
  }
  return workspaceId;
}

function validateRequestId(requestId: string) {
  const normalized = requestId.trim();
  if (!normalized || Array.from(normalized).length > fileTransferBounds.requestIdChars) {
    throw new RangeError("Workspace file request id is invalid.");
  }
  return normalized;
}

function validatePreparedUpload(
  value: FileUploadStateResponse,
  file: File,
) {
  const expectedOffset = value.nextPart * value.partBytes;
  if (
    !value.uploadId
    || value.status !== "pending"
    || value.partBytes !== fileTransferBounds.partBytes
    || !Number.isSafeInteger(value.nextPart)
    || value.nextPart < 0
    || value.nextPart > fileTransferBounds.maxParts
    || value.receivedBytes !== Math.min(file.size, expectedOffset)
  ) throw new Error("Native workspace file upload state is invalid.");
  return value;
}

function validateFileResult(value: FileTransferFile, workspaceId: string, file: File) {
  if (
    !value.id
    || value.workspaceId !== workspaceId
    || value.size !== file.size
    || !value.name
    || !/^[a-f0-9]{64}$/u.test(value.sha256)
  ) throw new Error("Native workspace file result is invalid.");
  return value;
}

function reportProgress(
  options: UploadWorkspaceFileOptions,
  assertCurrentSession: () => void,
  progress: UploadProgress,
) {
  assertCurrentSession();
  options.onProgress?.({
    ...progress,
    percent: Math.max(0, Math.min(100, Math.round(progress.percent))),
  });
}

function uploadedWorkspaceFile(
  value: FileTransferFile,
  fileLocator: (fileId: string) => string,
): UploadedWorkspaceFile {
  return {
    fileId: value.id,
    key: value.id,
    url: fileLocator(value.id),
    name: value.name,
    type: value.contentType || undefined,
    size: value.size,
    sha256: value.sha256,
  };
}

export function createWorkspaceFileStorage(options: WorkspaceFileStorageOptions = {}) {
  const transport = options.transport ?? nativeWorkspaceFileTransport;
  const currentSession = options.currentSession ?? loadSession;
  const createRequestId = options.createRequestId ?? newId;
  const digestPart = options.digestPart ?? partSha256Hex;
  const fileLocator = options.fileLocator ?? nativeWorkspaceFileLocator;
  const now = options.now ?? Date.now;
  const wait = options.wait ?? defaultWait;

  function captureSession() {
    const session = currentSession();
    if (!session?.token || !session.user?.id) throw new Error("A logged-in session is required.");
    const principalId = session.user.id;
    const token = session.token;
    return {
      assertCurrentSession() {
        const current = currentSession();
        if (current?.user.id !== principalId || current.token !== token) {
          throw new Error("The session changed during workspace file operation.");
        }
      },
      session,
    };
  }

  async function upload(
    file: File,
    scope = "uploads",
    target: WorkspaceFileTarget = {},
    uploadOptions: UploadWorkspaceFileOptions = {},
  ): Promise<UploadedWorkspaceFile> {
    const workspaceId = validateUploadInput(file, scope, target);
    const requestId = validateRequestId(createRequestId());
    const captured = captureSession();
    const retry = {
      assertCurrentSession: captured.assertCurrentSession,
      signal: uploadOptions.signal,
      wait,
    };
    let last: UploadProgress = { phase: "preparing", percent: 0 };
    const report = (progress: UploadProgress) => {
      last = progress;
      reportProgress(uploadOptions, captured.assertCurrentSession, progress);
    };
    // Each request of the upload waits for the server, saying so (runResponseLossRetry).
    const answered = (bytes: number): RetryContext => ({
      ...retry,
      untilAnswered: {
        bytes,
        onWaiting: (waiting) => {
          try {
            report({ ...last, waiting });
          } catch {
            // The session changed: the upload ends at its next step.
          }
        },
      },
    });
    const labels = activePersistentGeneratedLabels();
    const name = file.name.trim() || labels.untitled;
    const contentType = file.type.trim() || "application/octet-stream";
    // The page lets someone it is shared with, not a workspace member, send
    // the file: one who may edit it for its content, one who may comment for
    // a comment's file; a room lets a guest in it.
    const pageScope = target.roomId?.trim()
      ? { roomId: target.roomId.trim() }
      : target.pageId?.trim()
        ? { pageId: target.pageId.trim(), purpose: scope === "comments" ? fileUploadPurposes.comment : fileUploadPurposes.page }
        : {};
    report({ phase: "preparing", percent: 0 });
    report({
      phase: "uploading", percent: 5, loaded: 0, total: file.size,
    });

    let completedFile: FileTransferFile;
    if (file.size <= fileTransferBounds.directBytes) {
      completedFile = await runResponseLossRetry(
        () => transport.direct(captured.session, {
          contentType, file, name, requestId, workspaceId, ...pageScope, signal: uploadOptions.signal,
        }),
        answered(file.size),
      );
    } else {
      const prepared = validatePreparedUpload(await runStorageReconciliation(
        () => runResponseLossRetry(
          () => transport.prepare(captured.session, {
            requestId, workspaceId, name, size: file.size, contentType, ...pageScope, signal: uploadOptions.signal,
          }),
          answered(0),
        ),
        { now, signal: uploadOptions.signal, wait },
      ), file);
      report({
        phase: "uploading",
        percent: 5 + (prepared.receivedBytes / file.size) * 90,
        loaded: prepared.receivedBytes,
        total: file.size,
      });
      for (let partNumber = prepared.nextPart; partNumber < Math.ceil(file.size / prepared.partBytes); partNumber += 1) {
        const start = partNumber * prepared.partBytes;
        const end = Math.min(file.size, start + prepared.partBytes);
        const part = file.slice(start, end);
        const sha256 = await digestPart(part);
        captured.assertCurrentSession();
        const state = await runResponseLossRetry(
          () => transport.uploadPart(captured.session, {
            part, partNumber, sha256, uploadId: prepared.uploadId, signal: uploadOptions.signal,
          }),
          answered(part.size),
        );
        if (
          state.uploadId !== prepared.uploadId
          || state.status !== "pending"
          || state.receivedBytes !== end
          || state.nextPart !== partNumber + 1
        ) throw new Error("Native workspace file part state is invalid.");
        report({
          phase: "uploading",
          percent: 5 + (end / file.size) * 90,
          loaded: end,
          total: file.size,
        });
      }
      report({
        phase: "finalizing", percent: 96, loaded: file.size, total: file.size,
      });
      const completed = await runResponseLossRetry(
        () => transport.complete(captured.session, {
          requestId, uploadId: prepared.uploadId, signal: uploadOptions.signal,
        }),
        answered(0),
      );
      if (completed.uploadId !== prepared.uploadId || completed.status !== "completed") {
        throw new Error("Native workspace file completion state is invalid.");
      }
      completedFile = completed.file;
    }

    const validated = validateFileResult(completedFile, workspaceId, file);
    if (file.size <= fileTransferBounds.directBytes) {
      report({
        phase: "finalizing", percent: 96, loaded: file.size, total: file.size,
      });
    }
    report({
      phase: "complete", percent: 100, loaded: file.size, total: file.size,
    });
    return uploadedWorkspaceFile(validated, fileLocator);
  }

  async function deleteFile(input: DeleteWorkspaceFileInput) {
    const fileId = boundedIdentity(input.fileId ?? input.uploadId ?? input.key, "Workspace file id");
    const requestId = validateRequestId(input.requestId ?? createRequestId());
    const captured = captureSession();
    const result = await runResponseLossRetry(
      () => transport.deleteFile(captured.session, { fileId, requestId, signal: input.signal }),
      { assertCurrentSession: captured.assertCurrentSession, signal: input.signal, wait },
    );
    if (result.fileId !== fileId || result.status !== "completed") {
      throw new Error("Native workspace file deletion state is invalid.");
    }
    return result;
  }

  async function download(input: WorkspaceFileDownloadInput): Promise<FileDownloadCapability> {
    const fileId = boundedIdentity(input.fileId ?? input.uploadId ?? input.key, "Workspace file id");
    const captured = captureSession();
    const result = await transport.issueDownloadCapability(captured.session, {
      fileId,
      signal: input.signal,
    });
    captured.assertCurrentSession();
    return result;
  }

  return { delete: deleteFile, download, upload };
}

const workspaceFileStorage = createWorkspaceFileStorage();

export function uploadWorkspaceFile(
  file: File,
  scope = "uploads",
  target: WorkspaceFileTarget = {},
  options: UploadWorkspaceFileOptions = {},
) {
  return workspaceFileStorage.upload(file, scope, target, options);
}

export function createWorkspaceFileDownloadUrl(input: WorkspaceFileDownloadInput) {
  return workspaceFileStorage.download(input);
}
