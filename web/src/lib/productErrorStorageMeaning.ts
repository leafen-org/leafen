import { authErrorCodes } from "@/authWire.generated";
import { databaseAutomationErrorCodes } from "@/databaseAutomationWire.generated";
import { interactiveAutomationErrorCodes } from "@/interactiveAutomationWire.generated";
import { productErrorText } from "@/lib/productErrorText";
import { pageAnalyticsErrorCodes } from "@/pageAnalyticsWire.generated";
import { workspaceMembershipErrorCodes } from "@/workspaceMembershipWire.generated";
import type {
  ProductErrorContextSpec,
  ProductErrorText as LocalizedText,
} from "@/lib/productErrorContextCatalog";
import type {
  ProductErrorMeaning,
  ProductErrorRetryDisposition,
} from "@/lib/productErrorMeaningCatalog";

export type ProductErrorRecovery = {
  sameJobRetry?: boolean;
};

type PersistentWriteStorageFailureClass =
  | "persistent-write-database-busy"
  | "persistent-write-storage-unavailable";

export function persistentWriteStorageFailureClass(
  code: string,
  contextKey: string,
): PersistentWriteStorageFailureClass | null {
  const pageAnalyticsContext = contextKey === "pageAnalytics.analyticsRead"
    || contextKey === "pageAnalytics.preferencesRead"
    || contextKey === "pageAnalytics.preferencesUpdate"
    || contextKey === "pageAnalytics.policyUpdate";
  if ((code === workspaceMembershipErrorCodes.settingsStorageUnavailable
      && contextKey === "workspace.settings.update")
    || (code === workspaceMembershipErrorCodes.createStorageUnavailable
      && contextKey === "workspace.create")
    || (code === workspaceMembershipErrorCodes.memberAddStorageUnavailable
      && contextKey === "workspace.member.add")
    || (code === workspaceMembershipErrorCodes.memberMutationStorageUnavailable
      && contextKey === "workspace.member.revoke")
    || (code === authErrorCodes.accountProfileStorageUnavailable
      && contextKey === "accountProfile.update")
    || (code === workspaceMembershipErrorCodes.deleteStorageUnavailable
      && contextKey === "workspace.delete")
    || (code === databaseAutomationErrorCodes.storageUnavailable
      && contextKey === "databaseAutomation.write")
    || (code === interactiveAutomationErrorCodes.storageUnavailable
      && contextKey === "interactiveAutomation.execute")
    || (code === pageAnalyticsErrorCodes.storageUnavailable
      && pageAnalyticsContext)) {
    return "persistent-write-storage-unavailable";
  }
  if ((code === workspaceMembershipErrorCodes.settingsDatabaseBusy
      && contextKey === "workspace.settings.update")
    || (code === workspaceMembershipErrorCodes.createDatabaseBusy
      && contextKey === "workspace.create")
    || (code === workspaceMembershipErrorCodes.memberAddDatabaseBusy
      && contextKey === "workspace.member.add")
    || (code === workspaceMembershipErrorCodes.memberMutationDatabaseBusy
      && contextKey === "workspace.member.revoke")
    || (code === authErrorCodes.accountProfileDatabaseBusy
      && contextKey === "accountProfile.update")
    || (code === workspaceMembershipErrorCodes.deleteDatabaseBusy
      && contextKey === "workspace.delete")
    || (code === databaseAutomationErrorCodes.databaseBusy
      && contextKey === "databaseAutomation.write")
    || (code === interactiveAutomationErrorCodes.databaseBusy
      && contextKey === "interactiveAutomation.execute")
    || (code === pageAnalyticsErrorCodes.databaseBusy
      && pageAnalyticsContext)) {
    return "persistent-write-database-busy";
  }
  return null;
}

function local(text: LocalizedText): string {
  return productErrorText(text.key);
}

function meaningResult(spec: ProductErrorContextSpec) {
  const operation = local(spec.operation);
  return (
    cause: LocalizedText,
    effect: string,
    nextAction: LocalizedText | string,
    retryDisposition: ProductErrorRetryDisposition,
    retry: LocalizedText,
  ): ProductErrorMeaning => ({
    cause: local(cause),
    effect,
    known: true,
    nextAction: typeof nextAction === "string" ? nextAction : local(nextAction),
    operation,
    retry: local(retry),
    retryDisposition,
  });
}

export function persistentWriteStorageErrorMeaning(
  classification: string,
  spec: ProductErrorContextSpec,
): ProductErrorMeaning | null {
  if (classification !== "persistent-write-storage-unavailable"
    && classification !== "persistent-write-database-busy") return null;
  const result = meaningResult(spec);
  const storageUnavailable = classification === "persistent-write-storage-unavailable";
  return result(
    storageUnavailable
      ? { key: "meanings.serverStorageTemporarilyUnavailable" }
      : { key: "meanings.anotherServerOperationTemporarilyLockedProduct" },
    local(spec.deniedEffect),
    storageUnavailable
      ? { key: "meanings.retrySameOperationAfterServerStorage" }
      : { key: "meanings.retrySameOperationAfterOtherServer" },
    storageUnavailable ? "safe-after-action" : "safe-after-wait",
    storageUnavailable
      ? { key: "meanings.safeRetrySameOperationAfterStorage" }
      : { key: "meanings.safeRetrySameOperationAfterTemporary" },
  );
}

export function productErrorStorageMeaning(
  classification: string,
  spec: ProductErrorContextSpec,
  recovery: ProductErrorRecovery = {},
): ProductErrorMeaning | null {
  const result = meaningResult(spec);
  const deniedEffect = local(spec.deniedEffect);
  const uncertainEffect = local(spec.uncertainEffect);
  const stateCheckAction = local(spec.stateCheckAction);

  if (classification === "source-changed") return result(
    { key: "meanings.workspaceChangedWhileExportRunning" },
    local({ key: "meanings.jobSArchiveNotPublishedAnd" }),
    { key: "meanings.startReplacementExportFromLatestWorkspace" },
    "replacement-required",
    { key: "meanings.doNotResumeFailedJobStart" },
  );
  if (classification === "form-storage-unavailable") return result(
    { key: "meanings.serverFormStorageTemporarilyUnavailable" },
    deniedEffect,
    { key: "meanings.retrySameFormOperationAfterServer" },
    "safe-after-action",
    { key: "meanings.safeRetrySameFormOperationAfter" },
  );
  if (classification === "form-database-busy") return result(
    { key: "meanings.formStorageTemporarilyLockedByAnother" },
    deniedEffect,
    { key: "meanings.retrySameFormOperationAfterOther" },
    "safe-after-wait",
    { key: "meanings.safeRetrySameFormOperationAfter2" },
  );
  if (classification === "auth-storage-unavailable") return result(
    { key: "meanings.serverAccountStorageTemporarilyUnavailable" },
    deniedEffect,
    { key: "meanings.tryAgainAfterServerStorageRecovers" },
    "safe-after-action",
    { key: "meanings.safeRetrySameAuthenticationOperationAfter" },
  );
  if (classification === "auth-database-busy") return result(
    { key: "meanings.anotherServerOperationTemporarilyLockedAccount" },
    deniedEffect,
    { key: "meanings.waitForOtherServerOperationFinish" },
    "safe-after-wait",
    { key: "meanings.safeRetryAfterTemporaryLockClears" },
  );
  if (classification === "workspace-export-storage-unavailable") return result(
    { key: "meanings.serverStorageTemporarilyUnavailableSoArchive" },
    deniedEffect,
    recovery.sameJobRetry
      ? { key: "meanings.retryExportJobAfterServerStorage" }
      : stateCheckAction,
    recovery.sameJobRetry ? "safe-after-action" : "replacement-required",
    recovery.sameJobRetry
      ? { key: "meanings.safeRetryWithSameJobIdentity" }
      : { key: "meanings.jobCannotRetriedStartReplacementExport" },
  );
  if (classification === "workspace-export-database-busy") return result(
    { key: "meanings.anotherServerOperationTemporarilyLockedDatabase" },
    local({ key: "meanings.noNewArchivePublishedAndSource" }),
    recovery.sameJobRetry
      ? { key: "meanings.retryExportAfterOtherServerOperation" }
      : stateCheckAction,
    recovery.sameJobRetry ? "safe-after-action" : "state-check-required",
    recovery.sameJobRetry
      ? { key: "meanings.safeRetryWithSameJobIdentifier" }
      : { key: "meanings.checkCurrentJobStateBeforeDeciding" },
  );
  if (classification === "dlp-storage-unavailable") return result(
    { key: "meanings.serverStorageTemporarilyUnavailableSoDlp" },
    deniedEffect,
    { key: "meanings.saveSameDlpPolicyAgainAfter" },
    "safe-after-action",
    { key: "meanings.safeRetrySameDlpPolicySave" },
  );
  if (classification === "dlp-database-busy") return result(
    { key: "meanings.anotherServerOperationTemporarilyLockedDlp" },
    deniedEffect,
    { key: "meanings.saveSameDlpPolicyAgainAfter2" },
    "safe-after-wait",
    { key: "meanings.safeRetrySameDlpPolicySave2" },
  );
  if (classification === "organization-storage-limit-exceeded") return result(
    { key: "meanings.organizationStorageLimitWouldExceeded" },
    deniedEffect,
    { key: "meanings.removeUnneededFilesOrAskOrganization" },
    "safe-after-action",
    { key: "meanings.safeRetrySameFileOperationAfter" },
  );
  // A full disk, to someone who asks the administrator: the disk, whom to ask,
  // and that it works again once there is room. A refusal that says to whom
  // it was made and why — too full or too small — takes its own words
  // (lib/serverDiskRefusal.ts).
  if (classification === "server-disk-low") return result(
    { key: "meanings.serverDiskAlmostFull" },
    deniedEffect,
    { key: "meanings.askServerAdministratorToFreeSpace" },
    "safe-after-action",
    { key: "meanings.safeRetryOnceServerHasRoom" },
  );
  if (classification === "organization-storage-reconciliation-pending") return result(
    { key: "meanings.leafenRebuildingOrganizationStorageUsageState" },
    uncertainEffect,
    { key: "meanings.waitForStorageReconciliationAdvanceThen" },
    "safe-after-wait",
    { key: "meanings.sameRequestIdentityCanRetriedSafely" },
  );
  if (classification === "organization-storage-state-invalid") return result(
    { key: "meanings.organizationStorageAuthorityInconsistent" },
    uncertainEffect,
    { key: "meanings.keepDiagnosticReferenceAndAskServer" },
    "state-check-required",
    { key: "meanings.doNotAssumeUnchangedRetrySafe" },
  );
  if (classification === "file-storage-unavailable") return result(
    { key: "meanings.serverFileStorageTemporarilyUnavailable" },
    deniedEffect,
    { key: "meanings.retrySameFileOperationAfterServer" },
    "safe-after-action",
    { key: "meanings.safeRetrySameFileOperationAfter2" },
  );
  if (classification === "file-resumable-required") return result(
    { key: "meanings.fileLargerThanOneDirectUpload" },
    deniedEffect,
    { key: "meanings.useResumableUploadSoProgressCan" },
    "replacement-required",
    { key: "meanings.doNotRetryDirectRequestUnchanged" },
  );
  if (classification === "workspace-import-resumable-required") return result(
    { key: "meanings.leafenArchiveLargerThanOneDirect" },
    deniedEffect,
    { key: "meanings.useResumableArchiveUploadSoProgress" },
    "replacement-required",
    { key: "meanings.doNotRetryDirectRequestUnchanged2" },
  );
  if (classification === "file-database-busy") return result(
    { key: "meanings.anotherServerOperationTemporarilyLockedFile" },
    deniedEffect,
    { key: "meanings.retrySameFileOperationAfterOther" },
    "safe-after-wait",
    { key: "meanings.safeRetrySameFileOperationAfter3" },
  );
  return null;
}
