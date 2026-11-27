// The organization features' screens are a module kept apart from the core
// (web/src/organization), under the same AGPL-3.0-only license and free like the
// rest. The core never imports them: the release build puts the module's
// entry before the app's, and the module attaches what it adds here. Without
// it the core shows no organization screens.

import type { ComponentType, ReactNode } from "react";
import type { GraphProperty } from "@/contentGraph";
import type { OrganizationAuditExport, OrganizationAuditExportCreateInput } from "@/organizationAuditWire.generated";
import type { OrganizationSettingsMember, OrganizationSettingsSnapshot } from "@/organizationWire.generated";
import type { Session } from "@/types";
import type {
  EnterpriseCurrentContext,
  EnterpriseModuleService,
  EnterpriseModuleSnapshot,
  EnterpriseOperationContext,
  EnterpriseSettingsService,
  EnterpriseSettingsSnapshot,
} from "./enterpriseSettingsAdapter";
import type { EnterpriseAreas } from "./organizationSpecialistRolePolicy";
import type { WorkspaceEnterpriseReadContext } from "./workspaceEnterpriseSettings";
import type { OrganizationSettingsMutation } from "./workspaceOrganizationSettings";

export type OrganizationAdminRolesProps = {
  members: OrganizationSettingsMember[];
  userId: string;
  disabled: boolean;
  mutate: (mutation: OrganizationSettingsMutation) => void | Promise<void>;
};

export type OrganizationIdentityDomainsProps = {
  organization: OrganizationSettingsSnapshot;
  disabled: boolean;
  mutate: (mutation: OrganizationSettingsMutation) => void | Promise<void>;
};

export type OrganizationEnterprisePanelProps = {
  /** The areas the person's admin roles cover; the others aren't shown. */
  areas: EnterpriseAreas;
  isCurrent: (context: EnterpriseOperationContext) => boolean;
  /** The organization's Admin API tokens, which are the core's. */
  adminPanel: ReactNode;
  organizationId: string;
  requestGeneration: number;
  service: EnterpriseSettingsService;
  sessionIdentity: string;
  signal: AbortSignal;
  snapshot: EnterpriseSettingsSnapshot;
  view?: "identity" | "governance";
  /** Opens the organization's audit log, where its records are exported. */
  onOpenAuditLog?(): void;
};

/** The organization module's part of the settings' enterprise controls. */
export type OrganizationEnterpriseModule = {
  loadSnapshot(session: Session, context: WorkspaceEnterpriseReadContext): Promise<EnterpriseModuleSnapshot>;
  createService(session: Session, isCurrent: EnterpriseCurrentContext): EnterpriseModuleService;
  Panel: ComponentType<OrganizationEnterprisePanelProps>;
};

export type OrganizationModule = {
  name: string;
  /** The organization settings' "관리자 역할 관리". */
  AdminRoles?: ComponentType<OrganizationAdminRolesProps>;
  /** The settings' "신원" domains and who may create workspaces with them. */
  IdentityDomains?: ComponentType<OrganizationIdentityDomainsProps>;
  /** The settings' "신원" managed users and their policies. */
  ManagedUsers?: ComponentType<{ session: Session; organizationId: string }>;
  /** The settings' "보안": which workspaces require their guests to use 2-step verification. */
  GuestMFA?: ComponentType<{ session: Session; organizationId: string }>;
  /** The settings' "보안": whether members sign in with a password and 2-step verification rather than a device code. */
  SignInPolicy?: ComponentType<{ session: Session; organizationId: string }>;
  /** The settings' "보안" offer blocking sharing outside the organization. */
  sharingBlocks?: boolean;
  /** The role menu's "제한된 멤버" (Notion's restricted member): makes a workspace member one, or a member again. */
  restrictMember?: (session: Session, workspaceId: string, userId: string, restricted: boolean, signal?: AbortSignal) => Promise<void>;
  /** The settings' enterprise controls: single sign-on, SCIM, DLP, legal holds, audit and discovery. */
  enterprise?: OrganizationEnterpriseModule;
  /** The audit log's "내보내기": the log as it is filtered, as a file (the module's export routes). */
  exportAuditLog?: (session: Session, organizationId: string, input: OrganizationAuditExportCreateInput,
    signal?: AbortSignal) => Promise<OrganizationAuditLogFile>;
  /** A database column's "속성 사용 권한": who may see and change the property (propertyAccess.ts). */
  PropertyAccessDialog?: ComponentType<PropertyAccessDialogProps>;
  /** A database page's share "고급" › "속성 사용 권한": its rules, and "새 규칙 추가". */
  PropertyAccessShareRules?: ComponentType<PropertyAccessShareRulesProps>;
};

/** An exported audit log, checked against its receipt. */
export type OrganizationAuditLogFile = {
  record: OrganizationAuditExport;
  filename: string;
  content: string;
  contentType: string;
};

/** A property as the rule window names it. */
export type PropertyAccessDialogProperty = Pick<GraphProperty, "id" | "name" | "type"> & Partial<Pick<GraphProperty, "config">>;

export type PropertyAccessDialogProps = {
  session: Session;
  workspaceId: string;
  dataSourceId: string;
  /** The data source's properties that take a rule, the open one among them. */
  properties: readonly PropertyAccessDialogProperty[];
  /** Its person properties, which exceptions may name. */
  personProperties: readonly PropertyAccessDialogProperty[];
  propertyId: string;
  /** The database reads its properties and rows again under the saved rule. */
  onSaved?: () => void;
  onClose: () => void;
};

export type PropertyAccessShareRulesProps = {
  session: Session;
  workspaceId: string;
  pageId: string;
  disabled: boolean;
  /** The rules changed by this many, for "고급"'s count. */
  onCountChange: (delta: number) => void;
};

let attached: OrganizationModule | null = null;

export function attachOrganizationModule(module: OrganizationModule) {
  attached = module;
}

/** The attached organization module, or null in a core-only build. */
export function organizationModule(): OrganizationModule | null {
  return attached;
}
