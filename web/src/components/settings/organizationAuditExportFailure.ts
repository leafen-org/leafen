import { organizationAuditErrorCodes } from "@/organizationAuditWire.generated";
import { organizationErrorCodes } from "@/organizationWire.generated";
import { userErrorText, userFailureText } from "@/lib/settingsErrors";

type Translate = (key: string, options?: Record<string, unknown>) => string;

/**
 * Why an audit log export failed, in the reader's words (2026-11-13: it read
 * only "감사 로그를 내보내지 못했습니다."): no right to the log (lost while it
 * ran, too), a range too large or not one to export — each its reason alone —
 * or the server's own trouble, which carries its reference to look up. A
 * request with no answer or turned away for too many says so as every
 * setting does.
 */
export function auditExportFailureText(error: unknown, t: Translate) {
  const code = (error as { code?: unknown } | null)?.code;
  switch (code) {
    case "LEAFEN_PERMISSION_DENIED":
    case organizationErrorCodes.notFound:
      return userErrorText(error, t("auditExportFailedPermission"));
    case organizationAuditErrorCodes.tooLarge:
      return userErrorText(error, t("auditExportFailedTooLarge"));
    case organizationAuditErrorCodes.invalid:
      return userErrorText(error, t("auditExportFailedRange"));
    default:
      return userFailureText(error, t("auditExportFailed"));
  }
}
