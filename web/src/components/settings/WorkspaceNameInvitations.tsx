import { type FormEvent, useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { request } from "@/apiTransport";
import { serverUrl } from "@/lib/serverOrigin";
import { LocalAddressHint } from "@/components/product/LocalAddressHint";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import dialogCss from "./WorkspaceAddMembersDialog.module.css";
import phoneCss from "./WorkspaceNameInvitationsPhone.module.css";
import { useCopyNotice } from "@/lib/copyNotice";
import { useAccessRevision } from "@/lib/workspaceChangeCheck";

type NameInvitation = { id: string; name: string; token: string; expiresAt: number; createdAt: number };

const invitationsPath = (workspaceId: string) => `/workspaces/${encodeURIComponent(workspaceId)}/name-invitations`;
const DAY = 24 * 60 * 60 * 1000;
const linkOf = (token: string) => serverUrl(`/#invitation=${encodeURIComponent(token)}`);

/**
 * Inviting by name, for the workspace's owner (internal/app/workspace_name_invitations.go):
 * one link for one person, used once, for seven days, with no email; opening
 * it asks only a name, filled with this one. The waiting ones are listed with
 * their days left, to copy again or cancel.
 */
export function WorkspaceNameInvitations({ session, workspaceId, memberNames = [], phone = false }: {
  session: Session;
  workspaceId: string;
  /** The names of the people here, to say when the one typed is already one of them. */
  memberNames?: readonly string[];
  /** On a touch screen's "멤버 추가" screen: its rows and side room (WorkspaceNameInvitationsPhone.module.css). */
  phone?: boolean;
}) {
  const css = phone ? phoneCss : dialogCss;
  const { t } = useTranslation("workspaceSettingsDialog");
  const [invitations, setInvitations] = useState<NameInvitation[]>([]);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const notice = useCopyNotice();
  // Someone here by the same name is told apart by when they joined (the @
  // menu's "가입" time); the inviter learns of it here, not when choosing
  // between the two later (2026-11-06).
  const typedName = name.trim().normalize("NFC").toLocaleLowerCase();
  const sameName = Boolean(typedName) && memberNames.some((member) => member.trim().normalize("NFC").toLocaleLowerCase() === typedName);

  const load = useCallback(() => request<{ invitations: NameInvitation[] }>(invitationsPath(workspaceId), {}, session)
    .then((value) => setInvitations(value.invitations), (failure) => setError(settingsErrorMessage(failure, t("nameInviteFailed")))),
  [session, t, workspaceId]);
  // Someone joining with a link changes who is here, and the workspace's
  // change check says so: the list is read again, with the members'
  // (2026-10-27: a used link stayed "7일 남음").
  const accessRevision = useAccessRevision();
  useEffect(() => { void load(); }, [accessRevision, load]);

  function copy(invitation: NameInvitation, writing?: Promise<void>) {
    // The row's button keeps its words while a toast says the link was copied.
    return notice(linkOf(invitation.token), t("inviteLinkCopied"), undefined, writing);
  }

  async function invite(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!name.trim()) return;
    setBusy(true);
    setError("");
    const making = request<NameInvitation>(invitationsPath(workspaceId), {
      method: "POST", body: JSON.stringify({ name: name.trim() }),
    }, session);
    // Safari lets a click copy only what it starts copying during the click,
    // so the link still being made is handed to the clipboard as a promise.
    let writing: Promise<void> | undefined;
    if (typeof ClipboardItem !== "undefined" && navigator.clipboard?.write) {
      writing = navigator.clipboard.write([new ClipboardItem({
        "text/plain": making.then((made) => new Blob([linkOf(made.token)], { type: "text/plain" })),
      })]);
      writing.catch(() => undefined);
    }
    try {
      const made = await making;
      setName("");
      setInvitations((current) => [...current, made]);
      await copy(made, writing);
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("nameInviteFailed")));
    } finally {
      setBusy(false);
    }
  }

  async function cancel(invitation: NameInvitation) {
    setBusy(true);
    try {
      await request<void>(`${invitationsPath(workspaceId)}/${encodeURIComponent(invitation.id)}`, { method: "DELETE" }, session);
      setInvitations((current) => current.filter((item) => item.id !== invitation.id));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("nameInviteFailed")));
    } finally {
      setBusy(false);
    }
  }

  const waiting = invitations.filter((invitation) => invitation.expiresAt > Date.now());
  return (
    <div className={css.nameInvite} role="region" aria-label={t("nameInviteTitle")} data-name-invitations>
      <p className={css.hint}>{t("nameInviteDescription")}</p>
      <form className={css.nameInviteForm} onSubmit={(event) => void invite(event)}>
        <div className={css.field}>
          <input value={name} placeholder={t("nameInvitePlaceholder")} aria-label={t("nameInvitePlaceholder")} autoComplete="off"
            onChange={(event) => { setName(event.target.value); setError(""); }} />
        </div>
        {sameName ? <p className={css.nameInviteSameName} role="status" data-name-invite-same-name>{t("nameInviteSameName")}</p> : null}
        <button type="submit" className={css.send} disabled={busy || !name.trim()}>{t("nameInviteCreate")}</button>
      </form>
      {error ? <p className={css.error} role="alert">{error}</p> : null}
      {waiting.length ? <div className={css.nameInviteList}>
        {waiting.map((invitation) => {
          const days = Math.max(1, Math.ceil((invitation.expiresAt - Date.now()) / DAY));
          return (
            <div key={invitation.id} className={css.nameInviteRow} data-name-invitation={invitation.id}>
              <PersonAvatar userId={invitation.id} name={invitation.name} size={24} />
              <span className={css.nameInviteWho}>
                <span>{invitation.name}</span>
                <span>{t("nameInviteDaysLeft", { count: days })}</span>
              </span>
              <button type="button" disabled={busy} onClick={() => void copy(invitation)}>{t("nameInviteCopy")}</button>
              <button type="button" disabled={busy} onClick={() => void cancel(invitation)}>{t("nameInviteCancel")}</button>
            </div>
          );
        })}
      </div> : null}
      <LocalAddressHint session={session} className={css.nameInviteNote} />
    </div>
  );
}
