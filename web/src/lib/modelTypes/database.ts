import type { DatabaseButtonActionDocument } from "./content";
import type { Timestamped } from "./core";

export type PropertyType =
  | "title"
  | "rich_text"
  | "number"
  | "select"
  | "multi_select"
  | "status"
  | "date"
  | "person"
  | "checkbox"
  | "url"
  | "email"
  | "phone"
  | "files"
  | "created_time"
  | "last_edited_time"
  | "created_by"
  | "last_edited_by"
  | "relation"
  | "rollup"
  | "formula"
  | "unique_id"
  | "button"
  | "location"
  | "verification"
  | "last_visited_time"
  | "place";

export interface PlacePropertyValue {
  lat: number;
  lon: number;
  name?: string | null;
  address?: string | null;
  aws_place_id?: string | null;
  google_place_id?: string | null;
}

export interface VerificationDateValue {
  start: string;
  end?: string | null;
  time_zone?: string | null;
}

export type VerificationPropertyValue =
  | { state: "unverified"; date?: null; verified_by?: null }
  | {
      state: "verified" | "expired";
      date?: VerificationDateValue | null;
      verified_by?: unknown;
    };

export type NotionRollupFunction =
  | "count"
  | "count_values"
  | "empty"
  | "not_empty"
  | "unique"
  | "show_unique"
  | "percent_empty"
  | "percent_not_empty"
  | "sum"
  | "average"
  | "median"
  | "min"
  | "max"
  | "range"
  | "earliest_date"
  | "latest_date"
  | "date_range"
  | "checked"
  | "unchecked"
  | "percent_checked"
  | "percent_unchecked"
  | "count_per_group"
  | "percent_per_group"
  | "show_original";

/** Read-only compatibility for rollup configs stored before the current API names. */
export type LegacyRollupFunction = "count_all" | "count_empty" | "count_unique";
export type RollupFunction = NotionRollupFunction | LegacyRollupFunction;

export interface SelectOption {
  id: string;
  name: string;
  color: string; // named color
  description?: string | null;
  group?: string;
  /** A status option with no color chosen: drawn in its group's (its color holds that), none ticked in its colors. */
  colorFromGroup?: boolean;
  /** A status option standing for its whole group ("내 작업"'s own, named as it): a filter ticks the group instead. */
  standsForGroup?: boolean;
}

export interface StatusGroup {
  id: string;
  name: string;
  color: string;
  optionIds: string[];
}

export interface FileAttachment {
  id: string;
  source?: "external";
  fileId?: string;
  key?: string;
  uploadId?: string;
  bucket?: string;
  name: string;
  url: string;
  type?: string;
  contentType?: string;
  size?: number;
  sha256?: string;
  sourceUrl?: string;
  notionFileSource?: string;
  notionFileExpiryTime?: string;
  notionFileCopied?: boolean;
}

export type FileUploadStatus = "pending" | "uploaded" | "deleting" | "deleted" | "expired";

export interface FileUpload extends Timestamped {
  workspaceId: string;
  bucket: string;
  key: string;
  scope: string;
  pageId?: string;
  blockId?: string;
  databaseId?: string;
  propertyId?: string;
  templateId?: string;
  name: string;
  contentType?: string;
  size: number;
  status: FileUploadStatus;
  url?: string;
  createdBy?: string;
  expiresAt?: string | null;
  completedAt?: string | null;
  expiredAt?: string | null;
  deletedAt?: string | null;
  deletedBy?: string;
}

export interface FileStatBucket {
  count: number;
  bytes: number;
}

export interface FileMaintenanceRun extends Timestamped {
  workspaceId: string;
  kind?: string;
  actorId?: string;
  status?: "success" | "partial_failure" | "failed" | string;
  scheduledAt?: string | null;
  startedAt?: string;
  finishedAt?: string;
  scanned?: number;
  expired?: number;
  deletedObjects?: number;
  failedObjects?: number;
  failures?: unknown;
  details?: unknown;
}

export interface FileUsageReport {
  workspaceId?: string;
  organizationId?: string;
  organizationName?: string;
  storageLimitBytes?: number | null;
  workspaceCount?: number;
  generatedAt: string;
  totals: {
    files: number;
    bytes: number;
    activeStorageBytes: number;
    uploadedBytes: number;
    pendingBytes: number;
    deletedBytes: number;
    expiredBytes: number;
  };
  pending: {
    active: number;
    expired: number;
  };
  byStatus: Record<string, FileStatBucket>;
  byScope: Record<string, FileStatBucket>;
  byContentType: Record<string, FileStatBucket>;
  largestUploads: FileUpload[];
  recentUploads: FileUpload[];
  maintenanceRuns: FileMaintenanceRun[];
  byWorkspace?: Array<{
    workspaceId: string;
    name?: string | null;
    domain?: string | null;
    totals: FileUsageReport["totals"];
    pending: FileUsageReport["pending"];
  }>;
  byWorkspaceHasMore?: boolean;
  byWorkspaceNextCursor?: string | null;
}

export type NotificationKind = "comment" | "mention" | "link" | "page_edit" | "system";

export interface NotificationRecord extends Timestamped {
  workspaceId: string;
  userId: string;
  activityKey: string;
  kind: NotificationKind;
  pageId?: string | null;
  blockId?: string | null;
  commentId?: string | null;
  actorId?: string | null;
  title?: string;
  preview?: string;
  target?: string;
  metadata?: Record<string, unknown>;
  occurredAt: string;
  readAt?: string | null;
}

export interface PropertyConfig {
  options?: SelectOption[]; // select / multi_select / status
  statusGroups?: StatusGroup[]; // status only; absent preserves legacy flat Status
  /** A status's default, which its rows read while they set none; "" for none, absent for its first group's first option (statusDefaultValue.ts). */
  defaultOptionId?: string;
  /** A status over the rows of several databases ("내 작업"'s): each row's own database's options, by row (statusRowOptions.ts). */
  rowOptionIds?: Readonly<Record<string, readonly string[]>>;
  /** Such a status's options of the databases a row its view shows is in, which its filter's list opens (statusRowOptions.ts). */
  shownOptionIds?: readonly string[];
  /** A number format: Notion's name, "comma" for "number_with_commas" (numberFormat.ts). */
  numberFormat?: string;
  /** A number's decimal places, 0 to 5; absent shows the number as it is. */
  decimalPlaces?: number;
  /** A number shown as a bar or a ring filling up to `numberDivideBy`; absent shows the number. */
  numberDisplay?: "bar" | "ring";
  /** The bar's or ring's color, a Notion color name; green when absent. */
  numberDisplayColor?: string;
  /** The value a full bar or ring stands for; 100 when absent. */
  numberDivideBy?: number;
  /** False hides the number beside its bar or ring. */
  numberShowValue?: boolean;
  /** Whom a person property notifies when it names them; users by default. */
  notify?: "users_and_groups" | "users" | "none";
  /**
   * A person property that names no one shows its row's creator, as Notion's
   * "기본값: 생성자" (seen 2026-10-10). Read, not stored: turning it off
   * empties those rows again.
   */
  defaultCreator?: boolean;
  /** A date property's format: "full", "short", "month_day_year", "day_month_year", "year_month_day" or "relative". */
  dateFormat?: string;
  /** A date property's time format: "12", "24" or "hidden". */
  timeFormat?: string;
  notionType?: string;
  notionPropertyId?: string; // source Notion property id, when imported from Notion API
  notion?: {
    type?: string;
    number?: {
      format?: string | null;
    };
    date?: Record<string, unknown>;
  } & Record<string, unknown>;
  relationDatabaseId?: string;
  // A relation that holds at most this many pages; a sub-item's Parent item
  // holds one, and picking another page replaces it.
  limit?: number;
  // Two-way (Notion "Show on …") relation: the id of the paired relation
  // property on the related database. Present ⇒ two-way; absent ⇒ one-way.
  relatedPropertyId?: string;
  // A relation to its own database with no paired property that relates both
  // rows (A to B relates B to A), as Notion's made on its screen; one made
  // through Notion's API has none and relates one way.
  relationSymmetric?: boolean;
  rollupRelationPropertyId?: string;
  rollupTargetPropertyId?: string;
  rollupFunction?: RollupFunction;
  /** Set on the browser's copy only (withRollupFilterTargets): the property a rollup of the related values shows, for its filter. */
  rollupFilterTarget?: DbProperty;
  /** The status group a percent per group counts, by id; without one it shows the values, as one from Notion's API does. */
  rollupStatusGroup?: string;
  /** Its relation, property and calculation stay as they were made; only how its numbers show changes. */
  rollupLocked?: boolean;
  formula?: string;
  formulaReferenceAliases?: string[];
  idPrefix?: string; // unique_id property display prefix, e.g. "TASK"
  rollupVia?: string; // optional second relation hop for multi-hop rollups
  hideWhenEmpty?: boolean; // row/page property panel display option
  hideInPagePanel?: boolean; // always hide in row/page property panels unless hidden properties are expanded
  button?: DatabaseButtonActionDocument;
  /** An app that draws and enters the property's cells; the value keeps the property's type. */
  appEditor?: { app: string; editor: string };
  /** A URL's "전체 URL 표시": its values read whole wherever they show (urlDisplay.ts). */
  showFullUrl?: boolean;
}

export interface DatabaseAutomationDefinition {
  id: string;
  workspaceId: string;
  databaseId: string;
  name: string;
  enabled: boolean;
  scopeType: "database" | "view";
  viewId?: string | null;
  triggerType: "events" | "schedule";
  trigger: Record<string, unknown>;
  actionDocument: Record<string, unknown>;
  nextRunAt?: string | null;
  status: "active" | "disabled" | "paused";
  revision: number;
  pausedAt?: string | null;
  pausedReason?: string | null;
  createdBy?: string;
  updatedBy?: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface DbProperty extends Timestamped {
  databaseId: string;
  /** Immutable request identity for exact metadata-create replay. */
  createMutationId?: string;
  /** Server-owned actor bound to the immutable create receipt. */
  createMutationActorId?: string;
  name: string;
  description?: string;
  type: PropertyType;
  config?: PropertyConfig;
  position: number;
  /** What it is to its database's sprints, as the server marks it (GraphProperty). */
  sprintManaged?: string;
  /** Where its API lists it, as the server marks it (GraphProperty, inPublicOrder). */
  publicOrder?: number;
}
