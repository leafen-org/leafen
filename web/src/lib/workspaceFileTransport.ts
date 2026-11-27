import { API_ROOT, request } from "@/apiTransport";
import {
  fileLifecycleOperations,
  type FileDeleteResponse,
} from "@/fileLifecycleWire.generated";
import {
  fileTransferOperations,
  type FileDownloadCapability,
  type FileTransferFile,
  type FileUploadCompleteResponse,
  type FileUploadPartResponse,
  type FileUploadPrepareInput,
  type FileUploadPurpose,
  type FileUploadStateResponse,
} from "@/fileTransferWire.generated";
import type { Session } from "@/types";
import { serverOrigin } from "@/lib/serverOrigin";

export type WorkspaceFileRequestOptions = {
  signal?: AbortSignal;
};

export type DirectWorkspaceFileUploadInput = WorkspaceFileRequestOptions & {
  contentType: string;
  file: File;
  name: string;
  requestId: string;
  workspaceId: string;
  /** The page the file goes up for, which lets someone the page is shared with send it. */
  pageId?: string;
  purpose?: FileUploadPurpose;
  /** The room the file goes up for, which lets a guest in it send it. */
  roomId?: string;
};

export type WorkspaceFilePartInput = WorkspaceFileRequestOptions & {
  part: Blob;
  partNumber: number;
  sha256: string;
  uploadId: string;
};

export type WorkspaceFileCompleteInput = WorkspaceFileRequestOptions & {
  requestId: string;
  uploadId: string;
};

export type WorkspaceFileDeleteInput = WorkspaceFileRequestOptions & {
  fileId: string;
  requestId: string;
};

export type WorkspaceFileCapabilityInput = WorkspaceFileRequestOptions & {
  fileId: string;
};

export interface WorkspaceFileTransport {
  direct(session: Session, input: DirectWorkspaceFileUploadInput): Promise<FileTransferFile>;
  prepare(
    session: Session,
    input: FileUploadPrepareInput & WorkspaceFileRequestOptions,
  ): Promise<FileUploadStateResponse>;
  uploadPart(session: Session, input: WorkspaceFilePartInput): Promise<FileUploadPartResponse>;
  complete(session: Session, input: WorkspaceFileCompleteInput): Promise<FileUploadCompleteResponse>;
  deleteFile(session: Session, input: WorkspaceFileDeleteInput): Promise<FileDeleteResponse>;
  issueDownloadCapability(
    session: Session,
    input: WorkspaceFileCapabilityInput,
  ): Promise<FileDownloadCapability>;
}

function operationPath(path: string, name: string, value: string) {
  return path.replace(`{${name}}`, encodeURIComponent(value));
}

export function nativeWorkspaceFileLocator(fileId: string) {
  return `${API_ROOT}${operationPath(fileTransferOperations.download.path, "id", fileId)}`;
}

/**
 * A cover or image icon as the address of the file it shows: a managed
 * file's own ID, as the API and imports keep one, reads as that file's
 * address, as the screen keeps an uploaded one; any other value as it is.
 */
export function managedFileValueLocator(value: string | undefined) {
  const raw = value?.trim() ?? "";
  return /^[A-Za-z0-9_-]{15}$/u.test(raw) ? nativeWorkspaceFileLocator(raw) : raw;
}

export function nativeWorkspaceFileIdFromLocator(
  value: string | undefined,
  appOrigin = serverOrigin(),
) {
  const raw = value?.trim() ?? "";
  if (!raw || !/^(?:https?:\/\/|\/)/iu.test(raw)) return "";
  try {
    const application = new URL(appOrigin);
    const api = new URL(API_ROOT, application);
    const parsed = new URL(raw, application);
    if (
      !["http:", "https:"].includes(parsed.protocol)
      || (parsed.origin !== application.origin && parsed.origin !== api.origin)
    ) return "";
    const marker = `${API_ROOT}${fileTransferOperations.download.path.replace("{id}", "")}`;
    if (!parsed.pathname.startsWith(marker)) return "";
    const encodedId = parsed.pathname.slice(marker.length);
    if (!encodedId || encodedId.includes("/")) return "";
    const fileId = decodeURIComponent(encodedId);
    if (
      !fileId
      || fileId.trim() !== fileId
      || Array.from(fileId).length > 255
    ) return "";
    return fileId;
  } catch {
    return "";
  }
}

export const nativeWorkspaceFileTransport: WorkspaceFileTransport = {
  direct(session, input) {
    const body = new FormData();
    body.set("requestId", input.requestId);
    body.set("workspaceId", input.workspaceId);
    if (input.pageId) body.set("pageId", input.pageId);
    if (input.purpose) body.set("purpose", input.purpose);
    if (input.roomId) body.set("roomId", input.roomId);
    body.set("file", input.file, input.name);
    return request<FileTransferFile>(
      fileTransferOperations.directUpload.path,
      { method: fileTransferOperations.directUpload.method, body, signal: input.signal },
      session,
    );
  },

  prepare(session, input) {
    const { signal, ...body } = input;
    return request<FileUploadStateResponse>(
      fileTransferOperations.prepare.path,
      { method: fileTransferOperations.prepare.method, body: JSON.stringify(body), signal },
      session,
    );
  },

  uploadPart(session, input) {
    const path = operationPath(
      operationPath(fileTransferOperations.part.path, "uploadId", input.uploadId),
      "partNumber",
      String(input.partNumber),
    );
    return request<FileUploadPartResponse>(
      path,
      {
        method: fileTransferOperations.part.method,
        headers: {
          "content-type": "application/octet-stream",
          "x-leafen-part-sha256": input.sha256,
        },
        body: input.part,
        signal: input.signal,
      },
      session,
    );
  },

  complete(session, input) {
    const path = operationPath(fileTransferOperations.complete.path, "uploadId", input.uploadId);
    return request<FileUploadCompleteResponse>(
      path,
      {
        method: fileTransferOperations.complete.method,
        body: JSON.stringify({ requestId: input.requestId }),
        signal: input.signal,
      },
      session,
    );
  },

  deleteFile(session, input) {
    const path = operationPath(fileLifecycleOperations.deleteFile.path, "id", input.fileId);
    return request<FileDeleteResponse>(
      path,
      {
        method: fileLifecycleOperations.deleteFile.method,
        body: JSON.stringify({ requestId: input.requestId }),
        signal: input.signal,
      },
      session,
    );
  },

  issueDownloadCapability(session, input) {
    const path = operationPath(fileTransferOperations.issueCapability.path, "id", input.fileId);
    return request<FileDownloadCapability>(
      path,
      { method: fileTransferOperations.issueCapability.method, signal: input.signal },
      session,
    );
  },
};
