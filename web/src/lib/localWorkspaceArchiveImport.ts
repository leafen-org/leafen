import { sha256 } from "@noble/hashes/sha2.js";
import { bytesToHex } from "@noble/hashes/utils.js";

import { api } from "../api";
import { fileTransferBounds } from "../fileTransferWire.generated";
import type { Session } from "../types";
import type { WorkspaceArchiveImportUpload } from "../workspaceImportWire.generated";

export type LocalWorkspaceArchiveProgress = {
  stage: "hashing" | "uploading" | "admitting";
  receivedBytes: number;
  archiveBytes: number;
};

function validUpload(
  upload: WorkspaceArchiveImportUpload,
  workspaceId: string,
  requestId: string,
  file: File,
  digest: string,
) {
  const expectedOffset = Math.min(upload.nextPart * upload.partBytes, upload.archiveBytes);
  if (
    upload.workspaceId !== workspaceId
    || upload.requestId !== requestId
    || upload.archiveBytes !== file.size
    || upload.archiveSha256 !== digest
    || !Number.isSafeInteger(upload.partBytes)
    || upload.partBytes <= 0
    || !Number.isSafeInteger(upload.nextPart)
    || upload.nextPart < 0
    || upload.receivedBytes !== expectedOffset
    || upload.receivedBytes < 0
    || upload.receivedBytes > upload.archiveBytes
  ) {
    throw Object.assign(new Error("The local Leafen archive upload does not match the selected file."), {
      code: "LEAFEN_WORKSPACE_IMPORT_STATE_INVALID",
      stage: "local-archive-upload",
      status: 409,
    });
  }
  return upload;
}

async function hashFile(
  file: File,
  turnBytes: number,
  onProgress?: (progress: LocalWorkspaceArchiveProgress) => void,
) {
  const hasher = sha256.create();
  for (let offset = 0; offset < file.size; offset += turnBytes) {
    const end = Math.min(offset + turnBytes, file.size);
    hasher.update(new Uint8Array(await file.slice(offset, end).arrayBuffer()));
    onProgress?.({ stage: "hashing", receivedBytes: end, archiveBytes: file.size });
  }
  return bytesToHex(hasher.digest());
}

async function run(input: {
  file: File;
  onProgress?: (progress: LocalWorkspaceArchiveProgress) => void;
  requestId: string;
  session: Session;
  workspaceId: string;
}) {
  const digest = await hashFile(input.file, fileTransferBounds.partBytes, input.onProgress);
  let upload = validUpload(await api.archiveUploadPrepare(
    input.session,
    input.workspaceId,
    {
      requestId: input.requestId,
      filename: input.file.name,
      bytes: input.file.size,
      sha256: digest,
    },
  ), input.workspaceId, input.requestId, input.file, digest);
  if (upload.job) return upload.job;
  while (upload.receivedBytes < upload.archiveBytes) {
    const offset = upload.receivedBytes;
    const end = Math.min(offset + upload.partBytes, upload.archiveBytes);
    const part = new Uint8Array(await input.file.slice(offset, end).arrayBuffer());
    const partDigest = bytesToHex(sha256(part));
    upload = validUpload(await api.archiveUploadPart(
      input.session,
      input.workspaceId,
      upload.uploadId,
      upload.nextPart,
      part,
      partDigest,
    ), input.workspaceId, input.requestId, input.file, digest);
    input.onProgress?.({
      stage: "uploading",
      receivedBytes: upload.receivedBytes,
      archiveBytes: upload.archiveBytes,
    });
  }
  input.onProgress?.({
    stage: "admitting",
    receivedBytes: upload.receivedBytes,
    archiveBytes: upload.archiveBytes,
  });
  return api.archiveUploadComplete(
    input.session,
    input.workspaceId,
    upload.uploadId,
    { requestId: input.requestId },
  );
}

export const localWorkspaceArchiveImport = { run };
