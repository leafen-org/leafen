import { administratorErrorCodes } from "@/administratorWire.generated";
import { authErrorCodes } from "@/authWire.generated";
import { isSignedOutResponse } from "@/lib/signedOutResponse";
import { graphWireErrorCodes } from "@/contentGraphWire.generated";
import { databaseDataSourceErrorCodes } from "@/databaseDataSourceWire.generated";
import { enterpriseDLPErrorCodes } from "@/enterpriseDLPWire.generated";
import { enterpriseSAMLErrorCodes } from "@/enterpriseSAMLWire.generated";
import { fileLifecycleErrorCodes } from "@/fileLifecycleWire.generated";
import { fileTransferErrorCodes } from "@/fileTransferWire.generated";
import { formErrorCodes } from "@/formWire.generated";
import { organizationErrorCodes } from "@/organizationWire.generated";
import { workspaceExportErrorCodes } from "@/workspaceExportWire.generated";
import { workspaceImportErrorCodes } from "@/workspaceImportWire.generated";
import { workspaceMembershipErrorCodes } from "@/workspaceMembershipWire.generated";
import { isDiagnosticReference, messageWithDiagnosticReference } from "@/lib/diagnosticTrace";
import { productErrorText } from "@/lib/productErrorText";
import { limitWithWait } from "@/lib/retryWait";
import { settingsNoAnswerText } from "@/lib/settingsErrors";
import {
  PRODUCT_ERROR_CONTEXTS,
  type ProductErrorContext as ProductErrorContextKey,
  type ProductErrorText as LocalizedText,
} from "@/lib/productErrorContextCatalog";
import { administratorErrorMeaning, type ProductErrorRetryDisposition } from "@/lib/productErrorMeaningCatalog";
import { withServerDiskWords } from "@/lib/serverDiskRefusal";
import {
  persistentWriteStorageFailureClass,
  type ProductErrorRecovery,
} from "@/lib/productErrorStorageMeaning";
import { moveRefusalClass, type MoveRefusalClass } from "@/lib/productErrorMoveMeaning";
import { optionRefusalClass, type OptionRefusalClass } from "@/lib/productErrorOptionMeaning";
import { meaningFor } from "@/lib/productErrorClassMeaning";
import { isPageBusyRefusal } from "@/lib/pageBusy";
import { logProductError } from "@/lib/safeErrorLog";

export type ProductErrorContext = ProductErrorContextKey;
export type { ProductErrorRetryDisposition } from "@/lib/productErrorMeaningCatalog";
export type ProductErrorPresentation = {
  cause: string;
  effect: string;
  known: boolean;
  message: string;
  nextAction: string;
  operation: string;
  reference: string;
  /** The screen shows the reference, under what happened: only for a failure no one foresaw (presentationWithReference). */
  referenceShown: boolean;
  referenceLabel: string;
  retry: string;
  retryDisposition: ProductErrorRetryDisposition;
};

// The classes a failure is told by: failureClass picks one, and
// productErrorClassMeaning says each.
export type FailureClass =
  | "account-exists"
  // The account is signed in on as many devices as it may be.
  | "auth-session-capacity"
  | "auth-database-busy"
  | "auth-session-request-id-reused"
  | "auth-storage-unavailable"
  | "conflict"
  | "current-password-invalid"
  | "property-in-use"
  | "server-restored"
  | "page-locked"
  // An operation holds the page for a while (lib/pageBusy.ts): it takes edits again once done.
  | "page-busy"
  // A database's own lock (the server's lock_refusal.go): the reader may
  // unlock it, or only someone who can edit the database may.
  | "database-locked"
  | "database-locked-not-unlockable"
  | MoveRefusalClass | OptionRefusalClass
  | "content-graph-database-busy"
  | "content-graph-structure-recovered"
  | "content-graph-change-too-large"
  | "content-graph-reference-invalid"
  | "content-graph-storage-unavailable"
  | "denied"
  | "dlp-database-busy"
  | "dlp-storage-unavailable"
  | "email-delivery"
  | "expired-auth"
  | "signed-out"
  | "file-database-busy"
  | "file-resumable-required"
  | "file-storage-unavailable"
  | "form-database-busy"
  | "form-storage-unavailable"
  | "invalid-credentials"
  | "account-deactivated"
  | "mfa-code-invalid"
  | "sign-in-request-ended"
  | "email-not-verified"
  | "registration-failed"
  | "device-code-invalid"
  // The person's organization signs them in another way than a device code.
  | "organization-sso-required"
  | "organization-password-two-step"
  | "setup-code-invalid" | "setup-code-required"
  | "recovery-code-invalid"
  | "invalid-request"
  | "not-found"
  | "organization-policy-blocked"
  | "organization-dlp-blocked"
  | "organization-storage-limit-exceeded"
  | "organization-storage-reconciliation-pending"
  | "organization-storage-state-invalid"
  // The server's disk is under the room it keeps (data_disk_floor.go), whatever was refused.
  | "server-disk-low"
  | "persistent-write-database-busy"
  | "persistent-write-storage-unavailable"
  | "provider-unavailable"
  | "rate-limited"
  | "resource-unavailable"
  | "saml-account-creation-off"
  | "saml-dependency-unavailable"
  | "saml-identity-denied"
  | "saml-response-invalid"
  | "server-moved"
  | "server-paused"
  | "service-failure"
  | "source-changed"
  | "transport-unavailable"
  | "workspace-export-database-busy"
  | "workspace-import-resumable-required"
  | "workspace-import-target-not-fresh"
  | "workspace-import-page-in-trash"
  | "workspace-import-page-deleted"
  // A page in the trash, or under one, outside an import.
  | "page-in-trash"
  | "workspace-import-file-refused"
  | "workspace-export-storage-unavailable"
  | "unknown";

export type { ProductErrorRecovery } from "@/lib/productErrorStorageMeaning";

const SAML_RESPONSE_ERROR_CODES: ReadonlySet<string> = new Set([
  enterpriseSAMLErrorCodes.responseInvalid, enterpriseSAMLErrorCodes.requestMismatch,
  enterpriseSAMLErrorCodes.weakAlgorithm, enterpriseSAMLErrorCodes.responseTooLarge,
  enterpriseSAMLErrorCodes.xmlTooComplex, enterpriseSAMLErrorCodes.attributesTooLarge,
  enterpriseSAMLErrorCodes.assertionCardinalityInvalid, enterpriseSAMLErrorCodes.assertionReplayed,
]);

const PROVIDER_ERROR_CODES = new Set<string>([
  authErrorCodes.providerNotFound,
  authErrorCodes.providerUnavailable,
  authErrorCodes.providerResponseInvalid,
  authErrorCodes.providerStateInvalid,
  authErrorCodes.providerClaimsInvalid,
  authErrorCodes.providerExchangeInvalid,
  authErrorCodes.providerExchangeConsumed,
  authErrorCodes.providerAccountFailed,
]);

const EXPIRED_AUTH_CODES = new Set<string>([
  authErrorCodes.authTokenInvalid,
  authErrorCodes.authTokenConsumed,
  authErrorCodes.emailVerificationFailed,
  authErrorCodes.passwordResetFailed,
]);

function local(text: LocalizedText): string {
  return productErrorText(text.key);
}

function safeProperty(error: unknown, property: string): unknown {
  if ((typeof error !== "object" || error === null) && typeof error !== "function") {
    return undefined;
  }
  try {
    return Reflect.get(error, property);
  } catch {
    return undefined;
  }
}

function errorIdentity(error: unknown) {
  const candidateCode = safeProperty(error, "code");
  const candidateStatus = safeProperty(error, "status");
  return {
    code: typeof candidateCode === "string" && /^(?:LEAFEN|ORGANIZATION)_[A-Z0-9_]{1,58}$/.test(candidateCode)
      ? candidateCode
      : "",
    reference: safeProperty(error, "reference"),
    status: typeof candidateStatus === "number" && Number.isInteger(candidateStatus)
      && candidateStatus >= 400 && candidateStatus <= 599
      ? candidateStatus
      : null,
  };
}

function failureClass(
  code: string,
  status: number | null,
  contextKey: ProductErrorContext,
): FailureClass {
  const persistentWriteClass = persistentWriteStorageFailureClass(code, contextKey);
  if (persistentWriteClass) return persistentWriteClass;
  if (code === databaseDataSourceErrorCodes.propertyInUse) return "property-in-use";
  if (code === workspaceExportErrorCodes.sourceChanged) return "source-changed";
  if (code === workspaceExportErrorCodes.databaseBusy) {
    return "workspace-export-database-busy";
  }
  if (code === workspaceExportErrorCodes.storageUnavailable) {
    return "workspace-export-storage-unavailable";
  }
  if (code === workspaceImportErrorCodes.targetNotFresh) return "workspace-import-target-not-fresh";
  // The page an import adds to went to the trash or was deleted for good
  // (the server's workspace_import_target_gone.go).
  if (contextKey.startsWith("workspaceImport.") && code === "LEAFEN_PAGE_IN_TRASH") return "workspace-import-page-in-trash";
  if (contextKey.startsWith("workspaceImport.") && code === "LEAFEN_PAGE_NOT_FOUND") return "workspace-import-page-deleted";
  // A page in the trash, or under one, takes no change, new page or copy
  // until it is restored (the server's pageInTrashError): said so, not as a
  // conflict to reload.
  if (code === "LEAFEN_PAGE_IN_TRASH") return "page-in-trash";
  // A file an import can't read is the file's to fix, not the service's.
  if (code === workspaceImportErrorCodes.invalid && contextKey === "workspaceImport.job") return "workspace-import-file-refused";
  if (code === workspaceImportErrorCodes.archiveResumableRequired
    && contextKey === "workspaceImport.start") {
    return "workspace-import-resumable-required";
  }
  if (code === graphWireErrorCodes.databaseBusy
    && contextKey === "contentGraph.update") {
    return "content-graph-database-busy";
  }
  if (code === graphWireErrorCodes.storageUnavailable
    && contextKey === "contentGraph.update") {
    return "content-graph-storage-unavailable";
  }
  if (code === graphWireErrorCodes.structureRecovered
    && contextKey === "contentGraph.update") {
    return "content-graph-structure-recovered";
  }
  if (code === graphWireErrorCodes.changeTooLarge) return "content-graph-change-too-large";
  // A link the server won't keep says so, not "the input" (2026-11-03: a
  // dependency on itself showed only the general words).
  if (code === graphWireErrorCodes.referenceInvalid && contextKey === "contentGraph.update") return "content-graph-reference-invalid";
  if (code === formErrorCodes.databaseBusy
    && contextKey.startsWith("publicForm.")) {
    return "form-database-busy";
  }
  if (code === formErrorCodes.storageUnavailable
    && contextKey.startsWith("publicForm.")) {
    return "form-storage-unavailable";
  }
  if (code === authErrorCodes.invalidCredentials) {
    return contextKey === "auth.signIn" ? "invalid-credentials" : "current-password-invalid";
  }
  // An administrator turned the account off: said as a sentence, not the
  // server's English (2026-10-19: "This account is deactivated." in Korean).
  if (code === administratorErrorCodes.administratorAccountDisabled) return "account-deactivated";
  if (code === authErrorCodes.mfaCodeInvalid) return "mfa-code-invalid";
  if (code === authErrorCodes.mfaChallengeInvalid) return "sign-in-request-ended";
  // Said as what happened, not as a missing permission or a changed state.
  if (code === authErrorCodes.emailVerificationRequired) return "email-not-verified";
  if (code === authErrorCodes.registrationFailed) return "registration-failed";
  if (code === authErrorCodes.deviceCodeInvalid) return "device-code-invalid";
  if (code === enterpriseSAMLErrorCodes.required && (contextKey.startsWith("auth.") || contextKey.startsWith("accountSecurity."))) {
    return "organization-sso-required";
  }
  if (code === authErrorCodes.deviceSignInRestricted) return "organization-password-two-step";
  if (code === administratorErrorCodes.setupCodeInvalid || code === administratorErrorCodes.setupCodeRequired) return code === administratorErrorCodes.setupCodeRequired ? "setup-code-required" : "setup-code-invalid";
  if (code === authErrorCodes.mfaRecoveryCodeInvalid) return "recovery-code-invalid";
  if (code === authErrorCodes.authStorageUnavailable) return "auth-storage-unavailable";
  if (code === authErrorCodes.authDatabaseBusy) return "auth-database-busy";
  if (code === authErrorCodes.sessionRequestIdReused) return "auth-session-request-id-reused";
  if (code === authErrorCodes.accountExists) return "account-exists";
  if (code === authErrorCodes.sessionCapacity) return "auth-session-capacity";
  if (code === "LEAFEN_RATE_LIMITED" || code === "LEAFEN_SERVER_BUSY" || status === 429) return "rate-limited";
  if (code === "LEAFEN_MEMORY_SENSOR_UNAVAILABLE") return "resource-unavailable";
  if (code === administratorErrorCodes.serverMoved) return "server-moved";
  if (code === administratorErrorCodes.serverPaused) return "server-paused";
  if (code === enterpriseDLPErrorCodes.databaseBusy
    && contextKey === "enterpriseDlp.policyUpdate") {
    return "dlp-database-busy";
  }
  if (code === enterpriseDLPErrorCodes.storageUnavailable
    && contextKey === "enterpriseDlp.policyUpdate") {
    return "dlp-storage-unavailable";
  }
  // A setting of the organization — its sharing security or DLP policy —
  // refuses the operation, whoever asks.
  if (code === enterpriseDLPErrorCodes.policyBlocked) return "organization-dlp-blocked";
  if (code === organizationErrorCodes.sharingSecurityBlocked) {
    return "organization-policy-blocked";
  }
  if (code === organizationErrorCodes.storageLimitExceeded) {
    return "organization-storage-limit-exceeded";
  }
  if (code === organizationErrorCodes.storageReconciliationPending) {
    return "organization-storage-reconciliation-pending";
  }
  if (code === organizationErrorCodes.storageStateInvalid) {
    return "organization-storage-state-invalid";
  }
  if (code === fileTransferErrorCodes.serverDiskLow) return "server-disk-low";
  if ((code === fileTransferErrorCodes.uploadDatabaseBusy
    || code === fileLifecycleErrorCodes.deleteDatabaseBusy)
    && contextKey.startsWith("file.")) {
    return "file-database-busy";
  }
  if ((code === fileTransferErrorCodes.uploadStorageUnavailable
    || code === fileLifecycleErrorCodes.deleteStorageUnavailable)
    && contextKey.startsWith("file.")) {
    return "file-storage-unavailable";
  }
  if (code === fileTransferErrorCodes.uploadResumableRequired
    && contextKey === "file.upload") {
    return "file-resumable-required";
  }
  if (code === enterpriseSAMLErrorCodes.identityDenied) return "saml-identity-denied";
  if (code === enterpriseSAMLErrorCodes.accountCreationOff) return "saml-account-creation-off";
  if (SAML_RESPONSE_ERROR_CODES.has(code)) return "saml-response-invalid";
  if (code === enterpriseSAMLErrorCodes.unavailable
    && contextKey === "enterpriseSaml.domainVerification") {
    return "saml-dependency-unavailable";
  }
  if (PROVIDER_ERROR_CODES.has(code)) return "provider-unavailable";
  if (code === authErrorCodes.emailDeliveryFailed) return "email-delivery";
  if (EXPIRED_AUTH_CODES.has(code)) return "expired-auth";
  if (isSignedOutResponse(status, code)) return "signed-out";
  const moveRefusal = moveRefusalClass(code) ?? optionRefusalClass(code);
  if (moveRefusal) return moveRefusal;
  // An import refused at its page ends with the refusal's code alone, its
  // status the job's (workspace_import_errors.go).
  if (code === "LEAFEN_UNAUTHORIZED" || code === "LEAFEN_FORBIDDEN" || code === "LEAFEN_PERMISSION_DENIED"
    || status === 401 || status === 403) return "denied";
  if (code === "LEAFEN_TRANSPORT_UNAVAILABLE") return "transport-unavailable";
  if (status === 400 || status === 422) return "invalid-request";
  if (status === 404) return "not-found";
  if (code === "LEAFEN_DATABASE_LOCKED") return "database-locked";
  if (code === "LEAFEN_DATABASE_LOCKED_NOT_UNLOCKABLE") return "database-locked-not-unlockable";
  // A 423 is parted by its code in one place (isPageBusyRefusal): any other is a lock's.
  if (isPageBusyRefusal({ code, status })) return "page-busy";
  if (code === "LEAFEN_PAGE_LOCKED" || status === 423) return "page-locked";
  if (code === workspaceMembershipErrorCodes.dataEpochChanged) return "server-restored";
  if (status === 409) return "conflict";
  if (status !== null && status >= 500) return "service-failure";
  return "unknown";
}

// What someone can put right themselves reads as a sentence or two, not as a
// diagnosis with a reference (2026-10-16: a mistyped code read "현재 계정에는
// 이 작업을 수행할 권한이 없습니다", a wrong current password as a failed
// sign-in, a used reset link and a wrong password as a five-line diagnosis).
// A setting of the organization says so and whom to ask (2026-11-04: a blocked
// export told them to correct what they had entered), as does a server
// whose disk is almost full (2026-11-21: an import refused for it read as a
// service failure, with a reference).
const CORRECTABLE = new Set<FailureClass>([
  "organization-policy-blocked", "organization-dlp-blocked", "workspace-import-file-refused", "workspace-import-target-not-fresh",
  "workspace-import-page-in-trash", "workspace-import-page-deleted", "page-in-trash", "server-disk-low",
  "invalid-credentials", "current-password-invalid", "mfa-code-invalid", "recovery-code-invalid", "expired-auth", "account-deactivated", "sign-in-request-ended", "email-not-verified",
  "device-code-invalid", "setup-code-invalid", "setup-code-required", "organization-sso-required", "organization-password-two-step", "auth-session-capacity",
]);

function correctable(classification: FailureClass, contextKey: ProductErrorContext) {
  return CORRECTABLE.has(classification) || classification === "invalid-request"
    && (contextKey.startsWith("auth.") || contextKey.startsWith("accountSecurity."));
}

// What the settings window's sections do: the account's, the server's, the
// workspace's and the organization's settings.
const SETTINGS_CONTEXT = /^(account|administrator|workspace\.settings|organization|enterprise)/;

function presentationWithReference(
  error: unknown,
  contextKey: ProductErrorContext,
  reference: string,
  recovery: ProductErrorRecovery = {},
): ProductErrorPresentation {
  const identity = errorIdentity(error);
  const spec = PRODUCT_ERROR_CONTEXTS[contextKey];
  // The settings say a request the server didn't answer one way in every
  // section, with nothing to reload by hand (settingsNoAnswerText): a change's
  // is the settings window's to say, and the section shows nothing.
  const noAnswer = SETTINGS_CONTEXT.test(contextKey) ? settingsNoAnswerText(error) : null;
  if (noAnswer !== null) {
    const meaning = meaningFor("transport-unavailable", spec);
    return { ...meaning, message: noAnswer, reference, referenceShown: false, referenceLabel: local({ key: "labels.reference" }) };
  }
  const administrator = administratorErrorMeaning(identity.code, contextKey, {
    deniedEffect: local(spec.deniedEffect),
    operation: local(spec.operation),
    stateCheckAction: local(spec.stateCheckAction),
    uncertainEffect: local(spec.uncertainEffect),
  });
  const classification = failureClass(identity.code, identity.status, contextKey);
  const found = administrator ?? meaningFor(classification, spec, recovery);
  // A limit says the wait its refusal named, not "a moment" (lib/retryWait.ts); the disk, too full or too small,
  // what the server said of it to whoever asked (lib/serverDiskRefusal.ts).
  const limited = classification === "rate-limited" ? limitWithWait(safeProperty(error, "retryAfter")) : null;
  const meaning = limited ? { ...found, cause: limited.cause, nextAction: limited.wait } : withServerDiskWords(found, error);
  // No screen shows the diagnosis: what went wrong and what to do, the
  // reference under it (the sign-in screen 2026-10-28, every screen 2026-11-04)
  // only when no one foresaw the failure — no answer, a server fault, a cause
  // not known. A refusal the server explained reads as its reason alone
  // (2026-11-05: a policy refusal carried "참조: LF-…").
  const plain = `${meaning.cause} ${meaning.nextAction}`;
  const foreseen = (!administrator && correctable(classification, contextKey))
    || (meaning.known && identity.status !== null && identity.status < 500);
  return {
    ...meaning,
    message: foreseen ? plain
      : messageWithDiagnosticReference(plain, reference, local({ key: "labels.reference" })),
    reference,
    referenceShown: !foreseen,
    referenceLabel: local({ key: "labels.reference" }),
  };
}

export function productErrorPresentation(
  error: unknown,
  contextKey: ProductErrorContext,
  recovery: ProductErrorRecovery = {},
): ProductErrorPresentation {
  const spec = PRODUCT_ERROR_CONTEXTS[contextKey];
  const identity = errorIdentity(error);
  const reference = logProductError(spec.level, spec.logOperation, error, {
    ...(identity.code ? { errorCode: identity.code, causeCode: identity.code } : {}),
    ...(identity.status === null ? {} : { status: identity.status }),
    causeStage: contextKey,
  });
  return presentationWithReference(error, contextKey, reference, recovery);
}

/** A failure said again, in the language now, under the reference it was logged with — not logged again (lib/authLine.ts). */
export function productErrorRetold(error: unknown, contextKey: ProductErrorContext, reference: string): string {
  return presentationWithReference(error, contextKey, reference).message;
}

export function productErrorReceiptPresentation(
  error: unknown,
  contextKey: ProductErrorContext,
  recovery: ProductErrorRecovery = {},
): ProductErrorPresentation {
  const identity = errorIdentity(error);
  if (isDiagnosticReference(identity.reference)) {
    return presentationWithReference(error, contextKey, identity.reference, recovery);
  }
  return productErrorPresentation(error, contextKey, recovery);
}

/**
 * What a failure code meant, as its screen said it, without logging it again:
 * a past sign-in's reason in the sign-in history and the audit log (2026-10-26:
 * the histories fell back to the server's code).
 */
export function productErrorCause(code: string, contextKey: ProductErrorContext, status: number | null = null): string {
  const spec = PRODUCT_ERROR_CONTEXTS[contextKey];
  const administrator = administratorErrorMeaning(code, contextKey, {
    deniedEffect: local(spec.deniedEffect),
    operation: local(spec.operation),
    stateCheckAction: local(spec.stateCheckAction),
    uncertainEffect: local(spec.uncertainEffect),
  });
  return (administrator ?? meaningFor(failureClass(code, status, contextKey), spec)).cause;
}

/** The same, or nothing when no cause is known of the code: "cause unknown" tells no one anything. */
export function productErrorKnownCause(code: string, contextKey: ProductErrorContext): string {
  const spec = PRODUCT_ERROR_CONTEXTS[contextKey];
  const meaning = meaningFor(failureClass(code, null, contextKey), spec);
  return meaning.known ? meaning.cause : "";
}

// Refusals whose reason a person acts on: where a screen names its own
// failure, it says these instead (a move's, 2026-11-10).
const REFUSAL_REASONS = new Set<FailureClass>([
  "row-move-denied", "page-locked", "page-busy", "database-locked", "database-locked-not-unlockable", "denied",
  "page-in-trash", "other-workspace", "option-name-comma", "option-name-taken", "options-in-use",
]);

/** Why the server refused, as its screens say it, or "" when it named no reason to act on. */
export function productRefusalReason(error: unknown, contextKey: ProductErrorContext): string {
  const { code, status } = errorIdentity(error);
  const classification = failureClass(code, status, contextKey);
  if (!REFUSAL_REASONS.has(classification)) return "";
  const meaning = meaningFor(classification, PRODUCT_ERROR_CONTEXTS[contextKey]);
  return `${meaning.cause} ${meaning.nextAction}`;
}

const LOCK_REFUSALS = new Set<FailureClass>(["page-locked", "database-locked", "database-locked-not-unlockable"]);

/** Why a page's or a database's lock refused a change, or "" for any other failure: a screen that says its own failure says this instead. */
export function productLockRefusalReason(error: unknown): string {
  const { code, status } = errorIdentity(error);
  return LOCK_REFUSALS.has(failureClass(code, status, "contentGraph.update")) ? productRefusalReason(error, "contentGraph.update") : "";
}

export function productErrorMessage(
  error: unknown,
  contextKey: ProductErrorContext,
): string {
  const { message } = productErrorPresentation(error, contextKey);
  return message;
}
