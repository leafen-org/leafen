import { administratorErrorCodes } from "../../administratorWire.generated";
import { LeafenApiError } from "../../apiTransport";
import { pickedFoldersOnly } from "../../lib/deviceFolder";
import { productErrorMessage } from "../../lib/productErrorPresentation";

type Translate = (key: string, options?: Record<string, unknown>) => string;

/** Whether `folder` is the container's backups folder or one in it. */
function inContainerFolder(folder: string, containerFolder: string) {
  const path = folder.trim().replace(/\/+$/u, "");
  return path === containerFolder || path.startsWith(`${containerFolder}/`);
}

/**
 * The backup folder field's example, help and refusal, fitting where the
 * server runs: on a computer, a folder of that computer; in a container
 * (`containerFolder`, the folder Leafen's image mounts for backups —
 * scheduled_backup_targets.go), that folder, and a refused path says the
 * server runs in a container and names it (2026-11-18: a Docker server's
 * screen gave a Mac path as its example and refused one without saying why).
 * A path in the container's folder refused still gets the plain refusal,
 * which says the folder it goes in must be there. On a phone's own server a
 * folder is picked, not typed: its help says where to pick one, and a
 * refused one was there but took no file (2026-11-21).
 */
export function scheduledBackupFolderWords(t: Translate, containerFolder: string) {
  if (pickedFoldersOnly()) {
    const refusal = t("schedule.folderInvalidDevice");
    return { placeholder: "", help: t("schedule.helpDevice"), refusals: [refusal], refusal: () => refusal };
  }
  const plainRefusal = t("schedule.folderInvalid");
  if (!containerFolder) {
    return { placeholder: t("schedule.folderPlaceholder"), help: t("schedule.help"),
      refusals: [plainRefusal], refusal: () => plainRefusal };
  }
  const containerRefusal = t("schedule.folderInvalidContainer", { folder: containerFolder });
  return {
    placeholder: containerFolder, help: t("schedule.helpContainer", { folder: containerFolder }),
    refusals: [plainRefusal, containerRefusal],
    refusal: (folder: string) => inContainerFolder(folder, containerFolder) ? plainRefusal : containerRefusal,
  };
}

/** That a place off the machine needs a recovery code or a password first, in the words of where the server runs. */
export function protectionRequiredText(t: Translate) {
  return t(pickedFoldersOnly() ? "protection.requiredDevice" : "protection.required");
}

/** What a refused save, backup or move says; a refused folder in the words of where the server runs. */
export function scheduleMessage(error: unknown, t: Translate, folder = "", containerFolder = "") {
  if (error instanceof LeafenApiError) {
    if (error.code === administratorErrorCodes.scheduledBackupFolderInvalid) {
      return scheduledBackupFolderWords(t, containerFolder).refusal(folder);
    }
    if (error.code === administratorErrorCodes.scheduledBackupWebDAVInvalid) return t("schedule.webdavInvalid");
    if (error.code === administratorErrorCodes.scheduledBackupWebDAVUnauthorized) return t("schedule.webdavUnauthorized");
    if (error.code === administratorErrorCodes.scheduledBackupS3Invalid) return t("schedule.s3Invalid");
    if (error.code === administratorErrorCodes.scheduledBackupS3Unauthorized) return t("schedule.s3Unauthorized");
    if (error.code === administratorErrorCodes.scheduledBackupS3KeysMissing) return t("schedule.s3KeysMissing");
    if (error.code === administratorErrorCodes.scheduledBackupWebDAVCredentialsMissing) return t("schedule.webdavCredentialsMissing");
    if (error.code === administratorErrorCodes.scheduledBackupConflict) return t("schedule.conflict");
    if (error.code === administratorErrorCodes.scheduledBackupSetByEnvironment) return t("schedule.setByEnvironment");
    if (error.code === administratorErrorCodes.backupProtectionRequired) return protectionRequiredText(t);
  }
  return productErrorMessage(error, "administratorScheduledBackup.save");
}
