import { createPlaceSearchApi } from "./placeSearchApi";
import { createPreviewApi } from "./previewApi";
import { createNativePageApi } from "./nativePageApi";
import type { Bootstrap, Session } from "./types";
import type { WorkspaceSearchContentState } from "./lib/pageArchive";
import {
  graphWireBounds,
  graphWireErrorCodes,
  type GraphReadResponse,
} from "./contentGraph";
import {
  workspaceMembershipErrorCodes,
  workspaceMembershipOperations,
  type PageFavoriteMutationInput,
  type PageFavoriteMutationResponse,
  type DatabaseFavoriteMutationResponse,
} from "./workspaceMembershipWire.generated";
import {
  workspaceSearchBounds,
  workspaceSearchErrorCodes,
  type WorkspaceSearchHighlightsResponse,
  type WorkspaceSearchResponse,
} from "./workspaceSearchWire.generated";
import {
  databaseQueryOperations,
  type DatabaseDependencyEdgeRequest,
  type DatabaseDependencyEdgeResponse,
  type DatabaseDependencyGraphRequest,
  type DatabaseDependencyGraphResponse,
} from "./databaseQueryWire.generated";
import {
  organizationOperations,
  type OrganizationList,
  type OrganizationSharingSecurityPolicy,
  type OrganizationWorkspaceList,
  type OrganizationSharingSecurityPolicyUpdateInput,
  type OrganizationSharingSecurityPolicyUpdateResponse,
  type OrganizationStorageLimitUpdateInput,
  type OrganizationStorageLimitUpdateResponse,
} from "./organizationWire.generated";
import { mcpOAuthOperations, type MCPAuthorizationApprovalInput, type MCPAuthorizationConsent, type MCPAuthorizationRedirect } from "./mcpOAuth";
import type {
  PublicContentSearchRequest,
  PublicContentCheckRequest,
  PublicContentCheckResult,
  PublicContentDiscussionsRequest,
  PublicContentDiscussionsResult,
  PublicContentSearchResult,
  PublicContentSnapshotRequest,
  PublicContentSnapshotResult,
} from "./publicContentWire.generated";
import {
  pageDuplicationOperations,
  type PageDuplicationInput,
  type PageDuplicationResponse,
} from "./pageDuplicationWire.generated";
import {
  blockMoveOperations,
  type BlockMoveInput,
  type BlockMoveResponse,
} from "./blockMoveWire.generated";
import {
  fileTransferOperations,
  type FileDownloadCapability,
} from "./fileTransferWire.generated";
import type { PublicRuntimeConfig } from "./lib/runtimeConfigContract";
import {
  collaborationStorageOperation,
  type CollaborationStorageStatusResponse,
} from "./collaborationStorageWire.generated";
import {
  request,
  requestBinaryResponse,
  throwLeafenApiError,
} from "./apiTransport";
import { createWorkspaceExportApi } from "./workspaceExportApi";
import { createInstanceProductExportApi } from "./instanceProductExportApi";
import { createAdministratorApi } from "./administratorApi";
import { createWorkspaceMoveApi } from "./workspaceMoveApi";
import { createFormApi } from "./formApi";
import { createAccessRequestApi } from "./accessRequestApi";
import { createWorkspaceImportApi } from "./workspaceImportApi";
import { createImportedPeopleMappingApi } from "./importedPeopleMappingApi";
import { createWorkspaceMembershipApi } from "./workspaceMembershipApi";
import { createPasskeyApi } from "./passkeyApi";
import { createAccountApi } from "./accountApi";
import { createNotificationSettingsApi } from "./notificationSettingsApi";
import { createNotionImportApi } from "./notionImportApi";
import { createNotionMigrationApi } from "./notionMigrationApi";
import { createBundleSourcesApi } from "./bundleSourcesApi";
import { createPageAnalyticsApi } from "./pageAnalyticsApi";
import { createPageLockApi } from "./pageLockApi";
import { createDatabaseAutomationApi } from "./databaseAutomationApi";
import { createInteractiveAutomationApi } from "./interactiveAutomationApi";
import { createDatabaseMutationApi } from "./databaseMutationApi";
import { createDatabasePropertyConversionApi } from "./databasePropertyConversionApi";
import { createDatabaseDataSourceApi } from "./databaseDataSourceApi";
import { createWikiApi } from "./wikiApi";
import { createTrashApi } from "./trashApi";
import { createOrganizationPeopleApi } from "./organizationPeopleApi";
import { createCommentApi } from "./commentApi";
import { createNotificationApi } from "./notificationApi";
import { createFileUsageReportApi } from "./fileUsageReportApi";
import { createEnterpriseSettingsApi } from "./enterpriseSettingsApi";
import { createMCPConnectionListApi } from "./mcpConnectionListApi";
import type { DatabaseContainerPickerResponse } from "./lib/databaseContainerPicker";
import type { RowPageMentionCandidate } from "./components/editor/blockPickerModel";
import { publicMutationService } from "./lib/publicMutationService";

export { LeafenApiError, LeafenTransportError } from "./apiTransport";

const {
  createWorkspaceImport,
  createWorkspaceImportBatch,
  declareWorkspaceImportBatchItems,
  workspaceImportBatchItems,
  sendWorkspaceImportBatchDocuments,
  startWorkspaceImportBatch,
  appendMarkdownToPage,
  csvMergeTarget,
  csvMergeTargets,
  mergeCSVIntoDatabase,
  replaceMarkdownPage,
  createWorkspaceArchiveImport,
  archiveUploadPrepare,
  archiveUploadPart,
  archiveUploadComplete,
  archiveUploadCancel,
  workspaceImports,
  workspaceImportJob,
  cancelWorkspaceImport,
} = createWorkspaceImportApi(request);
const {
  updatePageFavorite: pageFavoriteUpdate,
  updateDatabaseFavorite: databaseFavoriteUpdate,
} = workspaceMembershipOperations;

export type SyncedBlockLocation = { blockId: string; pageId: string };
export type SyncedBlockLocations = {
  original: SyncedBlockLocation;
  copies: SyncedBlockLocation[];
  hasMore: boolean;
  /** Reads the copies after these, when there are more. */
  nextAfter?: string;
};

export const api = {
  /** Puts an uploaded file in the empty file block it was sent for, when no screen shows that block (page_file_attach.go). */
  attachPageBlockFile: (session: Session, pageId: string, blockId: string, fileId: string) => request<unknown>(
    `/pages/${encodeURIComponent(pageId)}/blocks/${encodeURIComponent(blockId)}/file-attach`,
    { method: "PUT", body: JSON.stringify({ fileId }) }, session,
  ),
  ...createNativePageApi(request),
 ...createPlaceSearchApi(request),
  ...createPreviewApi(request),
  ...createAccountApi(),
  ...createPasskeyApi(),
  ...createNotificationSettingsApi(),
  ...createPageAnalyticsApi(),
  ...createDatabaseAutomationApi(request),
  ...createInteractiveAutomationApi(request),
  ...createDatabaseMutationApi(request),
  ...createDatabasePropertyConversionApi(request),
  ...createDatabaseDataSourceApi(request),
  ...createWikiApi(request),
  ...createTrashApi(request),
  ...createOrganizationPeopleApi(request),
  ...createCommentApi(request),
  ...createNotificationApi(request),
  ...createFileUsageReportApi(request),
  ...createEnterpriseSettingsApi(request),
  ...createMCPConnectionListApi(request),
  ...createWorkspaceMembershipApi(),
  ...createImportedPeopleMappingApi(request),
  ...createFormApi(request),
  ...createAccessRequestApi(request),
  ...createWorkspaceExportApi(request, requestBinaryResponse),
  ...createInstanceProductExportApi(request, requestBinaryResponse),
  ...createAdministratorApi(request, requestBinaryResponse),
  ...createWorkspaceMoveApi(request),
  publicMutationState: publicMutationService.state,
  mutatePublicVisibility: publicMutationService.mutate,
  createWorkspaceImport,
  createWorkspaceImportBatch,
  declareWorkspaceImportBatchItems,
  workspaceImportBatchItems,
  sendWorkspaceImportBatchDocuments,
  startWorkspaceImportBatch,
  appendMarkdownToPage,
  csvMergeTarget,
  csvMergeTargets,
  mergeCSVIntoDatabase,
  replaceMarkdownPage,
  createWorkspaceArchiveImport,
  archiveUploadPrepare,
  archiveUploadPart,
  archiveUploadComplete,
  archiveUploadCancel,
  workspaceImports,
  workspaceImportJob,
  cancelWorkspaceImport,
  runtimeConfig: () => request<PublicRuntimeConfig>("/runtime-config"),
  async bootstrap(session: Session, cursor: string | null = null, signal?: AbortSignal,
    limits: { pageLimit?: number } = {}): Promise<Bootstrap> {
    const query = new URLSearchParams();
    if (cursor) query.set("cursor", cursor);
    if (limits.pageLimit) query.set("pageLimit", String(limits.pageLimit));
    return request<Bootstrap>(`/bootstrap${query.size ? `?${query.toString()}` : ""}`, { signal }, session);
  },
  /** The pages of a workspace made at or after a moment, ahead of the whole list (bootstrap_new_pages.go). */
  bootstrapNewPages: (session: Session, workspaceId: string, createdSince: string, signal?: AbortSignal) =>
    request<Bootstrap>(`/bootstrap?${new URLSearchParams({ workspaceId, createdSince }).toString()}`, { signal }, session),
  setPageFavorite: (
    session: Session,
    pageId: string,
    input: PageFavoriteMutationInput,
    signal?: AbortSignal,
  ) => request<PageFavoriteMutationResponse>(
    pageFavoriteUpdate.path.replace(
      "{pageId}",
      encodeURIComponent(pageId),
    ),
    {
      method: pageFavoriteUpdate.method,
      body: JSON.stringify(input),
      signal,
    },
    session,
  ),
  setDatabaseFavorite: (
    session: Session,
    containerId: string,
    input: PageFavoriteMutationInput,
    signal?: AbortSignal,
  ) => request<DatabaseFavoriteMutationResponse>(
    databaseFavoriteUpdate.path.replace("{containerId}", encodeURIComponent(containerId)),
    { method: databaseFavoriteUpdate.method, body: JSON.stringify(input), signal },
    session,
  ),
  collaborationStorageStatus: (session: Session, room: string, signal?: AbortSignal) => request<CollaborationStorageStatusResponse>(
    collaborationStorageOperation.path.replace("{room}", encodeURIComponent(room)),
    { method: collaborationStorageOperation.method, signal },
    session,
  ),
  /** What a page's link blocks point at, for the person reading. */
  readPageLinkTargets: (
    session: Session,
    pageId: string,
    targets: { kind: "page" | "database" | "comment"; id: string }[],
  ) => request<{ targets: import("./lib/pageLinkTargets").PageLinkTarget[] }>(
    `/pages/${encodeURIComponent(pageId)}/link-targets`,
    { method: "POST", body: JSON.stringify({ targets }) },
    session,
  ),
  /** The server's stored collaboration state of a page, as a Yjs state vector. */
  collaborationStateVector: (session: Session, room: string) => request<{ stateVector: string }>(
    `/collaboration-state/${encodeURIComponent(room)}`,
    { method: "GET" },
    session,
  ),
  /** Whether the server's stored copy of a page lacks anything in this Yjs update: text or deletions. */
  collaborationStateLacks: (session: Session, room: string, update: Uint8Array) => request<{ lacks: boolean }>(
    `/collaboration-state/${encodeURIComponent(room)}/lacks`,
    { method: "POST", body: update as BodyInit, headers: { "content-type": "application/octet-stream" } },
    session,
  ),
  async listOrganizations(session: Session, cursor: string | null = null): Promise<OrganizationList> {
    const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
    return request<OrganizationList>(`${organizationOperations.list.path}${query}`, {}, session);
  },
  updateOrganizationStorageLimit: (
    session: Session,
    organizationId: string,
    input: OrganizationStorageLimitUpdateInput,
    signal?: AbortSignal,
  ) => request<OrganizationStorageLimitUpdateResponse>(
    organizationOperations.updateStorageLimit.path.replace(
      "{organizationId}", encodeURIComponent(organizationId),
    ),
    { method: organizationOperations.updateStorageLimit.method, body: JSON.stringify(input), signal },
    session,
  ),
  /** The organization's workspaces, for an admin choosing among them. */
  listOrganizationWorkspaces: (
    session: Session,
    organizationId: string,
    signal?: AbortSignal,
  ) => request<OrganizationWorkspaceList>(
    organizationOperations.listWorkspaces.path.replace("{organizationId}", encodeURIComponent(organizationId)),
    { method: organizationOperations.listWorkspaces.method, signal },
    session,
  ),
  readOrganizationSharingSecurityPolicy: (
    session: Session,
    organizationId: string,
    signal?: AbortSignal,
  ) => request<OrganizationSharingSecurityPolicy>(
    organizationOperations.readSharingSecurityPolicy.path.replace(
      "{organizationId}", encodeURIComponent(organizationId),
    ),
    { method: organizationOperations.readSharingSecurityPolicy.method, signal },
    session,
  ),
  updateSharingSecurityPolicy: (
    session: Session,
    organizationId: string,
    input: OrganizationSharingSecurityPolicyUpdateInput,
    signal?: AbortSignal,
  ) => request<OrganizationSharingSecurityPolicyUpdateResponse>(
    organizationOperations.updateSharingSecurityPolicy.path.replace(
      "{organizationId}", encodeURIComponent(organizationId),
    ),
    { method: organizationOperations.updateSharingSecurityPolicy.method, body: JSON.stringify(input), signal },
    session,
  ),
  startPageDuplication: (
    session: Session,
    pageId: string,
    input: PageDuplicationInput,
    signal?: AbortSignal,
  ) => request<PageDuplicationResponse>(
    pageDuplicationOperations.startPageDuplication.path.replace("{pageId}", encodeURIComponent(pageId)),
    { method: pageDuplicationOperations.startPageDuplication.method, body: JSON.stringify(input), signal },
    session,
  ),
  readPageDuplication: (
    session: Session,
    jobId: string,
    signal?: AbortSignal,
  ) => request<PageDuplicationResponse>(
    pageDuplicationOperations.readPageDuplication.path.replace("{jobId}", encodeURIComponent(jobId)),
    { method: pageDuplicationOperations.readPageDuplication.method, signal },
    session,
  ),
  startBlockMove: (
    session: Session,
    sourcePageId: string,
    blockId: string,
    input: BlockMoveInput,
    signal?: AbortSignal,
  ) => request<BlockMoveResponse>(
    blockMoveOperations.startBlockMove.path
      .replace("{sourcePageId}", encodeURIComponent(sourcePageId))
      .replace("{blockId}", encodeURIComponent(blockId)),
    { method: blockMoveOperations.startBlockMove.method, body: JSON.stringify(input), signal },
    session,
  ),
  readBlockMove: (
    session: Session,
    jobId: string,
    signal?: AbortSignal,
  ) => request<BlockMoveResponse>(
    blockMoveOperations.readBlockMove.path.replace("{jobId}", encodeURIComponent(jobId)),
    { method: blockMoveOperations.readBlockMove.method, signal },
    session,
  ),
  unsyncSyncedBlock: (session: Session, pageId: string, blockId: string) => request<{ blockIds: string[]; graphRevision: number }>(
    `/pages/${encodeURIComponent(pageId)}/synced-blocks/${encodeURIComponent(blockId)}/unsync`,
    { method: "POST", body: "{}" },
    session,
  ),
  /** Where a synced block's original and its copies are, on pages the reader can open. */
  syncedBlockLocations: (session: Session, pageId: string, blockId: string, after?: string) => request<SyncedBlockLocations>(
    `/pages/${encodeURIComponent(pageId)}/synced-blocks/${encodeURIComponent(blockId)}/locations${after ? `?after=${encodeURIComponent(after)}` : ""}`,
    {},
    session,
  ),
  /** What stays with a full-page database's page as its database turns inline. */
  databasePageLeftovers: (session: Session, pageId: string) => request<{ shares: number; comments: number }>(
    `/pages/${encodeURIComponent(pageId)}/database-leftovers`,
    {},
    session,
  ),
  /**
   * A window of a page's graph; `rows: false` reads it without its databases'
   * rows, `text: true` with its text blocks' words, which otherwise come with
   * the page's collaborative document; `words: true` gives the first window
   * the page's words at its moment. `since` asks for what the page's
   * changes after that position changed, its blocks among them, which comes
   * when they say (changedSince), or else the first window.
   */
  async contentGraph(
    session: Session,
    pageId: string,
    cursor: string | null = null,
    options: { rows?: false; text?: true; since?: number; words?: true; signal?: AbortSignal } = {},
  ): Promise<GraphReadResponse> {
    const since: Record<string, string> = options.since === undefined ? {} : { since: String(options.since), blocks: "page" };
    const query = new URLSearchParams({ limit: String(graphWireBounds.readItems), ...since });
    if (cursor) query.set("cursor", cursor);
    if (options.rows === false) query.set("rows", "none");
    if (options.text) query.set("text", "blocks");
    if (options.words) query.set("words", "page");
    return request<GraphReadResponse>(
      `/pages/${encodeURIComponent(pageId)}/content-graph?${query}`,
      options.signal ? { signal: options.signal } : {},
      session,
    );
  },
  databaseDependencyEdges: (
    session: Session,
    pageId: string,
    input: DatabaseDependencyEdgeRequest,
  ) => request<DatabaseDependencyEdgeResponse>(
    databaseQueryOperations.dependencyEdges.path.replace("{pageId}", encodeURIComponent(pageId)),
    { method: databaseQueryOperations.dependencyEdges.method, body: JSON.stringify(input) },
    session,
  ),
  databaseDependencyGraph: (
    session: Session,
    pageId: string,
    input: DatabaseDependencyGraphRequest,
    signal?: AbortSignal,
  ) => request<DatabaseDependencyGraphResponse>(
    databaseQueryOperations.dependencyGraph.path.replace("{pageId}", encodeURIComponent(pageId)),
    { method: databaseQueryOperations.dependencyGraph.method, body: JSON.stringify(input), signal },
    session,
  ),
  publicContentSnapshot: (
    input: PublicContentSnapshotRequest,
    // A view link made with a secret opens with it, in a header.
    options: { signal?: AbortSignal; viewSecret?: string } = {},
  ) => request<PublicContentSnapshotResult>(
    "/public-content",
    {
      method: "POST", body: JSON.stringify(input), signal: options.signal,
      ...(options.viewSecret ? { headers: { "Leafen-View-Secret": options.viewSecret } } : {}),
    },
  ),
  checkPublicContent: (
    input: PublicContentCheckRequest,
    options: { signal?: AbortSignal; viewSecret?: string } = {},
  ) => request<PublicContentCheckResult>(
    "/public-content",
    {
      method: "POST", body: JSON.stringify(input), signal: options.signal,
      ...(options.viewSecret ? { headers: { "Leafen-View-Secret": options.viewSecret } } : {}),
    },
  ),
  publicContentDiscussions: (
    input: PublicContentDiscussionsRequest,
    options: { signal?: AbortSignal } = {},
  ) => request<PublicContentDiscussionsResult>(
    "/public-content",
    { method: "POST", body: JSON.stringify(input), signal: options.signal },
  ),
  searchPublicSiteContent: (
    input: PublicContentSearchRequest,
    options: { signal?: AbortSignal } = {},
  ) => request<PublicContentSearchResult>(
    "/public-content",
    { method: "POST", body: JSON.stringify(input), signal: options.signal },
  ),
  /** Where a page's or a database's public ID opens, the view `view` names (by its public ID or the app's) named the app's way. */
  resolvePageLink: (session: Session, id: string, signal?: AbortSignal, view?: string | null) => request<{ path: string; holderPageId?: string; databasePageId?: string }>(
    `/page-links/${encodeURIComponent(id)}${view ? `?${new URLSearchParams({ v: view })}` : ""}`,
    { method: "GET", signal },
    session,
  ),
  managedFileCapability: (session: Session, fileId: string) => request<FileDownloadCapability>(
    fileTransferOperations.issueCapability.path.replace("{id}", encodeURIComponent(fileId)),
    { method: fileTransferOperations.issueCapability.method },
    session,
  ),
  searchWorkspace: async (
    session: Session,
    workspaceId: string,
    queryValue: string,
    options: {
      cursor?: string | null;
      indexCursor?: string | null;
      limit?: number;
      contentState?: WorkspaceSearchContentState;
      /** Title only, created by, teamspace, in, date and sort, as parameters. */
      filterParams?: Record<string, string>;
      signal?: AbortSignal;
    } = {},
  ): Promise<WorkspaceSearchResponse> => {
    const query = new URLSearchParams({
      q: queryValue,
      limit: String(options.limit ?? workspaceSearchBounds.defaultResults),
    });
    if (options.cursor) query.set("cursor", options.cursor);
    if (options.indexCursor) query.set("indexCursor", options.indexCursor);
    if (options.contentState && options.contentState !== "active") query.set("contentState", options.contentState);
    for (const [name, value] of Object.entries(options.filterParams ?? {})) query.set(name, value);
    return request<WorkspaceSearchResponse>(
      `/workspaces/${encodeURIComponent(workspaceId)}/search?${query}`,
      { signal: options.signal },
      session,
    );
  },
  /** The lines of a found page holding the keywords, for the highlights beside search results: those beyond the line its row shows. */
  readWorkspaceSearchHighlights: (
    session: Session,
    workspaceId: string,
    pageId: string,
    queryValue: string,
    options: { signal?: AbortSignal; line?: string } = {},
  ) => request<WorkspaceSearchHighlightsResponse>(
    `/workspaces/${encodeURIComponent(workspaceId)}/search/highlights?${new URLSearchParams({ pageId, q: queryValue, ...(options.line ? { line: options.line } : {}) })}`,
    { signal: options.signal },
    session,
  ),
  searchRowPageMentions: (
    session: Session,
    workspaceId: string,
    queryValue: string,
    options: { signal?: AbortSignal; limit?: number } = {},
  ) => request<{ items: RowPageMentionCandidate[] }>(
    `/workspaces/${encodeURIComponent(workspaceId)}/row-page-mentions?${new URLSearchParams({ q: queryValue, ...(options.limit ? { limit: String(options.limit) } : {}) })}`,
    { signal: options.signal },
    session,
  ),
  // The workspace's forms the person can read, for an open action to choose (form_view_picker.go).
  formViewPicker: (session: Session, workspaceId: string, queryValue: string, options: { limit?: number; signal?: AbortSignal } = {}) =>
    request<{ items: Array<{ viewId: string; name: string; dataSourceId: string; pageId: string; pageTitle: string; pageKind?: string; viewBlock?: boolean; sourceTitles?: import("@/components/editor/databaseTitles").DatabaseSourceTitle[]; containerId?: string }> }>(
      `/workspaces/${encodeURIComponent(workspaceId)}/form-views?${new URLSearchParams({ q: queryValue, limit: String(options.limit ?? 25) })}`,
      { signal: options.signal },
      session,
    ),
  databaseContainerSources: (
    session: Session,
    workspaceId: string,
    queryValue: string,
    // `first`: sources to list before the rest, as a block's own under "추천"; `addsRows`: only those rows may be added to (no People).
    options: { cursor?: string; limit?: number; signal?: AbortSignal; first?: readonly string[]; addsRows?: boolean } = {},
  ) => {
    const query = new URLSearchParams({
      q: queryValue,
      limit: String(options.limit ?? 25), ...(options.addsRows ? { adds: "rows" } : {}),
    });
    if (options.cursor) query.set("cursor", options.cursor);
    if (options.first?.length) query.set("first", options.first.join(","));
    return request<DatabaseContainerPickerResponse>(
      `/workspaces/${encodeURIComponent(workspaceId)}/database-containers?${query}`,
      { signal: options.signal },
      session,
    );
  },
  ...createNotionImportApi(),
  ...createNotionMigrationApi(),
  ...createBundleSourcesApi(),
  ...createPageLockApi(),
  mcpAuthorizationConsent: (
    session: Session,
    authorizationRequest: string,
    cursor = "",
    options: { signal?: AbortSignal } = {},
  ) => request<MCPAuthorizationConsent>(
    `${mcpOAuthOperations.readConsent.path.replace("{request}", encodeURIComponent(authorizationRequest))}${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`,
    { signal: options.signal },
    session,
  ),
  approveMCPAuthorization: (
    session: Session,
    authorizationRequest: string,
    workspaceIds: string[],
    options: { signal?: AbortSignal; integrationRole?: MCPAuthorizationApprovalInput["integrationRole"]; botId?: string } = {},
  ) => request<MCPAuthorizationRedirect>(
    mcpOAuthOperations.approveConsent.path.replace("{request}", encodeURIComponent(authorizationRequest)),
    {
      method: mcpOAuthOperations.approveConsent.method,
      body: JSON.stringify({ workspaceIds, integrationRole: options.integrationRole,
        ...(options.botId ? { botId: options.botId } : {}) } satisfies MCPAuthorizationApprovalInput),
      signal: options.signal,
    },
    session,
  ),
  denyMCPAuthorization: (
    session: Session,
    authorizationRequest: string,
    options: { signal?: AbortSignal } = {},
  ) => request<MCPAuthorizationRedirect>(
    mcpOAuthOperations.denyConsent.path.replace("{request}", encodeURIComponent(authorizationRequest)),
    { method: mcpOAuthOperations.denyConsent.method, body: JSON.stringify({}), signal: options.signal },
    session,
  ),
};

export function throwInvalidBootstrapContinuation(): never {
  return throwLeafenApiError(
    workspaceMembershipErrorCodes.bootstrapCursorInvalid,
    500,
    "The bootstrap continuation repeated a position.",
    { stage: "bootstrap-pagination" },
  );
}

export function throwInvalidContentGraphContinuation(): never {
  return throwLeafenApiError(
    graphWireErrorCodes.cursorInvalid,
    500,
    "The content graph continuation repeated a position.",
    { stage: "content-graph-pagination" },
  );
}

export { workspaceSearchBounds, workspaceSearchErrorCodes };
export type * from "./workspaceExportWire.generated";
export type * from "./workspaceImportWire.generated";
