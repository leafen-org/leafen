import type { OrganizationAdminRole, OrganizationRole, OrganizationSettingsAuditEvent } from "@/organizationWire.generated";
import { formatFileSize } from "@/components/database/files";
import { organizationAdminRoleLabel, organizationRoleLabel } from "./organizationSettingsModel";

type Translate = (key: string, options?: Record<string, unknown>) => string;

// The events whose target the server names: a person, a workspace, a page,
// a teamspace or an invitation. One it can't find any more is gone.
const NAMED_TARGETS = new Set([
  "user", "workspace_member", "workspace_owner", "organization_owner",
  "workspace", "page", "teamspace", "workspace_invitation",
]);

// A person whose account is gone keeps no name on the log's lines but their
// own deletion's (user decision 2026-11-07): they are "알 수 없는 사용자", as
// the person who did something is. A line about a member keeps their account
// (metadata's userId, a group's memberId); the server takes the name
// it kept off it once that account is gone (organization_audit_gone_people.go).
const PERSON_TARGETS = new Set(["user", "workspace_member", "workspace_owner", "organization_owner"]);

function personTarget(event: OrganizationSettingsAuditEvent) {
  return PERSON_TARGETS.has(event.targetType) || (typeof event.metadata?.userId === "string" && Boolean(event.metadata.userId));
}

/** The member a group took or lost: by the name the line kept, or, their account gone, "알 수 없는 사용자". */
function auditMember(metadata: Record<string, unknown>, t: Translate) {
  if (typeof metadata.member === "string") return metadata.member;
  return typeof metadata.memberId === "string" && metadata.memberId ? t("auditUnknownPerson") : "";
}

// A record's generated id, which says nothing to a person (2026-11-05: a member
// no longer listed showed theirs as what an admin role was given to).
const RECORD_ID = /^[a-z0-9]{15}$/;

/**
 * An audit event's name, as Notion's audit log names its events ("멤버
 * 초대", "팀스페이스 생성됨", its guide read 2026-10-08). A call made with a
 * person's key names the key's connection. An event Leafen doesn't name yet
 * reads as "기타 이벤트", never as its stored action, which only the CSV
 * carries (2026-11-13: a workspace group's lines read
 * "workspace.permission group.update").
 */
export function organizationAuditLabel(event: OrganizationSettingsAuditEvent, t: Translate) {
  if (event.action === "connection.request") {
    // An app's own key carries no connection name: the app names it.
    const connection = String(event.metadata.connection ?? "") || (event as { actorApp?: string }).actorApp || "";
    return connection ? t("auditConnectionRequest", { connection }) : t("auditKeyRequest");
  }
  if (event.action === "user_key_policy.update") return t("auditKeyPolicyUpdate");
  return t(`auditEvents.${event.action}`, { defaultValue: "" }) || t("auditEventOther");
}

/** A kind of event by its name, for the filter: every key's call is one kind. */
export function organizationAuditKindLabel(event: OrganizationSettingsAuditEvent, t: Translate) {
  if (event.action === "connection.request") return t("auditEvents.connection.request");
  return organizationAuditLabel(event, t);
}

// What a key's call reached, by its path's part after the version, in a
// person's words: the method and path are a developer's, and stay in the CSV
// (decided 2026-11-06).
const CALL_TARGETS: Readonly<Record<string, string>> = {
  pages: "page", blocks: "block", databases: "database", data_sources: "database", "data-sources": "database",
  comments: "comment", files: "file", file_uploads: "file", "file-uploads": "file", users: "person",
  search: "search", views: "view", teamspaces: "teamspace",
};

function organizationAuditCallTarget(path: unknown, t: Translate) {
  // The last kind of thing the path names: a teamspace's members are the
  // teamspace's, a page's comments the comments.
  const parts = String(path ?? "").split("/").filter(Boolean);
  const target = [...parts].reverse().map((part) => CALL_TARGETS[part]).find(Boolean);
  return target ? t(`auditCallTarget.${target}`) : "";
}

/**
 * The log as a person reads it: a key's call that also wrote an event of its
 * own — the same person in the same workspace within two seconds — shows once,
 * as that event; the call stays in the CSV.
 */
export function withoutRepeatedKeyCalls<T extends { action: string; actorId?: string; workspaceId?: string; occurredAt: number }>(
  events: readonly T[],
) {
  return events.filter((event) => event.action !== "connection.request" || !events.some((other) => (
    other.action !== "connection.request" && other.actorId === event.actorId
      && (other.workspaceId ?? "") === (event.workspaceId ?? "") && Math.abs(other.occurredAt - event.occurredAt) <= 2000
  )));
}

/** A line where someone acted on themselves: left a workspace, deleted their own account. */
function organizationAuditSelf(event: OrganizationSettingsAuditEvent) {
  return Boolean(event.actorId) && event.actorId === event.targetId;
}

/**
 * Who did it: a person by name, or SCIM for what an identity provider sent,
 * which no person did. Someone who acted on themselves is named once, here,
 * by the name the line keeps for them once their account has none (the email
 * of an account's own deletion line).
 */
export function organizationAuditActor(
  event: OrganizationSettingsAuditEvent,
  t: Translate,
  names: Readonly<Record<string, string>>,
) {
  if (event.actorId === "scim") return "SCIM";
  const self = organizationAuditSelf(event) ? organizationAuditTarget(event, t, names) : "";
  return names[event.actorId] || event.actorName || self || (event.actorId ? t("auditUnknownPerson") : "");
}

/**
 * What a line did, after who did it: a person, workspace, page, teamspace or
 * invitation by its name ("삭제됨" once it is gone), a key's call by what it
 * reached, a setting by the value it took.
 */
export function organizationAuditSubject(
  event: OrganizationSettingsAuditEvent,
  t: Translate,
  names: Readonly<Record<string, string>> = {},
) {
  if (event.action === "connection.request") return organizationAuditCallTarget(event.metadata.path, t);
  const target = organizationAuditSelf(event) ? "" : organizationAuditTarget(event, t, names);
  return [target, organizationAuditChange(event, t)].filter(Boolean).join(" · ");
}

// A group's line, the organization's or a workspace's: its change names the
// group's names before and after as they were, and the member it took or lost.
const GROUP_LINE = /^(organization_settings\.group_|workspace\.permission_group\.|notion_admin\.permission_group\.)/u;

const ORGANIZATION_ROLES = new Set(["owner", "admin", "security_admin", "billing_admin", "member", "guest"]);
const ADMIN_ROLES = new Set(["compliance", "people", "it", "analytics", "billing"]);
const MEMBER_STATUSES: Readonly<Record<string, string>> = { active: "auditStatusActive", deactivated: "statusDeactivated" };

/**
 * What changed on an organization line, in the words its settings use: a
 * role or admin role, whether SCIM turned a person off or on, or a group's
 * name before and after.
 */
function organizationValueWord(action: string, value: unknown, t: Translate) {
  if (typeof value !== "string" || !value) return t("auditRoles.none");
  if (GROUP_LINE.test(action)) return value;
  if (ORGANIZATION_ROLES.has(value)) return organizationRoleLabel(value as OrganizationRole, t);
  if (ADMIN_ROLES.has(value)) return organizationAdminRoleLabel(value as OrganizationAdminRole, t);
  return MEMBER_STATUSES[value] ? t(MEMBER_STATUSES[value]) : value;
}

/**
 * What a change changed: a page-level rule's property, the
 * member a group took or lost, and the role, level, status or name before and
 * after ("읽기 가능 → 편집 가능"); SAML's settings by what they took.
 */
function organizationAuditChange(event: OrganizationSettingsAuditEvent, t: Translate) {
  if (event.action === "organization_settings.update") return organizationSettingsChange(event, t);
  if (event.action === "organization.saml.updated") return samlSettingsChange(event, t);
  if (event.action === "organization.audit_retention.updated") return retentionChange(event, t);
  const organizationLine = event.action.startsWith("organization_settings.") || event.action === "organization.scim.user.updated"
    || GROUP_LINE.test(event.action);
  if (!organizationLine && !event.action.startsWith("database.page_rule.")) return "";
  const metadata = event.metadata ?? {};
  const word = (value: unknown) => organizationLine ? organizationValueWord(event.action, value, t)
    : typeof value === "string" && value ? t(`auditRoles.${value}`, { defaultValue: value }) : t("auditRoles.none");
  const parts: string[] = [];
  const member = auditMember(metadata, t);
  if (member) parts.push(member);
  const property = metadata.creator === true ? t("auditRuleCreator") : typeof metadata.property === "string" ? metadata.property : "";
  if (property) parts.push(property);
  if ("from" in metadata || "to" in metadata) parts.push(`${word(metadata.from)} → ${word(metadata.to)}`);
  return parts.join(" · ");
}

/** The audit log's days kept before and after, as its setting words them: "30일 → 삭제하지 않음". */
function retentionChange(event: OrganizationSettingsAuditEvent, t: Translate) {
  const metadata = event.metadata ?? {};
  if (!("from" in metadata) && !("to" in metadata)) return "";
  const days = (value: unknown) => typeof value === "number" && value > 0
    ? t("trashRetentionDays", { count: value }) : t("activityRetention.keep");
  return `${days(metadata.from)} → ${days(metadata.to)}`;
}

// The security switches an organization setting change can name, by the
// stored setting, as the security settings word them; a "비활성화" switch is
// on while the setting is off.
const SETTING_SWITCHES: Readonly<Record<string, readonly [label: string, inverted: boolean]>> = {
  PublicWebSharing: ["disablePublicSharing", true],
  CrossWorkspaceCopies: ["disableCrossWorkspaceCopies", true],
  Exports: ["disableExports", true],
  GuestAccess: ["guestAccessRow", false],
  ExternalEmailSharing: ["externalEmailSharingRow", false],
};

/** What an organization setting change changed: "내보내기 비활성화: 켬", the storage limit it took. */
function organizationSettingsChange(event: OrganizationSettingsAuditEvent, t: Translate) {
  const metadata = event.metadata ?? {};
  const parts: string[] = [];
  let others = 0;
  for (const change of Array.isArray(metadata.changes) ? metadata.changes : []) {
    const setting = typeof change?.setting === "string" ? SETTING_SWITCHES[change.setting] : undefined;
    if (!setting) {
      others += 1;
      continue;
    }
    const on = Boolean(change.to) !== setting[1];
    parts.push(`${t(setting[0])}: ${t(on ? "auditSettingOn" : "auditSettingOff")}`);
  }
  if (others) parts.push(t("auditSettingOthers", { count: others }));
  if ("storageLimitBytes" in metadata) {
    const bytes = metadata.storageLimitBytes;
    parts.push(`${t("storageLimitField")}: ${typeof bytes === "number" && bytes > 0 ? formatFileSize(bytes) : t("noLimit")}`);
  }
  return parts.join(" · ");
}

// SAML's settings as its settings name them, and the sign-in rule's choices.
const SAML_SETTINGS: Readonly<Record<string, string>> = {
  enabled: "enterpriseEnableSso", issuer: "enterpriseSamlIdpEntityId", loginUrl: "enterpriseSamlLoginUrl",
  certificate: "enterpriseSamlCertificate", allowIdpInitiated: "enterpriseSamlAllowIdpInitiated",
  enforcement: "enterpriseEnforcement", linkedWorkspaceId: "enterpriseLinkedWorkspace", accountCreation: "enterpriseAutoCreate",
};
const SAML_ENFORCEMENTS: Readonly<Record<string, string>> = {
  optional: "enterpriseOptional", required_for_verified_domains: "enterpriseVerifiedDomains",
  required_for_all_members: "enterpriseAllMembers",
};

/** What a SAML save changed: "SAML SSO 활성화: 켬", the sign-in rule it took, "서명 인증서: 바뀜". */
function samlSettingsChange(event: OrganizationSettingsAuditEvent, t: Translate) {
  const parts: string[] = [];
  let others = 0;
  for (const change of Array.isArray(event.metadata?.changes) ? event.metadata.changes : []) {
    const label = typeof change?.setting === "string" ? SAML_SETTINGS[change.setting] : undefined;
    if (!label) {
      others += 1;
      continue;
    }
    const to = change.setting === "accountCreation" ? change.to === "automatic" : change.to;
    const value = typeof to === "boolean" ? t(to ? "auditSettingOn" : "auditSettingOff")
      : change.setting === "enforcement" && SAML_ENFORCEMENTS[String(to)] ? t(SAML_ENFORCEMENTS[String(to)]) : t("auditSettingChanged");
    parts.push(`${t(label)}: ${value}`);
  }
  if (others) parts.push(t("auditSettingOthers", { count: others }));
  return parts.join(" · ");
}

/**
 * What a target filter's chip calls the target: as the line it was picked on
 * names it, gone or not, never by its record's id.
 */
export function organizationAuditTargetName(
  event: OrganizationSettingsAuditEvent,
  t: Translate,
  names: Readonly<Record<string, string>> = {},
) {
  return organizationAuditTarget(event, t, names) || organizationAuditSubject(event, t, names);
}

/**
 * What a line touched, as the server names it for the log and its CSV alike
 * (its name now, else the one its line kept); then as this screen knows it.
 */
function organizationAuditTarget(event: OrganizationSettingsAuditEvent, t: Translate, names: Readonly<Record<string, string>>) {
  if (!event.targetId) return "";
  if (typeof event.targetName === "string" && event.targetName) return event.targetName;
  const known = names[event.targetId];
  if (known) return known;
  if (personTarget(event)) return t("auditUnknownPerson");
  if (typeof event.targetName === "string") return t("auditUntitled");
  const remembered = typeof event.metadata?.name === "string" ? event.metadata.name : "";
  if (remembered) return remembered;
  if (NAMED_TARGETS.has(event.targetType)) return t("auditTargetGone");
  // A setting's target is the value it took (a domain, a policy); other
  // targets are internal records with nothing to show.
  return event.targetType === "organization_settings" && !RECORD_ID.test(event.targetId) ? event.targetId : "";
}
