import { administratorErrorCodes } from "@/administratorWire.generated";
import { productErrorText } from "@/lib/productErrorText";

export type ProductErrorRetryDisposition =
  | "replacement-required"
  | "safe-after-action"
  | "safe-after-wait"
  | "state-check-required";

export type ProductErrorMeaning = {
  cause: string;
  effect: string;
  known: boolean;
  nextAction: string;
  operation: string;
  retry: string;
  retryDisposition: ProductErrorRetryDisposition;
};

type LocalizedText = { readonly key: string };

function local(text: LocalizedText): string {
  return productErrorText(text.key);
}

type AdministratorErrorContext = {
  deniedEffect: string;
  operation: string;
  stateCheckAction: string;
  uncertainEffect: string;
};

function administratorMeaning(
  context: AdministratorErrorContext,
  cause: LocalizedText,
  nextAction: LocalizedText | string,
  retryDisposition: ProductErrorRetryDisposition,
  retry: LocalizedText,
  effect = context.deniedEffect,
): ProductErrorMeaning {
  return {
    cause: local(cause),
    effect,
    known: true,
    nextAction: typeof nextAction === "string" ? nextAction : local(nextAction),
    operation: context.operation,
    retry: local(retry),
    retryDisposition,
  };
}

export function administratorErrorMeaning(
  code: string,
  contextKey: string,
  context: AdministratorErrorContext,
): ProductErrorMeaning | null {
  if (contextKey.startsWith("administratorReport.")) {
    if (code === administratorErrorCodes.reportConflict) {
      return administratorMeaning(
        context,
        { key: "meanings.reportChangedAfterScreenLoaded" },
        context.stateCheckAction,
        "state-check-required",
        { key: "meanings.loadCurrentReportBeforeRefreshingAgain" },
      );
    }
    if (code === administratorErrorCodes.reportIdentityReused) {
      return administratorMeaning(
        context,
        { key: "meanings.requestIdentifierBelongsDifferentReportRefresh" },
        context.stateCheckAction,
        "replacement-required",
        { key: "meanings.loadCurrentReportAndUseNew" },
      );
    }
    if (code === administratorErrorCodes.reportReadFailed) {
      return administratorMeaning(
        context,
        { key: "meanings.serverCouldNotReadOneExact" },
        context.stateCheckAction,
        "state-check-required",
        { key: "meanings.loadCurrentReportBeforeTryingAgain" },
        context.uncertainEffect,
      );
    }
  }
  if (contextKey.startsWith("administratorBackup.")) {
    if (code === administratorErrorCodes.backupNotReady) {
      return administratorMeaning(
        context,
        { key: "meanings.backupArtifactNotCompleteYet" },
        context.stateCheckAction,
        "safe-after-wait",
        { key: "meanings.waitForCompletionThenRetryDownload" },
      );
    }
    if (code === administratorErrorCodes.backupIdentityReused) {
      return administratorMeaning(
        context,
        { key: "meanings.requestIdentifierBelongsDifferentOperation" },
        context.stateCheckAction,
        "replacement-required",
        { key: "meanings.checkCurrentStateAndUseNew" },
      );
    }
    if (code === administratorErrorCodes.backupRetryUnavailable
      || code === administratorErrorCodes.backupCancelUnavailable) {
      return administratorMeaning(
        context,
        { key: "meanings.backupStateNoLongerPermitsControl" },
        context.stateCheckAction,
        "state-check-required",
        { key: "meanings.reloadCurrentBackupBeforeChoosingAnother" },
      );
    }
    if (code === administratorErrorCodes.backupArtifactInvalid) {
      return administratorMeaning(
        context,
        { key: "meanings.storedBackupDidNotMatchVerified" },
        { key: "meanings.createNewBackupAndInspectServer" },
        "safe-after-action",
        { key: "meanings.doNotReuseInvalidArtifactCreate" },
      );
    }
    if (code === administratorErrorCodes.backupNotFound
      || code === administratorErrorCodes.backupReadFailed) {
      return administratorMeaning(
        context,
        { key: "meanings.serverCouldNotLoadBackupState" },
        context.stateCheckAction,
        "safe-after-action",
        { key: "meanings.reloadCurrentBackupStateBeforeContinuing" },
      );
    }
    if (code === administratorErrorCodes.backupCreateFailed) {
      return administratorMeaning(
        context,
        { key: "meanings.serverCouldNotCompleteBackupLifecycle" },
        context.stateCheckAction,
        "state-check-required",
        { key: "meanings.checkCurrentBackupBeforeRetrying" },
        context.uncertainEffect,
      );
    }
  }
  if (code === administratorErrorCodes.administratorReadFailed
    && (contextKey === "auth.bootstrap" || contextKey.startsWith("administratorSettings."))) {
    return administratorMeaning(
      context,
      { key: "meanings.serverCouldNotReadAdministratorState" },
      { key: "meanings.checkServerStateThenLoadSettings" },
      "safe-after-action",
      { key: "meanings.readDoesNotChangeSavedData" },
    );
  }
  if (code === administratorErrorCodes.setupClosed && contextKey === "auth.serverSetup") {
    return administratorMeaning(
      context,
      { key: "meanings.anotherAdministratorAlreadyCompletedServerSetup" },
      { key: "meanings.signInWithExistingAdministratorAccount" },
      "replacement-required",
      { key: "meanings.doNotRetrySetupRequestSign" },
    );
  }
  if (code === administratorErrorCodes.setupClaimFailed && contextKey === "auth.serverSetup") {
    return administratorMeaning(
      context,
      { key: "meanings.serverCouldNotCompleteAdministratorSetup" },
      context.stateCheckAction,
      "state-check-required",
      { key: "meanings.checkCurrentServerSetupStateBefore" },
      context.uncertainEffect,
    );
  }
  if (code === administratorErrorCodes.settingsInvalid
    && contextKey === "administratorSettings.save") {
    return administratorMeaning(
      context,
      { key: "meanings.administratorSettingsDoNotMeetServer" },
      { key: "meanings.reviewAndCorrectEnteredServerSettings" },
      "safe-after-action",
      { key: "meanings.safeRetrySameSettingsSaveAfter" },
    );
  }
  if (code === administratorErrorCodes.settingsConflict
    && contextKey === "administratorSettings.save") {
    return administratorMeaning(
      context,
      { key: "meanings.serverSettingsChangedAfterScreenLoaded" },
      context.stateCheckAction,
      "state-check-required",
      { key: "meanings.doNotRetryBeforeLoadingLatest" },
    );
  }
  if (code === administratorErrorCodes.settingsSaveFailed
    && contextKey === "administratorSettings.save") {
    return administratorMeaning(
      context,
      { key: "meanings.serverCouldNotCompleteAdministratorSettings" },
      context.stateCheckAction,
      "state-check-required",
      { key: "meanings.checkCurrentServerSettingsBeforeTrying" },
      context.uncertainEffect,
    );
  }
  if (code === administratorErrorCodes.secretFailed
    && contextKey === "administratorSettings.save") {
    return administratorMeaning(
      context,
      { key: "meanings.serverCouldNotProtectAdministratorSecret" },
      { key: "meanings.checkServerSecretStorageThenSave" },
      "safe-after-action",
      { key: "meanings.safeRetrySameSettingsAfterSecret" },
    );
  }
  if (code === administratorErrorCodes.administratorNotFound
    && (contextKey.startsWith("administratorSettings.") || contextKey.startsWith("administratorBackup.")
      || contextKey.startsWith("administratorReport."))) {
    return administratorMeaning(
      context,
      { key: "meanings.administratorSettingsNotAvailableCurrentAccount" },
      { key: "meanings.signInWithServerAdministratorAccount" },
      "safe-after-action",
      { key: "meanings.retryOnlyAfterAccountOrPermission" },
    );
  }
  return null;
}

export function contentGraphErrorMeaning(
  classification: string,
  operation: string,
): ProductErrorMeaning | null {
  if (classification !== "content-graph-database-busy"
    && classification !== "content-graph-storage-unavailable"
    && classification !== "content-graph-structure-recovered"
    && classification !== "content-graph-change-too-large"
    && classification !== "content-graph-reference-invalid") {
    return null;
  }
  if (classification === "content-graph-reference-invalid") {
    return {
      cause: local({ key: "meanings.linkCannotBeKeptHere" }),
      effect: local({ key: "meanings.pageKeepsWhatItHadBefore" }),
      known: true,
      nextAction: local({ key: "meanings.chooseAnotherPageOrRefresh" }),
      operation,
      retry: local({ key: "meanings.sameLinkIsRefusedAgain" }),
      retryDisposition: "safe-after-action",
    };
  }
  // One change carries at most 1 MB, the size every open copy of the page
  // takes at a time; a bigger one is made in parts.
  if (classification === "content-graph-change-too-large") {
    return {
      cause: local({ key: "meanings.changeTooLargeForOneChange" }),
      effect: local({ key: "meanings.pageKeepsWhatItHadBefore" }),
      known: true,
      nextAction: local({ key: "meanings.makeTheChangeInParts" }),
      operation,
      retry: local({ key: "meanings.sameSizeIsRefusedAgain" }),
      retryDisposition: "safe-after-action",
    };
  }
  if (classification === "content-graph-structure-recovered") {
    return {
      cause: local({ key: "meanings.serverDetectedAndIsolatedIncompleteContent" }),
      effect: local({ key: "meanings.isolatedRecordNotAppliedYourRequested" }),
      known: true,
      nextAction: local({ key: "meanings.retrySameContentChangeNow" }),
      operation,
      retry: local({ key: "meanings.lastConfirmedSavedStateRestoredSo" }),
      retryDisposition: "safe-after-action",
    };
  }
  const effect = local({ key: "meanings.changeSaveReceiptAndSynchronizationState" });
  if (classification === "content-graph-database-busy") {
    return {
      cause: local({ key: "meanings.anotherServerOperationTemporarilyLockedPage" }),
      effect,
      known: true,
      nextAction: local({ key: "meanings.retrySameContentChangeAfterOther" }),
      operation,
      retry: local({ key: "meanings.safeRetrySameContentChangeAfter" }),
      retryDisposition: "safe-after-wait",
    };
  }
  return {
    cause: local({ key: "meanings.serverPageContentStorageTemporarilyUnavailable" }),
    effect,
    known: true,
    nextAction: local({ key: "meanings.retrySameContentChangeAfterServer" }),
    operation,
    retry: local({ key: "meanings.safeRetrySameContentChangeAfter2" }),
    retryDisposition: "safe-after-action",
  };
}
