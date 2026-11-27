"use client";

import type { Page } from "./types";

export const PAGE_ROOM_MUTATION_EVENT = "leafen:page-room-mutation";
export const PAGE_ROOM_MUTATION_RECEIVED_EVENT = "leafen:page-room-mutation-received";
export const LOCAL_DATABASE_MUTATION_EVENT = "leafen:local-database-mutation";

export type PageRoomMutationKind =
  | "page_meta_changed"
  | "database_rows_changed"
  | "database_schema_changed"
  | "database_views_changed"
  | "database_templates_changed"
  | "block_structure_changed"
  | "comments_changed"
  | "permissions_changed";

export type PageMetaMutationPatch = Partial<
  Pick<
    Page,
    | "backlinksDisplay"
    | "cover"
    | "coverPosition"
    | "font"
    | "fullWidth"
    | "icon"
    | "iconType"
    | "isFavorite"
    | "isLocked"
    | "lastEditedBy"
    | "pageCommentsDisplay"
    | "smallText"
    | "title"
    | "updatedAt"
    | "verificationExpiresAt"
    | "verifiedAt"
    | "verifiedBy"
  >
>;

export interface PageRoomMutationChange {
  blockIds?: string[];
  databaseId?: string;
  kind: PageRoomMutationKind;
  pageId: string;
  patch?: PageMetaMutationPatch;
  propertyIds?: string[];
  reason?: string;
  revision?: number;
  rowIds?: string[];
  targetPageId?: string;
  updatedAt?: string;
  viewIds?: string[];
}

export interface PageRoomMutationReceived extends PageRoomMutationChange {
  label?: string;
  memberId?: string;
  receivedAt: number;
  userId: string;
}

export function publishPageRoomMutation(change: PageRoomMutationChange) {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new CustomEvent<PageRoomMutationChange>(PAGE_ROOM_MUTATION_EVENT, { detail: change }));
}
