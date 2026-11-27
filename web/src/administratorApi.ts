import type {
  AdministratorAccountCleanup,
  AdministratorAccountCleanupAssignmentInput,
  AdministratorAccountCleanupCommitInput,
  AdministratorAccountCleanupStartInput,
  AdministratorAccountDevices,
  AdministratorAccountDirectory,
  AdministratorAccountInput,
  AdministratorAccountMutation,
  AdministratorAccountMutationInput,
  AdministratorAccountPasswordInput,
  AdministratorAccountRoleInput,
  AdministratorAccountStateInput,
  AdministratorAuditLog,
  AdministratorBackupCurrentResult,
  AdministratorBackupInput,
  AdministratorBackupJobResult,
  AdministratorInstanceReportCurrentResult,
  AdministratorInstanceReportInput,
  AdministratorInstanceReportResult,
  AdministratorReplicaResult,
  AdministratorServerMoveIn,
  AdministratorServerMoveOut,
  AdministratorStandbyCode,
  AdministratorBackupRecoveryCode,
  AdministratorScheduledBackupInput,
  AdministratorScheduledBackupResult,
  AdministratorNetworkProxy,
  AdministratorNetworkProxyInput,
  AdministratorRequestLog,
  AdministratorRequestLogInput,
  AdministratorScheduledBackupSnapshotList,
  AdministratorSession,
  AdministratorSettings,
  AdministratorSettingsInput,
  AdministratorSMTPTestInput,
  AdministratorSMTPTestResult,
  AdministratorServerWorkspaceList,
  AdministratorSetupInput,
  AdministratorSetupStatus,
} from "./administratorWire.generated";
import type { ServerVersion } from "@/components/admin/AdministratorServerBuild";
import type {
  SetupRestoreAgentBegun, SetupRestoreAgentOutside, SetupRestoreAgentSecrets, SetupRestoreAgentState,
  SetupRestoreChoice, SetupRestorePlace, SetupRestorePlaceInput, SetupRestoreProgress, SetupRestoreResult,
} from "./lib/setupRestore";
import type { AccountDeviceCode } from "./authWire.generated";
import { newId } from "./lib/ids";
import type { Session } from "./types";

type JsonRequest = <T>(
  path: string,
  init?: RequestInit,
  session?: Session | null,
) => Promise<T>;

type ResponseRequest = (
  path: string,
  session: Session,
  init?: RequestInit,
) => Promise<Response>;

const administratorBackupPath = "/admin/backups";
const administratorReportPath = "/admin/reports";
const administratorAccountsPath = "/admin/accounts";
const administratorBackupJobPath = (jobId: string) =>
  `${administratorBackupPath}/${encodeURIComponent(jobId)}`;

export function createAdministratorApi(request: JsonRequest, requestBinaryResponse: ResponseRequest) {
  return {
    setupStatus: () => request<AdministratorSetupStatus>("/setup/status"),
    serverVersion: (session: Session, signal?: AbortSignal) => request<ServerVersion>("/admin/server-version", { signal }, session),
    claimAdministrator: (input: AdministratorSetupInput) => request<AdministratorSession>(
      "/setup/claim", { method: "POST", body: JSON.stringify(input) },
    ),
    setupRestorePlace: (input: SetupRestorePlaceInput, signal?: AbortSignal) => request<SetupRestorePlace>(
      "/setup/restore/place", { method: "POST", body: JSON.stringify(input), signal },
    ),
    setupRestore: (input: SetupRestorePlaceInput & { choice: SetupRestoreChoice }) => request<SetupRestoreResult>(
      "/setup/restore", { method: "POST", body: JSON.stringify(input) },
    ),
    setupRestoreProgress: (watch: string, signal?: AbortSignal) => request<SetupRestoreProgress>(
      "/setup/restore/progress", { method: "POST", body: JSON.stringify({ watch }), signal },
    ),
    setupRestoreSignIn: (pass: string) => request<AdministratorSession>(
      "/setup/restore/sign-in", { method: "POST", body: JSON.stringify({ pass }) },
    ),
    beginSetupRestoreAgent: (setupCode: string, outside: SetupRestoreAgentOutside) => request<SetupRestoreAgentBegun>(
      "/setup/restore/agent", { method: "POST", body: JSON.stringify({ setupCode, outside }) },
    ),
    setupRestoreAgentState: (setupCode: string, signal?: AbortSignal) => request<SetupRestoreAgentState>(
      "/setup/restore/agent/state", { method: "POST", body: JSON.stringify({ setupCode }), signal },
    ),
    typeSetupRestoreAgentSecrets: (setupCode: string, secrets: SetupRestoreAgentSecrets) => request<SetupRestoreAgentState>(
      "/setup/restore/agent/secrets", { method: "POST", body: JSON.stringify({ setupCode, ...secrets }) },
    ),
    endSetupRestoreAgent: (setupCode: string) => request<SetupRestoreAgentState>(
      "/setup/restore/agent/end", { method: "POST", body: JSON.stringify({ setupCode }) },
    ),
    administratorSettings: (session: Session, signal?: AbortSignal) =>
      request<AdministratorSettings>("/admin/settings", { signal }, session),
    saveAdministratorSettings: (session: Session, input: AdministratorSettingsInput, signal?: AbortSignal) =>
      request<AdministratorSettings>(
        "/admin/settings",
        { method: "PATCH", body: JSON.stringify(input), signal },
        session,
      ),
    setAdministratorProviderReturn: (session: Session, provider: "google" | "oidc", added: boolean) =>
      request<AdministratorSettings>(
        `/admin/auth-providers/${provider}/return`,
        { method: "POST", body: JSON.stringify({ added }) },
        session,
      ),
    listAdministratorAccounts: (
      session: Session,
      input: { query?: string; cursor?: string; authorityRevision?: number } = {},
      signal?: AbortSignal,
    ) => {
      const query = new URLSearchParams();
      if (input.query) query.set("query", input.query);
      if (input.cursor) query.set("cursor", input.cursor);
      if (input.authorityRevision) query.set("authorityRevision", String(input.authorityRevision));
      const suffix = query.size ? `?${query}` : "";
      return request<AdministratorAccountDirectory>(`${administratorAccountsPath}${suffix}`, { signal }, session);
    },
    sendAdministratorSMTPTest: (session: Session, input: AdministratorSMTPTestInput, signal?: AbortSignal) =>
      request<AdministratorSMTPTestResult>(
        "/admin/settings/smtp-test",
        { method: "POST", body: JSON.stringify(input), signal },
        session,
      ),
    administratorWorkspaces: (
      session: Session, input: { query?: string; cursor?: string; id?: string } = {}, signal?: AbortSignal,
    ) => {
      const query = new URLSearchParams();
      if (input.query) query.set("query", input.query);
      if (input.cursor) query.set("cursor", input.cursor);
      // One workspace's row, read again after something there is removed.
      if (input.id) query.set("id", input.id);
      const suffix = query.size ? `?${query}` : "";
      return request<AdministratorServerWorkspaceList>(`/admin/workspaces${suffix}`, { signal }, session);
    },
    administratorAuditLog: (
      session: Session, input: { kind?: string; cursor?: string } = {}, signal?: AbortSignal,
    ) => {
      const query = new URLSearchParams();
      if (input.kind) query.set("kind", input.kind);
      if (input.cursor) query.set("cursor", input.cursor);
      const suffix = query.size ? `?${query}` : "";
      return request<AdministratorAuditLog>(`/admin/audit${suffix}`, { signal }, session);
    },
    createAdministratorAccount: (session: Session, input: AdministratorAccountInput, signal?: AbortSignal) =>
      request<AdministratorAccountMutation>(
        administratorAccountsPath,
        { method: "POST", body: JSON.stringify(input), signal },
        session,
      ),
    resetAdministratorAccountPassword: (
      session: Session, accountId: string, input: AdministratorAccountPasswordInput, signal?: AbortSignal,
    ) => request<AdministratorAccountMutation>(
      `${administratorAccountsPath}/${encodeURIComponent(accountId)}/password`,
      { method: "PUT", body: JSON.stringify(input), signal }, session,
    ),
    // A one-time code that signs one device in to the account (administrator_device_codes.go).
    createAdministratorDeviceCode: (session: Session, accountId: string, signal?: AbortSignal) =>
      request<AccountDeviceCode>(
        `${administratorAccountsPath}/${encodeURIComponent(accountId)}/device-codes`,
        { method: "POST", signal }, session,
      ),
    setAdministratorAccountState: (
      session: Session, accountId: string, input: AdministratorAccountStateInput, signal?: AbortSignal,
    ) => request<AdministratorAccountMutation>(
      `${administratorAccountsPath}/${encodeURIComponent(accountId)}/state`,
      { method: "PATCH", body: JSON.stringify(input), signal }, session,
    ),
    setAdministratorAccountRole: (
      session: Session, accountId: string, input: AdministratorAccountRoleInput, signal?: AbortSignal,
    ) => request<AdministratorAccountMutation>(
      `${administratorAccountsPath}/${encodeURIComponent(accountId)}/administrator`,
      { method: "PUT", body: JSON.stringify(input), signal }, session,
    ),
    revokeAdministratorAccountSessions: (
      session: Session, accountId: string, input: AdministratorAccountMutationInput, signal?: AbortSignal,
    ) => request<AdministratorAccountMutation>(
      `${administratorAccountsPath}/${encodeURIComponent(accountId)}/sessions/revoke`,
      { method: "POST", body: JSON.stringify(input), signal }, session,
    ),
    // How an account came in and its devices (account_joins.go).
    readAdministratorAccountDevices: (session: Session, accountId: string, signal?: AbortSignal) =>
      request<AdministratorAccountDevices>(`${administratorAccountsPath}/${encodeURIComponent(accountId)}/devices`, { signal }, session),
    revokeAdministratorAccountSession: (
      session: Session, accountId: string, sessionId: string, input: AdministratorAccountMutationInput, signal?: AbortSignal,
    ) => request<AdministratorAccountMutation>(
      `${administratorAccountsPath}/${encodeURIComponent(accountId)}/sessions/${encodeURIComponent(sessionId)}/revoke`,
      { method: "POST", body: JSON.stringify(input), signal }, session,
    ),
    startAdministratorAccountCleanup: (
      session: Session, accountId: string, input: AdministratorAccountCleanupStartInput, signal?: AbortSignal,
    ) => request<AdministratorAccountCleanup>(
      `${administratorAccountsPath}/${encodeURIComponent(accountId)}/cleanup`,
      { method: "POST", body: JSON.stringify(input), signal }, session,
    ),
    administratorAccountCleanup: (
      session: Session, accountId: string, jobId: string, cursor?: string, signal?: AbortSignal,
    ) => request<AdministratorAccountCleanup>(
      `${administratorAccountsPath}/${encodeURIComponent(accountId)}/cleanup/${encodeURIComponent(jobId)}${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`,
      { signal }, session,
    ),
    assignAdministratorAccountCleanupSuccessors: (
      session: Session, accountId: string, jobId: string,
      input: AdministratorAccountCleanupAssignmentInput, signal?: AbortSignal,
    ) => request<AdministratorAccountCleanup>(
      `${administratorAccountsPath}/${encodeURIComponent(accountId)}/cleanup/${encodeURIComponent(jobId)}/assignments`,
      { method: "PATCH", body: JSON.stringify(input), signal }, session,
    ),
    commitAdministratorAccountCleanup: (
      session: Session, accountId: string, jobId: string,
      input: AdministratorAccountCleanupCommitInput, signal?: AbortSignal,
    ) => request<AdministratorAccountCleanup>(
      `${administratorAccountsPath}/${encodeURIComponent(accountId)}/cleanup/${encodeURIComponent(jobId)}/commit`,
      { method: "POST", body: JSON.stringify(input), signal }, session,
    ),
    retryAdministratorAccountCleanup: (
      session: Session, accountId: string, jobId: string,
      input: AdministratorAccountCleanupCommitInput, signal?: AbortSignal,
    ) => request<AdministratorAccountCleanup>(
      `${administratorAccountsPath}/${encodeURIComponent(accountId)}/cleanup/${encodeURIComponent(jobId)}/retry`,
      { method: "POST", body: JSON.stringify(input), signal }, session,
    ),
    currentAdministratorReport: (session: Session, signal?: AbortSignal) =>
      request<AdministratorInstanceReportCurrentResult>(
        `${administratorReportPath}/current`, { signal }, session,
      ),
    refreshAdministratorReport: (session: Session, input: AdministratorInstanceReportInput, signal?: AbortSignal) =>
      request<AdministratorInstanceReportResult>(
        administratorReportPath,
        { method: "POST", body: JSON.stringify(input), signal },
        session,
      ),
    createAdministratorBackup: (session: Session, input: AdministratorBackupInput, signal?: AbortSignal) =>
      request<AdministratorBackupJobResult>(
        administratorBackupPath,
        { method: "POST", body: JSON.stringify(input), signal },
        session,
      ),
    currentAdministratorBackup: (session: Session, signal?: AbortSignal) =>
      request<AdministratorBackupCurrentResult>(
        `${administratorBackupPath}/current`, { signal }, session,
      ),
    administratorBackup: (session: Session, jobId: string, signal?: AbortSignal) =>
      request<AdministratorBackupJobResult>(
        administratorBackupJobPath(jobId), { signal }, session,
      ),
    retryAdministratorBackup: (session: Session, jobId: string, signal?: AbortSignal) =>
      request<AdministratorBackupJobResult>(
        `${administratorBackupJobPath(jobId)}/retry`, { method: "POST", signal }, session,
      ),
    cancelAdministratorBackup: (session: Session, jobId: string, signal?: AbortSignal) =>
      request<AdministratorBackupJobResult>(
        `${administratorBackupJobPath(jobId)}/cancel`, { method: "POST", signal }, session,
      ),
    downloadAdministratorBackup: (session: Session, jobId: string, signal?: AbortSignal) =>
      requestBinaryResponse(
        `${administratorBackupJobPath(jobId)}/download`, session, { signal },
      ),
    requestLog: (session: Session, signal?: AbortSignal) =>
      request<AdministratorRequestLog>("/admin/request-log", { signal }, session),
    saveRequestLog: (session: Session, input: AdministratorRequestLogInput, signal?: AbortSignal) =>
      request<AdministratorRequestLog>("/admin/request-log", { method: "PUT", body: JSON.stringify(input), signal }, session),
    networkProxy: (session: Session, signal?: AbortSignal) =>
      request<AdministratorNetworkProxy>("/admin/network-proxy", { signal }, session),
    saveNetworkProxy: (session: Session, input: AdministratorNetworkProxyInput, signal?: AbortSignal) =>
      request<AdministratorNetworkProxy>("/admin/network-proxy", { method: "PUT", body: JSON.stringify(input), signal }, session),
    scheduledBackup: (session: Session, signal?: AbortSignal) =>
      request<AdministratorScheduledBackupResult>(`${administratorBackupPath}/schedule`, { signal }, session),
    saveScheduledBackup: (session: Session, input: AdministratorScheduledBackupInput, signal?: AbortSignal) =>
      request<AdministratorScheduledBackupResult>(
        `${administratorBackupPath}/schedule`,
        { method: "PUT", body: JSON.stringify(input), signal },
        session,
      ),
    runScheduledBackup: (session: Session, signal?: AbortSignal) =>
      request<AdministratorScheduledBackupResult>(
        `${administratorBackupPath}/schedule/run`, { method: "POST", signal }, session,
      ),
    dismissScheduledBackupSuggestion: (session: Session, signal?: AbortSignal) =>
      request<AdministratorScheduledBackupResult>(
        `${administratorBackupPath}/schedule/suggestion/dismiss`, { method: "POST", signal }, session,
      ),
    scheduledBackupSnapshots: (session: Session, signal?: AbortSignal) =>
      request<AdministratorScheduledBackupSnapshotList>(`${administratorBackupPath}/schedule/snapshots`, { signal }, session),
    restoreScheduledBackup: (session: Session, snapshot: string, signal?: AbortSignal) =>
      request<AdministratorScheduledBackupResult>(
        `${administratorBackupPath}/schedule/restore`,
        { method: "POST", body: JSON.stringify({ snapshot, requestId: newId() }), signal },
        session,
      ),
    cancelScheduledBackupRestore: (session: Session, signal?: AbortSignal) =>
      request<AdministratorScheduledBackupResult>(
        `${administratorBackupPath}/schedule/restore`, { method: "DELETE", signal }, session,
      ),
    undoScheduledBackupRestore: (session: Session, signal?: AbortSignal) =>
      request<AdministratorScheduledBackupResult>(
        `${administratorBackupPath}/schedule/restore/undo`, { method: "POST", body: "{}", signal }, session,
      ),
    saveReplication: (session: Session, enabled: boolean, signal?: AbortSignal) =>
      request<AdministratorScheduledBackupResult>(
        `${administratorBackupPath}/replication`, { method: "PUT", body: JSON.stringify({ enabled }), signal }, session,
      ),
    keepServerApart: (session: Session, signal?: AbortSignal) =>
      request<AdministratorScheduledBackupResult>(
        `${administratorBackupPath}/replication/apart`, { method: "POST", body: "{}", signal }, session,
      ),
    backupReplica: (session: Session, signal?: AbortSignal) =>
      request<AdministratorReplicaResult>(`${administratorBackupPath}/replica`, { signal }, session),
    createBackupRecoveryCode: (session: Session, signal?: AbortSignal) =>
      request<AdministratorBackupRecoveryCode>(`${administratorBackupPath}/protection/code`, { method: "POST", body: "{}", signal }, session),
    setBackupPassword: (session: Session, password: string, signal?: AbortSignal) =>
      request<AdministratorScheduledBackupResult>(
        `${administratorBackupPath}/protection/password`, { method: "PUT", body: JSON.stringify({ password }), signal }, session,
      ),
    dropBackupProtection: (session: Session, way: "code" | "password", signal?: AbortSignal) =>
      request<AdministratorScheduledBackupResult>(`${administratorBackupPath}/protection/${way}`, { method: "DELETE", signal }, session),
    openBackups: (session: Session, secret: string, signal?: AbortSignal) =>
      request<AdministratorScheduledBackupResult>(
        `${administratorBackupPath}/protection/open`, { method: "POST", body: JSON.stringify({ secret }), signal }, session,
      ),
    writeBackupsHere: (session: Session, signal?: AbortSignal) =>
      request<AdministratorScheduledBackupResult>(`${administratorBackupPath}/protection/write-here`, { method: "POST", body: "{}", signal }, session),
    createStandbyCode: (session: Session, signal?: AbortSignal) =>
      request<AdministratorStandbyCode>(`${administratorBackupPath}/standby/code`, { method: "POST", body: "{}", signal }, session),
    dismissStandby: (session: Session, signal?: AbortSignal) =>
      request<AdministratorScheduledBackupResult>(`${administratorBackupPath}/standby`, { method: "DELETE", signal }, session),
    joinAsStandby: (session: Session, code: string, signal?: AbortSignal) =>
      request<AdministratorScheduledBackupResult>(
        `${administratorBackupPath}/standby/join`, { method: "PUT", body: JSON.stringify({ code }), signal }, session,
      ),
    leaveStandby: (session: Session, signal?: AbortSignal) =>
      request<AdministratorScheduledBackupResult>(`${administratorBackupPath}/standby/join`, { method: "DELETE", signal }, session),
    setStandbyAutoTakeover: (session: Session, enabled: boolean, signal?: AbortSignal) =>
      request<AdministratorScheduledBackupResult>(
        `${administratorBackupPath}/standby/auto`, { method: "PUT", body: JSON.stringify({ enabled }), signal }, session,
      ),
    takeOverNow: (session: Session, signal?: AbortSignal) =>
      request<AdministratorScheduledBackupResult>(`${administratorBackupPath}/standby/takeover`, { method: "POST", body: "{}", signal }, session),
    // Moving a whole server to another (server_move.go).
    serverMoveIn: (session: Session, signal?: AbortSignal) =>
      request<AdministratorServerMoveIn>("/admin/server-move/in", { method: "GET", signal }, session),
    openServerMoveIn: (session: Session) =>
      request<AdministratorServerMoveIn>("/admin/server-move/in", { method: "POST", body: "{}" }, session),
    closeServerMoveIn: (session: Session) =>
      request<AdministratorServerMoveIn>("/admin/server-move/in", { method: "DELETE" }, session),
    serverMoveOut: (session: Session, signal?: AbortSignal) =>
      request<AdministratorServerMoveOut>("/admin/server-move/out", { method: "GET", signal }, session),
    startServerMoveOut: (session: Session, code: string) =>
      request<AdministratorServerMoveOut>("/admin/server-move/out", { method: "POST", body: JSON.stringify({ code }) }, session),
    restoreReplica: (session: Session, signal?: AbortSignal) =>
      request<AdministratorScheduledBackupResult>(
        `${administratorBackupPath}/replica/restore`, { method: "POST", body: "{}", signal }, session,
      ),
  };
}
