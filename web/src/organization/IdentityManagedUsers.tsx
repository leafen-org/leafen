import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  listManagedUsers,
  listMoreManagedUsers,
  logOutManagedUsers,
  resetManagedUserPasswords,
  type ManagedUser,
  type ManagedUserFilter,
  type ManagedUserList,
} from "./organizationManagedUsers";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import table from "@/components/settings/DeveloperSettings.module.css";
import { ManagedUserPolicies } from "./ManagedUserPolicies";
import { ManagedUserProfile } from "./ManagedUserProfile";
import { ManagedUsersWorkspaceFilter } from "./ManagedUsersWorkspaceFilter";
import styles from "@/components/settings/SettingsItemRows.module.css";
import { styles as settingsStyles } from "@/components/settings/workspaceSettingsOverviewStyles";
import { ListNotice } from "@/components/settings/SettingsRowNotice";
import { activeDateLocale } from "@/lib/i18n";
import { AppSelect } from "@/components/AppSelect";

/**
 * The Identity item's "사용자 관리", as Notion's managed users (its help,
 * read 2026-10-02): it applies to everyone on the organization's verified
 * domains; the dashboard lists them with their workspaces — other
 * organizations' marked external — last activity, 2-step verification and
 * suspension, searched by name, email or ID and filtered by workspace and
 * 2-step method (the workspaces searched and read on, ManagedUsersWorkspaceFilter),
 * a window of people at a time read on with "더 보기" under the same search and
 * filters, and opens one person's profile with the actions on them;
 * the policies follow (ManagedUserPolicies), then "관리 대상 사용자 모두
 * 로그아웃" and "모든 관리 대상 사용자의 비밀번호 재설정", for all but the admin.
 */
export function IdentityManagedUsers({ session, organizationId }: { session: Session; organizationId: string }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState<ManagedUserFilter>({ query: "", workspaceId: "", twoStep: "" });
  const [users, setUsers] = useState<ManagedUserList | null>(null);
  const [selected, setSelected] = useState<ManagedUser | null>(null);
  const [confirmingAll, setConfirmingAll] = useState<"" | "logout" | "password">("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  // A failed read of the people (the next window's: more), said where they
  // show with a way to read again; an action that failed is `error`.
  const [readFailed, setReadFailed] = useState<{ text: string; more: boolean } | null>(null);
  const [readTurn, setReadTurn] = useState(0);
  // The filter the people shown were read with, and a read of the next window.
  const listed = useRef(filter);
  const more = useRef<AbortController | null>(null);
  const [readingMore, setReadingMore] = useState(false);

  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      listManagedUsers(session, organizationId, filter, controller.signal)
        .then((list) => {
          // A new list ends a read on from the one before.
          more.current?.abort();
          more.current = null;
          setReadingMore(false);
          listed.current = filter;
          setUsers(list);
          setReadFailed(null);
        })
        .catch((failure) => {
          if (controller.signal.aborted) return;
          setReadFailed({ text: settingsErrorMessage(failure, t("managedUsersLoadFailed")), more: false });
          // Nothing of the people another search or filter found stays.
          setUsers(null);
        });
    }, filter.query ? 250 : 0);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [open, organizationId, filter, readTurn, session, t]);

  // The next window, under the search and filters the people shown were read with.
  async function readMore() {
    const cursor = users?.nextCursor;
    if (!cursor || more.current) return;
    const controller = new AbortController();
    more.current = controller;
    setReadingMore(true);
    setReadFailed(null);
    try {
      const next = await listMoreManagedUsers(session, organizationId, listed.current, cursor, controller.signal);
      if (controller.signal.aborted) return;
      setUsers((current) => current && current.nextCursor === cursor
        ? { ...current, items: [...current.items, ...next.items], hasMore: next.hasMore, nextCursor: next.nextCursor }
        : current);
    } catch (failure) {
      if (!controller.signal.aborted) setReadFailed({ text: settingsErrorMessage(failure, t("managedUsersLoadFailed")), more: true });
    } finally {
      if (more.current === controller) {
        more.current = null;
        setReadingMore(false);
      }
    }
  }

  function changed(user: ManagedUser) {
    setSelected(user);
    setUsers((current) => current && { ...current, items: current.items.map((item) => (item.id === user.id ? user : item)) });
  }

  // "모두 로그아웃" and "모든 관리 대상 사용자의 비밀번호 재설정", each confirmed first.
  async function actOnAll(kind: "logout" | "password") {
    setBusy("all");
    setError("");
    try {
      if (kind === "logout") {
        const result = await logOutManagedUsers(session, organizationId);
        setNotice(t("managedUsersLoggedOutAll", { count: result.users }));
      } else {
        const result = await resetManagedUserPasswords(session, organizationId);
        setNotice(t("managedUsersPasswordResetAllDone", { count: result.users }));
      }
      setConfirmingAll("");
    } catch (failure) {
      setError(settingsErrorMessage(failure, t(kind === "logout" ? "managedUsersLogoutFailed" : "managedUserActionFailed")));
    } finally {
      setBusy("");
    }
  }

  const date = (value: number) => value > 0
    ? new Intl.DateTimeFormat(activeDateLocale(), { dateStyle: "long" }).format(new Date(value))
    : t("managedUsersNever");
  return (
    <section className={settingsStyles.section} role="region" aria-label={t("managedUsersTitle")} data-managed-users>
      <div className={settingsStyles.sectionTitle}>{t("managedUsersTitle")}</div>
      <p className={settingsStyles.organizationMeta}>{t("managedUsersDescription")}</p>
      <div className={styles.rowStack}>
        <div className={settingsStyles.securityRow}>
          <div className={settingsStyles.securityInstruction}>
            <strong>{t("managedUsersDashboard")}</strong>
            <span>{t("managedUsersDashboardDescription")}</span>
          </div>
          <button type="button" className={settingsStyles.secondaryButton} aria-expanded={open} onClick={() => { setOpen((value) => !value); setSelected(null); }}
            data-managed-users-dashboard>
            {t(open ? "managedUsersClose" : "managedUsersOpen")}
          </button>
        </div>
        {open && selected ? (
          <ManagedUserProfile key={selected.id} session={session} organizationId={organizationId} user={selected}
            onChange={changed} onBack={() => setSelected(null)} />
        ) : open ? (
          <div data-managed-users-list>
            <div className={styles.filters}>
              <input type="search" value={filter.query} placeholder={t("managedUsersSearch")} aria-label={t("managedUsersSearch")}
                onChange={(event) => setFilter((current) => ({ ...current, query: event.target.value }))} />
              <ManagedUsersWorkspaceFilter session={session} organizationId={organizationId} value={filter.workspaceId}
                first={users ? { items: users.workspaceChoices, nextCursor: users.workspaceChoicesNextCursor } : null}
                onChange={(workspaceId) => setFilter((current) => ({ ...current, workspaceId }))} />
              <AppSelect value={filter.twoStep} aria-label={t("managedUsersFilterTwoStep")} data-managed-users-two-step
                onChange={(event) => setFilter((current) => ({ ...current, twoStep: event.target.value as ManagedUserFilter["twoStep"] }))}>
                <option value="">{t("managedUsersFilterTwoStep")}</option>
                <option value="totp">{t("managedUsersTwoStepTotp")}</option>
                <option value="none">{t("managedUsersTwoStepNone")}</option>
              </AppSelect>
            </div>
            {/* Narrowed to no one, it says so, as Notion's people search does
                (2026-11-05: "nobody" read as no one on the verified domains). */}
            {readFailed ? (
              <ListNotice role="alert" data-managed-users-read-failed onRetry={() => {
                if (readFailed.more) void readMore(); else { setReadFailed(null); setReadTurn((turn) => turn + 1); }
              }}>{readFailed.text}</ListNotice>
            ) : null}
            {!users ? null : users.items.length === 0 ? (
              <div className={styles.empty}>
                {t(filter.query.trim() || filter.workspaceId || filter.twoStep ? "noSearchResults" : "managedUsersEmpty")}
              </div>
            ) : (
              <table className={table.tokens}>
                <thead>
                  <tr>
                    <th scope="col">{t("managedUsersColumnName")}</th>
                    <th scope="col">{t("managedUsersColumnWorkspaces")}</th>
                    <th scope="col">{t("managedUsersColumnLastActive")}</th>
                    <th scope="col">{t("managedUsersColumnTwoStep")}</th>
                    <th scope="col"><span className={table.hidden}>{t("managedUsersManage")}</span></th>
                  </tr>
                </thead>
                <tbody>
                  {users.items.map((user) => (
                    <tr key={user.id} data-managed-user={user.id}>
                      <td className={table.name}>
                        {user.name || user.email}
                        {user.suspended ? (
                          <span className={styles.badge}> · {t(user.deletionDueAt > 0 ? "managedUserDeletionScheduled" : "managedUsersSuspended")}</span>
                        ) : null}
                        <br /><small>{user.email}</small>
                      </td>
                      <td className={table.wraps}>
                        {user.workspaces.map((workspace) => (workspace.external
                          ? `${workspace.name} (${t("managedUsersExternal")})` : workspace.name)).join(", ") || "—"}
                      </td>
                      <td>{date(user.lastActiveAt)}</td>
                      <td>{t(user.twoStep ? "managedUsersTwoStepOn" : "managedUsersTwoStepOff")}</td>
                      <td className={table.actions}>
                        <button type="button" className={table.secondary} data-managed-user-open
                          aria-label={t("managedUsersManageNamed", { name: user.name || user.email })}
                          onClick={() => { setNotice(""); setSelected(user); }}>
                          {t("managedUsersManage")}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
            {users?.nextCursor ? (
              <button type="button" className={settingsStyles.secondaryButton} disabled={readingMore} data-managed-users-more
                onClick={() => void readMore()}>
                {t("loadMore")}
              </button>
            ) : null}
          </div>
        ) : null}
        <ManagedUserPolicies session={session} organizationId={organizationId} />
        {([
          ["logout", "managedUsersLogoutAll", "managedUsersLogoutAllDescription", "managedUsersLogoutAllButton", "managedUsersLogoutAllConfirm", "logout-all"],
          ["password", "managedUsersPasswordResetAll", "managedUsersPasswordResetAllDescription", "managedUsersPasswordResetAllButton",
            "managedUsersPasswordResetAllConfirm", "password-reset-all"],
        ] as const).map(([kind, title, description, button, confirm, data]) => (
          <div key={kind} className={settingsStyles.securityRow}>
            <div className={settingsStyles.securityInstruction}>
              <strong>{t(title)}</strong>
              <span>{t(description)}</span>
            </div>
            {confirmingAll === kind ? (
              <span className={table.actions}>
                <button type="button" className={table.danger} disabled={busy === "all"} onClick={() => void actOnAll(kind)}
                  {...{ [`data-managed-users-${data}-confirm`]: true }}>
                  {t(confirm)}
                </button>
                <button type="button" className={table.secondary} onClick={() => setConfirmingAll("")}>{t("managedUsersCancel")}</button>
              </span>
            ) : (
              <button type="button" className={settingsStyles.secondaryButton} onClick={() => { setNotice(""); setConfirmingAll(kind); }}
                {...{ [`data-managed-users-${data}`]: true }}>
                {t(button)}
              </button>
            )}
          </div>
        ))}
        {notice ? <div className={styles.empty} role="status">{notice}</div> : null}
        {error ? <div className={settingsStyles.errorText} role="alert">{error}</div> : null}
      </div>
    </section>
  );
}
