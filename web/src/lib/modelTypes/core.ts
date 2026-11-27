/**
 * Presentation-model primitives required by the retained UI declarations.
 * Native generated wire modules, not these compatibility types, own transport.
 */
export type FormulaReference = {
  kind: "page" | "person";
  id: string;
  name: string;
};

export type FormulaValue = string | number | boolean | null | FormulaReference | FormulaValue[];

export type NotionChartAggregate =
  | "count"
  | "count_values"
  | "sum"
  | "average"
  | "median"
  | "min"
  | "max"
  | "range"
  | "unique"
  | "empty"
  | "not_empty"
  | "percent_empty"
  | "percent_not_empty"
  | "checked"
  | "unchecked"
  | "percent_checked"
  | "percent_unchecked"
  | "earliest_date"
  | "latest_date"
  | "date_range";

export type RelativeDateFilterUnit = "day" | "week" | "month" | "year";
export type RelativeDateFilterValue =
  | {
      direction: "past" | "next";
      count: number;
      unit: RelativeDateFilterUnit;
    }
  | {
      direction: "this";
      unit: RelativeDateFilterUnit;
    };

export interface Timestamped {
  id: string;
  createdAt?: string;
  updatedAt?: string;
}
export type WorkspaceCreationPolicy = "owners_admins" | "members";
export type OrganizationMemberRole =
  | "owner"
  | "admin"
  | "security_admin"
  | "billing_admin"
  | "member"
  | "guest";
export type OrganizationSharingPolicyKey =
  | "publicWebSharing"
  | "externalEmailSharing"
  | "guestAccess"
  | "exports";
export type OrganizationSharingPolicy = Partial<Record<OrganizationSharingPolicyKey, boolean>>;
