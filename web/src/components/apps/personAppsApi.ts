import { request } from "@/apiTransport";
import {
  appPlatformOperations,
  type AppConsent,
  type AppPersonCatalog,
  type AppPasswordCreated,
  type AppPasswordList,
  type AppPersonCredentialList,
  type AppPersonScheduleList,
  type AppRequest,
} from "@/appPlatformWire.generated";
import { answeredUnlessAway } from "@/lib/serverReach";
import type { Session } from "@/types";

// A person's own app calls (internal/app/app_platform_install_policy.go):
// what they may ask for or add, asking, adding, and agreeing again.

const fill = (path: string, values: Record<string, string>) =>
  path.replace(/\{(\w+)\}/gu, (_, name: string) => encodeURIComponent(values[name] ?? ""));

/** The apps the person may open and those offered; failing at once while the server is away (answeredUnlessAway). */
export function readPersonCatalog(session: Session) {
  return answeredUnlessAway(request<AppPersonCatalog>(appPlatformOperations.personCatalog.path, { method: appPlatformOperations.personCatalog.method }, session));
}

export function requestApp(session: Session, input: { catalogId: string; appId: string; tenancyKey: string; reason: string }) {
  return request<AppRequest>(appPlatformOperations.requestApp.path, {
    method: appPlatformOperations.requestApp.method, body: JSON.stringify(input),
  }, session);
}

/** Without a hash, what the app may do; with the hash the person accepted, adds it. */
export function selfInstallApp(session: Session, input: { catalogId: string; appId: string; tenancyKey: string; acceptPermissionsHash?: string }) {
  return request<AppConsent>(appPlatformOperations.selfInstall.path, {
    method: appPlatformOperations.selfInstall.method, body: JSON.stringify(input),
  }, session);
}

/** Without a hash, what the person is asked to agree to again; with it, their consent. */
export function consentToApp(session: Session, installId: string, tenancyKey: string, permissionsHash = "") {
  return request<AppConsent>(fill(appPlatformOperations.consent.path, { installId }), {
    method: appPlatformOperations.consent.method, body: JSON.stringify({ permissionsHash, tenancyKey }),
  }, session);
}

/** What the person gave apps — a mail account's password and the like; failing at once while the server is away (answeredUnlessAway). */
export function readPersonCredentials(session: Session) {
  return answeredUnlessAway(request<AppPersonCredentialList>(appPlatformOperations.personCredentials.path,
    { method: appPlatformOperations.personCredentials.method }, session));
}

export function removePersonCredential(session: Session, installId: string, key: string) {
  return request<AppPersonCredentialList>(fill(appPlatformOperations.deleteCredential.path, { installId, key }), {
    method: appPlatformOperations.deleteCredential.method,
  }, session);
}

/** The apps with endpoints the person may use, their addresses and the person's app passwords; failing at once while the server is away. */
export function readPersonPasswords(session: Session) {
  return answeredUnlessAway(request<AppPasswordList>(appPlatformOperations.personPasswords.path,
    { method: appPlatformOperations.personPasswords.method }, session));
}

/** Makes an app password, whose secret this answer alone holds. */
export function createPersonPassword(session: Session, input: { installId: string; tenancy: string; name: string }) {
  return request<AppPasswordCreated>(appPlatformOperations.createPersonPassword.path, {
    method: appPlatformOperations.createPersonPassword.method, body: JSON.stringify(input),
  }, session);
}

export function deletePersonPassword(session: Session, passwordId: string) {
  return request<AppPasswordList>(fill(appPlatformOperations.deletePersonPassword.path, { passwordId }), {
    method: appPlatformOperations.deletePersonPassword.method,
  }, session);
}

/** The per-person schedules of the apps the person uses; failing at once while the server is away. */
export function readPersonSchedules(session: Session) {
  return answeredUnlessAway(request<AppPersonScheduleList>(appPlatformOperations.personSchedules.path,
    { method: appPlatformOperations.personSchedules.method }, session));
}

/** Turns one of them off for the person, or back on. */
export function setPersonSchedule(session: Session, installId: string, schedule: string, on: boolean) {
  return request<AppPersonScheduleList>(fill(appPlatformOperations.setPersonSchedule.path, { installId, schedule }), {
    method: appPlatformOperations.setPersonSchedule.method, body: JSON.stringify({ on }),
  }, session);
}
