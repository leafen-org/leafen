import {
  productErrorMessage,
  type ProductErrorContext,
} from "@/lib/productErrorPresentation";
import { notionAdminCapabilities } from "@/notionAdminTokenWire.generated";
import type {
  NotionAdminCapability,
  OrganizationAdminToken,
} from "@/lib/types";

export const NOTION_ADMIN_CAPABILITIES: NotionAdminCapability[] = [
  "legal-hold:read",
  "legal-hold:write",
  "legal-hold:write-high-impact",
  "legal-hold:export",
  notionAdminCapabilities.workspaceRead,
  "workspace:export",
  "managed-user-session:write",
  notionAdminCapabilities.mcpClientConnectionRead,
  notionAdminCapabilities.mcpClientConnectionWriteHighImpact,
  notionAdminCapabilities.permissionGroupRead,
  notionAdminCapabilities.permissionGroupWrite,
  notionAdminCapabilities.creditLimitPolicyRead,
  notionAdminCapabilities.creditLimitPolicyWrite,
  notionAdminCapabilities.personalAccessTokenRead,
  notionAdminCapabilities.personalAccessTokenWriteHighImpact,
  "user:read",
];

export interface NotionAdminCreateTokenInput {
  organizationId: string;
  label: string;
  capabilities: NotionAdminCapability[];
  workspaceIds: string[];
  legalHoldIds: string[];
  expiresAt: string | null;
}

export interface NotionAdminTokenMutationResult {
  adminToken?: OrganizationAdminToken;
  adminTokenSecret?: string;
}

export interface NotionAdminApiService {
  listTokens(organizationId: string): Promise<OrganizationAdminToken[]>;
  createToken(
    input: NotionAdminCreateTokenInput,
  ): Promise<NotionAdminTokenMutationResult>;
  revokeToken(input: {
    organizationId: string;
    tokenId: string;
  }): Promise<NotionAdminTokenMutationResult>;
  writeClipboard(value: string): Promise<void>;
}

export interface NotionAdminApiBounds {
  maxTokens: number;
  maxResourceIds: number;
  maxResourceIdCharacters: number;
  maxScalarCharacters: number;
  maxSecretCharacters: number;
  maxTotalCharacters: number;
}

type StringBudget = { characters: number };

function assertBounds(bounds: NotionAdminApiBounds) {
  for (const [name, value] of Object.entries(bounds)) {
    if (!Number.isSafeInteger(value) || value < 0) {
      throw new RangeError(`Notion Admin API ${name} must be a non-negative safe integer.`);
    }
  }
}

function reserveString(
  value: string | null | undefined,
  bounds: NotionAdminApiBounds,
  budget: StringBudget,
) {
  if (value === null || value === undefined) return;
  if (typeof value !== "string" || value.length > bounds.maxScalarCharacters) {
    throw new RangeError("Notion Admin API scalar bound exceeded.");
  }
  budget.characters += value.length;
  if (!Number.isSafeInteger(budget.characters)
    || budget.characters > bounds.maxTotalCharacters) {
    throw new RangeError("Notion Admin API total string bound exceeded.");
  }
}

function assertCapability(value: unknown): asserts value is NotionAdminCapability {
  if (!NOTION_ADMIN_CAPABILITIES.includes(value as NotionAdminCapability)) {
    throw new RangeError("Notion Admin API capability is invalid.");
  }
}

function reserveToken(
  token: OrganizationAdminToken,
  bounds: NotionAdminApiBounds,
  budget: StringBudget,
) {
  reserveString(token.id, bounds, budget);
  reserveString(token.organizationId, bounds, budget);
  reserveString(token.label, bounds, budget);
  reserveString(token.status, bounds, budget);
  reserveString(token.tokenPrefix, bounds, budget);
  reserveString(token.createdAt, bounds, budget);
  reserveString(token.updatedAt, bounds, budget);
  reserveString(token.lastUsedAt, bounds, budget);
  reserveString(token.expiresAt, bounds, budget);
  reserveString(token.revokedAt, bounds, budget);
  const capabilities = token.scopes?.capabilities ?? [];
  if (!Array.isArray(capabilities)
    || capabilities.length > NOTION_ADMIN_CAPABILITIES.length) {
    throw new RangeError("Notion Admin API capability bound exceeded.");
  }
  for (const capability of capabilities) {
    assertCapability(capability);
    reserveString(capability, bounds, budget);
  }
}

export function notionAdminBoundTokens(
  tokens: OrganizationAdminToken[],
  bounds: NotionAdminApiBounds,
) {
  assertBounds(bounds);
  if (!Array.isArray(tokens) || tokens.length > bounds.maxTokens) {
    throw new RangeError("Notion Admin API token bound exceeded.");
  }
  // Each token is held to one request's size, not the list together
  // (2026-10-13: a few hundred tokens filled one 64 KB budget and the
  // panel failed).
  for (const token of tokens) reserveToken(token, bounds, { characters: 0 });
  return tokens;
}

export function notionAdminResourceIds(
  value: string,
  bounds: NotionAdminApiBounds,
) {
  assertBounds(bounds);
  if (typeof value !== "string" || value.length > bounds.maxTotalCharacters) {
    throw new RangeError("Notion Admin API resource input bound exceeded.");
  }
  const ids = Array.from(new Set(
    value.split(/\r?\n|,/).map((item) => item.trim()).filter(Boolean),
  ));
  if (ids.length > bounds.maxResourceIds) {
    throw new RangeError("Notion Admin API resource ID bound exceeded.");
  }
  for (const id of ids) {
    if (id.length > bounds.maxResourceIdCharacters) {
      throw new RangeError("Notion Admin API resource ID scalar bound exceeded.");
    }
  }
  return ids.length ? ids : ["*"];
}

export function notionAdminCreateRequest(
  draft: {
    organizationId: string;
    label: string;
    capabilities: NotionAdminCapability[];
    workspaceIds: string;
    legalHoldIds: string;
    expiresAt: string;
  },
  bounds: NotionAdminApiBounds,
): NotionAdminCreateTokenInput {
  assertBounds(bounds);
  const budget = { characters: 0 };
  const label = draft.label.trim();
  reserveString(draft.organizationId, bounds, budget);
  reserveString(label, bounds, budget);
  if (!label || draft.capabilities.length === 0
    || draft.capabilities.length > NOTION_ADMIN_CAPABILITIES.length) {
    throw new RangeError("Notion Admin API create request is invalid.");
  }
  const capabilities = Array.from(new Set(draft.capabilities));
  if (capabilities.length !== draft.capabilities.length) {
    throw new RangeError("Notion Admin API capabilities must be unique.");
  }
  for (const capability of capabilities) assertCapability(capability);
  const expiresAt = draft.expiresAt
    ? new Date(draft.expiresAt).toISOString()
    : null;
  reserveString(expiresAt, bounds, budget);
  const workspaceIdList = notionAdminResourceIds(draft.workspaceIds, bounds);
  const legalHoldIdList = notionAdminResourceIds(draft.legalHoldIds, bounds);
  for (const id of [...workspaceIdList, ...legalHoldIdList]) {
    reserveString(id, bounds, budget);
  }
  return {
    organizationId: draft.organizationId,
    label,
    capabilities,
    workspaceIds: workspaceIdList,
    legalHoldIds: legalHoldIdList,
    expiresAt,
  };
}

export function notionAdminRevokeRequest(
  organizationId: string,
  tokenId: string,
  bounds: NotionAdminApiBounds,
) {
  assertBounds(bounds);
  const budget = { characters: 0 };
  reserveString(organizationId, bounds, budget);
  reserveString(tokenId, bounds, budget);
  if (!organizationId || !tokenId) {
    throw new RangeError("Notion Admin API revoke request is invalid.");
  }
  return { organizationId, tokenId };
}

export function notionAdminBoundSecret(
  secret: string,
  bounds: NotionAdminApiBounds,
) {
  assertBounds(bounds);
  if (typeof secret !== "string" || secret.length > bounds.maxSecretCharacters) {
    throw new RangeError("Notion Admin API secret bound exceeded.");
  }
  return secret;
}

export const NOTION_ADMIN_API_FAILURE_STAGES = [
  "list",
  "create",
  "revoke",
  "clipboard",
] as const;

export type NotionAdminApiFailureStage =
  typeof NOTION_ADMIN_API_FAILURE_STAGES[number];

const notionAdminFailureContexts = {
  list: "notionAdmin.list",
  create: "notionAdmin.create",
  revoke: "notionAdmin.revoke",
  clipboard: "notionAdmin.clipboard",
} as const satisfies Record<NotionAdminApiFailureStage, ProductErrorContext>;

export function notionAdminFailureMessage(
  error: unknown,
  stage: NotionAdminApiFailureStage,
) {
  return productErrorMessage(error, notionAdminFailureContexts[stage]);
}
