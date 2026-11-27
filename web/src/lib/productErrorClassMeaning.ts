import type { FailureClass } from "@/lib/productErrorPresentation";
import { productErrorText } from "@/lib/productErrorText";
import type {
  ProductErrorText as LocalizedText,
  ProductErrorContextSpec,
} from "@/lib/productErrorContextCatalog";
import {
  contentGraphErrorMeaning,
  type ProductErrorMeaning,
  type ProductErrorRetryDisposition,
} from "@/lib/productErrorMeaningCatalog";
import {
  persistentWriteStorageErrorMeaning,
  productErrorStorageMeaning,
  type ProductErrorRecovery,
} from "@/lib/productErrorStorageMeaning";
import { moveRefusalMeaning } from "@/lib/productErrorMoveMeaning";
import { optionRefusalMeaning } from "@/lib/productErrorOptionMeaning";
import { thisWindowKey } from "@/lib/thisWindowWords";

// Each failure class's public meaning — its cause, effect, next action and
// whether trying again helps — composed from the shared catalog for the
// operation that failed. productErrorPresentation classifies a failure into
// one FailureClass and says it with this meaning.

function local(text: LocalizedText): string {
  return productErrorText(text.key);
}

type PublicMeaning = ProductErrorMeaning;

export function meaningFor(
  classification: FailureClass,
  spec: ProductErrorContextSpec,
  recovery: ProductErrorRecovery = {},
): PublicMeaning {
  const operation = local(spec.operation);
  const deniedEffect = local(spec.deniedEffect);
  const uncertainEffect = local(spec.uncertainEffect);
  const stateCheckAction = local(spec.stateCheckAction);
  // A read, or a request as harmless to repeat (a code for another device),
  // that got no answer or a fault is tried again: nothing of it is stale to
  // reload (2026-11-05: a code request said to reload before changing).
  const failedAction: LocalizedText | string = spec.transportRetry === "safe-read"
    ? { key: "meanings.waitMomentBeforeTryingAgain" }
    : stateCheckAction;
  const result = (
    cause: LocalizedText,
    effect: string,
    nextAction: LocalizedText | string,
    retryDisposition: ProductErrorRetryDisposition,
    retry: LocalizedText,
    known = true,
  ): PublicMeaning => ({
    cause: local(cause),
    effect,
    known,
    nextAction: typeof nextAction === "string" ? nextAction : local(nextAction),
    operation,
    retry: local(retry),
    retryDisposition,
  });

  const contentGraphMeaning = contentGraphErrorMeaning(classification, operation);
  if (contentGraphMeaning) return contentGraphMeaning;

  const persistentWriteMeaning = persistentWriteStorageErrorMeaning(classification, spec);
  if (persistentWriteMeaning) return persistentWriteMeaning;
  const storageMeaning = productErrorStorageMeaning(classification, spec, recovery);
  if (storageMeaning) return storageMeaning;

  if (classification === "organization-dlp-blocked") return result(
    { key: "meanings.organizationDlpBlocksOperation" },
    deniedEffect,
    { key: "meanings.takeOutTermOrAskAboutDlp" },
    "safe-after-action",
    { key: "meanings.retryOnlyAfterOrganizationSettingChanges" },
  );
  if (classification === "workspace-import-file-refused") return result(
    { key: "meanings.importFileDoesNotFitFormat" },
    deniedEffect,
    { key: "meanings.exportFileAgainToImport" },
    "safe-after-action",
    { key: "meanings.sameFileRefusedAgain" },
  );
  if (classification === "workspace-import-target-not-fresh") return result(
    { key: "meanings.workspaceAlreadyHasContentForArchive" },
    deniedEffect,
    { key: "meanings.makeNewWorkspaceToImportArchive" },
    "safe-after-action",
    { key: "meanings.retryInNewWorkspace" },
  );
  if (classification === "workspace-import-page-in-trash") return result(
    { key: "meanings.importPageInTrash" },
    deniedEffect,
    { key: "meanings.restorePageThenImportAgain" },
    "safe-after-action",
    { key: "meanings.importAgainOnceRestored" },
  );
  if (classification === "page-in-trash") return result(
    { key: "meanings.pageInTrashCantChange" },
    deniedEffect,
    { key: "meanings.restorePageThenTryAgain" },
    "safe-after-action",
    { key: "meanings.tryAgainOnceRestored" },
  );
  if (classification === "workspace-import-page-deleted") return result(
    { key: "meanings.importPageDeletedForGood" },
    deniedEffect,
    { key: "meanings.ownerBringsDeletedPageBack" },
    "safe-after-action",
    { key: "meanings.importAgainOnceRestored" },
  );
  if (classification === "organization-policy-blocked") return result(
    { key: "meanings.organizationSettingsDoNotAllowOperation" },
    deniedEffect,
    { key: "meanings.askOrganizationAdministratorToAllow" },
    "safe-after-action",
    { key: "meanings.retryOnlyAfterOrganizationSettingChanges" },
  );
  if (classification === "invalid-credentials") return result(
    { key: "meanings.emailOrPasswordDidNotMatch" },
    local({ key: "meanings.youNotSignedInAndNo" }),
    { key: "meanings.checkEmailAndPassword" },
    "safe-after-action",
    { key: "meanings.safeTryAgainAfterCorrectingDetails" },
  );
  if (classification === "current-password-invalid") return result(
    { key: "correctable.currentPassword" },
    deniedEffect,
    { key: "correctable.currentPasswordNext" },
    "safe-after-action",
    { key: "meanings.safeTryAgainAfterCorrectingDetails" },
  );
  if (classification === "account-deactivated") return result(
    { key: "correctable.accountDeactivated" },
    deniedEffect,
    { key: "correctable.accountDeactivatedNext" },
    "safe-after-action",
    { key: "meanings.safeTryAgainAfterCorrectingDetails" },
  );
  // A two-step request that timed out or took too many wrong codes: the
  // sign-in starts again (2026-10-28: it read as a five-line diagnosis).
  if (classification === "sign-in-request-ended") return result(
    { key: "correctable.signInEnded" },
    deniedEffect,
    { key: "correctable.signInEndedNext" },
    "safe-after-action",
    { key: "meanings.safeTryAgainAfterCorrectingDetails" },
  );
  if (classification === "email-not-verified") return result(
    { key: "correctable.emailNotVerified" },
    deniedEffect,
    { key: "correctable.emailNotVerifiedNext" },
    "safe-after-action",
    { key: "meanings.safeTryAgainAfterCorrectingDetails" },
  );
  if (classification === "registration-failed") return result(
    { key: "correctable.registrationFailed" },
    uncertainEffect,
    { key: "correctable.registrationFailedNext" },
    "state-check-required",
    { key: "meanings.currentStateMustCheckedBeforeRetry" },
  );
  if (classification === "mfa-code-invalid") return result(
    { key: "correctable.code" },
    deniedEffect,
    { key: "correctable.codeNext" },
    "safe-after-action",
    { key: "meanings.safeTryAgainAfterCorrectingDetails" },
  );
  if (classification === "setup-code-invalid") return result(
    { key: "correctable.setupCode" },
    deniedEffect,
    { key: "correctable.setupCodeNext" },
    "safe-after-action",
    { key: "meanings.safeTryAgainAfterCorrectingDetails" },
  );
  // No code at all is asked for, as the setup's other steps ask it, not said wrong (2026-11-25).
  if (classification === "setup-code-required") return result(
    { key: "correctable.setupCodeMissing" },
    deniedEffect,
    { key: "correctable.setupCodeMissingNext" },
    "safe-after-action",
    { key: "meanings.safeTryAgainAfterCorrectingDetails" },
  );
  if (classification === "organization-sso-required") return result(
    { key: "correctable.organizationSSO" },
    deniedEffect,
    { key: "correctable.organizationSSONext" },
    "safe-after-action",
    { key: "meanings.safeTryAgainAfterCorrectingDetails" },
  );
  if (classification === "organization-password-two-step") return result(
    { key: "correctable.organizationPasswordTwoStep" },
    deniedEffect,
    { key: "correctable.organizationPasswordTwoStepNext" },
    "safe-after-action",
    { key: "meanings.safeTryAgainAfterCorrectingDetails" },
  );
  if (classification === "device-code-invalid") return result(
    { key: "correctable.deviceCode" },
    deniedEffect,
    { key: "correctable.deviceCodeNext" },
    "safe-after-action",
    { key: "meanings.safeTryAgainAfterCorrectingDetails" },
  );
  if (classification === "recovery-code-invalid") return result(
    { key: "correctable.recoveryCode" },
    deniedEffect,
    { key: "correctable.recoveryCodeNext" },
    "safe-after-action",
    { key: "meanings.safeTryAgainAfterCorrectingDetails" },
  );
  if (classification === "auth-session-request-id-reused") return result(
    { key: "meanings.signInAttemptIdentifierAlreadyBelongs" },
    local({ key: "meanings.noNewSignInSessionCreated" }),
    { key: "meanings.reloadSignInPageAndStart" },
    "safe-after-action",
    { key: "meanings.doNotKeepRetryingIdentifierNew" },
  );
  if (classification === "auth-session-capacity") return result(
    { key: "correctable.sessionCapacity" },
    deniedEffect,
    { key: "correctable.sessionCapacityNext" },
    "safe-after-action",
    { key: "meanings.safeTryAgainAfterCorrectingDetails" },
  );
  if (classification === "account-exists") return result(
    { key: "meanings.accountAlreadyExistsForEmailAddress" },
    deniedEffect,
    { key: "meanings.returnSignInWithExistingAccount" },
    "safe-after-action",
    { key: "meanings.safeSignInInsteadCreatingAnother" },
  );
  if (classification === "rate-limited") return result(
    { key: "meanings.tooManyRequestsMadeSoOperation" },
    deniedEffect,
    { key: "meanings.waitMomentBeforeTryingAgain" },
    "safe-after-wait",
    { key: "meanings.safeTryAgainAfterWait" },
  );
  if (classification === "server-moved") return result(
    { key: "meanings.serverDataMovedElsewhere" },
    deniedEffect,
    { key: "meanings.doItOnNewServer" },
    "safe-after-action",
    { key: "meanings.serverDataMovedNoRetry" },
  );
  if (classification === "server-paused") return result(
    { key: "meanings.serverTakesNoChangesNow" },
    deniedEffect,
    { key: "meanings.waitMomentBeforeTryingAgain" },
    "safe-after-wait",
    { key: "meanings.safeTryAgainAfterWait" },
  );
  if (classification === "resource-unavailable") return result(
    { key: "meanings.serverCannotCurrentlyConfirmResourceState" },
    deniedEffect,
    { key: "meanings.waitMomentBeforeTryingAgain" },
    "safe-after-wait",
    { key: "meanings.safeRetryAfterServerResourceChecks" },
  );
  if (classification === "saml-account-creation-off") return result(
    { key: "meanings.samlAccountCreationOff" },
    deniedEffect,
    { key: "meanings.samlAccountCreationOffNext" },
    "safe-after-action",
    { key: "meanings.samlRetryAfterInvite" },
  );
  if (classification === "saml-identity-denied") return result(
    { key: "meanings.samlIdentityDenied" },
    deniedEffect,
    { key: "meanings.samlIdentityDeniedNext" },
    "safe-after-action",
    { key: "meanings.samlRetryAfterAdministrator" },
  );
  if (classification === "saml-response-invalid") return result(
    { key: "meanings.samlResponseInvalid" },
    deniedEffect,
    { key: "meanings.samlResponseInvalidNext" },
    "safe-after-action",
    { key: "meanings.samlRetryAfterAdministrator" },
  );
  if (classification === "saml-dependency-unavailable") return result(
    { key: "meanings.dnsVerificationServiceTemporarilyUnavailable" },
    deniedEffect,
    { key: "meanings.retrySameDomainVerificationAfterDns" },
    "safe-after-action",
    { key: "meanings.safeRetrySameDomainVerificationAfter" },
  );
  if (classification === "provider-unavailable") return result(
    { key: "meanings.externalSignInProviderCouldNot" },
    deniedEffect,
    { key: "meanings.useAnotherSignInMethodOr" },
    "safe-after-wait",
    { key: "meanings.safeStartNewProviderSignIn" },
  );
  if (classification === "email-delivery") return result(
    { key: "meanings.accountEmailCouldNotAdmittedFor" },
    deniedEffect,
    { key: "meanings.askServerAdministratorCheckEmailDelivery" },
    "safe-after-action",
    { key: "meanings.safeRetryAfterEmailDeliveryRestored" },
  );
  if (classification === "expired-auth") return result(
    { key: "meanings.signInOrAccountActionLink" },
    deniedEffect,
    { key: "meanings.startAccountActionAgainGetNew" },
    "safe-after-action",
    { key: "meanings.doNotReuseLinkNewAction" },
  );
  if (classification === "signed-out") return result(
    // "이 브라우저의 로그인" or, in Leafen's own app window, "이 앱의 로그인".
    { key: thisWindowKey("meanings.signInEndedForThisBrowser", "meanings.signInEndedForThisApp") },
    deniedEffect,
    { key: "meanings.signInAgainToContinue" },
    "safe-after-action",
    { key: "meanings.retryAfterSigningInAgain" },
  );
  if (classification === "denied") return result(
    { key: "meanings.currentAccountNotAuthorizedForOperation" },
    deniedEffect,
    { key: "meanings.signInWithAuthorizedAccountOr" },
    "safe-after-action",
    { key: "meanings.retryOnlyAfterAccountOrPermission" },
  );
  if (classification === "invalid-request") return result(
    { key: "meanings.submittedInformationDidNotMeetOperation" },
    deniedEffect,
    { key: "meanings.reviewEnteredValuesAndCorrectHighlighted" },
    "safe-after-action",
    { key: "meanings.safeTryAgainAfterCorrectingInput" },
  );
  if (classification === "not-found") return result(
    { key: "meanings.requestedItemNoLongerExistsOr" },
    deniedEffect,
    stateCheckAction,
    "safe-after-action",
    { key: "meanings.checkCurrentListBeforeTryingDifferent" },
  );
  const moveMeaning = moveRefusalMeaning(classification, operation, deniedEffect)
    ?? optionRefusalMeaning(classification, operation, deniedEffect);
  if (moveMeaning) return moveMeaning;
  if (classification === "page-locked") return result(
    { key: "meanings.pageIsLocked" },
    deniedEffect,
    { key: "meanings.unlockPageThenTryAgain" },
    "safe-after-action",
    { key: "meanings.safeToRetryAfterUnlocking" },
  );
  if (classification === "page-busy") return result(
    { key: "meanings.pageBusyWithAnotherChange" },
    deniedEffect,
    { key: "meanings.tryAgainOnceThatChangeIsDone" },
    "safe-after-wait",
    { key: "meanings.safeTryAgainAfterWait" },
  );
  // A database's lock tells whoever can't unlock it who can, as Notion shows
  // them no lock at all (the server's lock_refusal.go).
  if (classification === "database-locked" || classification === "database-locked-not-unlockable") return result(
    { key: "meanings.databaseIsLocked" },
    deniedEffect,
    { key: classification === "database-locked" ? "meanings.unlockDatabaseThenTryAgain" : "meanings.databaseEditorsCanUnlock" },
    "safe-after-action",
    { key: "meanings.safeToRetryAfterDatabaseUnlocked" },
  );
  if (classification === "server-restored") return result(
    { key: "meanings.serverRestoredSinceDeviceCopies" },
    local({ key: "meanings.changeKeptOnThisDeviceOnly" }),
    { key: "meanings.takeServerContentAnew" },
    "safe-after-action",
    { key: "meanings.retryAfterTakingContentAnew" },
  );
  // A title stays, and a deleted property changes once restored; the reader
  // is told so rather than of a conflict.
  if (classification === "property-in-use") return result(
    { key: "meanings.propertyUsedByViewOrDependency" },
    deniedEffect,
    { key: "meanings.changeWhatUsesPropertyFirst" },
    "safe-after-action",
    { key: "meanings.retryAfterChangingWhatUsesProperty" },
  );
  if (classification === "conflict") return result(
    { key: "meanings.newerOrIncompatibleStatePreventedOperation" },
    deniedEffect,
    stateCheckAction,
    "state-check-required",
    { key: "meanings.reloadCurrentStateBeforeDecidingWhether" },
  );
  if (classification === "transport-unavailable") return result(
    { key: "meanings.leafenDidNotReceiveServerResponse" },
    uncertainEffect,
    failedAction,
    spec.transportRetry === "safe-read" ? "safe-after-action" : "state-check-required",
    spec.transportRetry === "safe-read"
      ? { key: "meanings.safeRetryRead" }
      : { key: "meanings.currentStateMustCheckedBeforeRetry" },
  );
  if (classification === "service-failure") return result(
    { key: "meanings.leafenServiceCouldNotCompleteOperation" },
    uncertainEffect,
    failedAction,
    "state-check-required",
    { key: "meanings.currentStateMustCheckedBeforeRetrying" },
  );
  return result(
    { key: "meanings.causeCouldNotDetermined" },
    uncertainEffect,
    failedAction,
    "state-check-required",
    { key: "meanings.notKnownWhetherRetryingSafeUntil" },
    false,
  );
}
