import { createDurableJobMemory } from "./durableJobMemory";

const LIVE_LEAFEN_TRANSFER_PREFIX = "leafen:live-archive-transfer";
const SHA256 = /^[a-f0-9]{64}$/u;

export type RememberedLiveLeafenArchiveTransfer = {
  sourceUrl: string;
  sourceWorkspaceId: string;
  sourceRequestId?: string;
  sourceJobId?: string;
  artifactBytes?: number;
  artifactSha256?: string;
  targetRequestId: string;
  targetUploadId?: string;
  targetJobId?: string;
};

function boundedString(value: unknown, max: number): value is string {
  return typeof value === "string" && value.length > 0 && value.length <= max;
}

function optionalBoundedString(value: unknown, max: number) {
  return value === undefined || boundedString(value, max);
}

function validRememberedLiveLeafenArchiveTransfer(
  value: unknown,
): value is RememberedLiveLeafenArchiveTransfer {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const record = value as Record<string, unknown>;
  return (
    boundedString(record.sourceUrl, 2_048)
    && boundedString(record.sourceWorkspaceId, 255)
    && boundedString(record.targetRequestId, 255)
    && optionalBoundedString(record.sourceRequestId, 255)
    && optionalBoundedString(record.sourceJobId, 96)
    && optionalBoundedString(record.targetUploadId, 96)
    && optionalBoundedString(record.targetJobId, 96)
    && (record.artifactBytes === undefined
      || Number.isSafeInteger(record.artifactBytes) && Number(record.artifactBytes) > 0)
    && (record.artifactSha256 === undefined
      || typeof record.artifactSha256 === "string" && SHA256.test(record.artifactSha256))
    && ((record.artifactBytes === undefined) === (record.artifactSha256 === undefined))
    && !Object.hasOwn(record, "sourceToken")
    && !Object.hasOwn(record, "authorization")
  );
}

const memory = createDurableJobMemory<RememberedLiveLeafenArchiveTransfer>({
  bounds: {
    maxIdentityCharacters: 512,
    maxSerializedCharacters: 4_096,
    maxStorageKeyCharacters: 1_600,
  },
  decode(value) {
    const parsed: unknown = JSON.parse(value);
    return validRememberedLiveLeafenArchiveTransfer(parsed) ? parsed : null;
  },
  encode(value) {
    return validRememberedLiveLeafenArchiveTransfer(value) ? JSON.stringify(value) : null;
  },
  identityPartCount: 2,
  storagePrefix: LIVE_LEAFEN_TRANSFER_PREFIX,
});

export function rememberLiveLeafenArchiveTransfer(
  actorId: string,
  targetWorkspaceId: string,
  value: RememberedLiveLeafenArchiveTransfer,
) {
  return memory.remember([actorId, targetWorkspaceId], value);
}

export function readRememberedLiveLeafenArchiveTransfer(
  actorId: string,
  targetWorkspaceId: string,
) {
  return memory.read([actorId, targetWorkspaceId]);
}

export function clearRememberedLiveLeafenArchiveTransfer(
  actorId: string,
  targetWorkspaceId: string,
) {
  return memory.clear([actorId, targetWorkspaceId]);
}
