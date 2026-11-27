import { SwitchMark } from "@/components/Switch";
import { useEffect, useState } from "react";
import { LocalAddressHint } from "@/components/product/LocalAddressHint";
import { Trans, useTranslation } from "react-i18next";
import { api } from "@/api";
import type { WorkspaceInviteLink } from "@/accessRequestWire.generated";
import { serverUrl } from "@/lib/serverOrigin";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session, Workspace } from "@/types";
import { styles } from "./workspaceSettingsOverviewStyles";
import { useCopyNotice } from "@/lib/copyNotice";
import { useNoticeFor } from "@/lib/useNoticeFor";
import phoneCss from "./WorkspacePeoplePhone.module.css";

/** The address an invite link opens; its key rides after "#", out of logs. */
export function workspaceInviteAddress(token: string) {
  return serverUrl(`/join#${encodeURIComponent(token)}`);
}

/**
 * 사람's "링크로 멤버 추가", as Notion's (seen 2026-10-09; its row 2026-10-30:
 * words three parts to the controls' one, the description 13px wrapping and
 * saying who sees the link and ending with an offer of a new link, its words
 * a link, "링크 복사" in blue beside the switch): off until an
 * owner turns it on; whoever opens the link asks to join, and an owner admits
 * them in Requests, as Notion's help describes it. A new link ends the old one.
 */
export function WorkspaceInviteLinkControls({ session, workspace, phone = false }: {
  session: Session; workspace: Workspace;
  /**
   * Notion's phone "초대 링크" (seen 2026-11-15 at 402): its words over the
   * switch alone, then the link in a field and "링크 복사" across under it
   * (WorkspacePeoplePhone.module.css). Turning it off ends the link, so the
   * phone leaves out the desktop's new-link words, as Notion's.
   */
  phone?: boolean;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [link, setLink] = useState<WorkspaceInviteLink | null>(null);
  const [busy, setBusy] = useState(false);
  // A change's line (a new link made) is about the link as it left it, and
  // goes once the link is otherwise (lib/useNoticeFor).
  const [notice, setNotice] = useNoticeFor<string>(JSON.stringify([link?.enabled, link?.token]));
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    api.readWorkspaceInviteLink(session, workspace.id, controller.signal).then(setLink, (reason) => {
      if (!controller.signal.aborted) setError(settingsErrorMessage(reason, t("inviteLinkFailed")));
    });
    return () => controller.abort();
  }, [session, t, workspace.id]);
  async function change(run: () => Promise<WorkspaceInviteLink>, done?: string) {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      setLink(await run());
      if (done) setNotice(done);
    } catch (reason) {
      setError(settingsErrorMessage(reason, t("inviteLinkFailed")));
    } finally {
      setBusy(false);
    }
  }
  // The button keeps its words while a toast says the link was copied (lib/copyNotice).
  const copyNotice = useCopyNotice();
  function copy() {
    if (!link?.token) return;
    void copyNotice(workspaceInviteAddress(link.token), t("inviteLinkCopied"));
  }
  const enabled = link?.enabled === true;
  const shared = enabled && Boolean(link?.token);
  const rotate = () => void change(() => api.rotateWorkspaceInviteLink(session, workspace.id), t("inviteLinkRotated"));
  return (
    <div className={phone ? `${styles.inviteLink} ${phoneCss.invite}` : styles.inviteLink} data-workspace-invite-link>
      <div className={styles.inviteLinkRow}>
        <span className={styles.inviteLinkText}>
          <strong>{t(phone ? "inviteLinkPhoneTitle" : "inviteLinkTitle")}</strong>
          <span>{t(phone ? "inviteLinkPhoneDescription" : "inviteLinkDescription")}{shared && !phone ? <>{" "}<Trans t={t} i18nKey="inviteLinkRotateHint" components={{
            rotate: <button type="button" className={styles.inviteLinkRotate} disabled={busy} data-invite-link-rotate onClick={rotate} />,
          }} /></> : null}</span>
        </span>
        <span className={styles.inviteLinkControls}>
          {shared && !phone ? <button type="button" className={styles.inviteLinkCopy} disabled={busy} data-invite-link-copy onClick={() => void copy()}>
            {t("inviteLinkCopy")}
          </button> : null}
          {/* A switch, as Notion's and the other settings'. */}
          <button type="button" role="switch" aria-checked={enabled} disabled={!link || busy} aria-label={t("inviteLinkTitle")}
            data-invite-link-switch onClick={() => void change(() => api.setWorkspaceInviteLink(session, workspace.id, !enabled))}>
            <SwitchMark on={enabled} />
          </button>
        </span>
      </div>
      {phone && shared && link?.token ? <>
        <input className={phoneCss.linkField} readOnly value={workspaceInviteAddress(link.token)} aria-label={t("inviteLinkPhoneTitle")} data-invite-link-field
          onFocus={(event) => event.currentTarget.select()} />
        <button type="button" className={phoneCss.copy} disabled={busy} data-invite-link-copy onClick={() => void copy()}>{t("inviteLinkCopy")}</button>
      </> : null}
      {shared && link?.token ? <LocalAddressHint address={workspaceInviteAddress(link.token)} className={styles.organizationMeta} session={session} /> : null}
      {error ? <div className={styles.notice} role="alert">{error}</div> : null}
      {notice && !error ? <div className={styles.notice} data-tone="neutral" role="status">{notice}</div> : null}
    </div>
  );
}
