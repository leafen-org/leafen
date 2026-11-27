import type { Session } from "./types";
import {
  notionAdminTokenOperations,
  type NotionAdminTokenCreateInput,
  type NotionAdminTokenListResponse,
  type NotionAdminTokenMutationResponse,
  type NotionAdminTokenRevokeInput,
} from "./notionAdminTokenWire.generated";

type JsonRequest = <T>(
  path: string,
  init?: RequestInit,
  session?: Session | null,
) => Promise<T>;

const organizationPath = (template: string, organizationId: string) => template.replace(
  "{organizationId}",
  encodeURIComponent(organizationId),
);

function scopedPath(template: string, values: Record<string, string>) {
  return Object.entries(values).reduce(
    (path, [name, value]) => path.replace(`{${name}}`, encodeURIComponent(value)),
    template,
  );
}

function pagePath(path: string, cursor?: string) {
  return cursor ? `${path}?cursor=${encodeURIComponent(cursor)}` : path;
}

/**
 * The organization's Notion-compatible Admin API tokens; the organization
 * features' settings have their own client in the organization module.
 */
export function createEnterpriseSettingsApi(request: JsonRequest) {
  return {
    listNotionAdminTokens: (
      session: Session,
      organizationId: string,
      cursor?: string,
      signal?: AbortSignal,
    ) => request<NotionAdminTokenListResponse>(pagePath(organizationPath(
      notionAdminTokenOperations.list.path,
      organizationId,
    ), cursor), { signal }, session),
    createNotionAdminToken: (
      session: Session,
      organizationId: string,
      input: NotionAdminTokenCreateInput,
      signal?: AbortSignal,
    ) => request<NotionAdminTokenMutationResponse>(organizationPath(
      notionAdminTokenOperations.create.path,
      organizationId,
    ), {
      method: notionAdminTokenOperations.create.method,
      body: JSON.stringify(input),
      signal,
    }, session),
    revokeNotionAdminToken: (
      session: Session,
      organizationId: string,
      tokenId: string,
      input: NotionAdminTokenRevokeInput,
      signal?: AbortSignal,
    ) => request<NotionAdminTokenMutationResponse>(scopedPath(
      notionAdminTokenOperations.revoke.path,
      { organizationId, tokenId },
    ), {
      method: notionAdminTokenOperations.revoke.method,
      body: JSON.stringify(input),
      signal,
    }, session),
  };
}
