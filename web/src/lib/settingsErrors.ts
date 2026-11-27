import { i18next } from "@/i18n";
import { logProductError } from "@/lib/safeErrorLog";
import { enterpriseDLPErrorCodes } from "@/enterpriseDLPWire.generated";
import { nativeNotionImportErrorCodes } from "@/notionImportWire.generated";
import { limitWithWait } from "./retryWait";
import { isRefusedForNow } from "./serverBusy";
import { isUncertainWrite } from "./uncertainWrites";
import { isDiagnosticReference, messageWithDiagnosticReference } from "./diagnosticTrace";
import { isSignedOutResponse } from "@/lib/signedOutResponse";
import { isPageBusyRefusal, pageBusyNoticeKey } from "./pageBusy";
import { optionRefusalText } from "./productErrorOptionMeaning";

// Well-known backend error messages (thrown as plain Errors by
// backend/functions/workspace-mutation.ts and collaboration-mutation.ts)
// mapped to a localized-copy key. The map keys are the backend's own English
// wording and are used only for data-matching, so they stay literal.
const KNOWN_BACKEND_ERROR_KEYS: Record<string, string> = {
  "Workspace admin access required.": "workspaceAdminRequired",
  "Workspace owner access required.": "workspaceOwnerRequired",
  "Only workspace owners can assign admin.": "onlyOwnersAssignAdmin",
  "Only workspace owners can manage admins.": "onlyOwnersManageAdmins",
  "Only workspace owners can remove admins.": "onlyOwnersRemoveAdmins",
  "Workspace owners cannot be changed from member management.": "ownersCannotChangeRole",
  "Workspace owners cannot be removed.": "ownersCannotBeRemoved",
  "You cannot change your own workspace role.": "cannotChangeOwnRole",
  "You cannot remove yourself from the workspace.": "cannotRemoveSelf",
  "Only admin, member, and guest roles can be assigned.": "onlyAssignableRoles",
  "Workspace member was not found.": "memberNotFound",
  "Workspace invitation was not found.": "invitationNotFound",
  "This workspace has too many invitations waiting. Cancel some first.": "invitationsFull",
  "Workspace owner transfer target must be another member.": "transferTargetMustBeMember",
  "Email is required.": "emailRequired",
  "Email is invalid.": "emailInvalid",
  "External guest invitations are disabled by organization policy.": "externalGuestDisabled",
  "Verified organization domain is required for organization members.": "verifiedDomainRequired",
  "Workspace URL is already in use.": "workspaceUrlInUse",
};

// Error codes whose reason the reader should see in their own words.
const KNOWN_BACKEND_ERROR_CODES: Record<string, string> = {
  LEAFEN_MCP_CONNECTION_CAPACITY: "mcpConnectionsFull",
  LEAFEN_MCP_CONNECTION_CODE_CAPACITY: "mcpConnectionCodesFull",
  LEAFEN_MCP_CONNECTION_HTTPS_REQUIRED: "mcpConnectionHttpsRequired",
  // A refusal of the organization's DLP policy says so, wherever it comes
  // from (2026-10-16: a refused comment read "try again").
  [enterpriseDLPErrorCodes.policyBlocked]: "dlpPolicyBlocked",
  LEAFEN_ORGANIZATION_STORAGE_LIMIT_EXCEEDED: "organizationStorageLimitExceeded",
  LEAFEN_LEGAL_HOLD_PREVENTS_PERMANENT_DELETE: "legalHoldKeeps",
  // An added person the organization deactivated (2026-11-06: it read "couldn't add").
  LEAFEN_WORKSPACE_MEMBER_DEACTIVATED: "memberDeactivated",
  LEAFEN_ORGANIZATION_STORAGE_RECONCILIATION_PENDING: "organizationStorageRecounting",
  LEAFEN_SERVER_DISK_LOW: "serverDiskLow",
  // Which version, and that another will do (2026-11-04: it read "couldn't change the app").
  LEAFEN_APP_REVOKED: "appVersionRevoked",
  // Someone put the app in the trash while its review stood open.
  LEAFEN_APP_IN_TRASH: "appInTrash",
  LEAFEN_APP_IN_TRASH_ASK_ADMINISTRATOR: "appInTrashAskAdministrator",
  // Which SAML setting to fix (2026-10-16: an http sign-in URL read only "check the settings").
  LEAFEN_SAML_FIELDS_REQUIRED: "samlFieldsRequired",
  LEAFEN_SAML_REQUIRED: "samlRequiredPerson",
  LEAFEN_SAML_LINKED_WORKSPACE_INVALID: "samlLinkedWorkspaceInvalid",
  LEAFEN_SAML_ISSUER_INVALID: "samlIssuerInvalid",
  LEAFEN_SAML_LOGIN_URL_NOT_HTTPS: "samlLoginUrlNotHttps",
  LEAFEN_SAML_CERTIFICATE_INVALID: "samlCertificateInvalid",
  LEAFEN_SAML_ENFORCEMENT_NEEDS_SSO: "samlEnforcementNeedsSso",
  // A change refused for the person's access says so, not that it failed
  // (2026-11-26: a refusal with its cause read "couldn't save the changes").
  LEAFEN_PERMISSION_DENIED: "permissionDenied",
  // A Notion move of what a move already brought says to run that one again,
  // not that it failed (2026-11-26: a second move made every page twice).
  [nativeNotionImportErrorCodes.migrationMovedBefore]: "notionMigrationMovedBefore",
};

function statusCode(error: unknown) {
  const record = error as { status?: unknown; code?: unknown } | null;
  const status = record?.status ?? record?.code;
  return typeof status === "number" ? status : undefined;
}

function messageText(error: unknown) {
  if (error instanceof Error) return error.message;
  if (typeof error === "string") return error;
  const record = error as { message?: unknown } | null;
  return typeof record?.message === "string" ? record.message : "";
}

function isRateLimitError(error: unknown) {
  if (statusCode(error) === 429) return true;
  return /\b429\b|too many requests|rate limit/i.test(messageText(error));
}

/** An error whose message was written for the person reading it, in their language. */
export class LocalizedError extends Error {
  constructor(message: string, options?: ErrorOptions) {
    super(message, options);
    this.name = "LocalizedError";
  }
}

/** Whether the server didn't answer the request at all. */
export function isServerUnreachable(error: unknown) {
  return (error as { code?: unknown } | null)?.code === "LEAFEN_TRANSPORT_UNAVAILABLE";
}

/**
 * A failure as a person reads it: one line in their language. An error's own
 * message is the transport's English with a reference ("Leafen request
 * failed (409).", the reference going to the log where it was made) or a
 * script's, so it shows only when it was written for them (LocalizedError);
 * else the caller's line for what failed.
 */
export function userErrorText(error: unknown, fallback: string) {
  const known = knownErrorText(error);
  if (known !== null) return known;
  const code = (error as { code?: unknown } | null)?.code;
  if (!(typeof code === "string" && code.startsWith("LEAFEN_")) && messageText(error).trim()) {
    logProductError("warn", "settings.unlocalized-backend-error", error);
  }
  return fallback || i18next.t("settingsErrors:requestFailed");
}

/** What the failure was, when it is one known to say in the reader's words. */
function knownErrorText(error: unknown): string | null {
  if (error instanceof LocalizedError) return error.message;
  const noAnswer = settingsNoAnswerText(error);
  if (noAnswer !== null) return noAnswer;
  // Refused for now — busy, short of memory — once asked again long enough: no limit of the reader's (lib/serverBusy.ts).
  if (isRefusedForNow(error)) return i18next.t("common:serverBusy");
  // A page an operation holds for a while says which, and that it takes changes once that's done (the server's page_held_refusal.go).
  if (isPageBusyRefusal(error)) {
    const holder = (error as { causeKey?: unknown }).causeKey;
    return i18next.t(pageBusyNoticeKey(typeof holder === "string" ? holder : "other"));
  }
  if (isRateLimitError(error)) {
    // A limit that named a longer wait says it, not "a moment" (lib/retryWait.ts).
    const limited = limitWithWait((error as { retryAfter?: unknown } | null)?.retryAfter);
    return limited ? `${limited.cause} ${limited.wait}` : i18next.t("settingsErrors:rateLimited");
  }
  const code = (error as { code?: unknown } | null)?.code;
  // A refused change of a column's options says why, as the product's errors say it.
  const optionRefusal = optionRefusalText(code);
  if (optionRefusal) return optionRefusal;
  const codeKey = typeof code === "string" ? KNOWN_BACKEND_ERROR_CODES[code] : undefined;
  if (codeKey) return i18next.t(`settingsErrors:backend.${codeKey}`);
  const knownKey = KNOWN_BACKEND_ERROR_KEYS[messageText(error).trim()];
  return knownKey ? i18next.t(`settingsErrors:backend.${knownKey}`) : null;
}

/** What the settings window says of a request the server didn't answer. */
export type SettingsNoAnswer = { text: string; write: boolean };

const noAnswerListeners = new Set<(said: SettingsNoAnswer) => void>();

/** The settings window, while open, says what its sections' requests got no answer to. */
export function onSettingsNoAnswer(listener: (said: SettingsNoAnswer) => void) {
  noAnswerListeners.add(listener);
  return () => {
    noAnswerListeners.delete(listener);
  };
}

/**
 * A request the server didn't answer, as every settings section says it
 * (2026-11-07: five sections said it four ways, two asked to reload a list
 * or "the audit log", one line stayed after the server came back, and a
 * line put above the rows moved the switch under the pointer). While the
 * settings window is open, a change (a write that went out) is said once by
 * the window, in a notice that pushes no row, and the section shows nothing
 * (its control what the server last said); a read is said in place of what
 * it would have shown. The window reads the section again once the server
 * answers, and the words say so. Without the window, the caller says it as
 * before. Null for any other failure.
 */
export function settingsNoAnswerText(error: unknown): string | null {
  const write = isUncertainWrite(error);
  if (!write && !isServerUnreachable(error)) return null;
  if (noAnswerListeners.size === 0) return i18next.t(write ? "settingsErrors:writeUncertain" : "settingsErrors:serverUnreachable");
  const text = i18next.t(write ? "settingsErrors:writeUncertain" : "settingsErrors:readUnanswered");
  for (const listener of [...noAnswerListeners]) listener({ text, write });
  return write ? "" : text;
}

/**
 * userErrorText with the diagnostic reference only for a failure no one
 * foresaw: no answer, a server fault, a refusal with no known reason. A
 * refusal the server answered with one (a policy, a right, a value) reads as
 * that reason alone, as the product's error presentation does (2026-11-05).
 */
export function userFailureText(
  error: unknown,
  fallback: string,
  referenceLabel = i18next.t("productErrors:labels.reference"),
) {
  const text = userErrorText(error, fallback);
  // A change the settings window says has nothing here (settingsNoAnswerText).
  if (!text) return text;
  const reference = (error as { reference?: unknown } | null)?.reference;
  const status = statusCode(error);
  // A change whose outcome isn't known is no failure to look up: it says so
  // in the secondary colour, which only its own words carry (2026-11-06: a
  // teamspace's said it in red with a reference).
  const foreseen = isUncertainWrite(error) || (status !== undefined && status < 500 && knownErrorText(error) !== null);
  if (!isDiagnosticReference(reference) || foreseen) return text;
  return messageWithDiagnosticReference(text, reference, referenceLabel);
}

/**
 * A failure for a toast, or null when the server didn't answer: the page's
 * connection bar says so once, as Notion's offline mark does, rather than a
 * toast for each request.
 */
/**
 * Why the organization's DLP policy refused a write (a comment, a file, an
 * item sent), in the words the page title's refusal uses: a sensitive term,
 * and what to do about it. Settings, exports and publishing keep the general
 * sentence (dlpPolicyBlocked), where nothing was written (2026-11-05: a
 * refused comment said only that the policy blocks it).
 */
export function dlpWriteRefusal(error: unknown): string | null {
  if ((error as { code?: unknown } | null)?.code !== enterpriseDLPErrorCodes.policyBlocked) return null;
  return `${i18next.t("productErrors:meanings.organizationDlpBlocksOperation")} ${i18next.t("productErrors:meanings.takeOutTermOrAskAboutDlp")}`;
}

/** userErrorText for a refused write: a DLP refusal says what dlpWriteRefusal does. */
export function writeRefusalText(error: unknown, fallback: string) {
  return dlpWriteRefusal(error) ?? userErrorText(error, fallback);
}

export function userErrorToast(error: unknown, fallback: string) {
  // A request answered as signed out ends the sign-in (App shows the sign-in
  // screen); what it was loading isn't said as a failure as well.
  const failure = error as { status?: unknown; code?: unknown } | null;
  if (isSignedOutResponse(failure?.status, failure?.code)) return null;
  return isServerUnreachable(error) ? null : userErrorText(error, fallback);
}

export function settingsErrorMessage(error: unknown, fallback: string) {
  return userErrorText(error, fallback);
}

/**
 * Marks a message that says a change's outcome isn't known rather than that
 * it failed: it shows in the secondary colour, not as a failure's red
 * (red only for what failed; app/globals.css).
 */
export function writeUncertainMark(text: string | null | undefined) {
  return isWriteUncertainText(text) ? { "data-write-uncertain": "" } : {};
}

/** Whether words shown are those of a change whose outcome isn't known. */
export function isWriteUncertainText(text: string | null | undefined) {
  return Boolean(text) && text === i18next.t("settingsErrors:writeUncertain");
}
