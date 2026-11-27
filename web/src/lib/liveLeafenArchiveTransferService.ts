import { api } from "@/api";
import {
  API_ROOT,
  createJsonApiTransport,
} from "@/apiTransport";
import { createAuthenticatedBinaryResponseRequest } from "@/authenticatedBinaryResponse";
import type { Session } from "@/types";
import { createWorkspaceExportApi } from "@/workspaceExportApi";
import {
  workspaceExportFormats,
  type WorkspaceExportJob,
} from "@/workspaceExportWire.generated";
import type {
  WorkspaceArchiveImportUpload,
  WorkspaceImportJob,
} from "@/workspaceImportWire.generated";
import { newId } from "./ids";
import {
  resolveLiveLeafenSourceAuthority,
  streamLiveLeafenArchiveParts,
  type LiveLeafenSourceAuthority,
} from "./liveLeafenArchiveTransferCore";
import {
  clearRememberedLiveLeafenArchiveTransfer,
  readRememberedLiveLeafenArchiveTransfer,
  rememberLiveLeafenArchiveTransfer,
  type RememberedLiveLeafenArchiveTransfer,
} from "./liveLeafenArchiveTransferMemory";
import {
  createWorkspaceExportService,
  workspaceExportService,
} from "./workspaceExportService";
import { waitForWorkspaceImport } from "./waitForWorkspaceImport";
import { fetchAskingAgain } from "./serverBusy";
import { serverOrigin } from "@/lib/serverOrigin";
import { sha256Hex } from "./browserCrypto";

const SHA256 = /^[a-f0-9]{64}$/u;

export type LiveLeafenArchiveTransferProgress = {
  stage: "source-export" | "source-download" | "target-upload" | "target-import";
  sourceJobId?: string;
  targetUploadId?: string;
  targetJob?: WorkspaceImportJob;
  receivedBytes?: number;
  archiveBytes?: number;
};

type LiveLeafenTransferInput = {
  session: Session;
  targetWorkspaceId: string;
  sourceUrl: string;
  sourceWorkspaceId: string;
  sourceToken: string;
  onProgress?: (progress: LiveLeafenArchiveTransferProgress) => void;
  signal?: AbortSignal;
};

type SourceRuntime = {
  authority: LiveLeafenSourceAuthority;
  service: ReturnType<typeof createWorkspaceExportService>;
  session: Session;
};

type LiveTransferError = Error & {
  code: "LEAFEN_LIVE_TRANSFER_RESPONSE_INVALID";
  stage: string;
  status: 502;
};

function responseError(stage: string, message: string): LiveTransferError {
  return Object.assign(new Error(message), {
    code: "LEAFEN_LIVE_TRANSFER_RESPONSE_INVALID" as const,
    stage,
    status: 502 as const,
  });
}

function rawAuthorityToken(authority: LiveLeafenSourceAuthority) {
  return authority.authorization.slice("Bearer ".length);
}

function sourceRuntime(input: LiveLeafenTransferInput): SourceRuntime {
  const appOrigin = serverOrigin();
  const authority = resolveLiveLeafenSourceAuthority({
    appOrigin,
    sourceUrl: input.sourceUrl,
    currentToken: input.session.token,
    sourceToken: input.sourceToken,
  });
  if (authority.baseUrl === appOrigin) {
    return { authority, service: workspaceExportService, session: input.session };
  }
  const json = createJsonApiTransport({ apiRoot: `${authority.baseUrl}${API_ROOT}` });
  const binary = createAuthenticatedBinaryResponseRequest({
    apiRoot: `${authority.baseUrl}${API_ROOT}`,
    // Asked again while that server refuses it for now, as its JSON requests are (lib/serverBusy.ts).
    fetch: (input, init) => fetchAskingAgain(() => globalThis.fetch(input, init), init?.signal),
    throwResponseError: json.throwResponseError,
  });
  const remoteRequest: typeof json.request = (path, init = {}, session) => json.request(
    path,
    { ...init, credentials: authority.credentials },
    session,
  );
  const remoteBinary: typeof binary = (path, session, init = {}) => binary(
    path,
    { ...session, token: rawAuthorityToken(authority) },
    { ...init, credentials: authority.credentials },
  );
  const transport = createWorkspaceExportApi(remoteRequest, remoteBinary);
  return {
    authority,
    service: createWorkspaceExportService(transport),
    session: {
      ...input.session,
      token: rawAuthorityToken(authority),
      user: {
        ...input.session.user,
        id: `${input.session.user.id}@${authority.baseUrl}`,
      },
    },
  };
}

function remember(
  input: LiveLeafenTransferInput,
  state: RememberedLiveLeafenArchiveTransfer,
) {
  rememberLiveLeafenArchiveTransfer(
    input.session.user.id,
    input.targetWorkspaceId,
    state,
  );
  return state;
}

function initialState(
  input: LiveLeafenTransferInput,
  authority: LiveLeafenSourceAuthority,
) {
  const remembered = readRememberedLiveLeafenArchiveTransfer(
    input.session.user.id,
    input.targetWorkspaceId,
  );
  if (
    remembered?.sourceUrl === authority.baseUrl
    && remembered.sourceWorkspaceId === input.sourceWorkspaceId
  ) return remembered;
  return remember(input, {
    sourceUrl: authority.baseUrl,
    sourceWorkspaceId: input.sourceWorkspaceId,
    targetRequestId: newId(),
  });
}

async function completedSourceExport(
  input: LiveLeafenTransferInput,
  source: SourceRuntime,
  state: RememberedLiveLeafenArchiveTransfer,
) {
  let job: WorkspaceExportJob;
  if (state.sourceJobId) {
    job = (await source.service.exact(
      source.session,
      state.sourceWorkspaceId,
      state.sourceJobId,
    )).job;
  } else {
    job = (await source.service.start(source.session, {
      workspaceId: state.sourceWorkspaceId,
      format: workspaceExportFormats.nativeArchive,
      scope: { kind: "workspace", targetId: "" },
      options: { includeFiles: true },
    })).job;
  }
  state = remember(input, {
    ...state,
    sourceJobId: job.id,
    sourceRequestId: job.requestId,
  });
  input.onProgress?.({ stage: "source-export", sourceJobId: job.id });
  if (job.status !== "completed") {
    job = await source.service.wait(source.session, state.sourceWorkspaceId, job, {
      signal: input.signal,
      onProgress: (next) => input.onProgress?.({
        stage: "source-export",
        sourceJobId: next.id,
      }),
    });
  }
  if (
    job.status !== "completed"
    || job.workspaceId !== state.sourceWorkspaceId
    || job.requestId !== state.sourceRequestId
    || job.format !== workspaceExportFormats.nativeArchive
    || job.options.includeFiles !== true
    || !Number.isSafeInteger(job.artifactBytes)
    || job.artifactBytes <= 0
    || !SHA256.test(job.artifactSha256)
  ) {
    throw responseError("source-export", "The source Leafen export does not match the requested archive.");
  }
  state = remember(input, {
    ...state,
    sourceJobId: job.id,
    artifactBytes: job.artifactBytes,
    artifactSha256: job.artifactSha256,
  });
  return { job, state };
}

function validateTargetUpload(
  upload: WorkspaceArchiveImportUpload,
  state: RememberedLiveLeafenArchiveTransfer,
  targetWorkspaceId: string,
) {
  if (
    upload.workspaceId !== targetWorkspaceId
    || upload.requestId !== state.targetRequestId
    || upload.archiveBytes !== state.artifactBytes
    || upload.archiveSha256 !== state.artifactSha256
    || !Number.isSafeInteger(upload.receivedBytes)
    || upload.receivedBytes < 0
    || upload.receivedBytes > upload.archiveBytes
    || !Number.isSafeInteger(upload.nextPart)
    || upload.nextPart < 0
    || !Number.isSafeInteger(upload.partBytes)
    || upload.partBytes <= 0
    || upload.receivedBytes !== Math.min(upload.nextPart * upload.partBytes, upload.archiveBytes)
  ) {
    throw responseError("target-prepare", "The target Leafen resume state does not match the source archive.");
  }
  return upload;
}

function validateDownload(
  response: Response,
  receivedBytes: number,
  archiveBytes: number,
) {
  if (receivedBytes === 0) {
    if (response.status !== 200) {
      throw responseError("source-download", "The source Leafen server did not return the whole archive.");
    }
  } else {
    const expectedRange = `bytes ${receivedBytes}-${archiveBytes - 1}/${archiveBytes}`;
    if (response.status !== 206 || response.headers.get("content-range") !== expectedRange) {
      throw responseError("source-download", "The source Leafen server did not return the exact resume range.");
    }
  }
  const expectedBytes = archiveBytes - receivedBytes;
  const contentLength = Number(response.headers.get("content-length"));
  if (!Number.isSafeInteger(contentLength) || contentLength !== expectedBytes) {
    throw responseError("source-download", "The source Leafen archive length does not match its export.");
  }
  return response;
}

async function waitForTargetImport(
  input: LiveLeafenTransferInput,
  initial: WorkspaceImportJob,
) {
  const signal = input.signal ?? new AbortController().signal;
  return waitForWorkspaceImport({
    initial,
    workspaceId: input.targetWorkspaceId,
    signal,
    read: () => api.workspaceImportJob(
      input.session,
      input.targetWorkspaceId,
      initial.id,
      { signal },
    ),
    onProgress: (targetJob) => input.onProgress?.({
      stage: "target-import",
      targetJob,
    }),
  });
}

async function run(input: LiveLeafenTransferInput) {
  const source = sourceRuntime(input);
  let state = initialState(input, source.authority);
  if (state.targetJobId) {
    const targetJob = await api.workspaceImportJob(
      input.session,
      input.targetWorkspaceId,
      state.targetJobId,
      { signal: input.signal },
    );
    return waitForTargetImport(input, targetJob);
  }

  const exported = await completedSourceExport(input, source, state);
  state = exported.state;
  const archiveBytes = state.artifactBytes as number;
  const archiveSha256 = state.artifactSha256 as string;
  let upload = validateTargetUpload(await api.archiveUploadPrepare(
    input.session,
    input.targetWorkspaceId,
    {
      requestId: state.targetRequestId,
      filename: `${exported.job.id}.leafen.zip`,
      bytes: archiveBytes,
      sha256: archiveSha256,
    },
  ), state, input.targetWorkspaceId);
  state = remember(input, { ...state, targetUploadId: upload.uploadId });
  if (upload.job) {
    state = remember(input, { ...state, targetJobId: upload.job.id });
    return waitForTargetImport(input, upload.job);
  }

  input.onProgress?.({
    stage: "source-download",
    sourceJobId: exported.job.id,
    targetUploadId: upload.uploadId,
    receivedBytes: upload.receivedBytes,
    archiveBytes,
  });
  if (upload.receivedBytes < archiveBytes) {
    const response = validateDownload(await source.service.download(
      source.session,
      state.sourceWorkspaceId,
      exported.job.id,
      {
        rangeStart: upload.receivedBytes === 0 ? undefined : upload.receivedBytes,
        signal: input.signal,
      },
    ), upload.receivedBytes, archiveBytes);
    await streamLiveLeafenArchiveParts({
      archiveBytes,
      body: response.body,
      nextPart: upload.nextPart,
      partBytes: upload.partBytes,
      receivedBytes: upload.receivedBytes,
      digestPart: sha256Hex,
      signal: input.signal,
      putPart: async (partNumber, bytes, digest) => {
        upload = validateTargetUpload(await api.archiveUploadPart(
          input.session,
          input.targetWorkspaceId,
          upload.uploadId,
          partNumber,
          bytes,
          digest,
        ), state, input.targetWorkspaceId);
      },
      onProgress: (receivedBytes, totalBytes) => input.onProgress?.({
        stage: "target-upload",
        sourceJobId: exported.job.id,
        targetUploadId: upload.uploadId,
        receivedBytes,
        archiveBytes: totalBytes,
      }),
    });
  }

  const targetJob = await api.archiveUploadComplete(
    input.session,
    input.targetWorkspaceId,
    upload.uploadId,
    { requestId: state.targetRequestId },
  );
  state = remember(input, { ...state, targetJobId: targetJob.id });
  input.onProgress?.({ stage: "target-import", targetJob });
  return waitForTargetImport(input, targetJob);
}

async function cancel(input: Pick<LiveLeafenTransferInput, "session" | "targetWorkspaceId">) {
  const state = readRememberedLiveLeafenArchiveTransfer(
    input.session.user.id,
    input.targetWorkspaceId,
  );
  if (!state) return null;
  if (state.targetJobId) {
    return api.cancelWorkspaceImport(
      input.session,
      input.targetWorkspaceId,
      state.targetJobId,
    );
  }
  if (state.targetUploadId) {
    await api.archiveUploadCancel(
      input.session,
      input.targetWorkspaceId,
      state.targetUploadId,
      { requestId: state.targetRequestId },
    );
  }
  clearRememberedLiveLeafenArchiveTransfer(
    input.session.user.id,
    input.targetWorkspaceId,
  );
  return null;
}

export const liveLeafenArchiveTransferService = {
  cancel,
  clear(actorId: string, targetWorkspaceId: string) {
    return clearRememberedLiveLeafenArchiveTransfer(actorId, targetWorkspaceId);
  },
  read(actorId: string, targetWorkspaceId: string) {
    return readRememberedLiveLeafenArchiveTransfer(actorId, targetWorkspaceId);
  },
  run,
};
