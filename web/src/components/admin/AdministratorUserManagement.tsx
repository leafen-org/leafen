"use client";
import { personDistinction, withPersonDistinction } from "@/lib/peopleDirectory";
import { useAttemptNotice } from "@/lib/useNoticeFor";

import { PersonAvatar } from "@/components/people/PersonAvatar";
import { newId } from "../../lib/ids";
import { FormEvent, Fragment, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { SuspendedPersonGlyph } from "@/components/SuspendedPersonGlyph";
import { DotsHorizontal, Search } from "@/icons/leafen";
import { SidebarActionMenu, type SidebarMenuRow } from "@/components/sidebar/SidebarActionMenu";
import { SettingField } from "./AdministratorSettingRows";
import { api } from "../../api";
import { LeafenApiError } from "../../apiTransport";
import { LocalizedError } from "@/lib/settingsErrors";
import type {
  AdministratorAccountCleanup,
  AdministratorAccountTransferAssignment,
  AdministratorManagedAccount,
} from "../../administratorWire.generated";
import { waitForDurableJob } from "../../lib/durableJobPolling";
import type { Session } from "../../types";
import styles from "./AdministratorSettingsPage.module.css";
import { administratorUserStyles as userStyles } from "./administratorUserManagementStyles";
import { confirmAction } from "../ConfirmActionDialog";
import { useProductToast } from "@/lib/productToast";
import { passwordAcceptable } from "@/lib/passwordRule";
import { authBounds, type AccountDeviceCode } from "@/authWire.generated";
import { SettingsSectionTitle } from "@/components/settings/SettingsSectionTitle";
import { AppSelect } from "@/components/AppSelect";
import { EmptyLineReadFailed } from "@/components/settings/SettingsRowNotice";
import { administratorErrorCodes } from "../../administratorWire.generated";
import { AdministratorAccountDevices } from "./AdministratorAccountDevices";
import { AdministratorDeviceCode } from "./AdministratorDeviceCode";
import { accountPlaces } from "./administratorAccountPlaces";
import { accountChangedMeanwhile, accountError } from "./administratorAccountFailures";
import { useCopyNotice } from "@/lib/copyNotice";
import { useAccessRevision } from "@/lib/workspaceChangeCheck";

type DirectoryLoad = {
  controller: AbortController;
  consumers: number;
  promise: ReturnType<typeof api.listAdministratorAccounts>;
};

const SEARCH_WAIT_MS = 250;
const directoryLoads = new Map<string, DirectoryLoad>();

async function listAccountsSingleFlight(
  session: Session,
  input: { query?: string; cursor?: string; authorityRevision?: number },
  signal: AbortSignal,
) {
  const key = JSON.stringify([session.user.id, session.token, input.query ?? "", input.cursor ?? "", input.authorityRevision ?? 0]);
  let load = directoryLoads.get(key);
  if (!load || load.controller.signal.aborted) {
    const controller = new AbortController();
    load = { controller, consumers: 0, promise: api.listAdministratorAccounts(session, input, controller.signal) };
    const tracked = load;
    directoryLoads.set(key, load);
    const release = () => { if (directoryLoads.get(key) === tracked) directoryLoads.delete(key); };
    void load.promise.then(release, release);
  }
  load.consumers += 1;
  let onAbort = () => {};
  const aborted = new Promise<never>((_, reject) => {
    onAbort = () => reject(new DOMException("Aborted", "AbortError"));
    if (signal.aborted) onAbort();
    else signal.addEventListener("abort", onAbort, { once: true });
  });
  try {
    return await Promise.race([load.promise, aborted]);
  } finally {
    signal.removeEventListener("abort", onAbort);
    load.consumers -= 1;
    if (load.consumers === 0 && directoryLoads.get(key) === load) {
      directoryLoads.delete(key);
      load.controller.abort();
    }
  }
}

export function AdministratorUserManagement({ session }: { session: Session }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const { t: tc } = useTranslation("serverConsole");
  const copy = useCopyNotice();
  const [accounts, setAccounts] = useState<AdministratorManagedAccount[]>([]);
  const [query, setQuery] = useState("");
  const [appliedQuery, setAppliedQuery] = useState("");
  const [authorityRevision, setAuthorityRevision] = useState(0);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [busyAccount, setBusyAccount] = useState("");
  // The account whose way in and devices are open under its row.
  const [devicesFor, setDevicesFor] = useState("");
  const [notice, setNotice] = useState("");
  // What an action said goes in a toast, as Notion's list actions do: a line
  // above the list pushed it down 38px and stayed (2026-11-05). A taken email
  // says so under its own field.
  const { notify } = useProductToast();
  const [newEmail, setNewEmail] = useState("");
  const [newName, setNewName] = useState("");
  const [newPassword, setNewPassword] = useState("");
  // A password the server's rule won't take is told under its field before
  // anything is sent: the server answers only "check the details" (2026-11-05).
  const [passwordRefused, setPasswordRefused] = useState(false);
  const [creating, setCreating] = useState(false);
  const [menuFor, setMenuFor] = useState<AdministratorManagedAccount | null>(null);
  const menuButton = useRef<HTMLButtonElement | null>(null);
  const newButton = useRef<HTMLButtonElement | null>(null);
  // The form closes back to the button that opened it.
  const closeCreate = () => {
    setCreating(false);
    window.setTimeout(() => newButton.current?.focus());
  };
  const [error, setError] = useAttemptNotice<string>(newEmail, newName, newPassword), [readFailed, setReadFailed] = useState({ text: "", query: "" });
  useEffect(() => { if (notice) notify(notice); }, [notice, notify]);
  // A new account's email already in use: that field is marked with the line (rejected-entry focus).
  const [emailTaken, setEmailTaken] = useState(false);
  useEffect(() => { if (error && !emailTaken) notify(error, "error"); }, [emailTaken, error, notify]);
  // Whose a temporary password is, said with it (2026-10-27: under another account's device code it read as that one's).
  const [temporaryPassword, setTemporaryPassword] = useState<{ label: string; password: string } | null>(null);
  const [deviceCode, setDeviceCode] = useState<{ accountId: string; label: string; code: AccountDeviceCode } | null>(null);
  const [cleanup, setCleanup] = useState<AdministratorAccountCleanup | null>(null);
  const [successors, setSuccessors] = useState<Record<string, string>>({});
  const generationRef = useRef(0);
  const actionGenerationRef = useRef(0);
  const readAbortRef = useRef<AbortController | null>(null);
  const actionAbortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    void refreshDirectory("");
    return () => {
      generationRef.current += 1;
      actionGenerationRef.current += 1;
      readAbortRef.current?.abort();
      actionAbortRef.current?.abort();
    };
  }, [session]);
  // Someone joins or leaves while the list is open (the workspace's change
  // check says so): the list is read again, its search kept.
  const accessRevision = useAccessRevision();
  const seenAccessRevision = useRef(accessRevision);
  useEffect(() => {
    if (seenAccessRevision.current === accessRevision) return;
    seenAccessRevision.current = accessRevision;
    void refreshDirectory();
  }, [accessRevision]);

  const activeSuccessors = useMemo(
    () => accounts.filter((account) => account.status === "active" && account.id !== cleanup?.targetUserId),
    [accounts, cleanup?.targetUserId],
  );

  async function refreshDirectory(nextQuery = appliedQuery) {
    const generation = ++generationRef.current;
    const controller = new AbortController();
    readAbortRef.current?.abort();
    readAbortRef.current = controller;
    setLoading(true);
    setError(""); setReadFailed({ text: "", query: "" });
    try {
      const result = await listAccountsSingleFlight(session, { query: nextQuery || undefined }, controller.signal);
      if (generationRef.current !== generation) return;
      setAccounts(result.items);
      setAuthorityRevision(result.authorityRevision);
      setNextCursor(result.nextCursor);
      setAppliedQuery(nextQuery);
    } catch (cause) {
      if (generationRef.current === generation && !controller.signal.aborted) { setReadFailed({ text: accountError(cause, tc("users.readFailed"), t("common:labels.reference")), query: nextQuery }); setAccounts([]); setNextCursor(null); }
    } finally {
      if (generationRef.current === generation) setLoading(false);
    }
  }

  async function loadMore() {
    if (!nextCursor || loading) return;
    const generation = ++generationRef.current;
    const controller = new AbortController();
    readAbortRef.current?.abort();
    readAbortRef.current = controller;
    setLoading(true);
    setError("");
    try {
      const result = await listAccountsSingleFlight(session, {
        query: appliedQuery || undefined, cursor: nextCursor, authorityRevision,
      }, controller.signal);
      if (generationRef.current !== generation) return;
      setAccounts((current) => mergeAccounts(current, result.items));
      setNextCursor(result.nextCursor);
    } catch (cause) {
      if (generationRef.current === generation && !controller.signal.aborted) {
        setError(accountError(cause, tc("users.actionFailed"), t("common:labels.reference")));
        await refreshDirectory(appliedQuery);
      }
    } finally {
      if (generationRef.current === generation) setLoading(false);
    }
  }

  function submitSearch(event: FormEvent) {
    event.preventDefault();
    void refreshDirectory(query.trim());
  }

  // The list follows what is typed, as 사람's does.
  useEffect(() => {
    const wanted = query.trim();
    if (wanted === appliedQuery) return undefined;
    const timer = window.setTimeout(() => void refreshDirectory(wanted), SEARCH_WAIT_MS);
    return () => window.clearTimeout(timer);
    // refreshDirectory reads the state it renders with.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query, appliedQuery]);

  async function createAccount(event: FormEvent) {
    event.preventDefault();
    if (!newEmail.trim() || !newName.trim()) return;
    if (newPassword && !passwordAcceptable(newPassword)) {
      setPasswordRefused(true);
      return;
    }
    await runAction("new", async (signal) => {
      const result = await api.createAdministratorAccount(session, {
        email: newEmail.trim(), name: newName.trim(), password: newPassword || null,
      }, signal).catch((cause: unknown) => {
        setEmailTaken(cause instanceof LeafenApiError && cause.code === administratorErrorCodes.administratorAccountExists);
        throw cause;
      });
      setAccounts((current) => mergeAccounts([result.account], current));
      setNewEmail("");
      setNewName("");
      setNewPassword("");
      closeCreate();
      const label = result.account.name || result.account.email;
      setTemporaryPassword(result.temporaryPassword ? { label, password: result.temporaryPassword } : null);
      setNotice(tc("users.created", { name: label }));
    });
  }

  // Disabling someone or ending their sessions signs them out at once, so it
  // asks first; restoring does not.
  async function setAccountDisabled(account: AdministratorManagedAccount) {
    const status = account.status === "disabled" ? "active" : "disabled";
    if (status === "disabled" && !await confirmAction({ message: tc("users.confirmDisable", { email: account.email || account.name }),
      detail: tc(account.email ? "users.confirmDisableDetail" : "users.confirmDisableNoEmail"), confirmLabel: tc("users.disable"), danger: true })) return;
    await runAction(account.id, async (signal) => {
      const result = await api.setAdministratorAccountState(session, account.id, {
        requestId: newId(), expectedRevision: account.revision, status,
      }, signal);
      applyAccount(result.account);
      setNotice(tc(status === "disabled" ? "users.disabled" : "users.restored", { name: account.name || account.email }));
    });
  }

  async function setAdministrator(account: AdministratorManagedAccount) {
    await runAction(account.id, async (signal) => {
      try {
        const result = await api.setAdministratorAccountRole(session, account.id, {
          requestId: newId(), expectedRevision: account.revision, administrator: !account.administrator,
        }, signal);
        applyAccount(result.account);
      } catch (cause) {
        if (signal.aborted || accountChangedMeanwhile(cause)) throw cause;
        throw new LocalizedError(t("errUpdateInstanceAdmin"));
      }
    });
  }

  async function revokeSessions(account: AdministratorManagedAccount) {
    // An account with no email signs in by its devices only: once they're out, a device code brings it back.
    if (!await confirmAction({ message: tc("users.confirmRevokeSessions", { email: account.email || account.name }),
      detail: tc(account.email ? "users.confirmRevokeSessionsDetail" : "users.confirmRevokeSessionsNoEmail"),
      confirmLabel: tc("users.revokeSessions"), danger: true })) return;
    await runAction(account.id, async (signal) => {
      const result = await api.revokeAdministratorAccountSessions(session, account.id, {
        requestId: newId(), expectedRevision: account.revision,
      }, signal);
      applyAccount(result.account);
      setNotice(tc("users.sessionsRevoked", { name: account.name || account.email }));
    });
  }

  async function resetPassword(account: AdministratorManagedAccount) {
    // The reset ends every browser session and asks for a new password (2026-10-16: the confirm said neither).
    if (!await confirmAction({ message: tc("users.confirmReset", { email: account.email || account.name }), detail: tc("users.confirmResetDetail"),
      confirmLabel: tc("users.resetPassword"), danger: true })) return;
    // The server makes the temporary password and answers it this once, as
    // it does for a new account and an agent's reset (administrator_accounts.go).
    await runAction(account.id, async (signal) => {
      const result = await api.resetAdministratorAccountPassword(session, account.id, {
        requestId: newId(), expectedRevision: account.revision, password: null,
      }, signal);
      applyAccount(result.account);
      const label = account.email || account.name;
      setTemporaryPassword(result.temporaryPassword ? { label, password: result.temporaryPassword } : null);
      setNotice(tc("users.passwordReset", { name: label }));
    });
  }

  // A member who lost every device joins one with a code (the multi-server decision of 2026-10-20).
  async function makeDeviceCode(account: AdministratorManagedAccount) {
    const label = account.email || account.name;
    // Nothing to undo: the code lasts ten minutes, once, and its panel says
    // to give it to the account's owner alone (리펜 고유 화면: ask only when there is no way back).
    await runAction(account.id, async (signal) => {
      setDeviceCode({ accountId: account.id, label, code: await api.createAdministratorDeviceCode(session, account.id, signal) });
    });
  }

  async function startCleanup(account: AdministratorManagedAccount) {
    if (account.status === "deleting" && account.cleanupJobId) {
      await runAction(account.id, async (signal) => {
        const current = await api.administratorAccountCleanup(session, account.id, account.cleanupJobId, undefined, signal);
        await observeCleanup(current, signal);
      });
      return;
    }
    if (!await confirmAction({ message: tc("users.confirmDelete", { email: account.email || account.name }), detail: tc("users.confirmDeleteDetail"), confirmLabel: tc("users.delete"), danger: true })) return;
    await runAction(account.id, async (signal) => {
      const initial = await api.startAdministratorAccountCleanup(session, account.id, {
        requestId: newId(), expectedRevision: account.revision,
      }, signal);
      await observeCleanup(initial, signal);
    });
  }

  function accountLabel(accountId: string) {
    const account = accounts.find((item) => item.id === accountId);
    return account ? account.name || account.email : "";
  }

  async function observeCleanup(initial: AdministratorAccountCleanup, signal: AbortSignal) {
    const result = await waitForDurableJob({
      initial,
      signal,
      isSameJob: (next) => next.jobId === initial.jobId && next.targetUserId === initial.targetUserId,
      isTerminal: (next) => next.status === "waiting" || next.status === "completed" || next.status === "failed",
      read: () => api.administratorAccountCleanup(session, initial.targetUserId, initial.jobId, undefined, signal),
      onProgress: (next) => setCleanup(next),
    });
    setCleanup(result);
    if (result.status === "completed") {
      setNotice(tc("users.deleted", { name: accountLabel(result.targetUserId) }));
      await refreshDirectory(appliedQuery);
    } else if (result.status === "waiting") {
      setNotice(tc("users.chooseOwners"));
    } else if (result.status === "failed") {
      setError(result.errorCode ? `${tc("users.deleteFailed")} (${result.errorCode})` : tc("users.deleteFailed"));
    }
  }

  async function loadMoreCleanupAssignments() {
    if (!cleanup?.nextCursor) return;
    await runAction(cleanup.targetUserId, async (signal) => {
      const next = await api.administratorAccountCleanup(
        session, cleanup.targetUserId, cleanup.jobId, cleanup.nextCursor ?? undefined, signal,
      );
      setCleanup({ ...next, assignments: mergeAssignments(cleanup.assignments, next.assignments) });
    });
  }

  // The owners chosen and the deletion go together: a separate save between
  // them only asked for another press (2026-11-05).
  async function commitCleanup() {
    if (!cleanup) return;
    await runAction(cleanup.targetUserId, (signal) => assignAndCommit(cleanup, signal));
  }

  async function assignAndCommit(current: AdministratorAccountCleanup, signal: AbortSignal) {
    const assignments = current.assignments
      .filter((assignment) => assignment.status !== "completed")
      .map((assignment) => ({
        resourceType: assignment.resourceType,
        resourceId: assignment.resourceId,
        successorUserId: successors[assignmentKey(assignment)] || assignment.successorUserId || "",
      }))
      .filter((assignment) => assignment.successorUserId);
    const assigned = assignments.length ? await api.assignAdministratorAccountCleanupSuccessors(
      session, current.targetUserId, current.jobId,
      { expectedRevision: current.revision, assignments }, signal,
    ) : current;
    const next = await api.commitAdministratorAccountCleanup(
      session, current.targetUserId, current.jobId, { expectedRevision: assigned.revision }, signal,
    );
    await observeCleanup(next, signal);
  }

  async function retryCleanup() {
    if (!cleanup || cleanup.status !== "failed") return;
    await runAction(cleanup.targetUserId, async (signal) => {
      const next = await api.retryAdministratorAccountCleanup(
        session, cleanup.targetUserId, cleanup.jobId, { expectedRevision: cleanup.revision }, signal,
      );
      // A retry that asks for owners again goes on with the ones now chosen,
      // rather than asking for another press.
      if (next.status === "waiting") await assignAndCommit(next, signal);
      else await observeCleanup(next, signal);
    });
  }

  async function runAction(accountId: string, operation: (signal: AbortSignal) => Promise<void>) {
    const generation = ++actionGenerationRef.current;
    generationRef.current += 1;
    readAbortRef.current?.abort();
    const controller = new AbortController();
    actionAbortRef.current?.abort();
    actionAbortRef.current = controller;
    setBusyAccount(accountId);
    setError("");
    setEmailTaken(false);
    setNotice("");
    // A new action puts away what the last one said of an account, so one account's result shows at a time.
    setDeviceCode(null);
    setTemporaryPassword(null);
    try {
      await operation(controller.signal);
    } catch (cause) {
      if (actionGenerationRef.current === generation && !controller.signal.aborted) {
        if (accountChangedMeanwhile(cause)) await staleAccounts();
        else setError(accountError(cause, tc("users.actionFailed"), t("common:labels.reference")));
      }
    } finally {
      if (actionGenerationRef.current === generation) setBusyAccount("");
    }
  }

  // Changed elsewhere meanwhile: the list is read again, and says so.
  async function staleAccounts() { await refreshDirectory(appliedQuery); setError(tc("users.changedMeanwhile")); }

  function applyAccount(account: AdministratorManagedAccount) {
    setAccounts((current) => current.map((item) => item.id === account.id ? account : item));
  }

  function userMenuRows(account: AdministratorManagedAccount): SidebarMenuRow[] {
    const mutable = account.status === "active" || account.status === "disabled";
    const act = (run: (account: AdministratorManagedAccount) => unknown) => () => { setMenuFor(null); void run(account); };
    return [
      { key: "admin", label: account.administrator ? t("removeAdmin") : t("makeAdmin"), disabled: account.administrator ? !mutable : account.status !== "active",
        data: { "data-administrator-user-action": "admin" }, run: act(setAdministrator) },
      { key: "devices", label: tc("users.devices"), disabled: account.status === "deleted", data: { "data-administrator-user-action": "devices" },
        run: () => { setMenuFor(null); setDevicesFor((current) => current === account.id ? "" : account.id); } },
      { key: "sessions", label: tc("users.revokeSessions"), disabled: !mutable, data: { "data-administrator-user-action": "sessions" }, run: act(revokeSessions) },
      // A password signs in with an email only, so one with none gets a device code instead (2026-10-26:
      // its reset gave a password it couldn't use and signed it out).
      ...(account.email ? [{ key: "password", label: tc("users.resetPassword"), disabled: !mutable, data: { "data-administrator-user-action": "password" }, run: act(resetPassword) }] : []),
      { key: "code", label: tc("users.deviceCode"), disabled: account.status !== "active", data: { "data-administrator-user-action": "code" }, run: act(makeDeviceCode) },
      { key: "disable", label: account.status === "disabled" ? tc("users.restore") : tc("users.disable"), disabled: !mutable, separatorBefore: true,
        data: { "data-administrator-user-action": "disable" }, run: act(setAccountDisabled) },
      { key: "delete", label: account.status === "deleting" ? tc("users.continueDeletion") : tc("users.delete"),
        data: { "data-administrator-user-action": "delete" }, run: act(startCleanup) },
    ];
  }

  return (
    <section className={styles.section} aria-labelledby="administrator-users-title">
      <SettingsSectionTitle id="administrator-users-title" restates>{tc("users.title")}</SettingsSectionTitle>
      <form className={userStyles.memberDirectoryTools} onSubmit={submitSearch}>
        <label className={userStyles.memberSearch}><Search size="small" aria-hidden="true" /><input value={query} aria-label={tc("users.searchLabel")} placeholder={tc("users.searchPlaceholder")} autoComplete="off" onChange={(event) => setQuery(event.target.value)} /></label>
        {nextCursor ? <span className={userStyles.memberDirectoryCount}>{tc("users.shown", { count: accounts.length })}</span> : null}
        {creating ? null : <button ref={newButton} type="button" className={styles.primaryButton} aria-expanded={false} onClick={() => setCreating(true)} data-administrator-user-new>{tc("users.create")}</button>}
      </form>

      {creating ? (
        <form className={`${styles.settingRows} ${userStyles.accountCreate}`} onSubmit={(event) => void createAccount(event)} data-administrator-user-create>
          <SettingField title={tc("users.email")} error={error && emailTaken ? error : ""}>
            <input value={newEmail} type="email" autoComplete="off" autoFocus aria-label={tc("users.newEmail")} onChange={(event) => setNewEmail(event.target.value)} disabled={Boolean(busyAccount)} />
          </SettingField>
          <SettingField title={tc("users.name")}>
            <input value={newName} autoComplete="off" aria-label={tc("users.newName")} onChange={(event) => setNewName(event.target.value)} disabled={Boolean(busyAccount)} />
          </SettingField>
          <SettingField title={tc("users.passwordField")} description={tc("users.passwordFieldBody")}
            error={passwordRefused ? t("authGate:passwordRequirements", { count: authBounds.minimumPasswordLength }) : ""}>
            <input value={newPassword} type="password" autoComplete="new-password" aria-label={tc("users.newPassword")}
              onChange={(event) => { setNewPassword(event.target.value); setPasswordRefused(false); }} disabled={Boolean(busyAccount)} />
          </SettingField>
          <div className={styles.settingEnd}>
            <button type="button" className={styles.secondaryButton} onClick={closeCreate}>{t("common:actions.cancel")}</button>
            <button type="submit" className={styles.primaryButton} disabled={Boolean(busyAccount) || !newEmail.trim() || !newName.trim()}>{tc("users.create")}</button>
          </div>
        </form>
      ) : null}

      {deviceCode ? <AdministratorDeviceCode key={deviceCode.code.id} session={session} accountId={deviceCode.accountId}
        label={deviceCode.label} code={deviceCode.code} /> : null}
      {/* Shown once to pass on, as the backup's recovery code is, on a line of its own (2026-10-27: inline in a sentence, it broke at its hyphen). */}
      {temporaryPassword ? (
        <div className={styles.tunnelFields} data-temporary-password>
          <p className={styles.emailProviderMeta}>{tc("users.temporaryPassword", { name: temporaryPassword.label })}</p>
          <div className={userStyles.temporaryPasswordRow}>
            <code className={userStyles.temporaryPassword}>{temporaryPassword.password}</code>
            <button type="button" className={styles.secondaryButton} onClick={() => void copy(temporaryPassword.password)}>{tc("users.copy")}</button>
          </div>
        </div>
      ) : null}

      {cleanup && cleanup.status !== "completed" ? <CleanupPanel cleanup={cleanup} label={accountLabel(cleanup.targetUserId)} accounts={activeSuccessors} successors={successors} busy={Boolean(busyAccount)} onSuccessor={(key, value) => setSuccessors((current) => ({ ...current, [key]: value }))} onLoadMore={() => void loadMoreCleanupAssignments()} onCommit={() => void commitCleanup()} onRetry={() => void retryCleanup()} /> : null}

      <div className={userStyles.memberList} aria-busy={loading || Boolean(busyAccount)}>
        {accounts.map((account) => {
          const self = account.id === session.user.id;
          return <Fragment key={account.id}><div className={userStyles.memberRow} data-muted={account.status === "disabled" ? "true" : undefined}>
            {account.organizationSuspended
              ? <span className={userStyles.memberAvatar} aria-hidden="true"><SuspendedPersonGlyph /></span>
              : <PersonAvatar className={userStyles.memberAvatar} userId={account.id} avatar={account.avatar} name={account.name || account.email} size={34} />}
            <span className={userStyles.memberText}><strong>{account.name || account.email}</strong><span className={userStyles.memberMetaWrap}>{[
              // An account may have no email (one that came in by name, 2026-10-20). Active goes without saying.
              account.email || personDistinction(account.id), account.status === "active" ? "" : tc(accountStatusKey(account.status)),
              account.organizationSuspended ? tc("users.organizationSuspended") : "", account.administrator ? t("instanceAdminBadge") : "",
              ...accountPlaces(account, tc), account.ownedOrganizationCount ? tc("users.organizationCount", { count: account.ownedOrganizationCount }) : "",
            ].filter(Boolean).join(" · ")}</span></span>
            <span className={userStyles.memberControls}>
              {/* "확인됨" is said of an email. */}
              {!account.verified && account.email ? <span className={userStyles.memberRole}>{tc("users.unverified")}</span> : null}
              {/* The rest is in the row's menu, as 사람's are; one's own account is managed in 설정 › 계정. */}
              {self ? null : (
                <button type="button" className={userStyles.more} aria-label={tc("users.menuFor", { name: account.email ? account.name || account.email : withPersonDistinction(account.id, account.name) })}
                  aria-haspopup="menu" aria-expanded={menuFor?.id === account.id} disabled={Boolean(busyAccount)} data-administrator-user-menu={account.id}
                  onClick={(event) => { menuButton.current = event.currentTarget; setMenuFor(account); }}>
                  <DotsHorizontal size="regular" aria-hidden="true" />
                </button>
              )}
            </span>
          </div>
          {devicesFor === account.id && !self ? <AdministratorAccountDevices session={session} account={account} onAccount={applyAccount} onStale={() => void staleAccounts()} /> : null}
          </Fragment>;
        })}
      </div>
      {menuFor ? (
        <SidebarActionMenu button={menuButton} label={tc("users.menuFor", { name: menuFor.email ? menuFor.name || menuFor.email : withPersonDistinction(menuFor.id, menuFor.name) })} width={240} gap={4}
          backdropLabel={t("common:actions.close")} close={() => setMenuFor(null)} rows={userMenuRows(menuFor)} />
      ) : null}
      {loading ? <div className={userStyles.emptyState} role="status">{tc("users.loading")}</div> : null}
      {readFailed.text ? <EmptyLineReadFailed onRetry={() => void refreshDirectory(readFailed.query)} data-users-read-failed>{readFailed.text}</EmptyLineReadFailed> : !loading && accounts.length === 0 ? <div className={userStyles.emptyState}>{appliedQuery ? tc("users.noResults") : tc("users.empty")}</div> : null}
      {nextCursor ? <button type="button" className={styles.secondaryButton} onClick={() => void loadMore()} disabled={loading || Boolean(busyAccount)}>{tc("users.more")}</button> : null}
    </section>
  );
}

function CleanupPanel({ cleanup, label, accounts, successors, busy, onSuccessor, onLoadMore, onCommit, onRetry }: {
  cleanup: AdministratorAccountCleanup;
  /** The account being deleted, named beside the panel's title. */
  label: string;
  accounts: AdministratorManagedAccount[];
  successors: Record<string, string>;
  busy: boolean;
  onSuccessor: (key: string, value: string) => void;
  onLoadMore: () => void;
  onCommit: () => void;
  onRetry: () => void;
}) {
  const { t: tc } = useTranslation("serverConsole");
  const waiting = cleanup.status === "waiting";
  // Every owner chosen, none of them left on a page not yet read.
  const chosen = !cleanup.nextCursor && cleanup.assignments.every((assignment) => (
    assignment.status === "completed" || successors[assignmentKey(assignment)] || assignment.successorUserId
  ));
  return <div className={userStyles.cleanup} aria-label={tc("users.cleanupLabel")}>
    <div className={userStyles.cleanupHeader}>
      <strong>{`${tc(waiting ? "users.cleanupTitle" : "users.delete")}${label ? ` · ${label}` : ""}`}</strong>
      <span>{waiting ? tc("users.cleanupChooseNote") : `${tc(`users.cleanupPhase_${cleanup.phase}`, { defaultValue: cleanup.phase })} · ${cleanup.processedItems}/${cleanup.totalItems}`}</span>
    </div>
    {cleanup.assignments.map((assignment) => <label key={assignmentKey(assignment)} className={userStyles.assignment}>
      <span><strong>{assignment.resourceName}</strong><small>{assignment.resourceType === "workspace" ? tc("users.resourceWorkspace") : tc("users.resourceOrganization")}</small></span>
      <AppSelect aria-label={assignment.resourceName} className={userStyles.memberSelect} value={successors[assignmentKey(assignment)] ?? assignment.successorUserId ?? ""} disabled={busy || assignment.status === "completed"} onChange={(event) => onSuccessor(assignmentKey(assignment), event.target.value)}>
        <option value="">{tc("users.chooseOwner")}</option>
        {accounts.map((account) => {
          // One person owns one organization at most: someone who owns one
          // already can't take this one, and says why.
          const ownsOne = assignment.resourceType === "organization" && account.organizationCount > 0;
          // Name and email, each once: email is optional, so a person without
          // one shows the name alone and one without a name the email alone.
          const words = [account.name || account.email, account.name && account.email, ownsOne && tc("users.ownsOrganization")];
          return <option key={account.id} value={account.id} disabled={ownsOne}>{words.filter(Boolean).join(" · ")}</option>;
        })}
      </AppSelect>
    </label>)}
    <div className={userStyles.cleanupActions}>
      {cleanup.nextCursor ? <button type="button" className={styles.secondaryButton} onClick={onLoadMore} disabled={busy}>{tc("users.moreResources")}</button> : null}
      {waiting ? <button type="button" className={styles.primaryButton} onClick={onCommit} disabled={busy || !chosen}>{tc("users.transferAndDelete")}</button> : null}
      {cleanup.status === "failed" ? <button type="button" className={styles.primaryButton} onClick={onRetry} disabled={busy}>{tc("users.retryCleanup")}</button> : null}
    </div>
  </div>;
}

function mergeAccounts(current: AdministratorManagedAccount[], incoming: AdministratorManagedAccount[]) {
  const values = new Map(current.map((account) => [account.id, account]));
  for (const account of incoming) values.set(account.id, account);
  return Array.from(values.values());
}

function accountStatusKey(status: AdministratorManagedAccount["status"]) {
  if (status === "disabled") return "users.statusDisabled";
  if (status === "deleting") return "users.statusDeleting";
  return "users.statusActive";
}

function assignmentKey(assignment: Pick<AdministratorAccountTransferAssignment, "resourceType" | "resourceId">) {
  return `${assignment.resourceType}:${assignment.resourceId}`;
}

function mergeAssignments(current: AdministratorAccountTransferAssignment[], incoming: AdministratorAccountTransferAssignment[]) {
  const values = new Map(current.map((assignment) => [assignmentKey(assignment), assignment]));
  for (const assignment of incoming) values.set(assignmentKey(assignment), assignment);
  return Array.from(values.values());
}
