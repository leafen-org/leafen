import type { Timestamped } from "./core";
import type { TextSpan } from "./content";
import type { DatabaseSourceTitle } from "@/components/editor/databaseTitles";

export interface Workspace extends Timestamped {
  organizationId?: string | null;
  name: string;
  icon?: string;
  domain?: string;
  ownerId?: string;
  creationStatus?: "initializing" | "completed";
  creationOrigin?: "workspace_mutation" | "workspace_bootstrap" | null;
  creationRequestedBy?: string | null;
  creationSkipDefaultPages?: boolean;
  creationLocale?: "en" | "ko";
  creationCompletedAt?: string | null;
  deletionPendingAt?: string | null;
  pageAnalyticsEnabled?: boolean;
  pageAnalyticsAnonymousTrackingEnabled?: boolean;
}

export type PageParentType = "workspace" | "page" | "database";
export type PageKind = "page" | "database";
export type IconType = "none" | "emoji" | "image" | "custom_emoji" | "icon";
export type PageFont = "default" | "serif" | "mono";
export type BacklinksDisplay = "default" | "expanded" | "off";
export type PageCommentsDisplay = "default" | "expanded" | "off";

export interface PageLayoutHints {
  hasRootColumnList?: boolean;
  hasRootInlineDatabase?: boolean;
}

export interface DatabaseSubitemsFeature {
  childrenPropertyId: string;
  enabled: true;
  nestedPropertyId?: string;
  parentPropertyId: string;
  revision: number;
  showToggleOnTitle?: boolean;
}

interface DatabaseDependenciesFeatureBase {
  avoidWeekends: boolean;
  dataKey?: string;
  enabled: true;
  predecessorPropertyId: string;
  revision: number;
  shiftMode: "overlap" | "maintain_spacing" | "none";
  successorPropertyId: string;
}

export type DatabaseDependenciesFeature = DatabaseDependenciesFeatureBase & (
  | {
      dateMode?: "range";
      datePropertyId: string;
    }
  | {
      dateMode: "separate";
      endDatePropertyId: string;
      startDatePropertyId: string;
    }
);

type DisabledDatabaseTaskFeature<T extends { enabled: true }> = T extends unknown
  ? Omit<T, "enabled"> & { enabled: false }
  : never;

export interface PreservedDatabaseTaskFeatures {
  dependencies?: Array<DisabledDatabaseTaskFeature<DatabaseDependenciesFeature>>;
  subitems?: Array<DisabledDatabaseTaskFeature<DatabaseSubitemsFeature>>;
}

export interface DatabaseFeatures {
  dependencies?: DatabaseDependenciesFeature;
  preservedTaskFeatures?: PreservedDatabaseTaskFeatures;
  subitems?: DatabaseSubitemsFeature;
}

export interface Page extends Timestamped {
  workspaceId: string;
  parentId?: string | null;
  parentType: PageParentType;
  teamspaceId?: string | null;
  teamspacePermissionMode?: "inherit" | "restricted";
  kind: PageKind;
  title: string;
  /** What a database row is called while it has no title: its database's default template's name (pageDisplayTitle). */
  untitledName?: string;
  /** Whether this full-page database is a view block, with no database of its own: "제목 없음" while untitled (untitledPageDisplayTitle). */
  /** A full-page database's own database, which a mention of it names. */
  databaseContainerId?: string;
  viewBlock?: boolean;
  /** An untitled full-page database's sources' titles while it holds several, which name it (untitledPageDisplayTitle). */
  sourceTitles?: DatabaseSourceTitle[];
  /** A page read on its own that is a database's page, not a wiki's: it opens as its row's page (pagesOutsideList.ts). */
  databaseRow?: boolean;
  icon?: string;
  iconType: IconType;
  notionIcon?: Record<string, unknown> | null;
  /** Whether the page's body holds any block, which its icon shows when it has none of its own. */
  hasContent?: boolean;
  cover?: string;
  coverPosition?: number;
  font?: PageFont;
  smallText?: boolean;
  fullWidth?: boolean;
  isLocked?: boolean;
  /** Who locked the page, for its lock's tip; none for a page locked before it was kept. */
  lockedBy?: string;
  backlinksDisplay?: BacklinksDisplay;
  pageCommentsDisplay?: PageCommentsDisplay;
  tableOfContentsDisplay?: import("@/workspaceMembershipWire.generated").PageTableOfContentsDisplay;
  inlineCommentsDisplay?: import("@/workspaceMembershipWire.generated").PageInlineCommentsDisplay;
  isWiki?: boolean;
  wikiRootId?: string | null;
  verifiedAt?: string | null;
  verifiedBy?: string | null;
  verificationExpiresAt?: string | null;
  /** Column values when this page is a row in a database: { [propertyId]: value } */
  properties?: Record<string, unknown>;
  /** Formatting of a row's title and text values: { [propertyId]: spans spelling the value } */
  propertySpans?: Record<string, TextSpan[]>;
  /** Formatting of a row's title, spelling `title`. */
  titleSpans?: TextSpan[];
  /** Server-owned database feature bindings. Property names remain presentation-only. */
  databaseFeatures?: DatabaseFeatures;
  databaseFeaturesRevision?: number;
  /** Server-owned monotonic generation for bounded block continuations. */
  blockRevision?: number;
  /** Server-owned monotonic generation for bounded comment continuations. */
  commentRevision?: number;
  /** Exact live count of non-integration direct grants for Shared placement. */
  directShareCount?: number;
  /** Indexed hierarchy authority for database rows; empty means a root row. */
  subitemParentId?: string;
  /** Server-maintained count of live direct children; child IDs remain cursor-loaded. */
  subitemChildCount?: number;
  /** A database row archived out of its views, by whom and when. */
  archivedAt?: string;
  archivedBy?: string;
  /** Transient restricted-ancestor projection; never persisted as page content. */
  __structuralPlaceholder?: true;
  /** Transient client-side order from the remote database row query. Never persisted. */
  __databaseRowOrder?: number;
  isFavorite?: boolean;
  /** Local Notion-style "Share to web" flag. It controls UI/link semantics only. */
  isPublic?: boolean;
  /** Non-persisted hints from bootstrap so first paint can match late-loaded block layout. */
  layoutHints?: PageLayoutHints;
  inTrash?: boolean;
  trashedAt?: string | null;
  blockTreeLifecycleJobId?: string | null;
  commentDeleteLifecycleJobId?: string | null;
  databaseRowMoveGeneration?: string | null;
  position: number;
  createdBy?: string;
  lastEditedBy?: string;
  /** Immutable authenticated receipt for exact page-create replay. */
  createMutationId?: string;
  /** Server receipt for page/row update replay and same-client chaining. */
  lastMutationId?: string;
}
