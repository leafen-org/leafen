import { LeafenApiError, request } from "@/apiTransport";
import type { GraphProperty } from "@/contentGraph";
import type { PropertyAccessLevel } from "@/lib/propertyAccess";
import type { Session } from "@/types";

export type PropertyAccessPrincipalType = "user" | "group" | "person_property" | "integration";

export type PropertyAccessException = { principalType: PropertyAccessPrincipalType; principalId: string; level: Exclude<PropertyAccessLevel, "inherit"> };

/** One way full access to the database comes: a person (or an address), a group, an app, everyone else in the
 *  workspace, its teamspace's members or owners. */
export type PropertyAccessHolder = {
  kind: "person" | "group" | "app" | "workspace" | "teamspace_members" | "teamspace_owners"; id?: string; name?: string;
};

/** A rule as read says who has full access to its database (fullAccess), whom its fixed first exception always lets edit it. */
export type PropertyAccessRule = {
  dataSourceId: string; propertyId: string; defaultLevel: PropertyAccessLevel; exceptions: PropertyAccessException[];
  fullAccess?: { holders: PropertyAccessHolder[]; more: number };
};

/** Why the person gets the level, as Notion's preview says it under the level (PropertyAccessPreviewResult). */
export type PropertyAccessPreviewReason = "noDatabaseAccess" | "fullAccess" | "personException" | "groupException" | "defaultAccess" | "inherited";

/** A person property's exception that gives more on the pages that name the person, or that they made. */
export type PropertyAccessCondition = { level: Exclude<PropertyAccessLevel, "inherit">; propertyName?: string; pageCreator?: boolean };

export type PropertyAccessPreview = {
  userId: string; level: Exclude<PropertyAccessLevel, "inherit">; inherited: boolean;
  reason: PropertyAccessPreviewReason; groupName?: string;
  /** How the database gave the level, where it did. */
  via?: "teamspace" | "group" | "workspace" | "database"; viaGroupName?: string;
  /** The database gives the person only commenting. */
  comment?: boolean;
  conditions?: PropertyAccessCondition[];
};

const path = (dataSourceId: string, propertyId: string) =>
  `/data-sources/${encodeURIComponent(dataSourceId)}/properties/${encodeURIComponent(propertyId)}/access`;

export function readPropertyAccess(session: Session, dataSourceId: string, propertyId: string, signal?: AbortSignal) {
  return request<PropertyAccessRule>(path(dataSourceId, propertyId), { signal }, session);
}

export function savePropertyAccess(session: Session, rule: PropertyAccessRule) {
  return request<PropertyAccessRule>(path(rule.dataSourceId, rule.propertyId), {
    method: "PUT", body: JSON.stringify({ defaultLevel: rule.defaultLevel, exceptions: rule.exceptions }),
  }, session);
}

export function previewPropertyAccess(session: Session, rule: PropertyAccessRule, userId: string, signal?: AbortSignal) {
  return request<PropertyAccessPreview>(`${path(rule.dataSourceId, rule.propertyId)}/preview`, {
    method: "POST", body: JSON.stringify({ userId, rule: { defaultLevel: rule.defaultLevel, exceptions: rule.exceptions } }), signal,
  }, session);
}

export type PagePropertyAccessProperty = { dataSourceId: string; propertyId: string; name: string; type: GraphProperty["type"]; icon?: string };

/** A database page's properties that take a rule and their rules, for its share window's "고급". */
export type PagePropertyAccess = {
  properties: PagePropertyAccessProperty[];
  rules: { dataSourceId: string; propertyId: string; defaultLevel: PropertyAccessLevel }[];
};

export function readPagePropertyAccess(session: Session, pageId: string, signal?: AbortSignal) {
  return request<PagePropertyAccess>(`/pages/${encodeURIComponent(pageId)}/property-access`, { signal }, session);
}

export type PropertyAccessGroup = { id: string; name: string };
export type PropertyAccessIntegration = { userId: string; name: string };

/** The workspace's groups and its apps' accounts, which exceptions may name; none where the reader may not list them. */
export async function loadPropertyAccessPrincipals(session: Session, workspaceId: string, signal?: AbortSignal) {
  const workspace = encodeURIComponent(workspaceId);
  // Only a refusal reads as none; any other failure is the caller's to say.
  const noneIfRefused = <T,>(none: T) => (failure: unknown) => {
    if (failure instanceof LeafenApiError && (failure.status === 403 || failure.status === 404)) return none;
    throw failure;
  };
  const [groups, apps] = await Promise.all([
    request<{ groups: PropertyAccessGroup[] }>(`/workspaces/${workspace}/groups`, { signal }, session)
      .catch(noneIfRefused<{ groups: PropertyAccessGroup[] }>({ groups: [] })),
    request<{ apps: PropertyAccessIntegration[] }>(`/workspaces/${workspace}/apps`, { signal }, session)
      .catch(noneIfRefused<{ apps: PropertyAccessIntegration[] }>({ apps: [] })),
  ]);
  return { groups: groups.groups ?? [], integrations: (apps.apps ?? []).map((app) => ({ userId: app.userId, name: app.name })) };
}
