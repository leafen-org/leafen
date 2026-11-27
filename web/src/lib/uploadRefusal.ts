import { i18next } from "@/i18n";
import { enterpriseDLPErrorCodes } from "@/enterpriseDLPWire.generated";
import { fileTransferErrorCodes } from "@/fileTransferWire.generated";
import { organizationErrorCodes } from "@/organizationWire.generated";
import { dlpWriteRefusal } from "@/lib/settingsErrors";
import { isRefusedForNow } from "@/lib/serverBusy";

/**
 * Why the server refused an upload, in the person's words, when it said why:
 * the organization's storage limit is reached, its usage is being counted
 * again, its DLP policy blocks the file, the server's disk is almost full,
 * or the server was too busy for it, asked again long enough (lib/serverBusy.ts)
 * — or null for any other failure,
 * which keeps the place's own message (2026-10-16: an upload past the
 * organization's storage limit read only "couldn't upload").
 */
export function uploadRefusalReason(error: unknown): string | null {
  const code = (error as { code?: unknown } | null)?.code;
  if (code === organizationErrorCodes.storageLimitExceeded) return i18next.t("settingsErrors:backend.organizationStorageLimitExceeded");
  if (code === organizationErrorCodes.storageReconciliationPending) return i18next.t("settingsErrors:backend.organizationStorageRecounting");
  if (code === enterpriseDLPErrorCodes.policyBlocked) return dlpWriteRefusal(error);
  if (code === fileTransferErrorCodes.serverDiskLow) return i18next.t("settingsErrors:backend.serverDiskLow");
  if (isRefusedForNow(error)) return i18next.t("common:serverBusy");
  return null;
}
