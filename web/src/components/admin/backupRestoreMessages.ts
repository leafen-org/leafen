import { administratorErrorCodes } from "../../administratorWire.generated";
import { LeafenApiError } from "../../apiTransport";
import { productErrorMessage } from "../../lib/productErrorPresentation";
import { backupRefusalDetails, scheduledBackupFailureText } from "./scheduledBackupFailure";

type Translate = (key: string, options?: Record<string, unknown>) => string;

/** Why the backups at their place couldn't be listed, said for that place, with what it answered. */
export function listMessage(error: unknown, target: string, t: Translate) {
  return [listRefusal(error, target, t), backupRefusalDetails(error, t)].filter(Boolean).join(" ");
}

function listRefusal(error: unknown, target: string, t: Translate) {
  if (error instanceof LeafenApiError) {
    if (error.code === administratorErrorCodes.scheduledBackupPlaceMissing || error.code === administratorErrorCodes.scheduledBackupPlaceMoved
      || error.code === administratorErrorCodes.scheduledBackupPlaceUnreachable) return scheduledBackupFailureText(error.code, target, t);
    if (error.code === administratorErrorCodes.scheduledBackupWebDAVUnauthorized) return t("schedule.failedWebdavUnauthorized");
    if (error.code === administratorErrorCodes.scheduledBackupS3Unauthorized) return t("schedule.failedS3Unauthorized");
    if (error.code === administratorErrorCodes.scheduledBackupFolderUnavailable) {
      if (target === "s3") return t("schedule.failedS3");
      return target === "webdav" ? t("schedule.failedWebdav") : t("schedule.failedFolder");
    }
  }
  return productErrorMessage(error, "administratorScheduledBackup.read");
}

export function restoreMessage(error: unknown, target: string, t: Translate) {
  if (error instanceof LeafenApiError) {
    return restoreCodeMessage(error.code, target, t) ?? productErrorMessage(error, "administratorScheduledBackup.restore");
  }
  return productErrorMessage(error, "administratorScheduledBackup.restore");
}

// Why a restore was refused or stopped, by its code, where the restore says it its own way.
function restoreCodeMessage(code: string, target: string, t: Translate): string | null {
  // Its place not answering, moved or refusing, or no room here (backup_restore_background.go).
  if (code === administratorErrorCodes.scheduledBackupNoSpace) return t("restore.noSpace");
  if (code === administratorErrorCodes.scheduledBackupPlaceMoved || code === administratorErrorCodes.scheduledBackupPlaceUnreachable
    || code === administratorErrorCodes.scheduledBackupWebDAVUnauthorized || code === administratorErrorCodes.scheduledBackupS3Unauthorized
    || code === administratorErrorCodes.scheduledBackupFolderUnavailable) return scheduledBackupFailureText(code, target, t);
  if (code === administratorErrorCodes.scheduledBackupSnapshotUnavailable) return t("restore.snapshotUnavailable");
  // Data a restore can't take says why, and what to do instead.
  if (code === administratorErrorCodes.dataBeforeSchemaBaseline) return t("restore.earlierLeafen");
  if (code === administratorErrorCodes.dataFromNewerLeafen) return t("restore.newerLeafen");
  if (code === administratorErrorCodes.scheduledBackupRestorePending) return t("restore.alreadyWaiting");
  if (code === administratorErrorCodes.scheduledBackupBusy) return t("restore.busy");
  if (code === administratorErrorCodes.scheduledBackupUndoUnavailable) return t("restore.undoUnavailable");
  return null;
}
