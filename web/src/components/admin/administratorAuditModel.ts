import type { TFunction } from "i18next";
import type { AdministratorAuditEvent } from "../../administratorWire.generated";
import { personDistinction } from "@/lib/peopleDirectory";
import { authAuditMethodText, authAuditOutcomeText, authAuditPhaseText, authAuditReasonText, authAuditSignInText } from "@/lib/authAuditText";

/** A catalog key and its value for one line of an audit event's details. */
export type AdministratorAuditDetail = readonly [key: string, value: string];

const accountActions: Record<string, string> = {
  setup: "auditInstanceMasterBootstrap",
  create: "auditInstanceUserCreate",
  "sessions-revoke": "auditInstanceUserSessionsRevoke",
  "session-revoke": "auditInstanceUserSessionRevoke",
  "password-reset": "auditInstanceUserPasswordReset",
  "password-reset-command": "auditInstanceUserPasswordResetCommand",
  "device-code": "auditInstanceUserDeviceCode",
  "device-code-command": "auditInstanceUserDeviceCodeCommand",
  "device-code-owner": "auditInstanceUserDeviceCodeOwner",
  "device-code-server-log": "auditInstanceUserDeviceCodeServerLog",
  "device-code-own-window": "auditInstanceUserDeviceCodeOwnWindow",
  "delete-start": "auditInstanceUserDelete",
  "administrator-grant": "auditInstanceAdminGrant",
  "administrator-revoke": "auditInstanceAdminRevoke",
};

const settingsFields: Record<string, string> = {
  signup_policy: "auditInstanceSignupPolicyUpdate",
  signup_contact: "auditSignupContactUpdate",
  server_name: "auditServerNameUpdate",
  member_add_policy: "auditInstanceMemberAddPolicyUpdate",
  request_log_retention_days: "auditRequestLogRetentionUpdate",
  request_log_successful: "auditRequestLogSuccessfulUpdate",
  read_log: "auditReadLogUpdate",
  google_return_address: "auditSignInReturnUpdate",
  oidc_return_address: "auditSignInReturnUpdate",
  git_install: "auditGitInstall",
};

// What a settings change's values are shown as.
const settingsValueKeys: Record<string, string> = {
  signup_policy: "auditSignupPolicyRef",
  signup_contact: "auditDetailChange",
  server_name: "auditDetailChange",
  request_log_retention_days: "auditDetailChange",
  request_log_successful: "auditDetailChange",
  google_return_address: "auditDetailChange",
  oidc_return_address: "auditDetailChange",
  git_install: "auditDetailChange",
};

/** The catalog key that names what an audit event records. */
export function administratorAuditLabelKey(event: AdministratorAuditEvent): string {
  if (event.kind === "sign_in") return "auditLoginAttempt";
  if (event.kind === "settings") return settingsFields[event.action] ?? "auditSettingsUpdate";
  if (event.action === "state") {
    return event.toValue === "disabled" ? "auditInstanceUserDisable" : "auditInstanceUserRestore";
  }
  return accountActions[event.action] ?? "auditSettingsUpdate";
}

function person(name: string, email: string, id: string, t: TFunction) {
  if (name && email) return `${name} · ${email}`;
  // No email: another of the same name is told apart as the people lists do.
  const distinction = name && id ? personDistinction(id) : null;
  if (distinction) return `${name} · ${distinction}`;
  return name || email || (id ? t("auditUnknownPerson") : "");
}

/** The detail lines an audit event shows under its label (a sign-in's way,
 * step, result and reason in words, as the sign-in history says them). */
export function administratorAuditDetails(event: AdministratorAuditEvent, t: TFunction): AdministratorAuditDetail[] {
  const details: AdministratorAuditDetail[] = [];
  const actor = person(event.actorName, event.actorEmail, event.actorId, t);
  const target = person(event.targetName, event.targetEmail, event.targetId, t);
  if (event.kind === "sign_in") {
    if (target) details.push(["auditDetailTarget", target]);
    // An account's right first step that 2-step verification asked on from says so, as its history does;
    // a sign-in provider's start, before any account, stays a step's success.
    const continued = event.outcome === "success" && event.phase === "request" && event.targetId;
    details.push(["auditDetailMethod", authAuditMethodText(t, event.method)], ["auditDetailPhase", authAuditPhaseText(t, event.phase)],
      ["auditDetailOutcome", continued ? authAuditSignInText(t, event.outcome, event.phase) : authAuditOutcomeText(t, event.outcome)]);
    if (event.reasonCode) details.push(["accountHistory.reason", authAuditReasonText(t, event.method, event.reasonCode, event.reasonStatus)]);
    if (event.ip) details.push(["auditDetailPrincipal", event.ip]);
    return details;
  }
  if (actor) details.push(["auditDetailActor", actor]);
  if (event.kind === "settings") {
    if (!settingsFields[event.action]) details.push(["auditDetailTarget", event.action]);
    if (event.fromValue !== event.toValue) {
      const change = `${event.fromValue} → ${event.toValue}`;
      details.push([settingsValueKeys[event.action] ?? "auditDetailEnabled", change]);
    }
    return details;
  }
  if (target && event.targetId !== event.actorId) details.push(["auditDetailTarget", target]);
  return details;
}
