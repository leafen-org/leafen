import { administratorErrorCodes } from "../../administratorWire.generated";
import { LeafenApiError } from "../../apiTransport";
import { pickedFoldersOnly } from "../../lib/deviceFolder";
import { protectionRequiredText } from "./scheduledBackupFolderWords";

type Translate = (key: string, options?: Record<string, unknown>) => string;

/** Why the last scheduled backup failed, in the settings screen's words. */
export function scheduledBackupFailureText(code: string, target: string, t: Translate) {
  if (code === administratorErrorCodes.scheduledBackupFolderUnavailable) {
    if (target === "s3") return t("schedule.failedS3");
    if (target === "webdav") return t("schedule.failedWebdav");
    // A phone's picked folder is checked by picking it again, not by its path.
    return t(pickedFoldersOnly() ? "schedule.failedFolderDevice" : "schedule.failedFolder");
  }
  // The common causes, each said for the place it is about (2026-11-22: a
  // WebDAV address answering 301 said only "백업하지 못했습니다").
  if (code === administratorErrorCodes.scheduledBackupPlaceMissing) {
    if (target === "s3") return t("schedule.failedMissingS3");
    if (target === "webdav") return t("schedule.failedMissingWebdav");
    return t(pickedFoldersOnly() ? "schedule.failedFolderDevice" : "schedule.failedMissingFolder");
  }
  if (code === administratorErrorCodes.scheduledBackupPlaceMoved) {
    if (target === "s3") return t("schedule.failedMovedS3");
    if (target === "webdav") return t("schedule.failedMovedWebdav");
  }
  if (code === administratorErrorCodes.scheduledBackupPlaceUnreachable) {
    if (target === "s3") return t("schedule.failedUnreachableS3");
    if (target === "webdav") return t("schedule.failedUnreachableWebdav");
  }
  if (code === administratorErrorCodes.scheduledBackupPlaceMoved || code === administratorErrorCodes.scheduledBackupPlaceUnreachable) {
    return t(pickedFoldersOnly() ? "schedule.failedFolderDevice" : "schedule.failedFolder");
  }
  if (code === administratorErrorCodes.scheduledBackupWebDAVUnauthorized) return t("schedule.failedWebdavUnauthorized");
  if (code === administratorErrorCodes.scheduledBackupS3Unauthorized) return t("schedule.failedS3Unauthorized");
  if (code === administratorErrorCodes.scheduledBackupNoSpace) return t("schedule.failedSpace");
  if (code === administratorErrorCodes.backupProtectionRequired) return protectionRequiredText(t);
  if (code === administratorErrorCodes.scheduledBackupPlaceForeign) return t("schedule.failedForeign");
  if (code === administratorErrorCodes.scheduledBackupPlaceOpened) return t("schedule.failedOpened");
  return t("schedule.failed");
}

// The common cause of what a backup place answered — its sign-in, its
// permission, its address, its space (backup_failure_reason.go) — one line
// in this screen's words; not where the failure's own words say it already.
const FAILURE_CAUSES: Record<string, string> = {
  signIn: "serverConsole:schedule.cause.signIn", permission: "serverConsole:schedule.cause.permission",
  address: "serverConsole:schedule.cause.address", space: "serverConsole:schedule.cause.space",
};
const CAUSE_SAID_BY_CODE: Record<string, string> = {
  [administratorErrorCodes.scheduledBackupPlaceMoved]: "address", [administratorErrorCodes.scheduledBackupNoSpace]: "space",
};

/** Under a backup place's failure: the common cause of what the place answered, then what it answered. */
export function backupFailureDetails(reason: string | undefined, cause: string | undefined, code: string, t: Translate) {
  const line = cause && CAUSE_SAID_BY_CODE[code] !== cause && FAILURE_CAUSES[cause] ? t(FAILURE_CAUSES[cause]) : "";
  return [line, reason ? t("serverConsole:schedule.failureReason", { reason }) : ""].filter(Boolean).join(" ");
}

/** The same under a refusal that came from what the place answered. */
export function backupRefusalDetails(error: unknown, t: Translate) {
  return error instanceof LeafenApiError ? backupFailureDetails(error.reason, error.causeKey, error.code, t) : "";
}

/** Why changes no longer reach the continuous replica. */
export function replicationFailureText(code: string, target: string, t: Translate) {
  if (code === administratorErrorCodes.replicaOwnedElsewhere) return t("replication.ownedElsewhere");
  if (code === administratorErrorCodes.replicaUnavailable) return t("replication.missing");
  if (!code) return t("replication.behind");
  return scheduledBackupFailureText(code, target, t);
}

/** A place's failure codes, which the screens that read a place tell as they tell an unreadable place. */
export function backupPlaceUnavailable(code: string) {
  return code === administratorErrorCodes.scheduledBackupFolderUnavailable || code === administratorErrorCodes.scheduledBackupPlaceMissing
    || code === administratorErrorCodes.scheduledBackupPlaceMoved || code === administratorErrorCodes.scheduledBackupPlaceUnreachable;
}
