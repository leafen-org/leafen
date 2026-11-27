import type { WorkspaceMember } from "./admin";
import type { NotionAdminCapability } from "@/notionAdminTokenWire.generated";
import type {
  OrganizationMemberRole,
  Timestamped,
} from "./core";

export interface OrganizationMember extends Timestamped {
  organizationId: string;
  userId: string;
  displayName?: string | null;
  email?: string | null;
  avatar?: string | null;
  role: OrganizationMemberRole | string;
  status?: "active" | "deactivated" | string;
  createdBy?: string;
  deactivatedAt?: string | null;
  deactivatedBy?: string | null;
}

export interface OrganizationProfileWorkspaceMembership {
  workspaceId: string;
  workspaceName: string;
  workspaceDomain?: string | null;
  workspaceMemberId: string;
  role: WorkspaceMember["role"];
}

export interface OrganizationProfilePendingInvitation {
  workspaceId: string;
  workspaceName: string;
  workspaceDomain?: string | null;
  invitationId: string;
  email: string;
  role: WorkspaceMember["role"];
  status: string;
}

export interface OrganizationProfile {
  organizationMemberId?: string | null;
  userId?: string | null;
  displayName?: string | null;
  email?: string | null;
  avatar?: string | null;
  organizationRole: OrganizationMember["role"];
  status: OrganizationMember["status"] | "invited" | string;
  workspaceMemberships: OrganizationProfileWorkspaceMembership[];
  pendingInvitations: OrganizationProfilePendingInvitation[];
}

export type { NotionAdminCapability } from "@/notionAdminTokenWire.generated";

export interface OrganizationAdminToken extends Timestamped {
  organizationId: string;
  label: string;
  status?: "active" | "revoked" | "expired" | string;
  tokenPrefix?: string | null;
  scopes?: {
    version?: number;
    capabilities?: NotionAdminCapability[];
    resources?: {
      organizationId?: string;
      workspaceIds?: string[];
      legalHoldIds?: string[];
    };
  } | null;
  createdBy?: string | null;
  lastUsedAt?: string | null;
  expiresAt?: string | null;
  revokedAt?: string | null;
  revokedBy?: string | null;
}
