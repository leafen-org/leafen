import type {
  OrganizationSharingPolicy,
  Timestamped,
  WorkspaceCreationPolicy,
} from "./core";

export interface Comment extends Timestamped {
  pageId: string;
  blockId?: string | null;
  parentId?: string | null;
  authorId: string;
  body?: unknown;
  resolved?: boolean;
  /** Immutable request identity for exact comment-create replay. */
  createMutationId?: string;
  /** Set only when the BODY is edited (resolve/move also bump updatedAt). */
  editedAt?: string;
}

export interface WorkspaceMember extends Timestamped {
  workspaceId: string;
  userId: string;
  displayName?: string;
  email?: string;
  avatar?: string | null;
  role: "owner" | "admin" | "member" | "guest" | string;
  createdBy?: string;
}

/** Canonical read-only auth projection carried with actor-bearing responses. */
export interface ActorProfile {
  userId: string;
  displayName?: string | null;
  email?: string | null;
  avatar?: string | null;
}

export interface Organization extends Timestamped {
  name: string;
  icon?: string | null;
  ownerId?: string;
  workspaceCreationPolicy?: WorkspaceCreationPolicy | string;
  sharingPolicy?: OrganizationSharingPolicy | Record<string, unknown> | null;
  storageLimitBytes?: number | null;
}
