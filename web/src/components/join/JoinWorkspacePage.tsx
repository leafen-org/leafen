import { WorkspaceIconGlyph, workspaceIconLetter } from "@/components/page/PageIcon";
import { useEffect, useState } from "react";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import type { WorkspaceInvite } from "@/accessRequestWire.generated";
import { useProductPages } from "@/components/product/productPagesContext";
import { resetDocumentChrome } from "@/lib/documentChrome";
import { requestOpenWorkspace } from "@/lib/openWorkspace";
import { pushRoute } from "@/lib/router";
import { invitationEndedText, personalInvitation, readSignupInvitation } from "@/lib/signupInvitation";
import styles from "./JoinWorkspacePage.module.css";
import { KeptIconGlyph } from "@/components/teamspace/KeptIconGlyph";

type JoinState =
  | { kind: "loading" }
  // Why, where the server says (invitation_ended.go): turned off, not found, its workspace deleted, member adds stopped.
  | { kind: "invalid"; why?: string }
  | { kind: "failed" }
  | { kind: "ready"; invite: WorkspaceInvite };

// A link that invites to nothing says why where the server says.
function refused(reason: unknown, t: TFunction): JoinState {
  if ((reason as { code?: string } | null)?.code !== "LEAFEN_WORKSPACE_INVITE_LINK_INVALID") return { kind: "failed" };
  return { kind: "invalid", why: invitationEndedText(reason, t) };
}

/**
 * Where a workspace's invite link opens (/join#key): the workspace it
 * invites to, which joining makes the person a member of at once, as
 * Notion's link does; a member goes on into it. A teamspace's link opens
 * here too: a member of its workspace joins the teamspace at once, anyone
 * else asks to join the workspace first. An invite link turned off or
 * replaced invites to nothing, and says why where the server says.
 */
export default function JoinWorkspacePage() {
  const { t } = useTranslation("joinWorkspace");
  const session = useProductPages()?.session;
  const [token] = useState(readSignupInvitation);
  const [state, setState] = useState<JoinState>({ kind: "loading" });
  const [busy, setBusy] = useState(false);
  const [joined, setJoined] = useState(false);
  // The screen has no name of its own: the window and its tab say "Leafen".
  useEffect(() => { resetDocumentChrome(); }, []);
  useEffect(() => {
    if (!session) return;
    // An email's or a name's invitation under /join, as the sign-in screen
    // takes it there, opens as its own link does for a signed-in browser:
    // taken where it's the account's, said where it's someone else's
    // (SignedInLinkNotice.tsx; 2026-11-12: it read as a turned-off link).
    if (personalInvitation(token)) {
      window.location.replace(`/#invitation=${encodeURIComponent(token)}`);
      return;
    }
    if (!token) {
      setState({ kind: "invalid" });
      return;
    }
    const controller = new AbortController();
    api.readWorkspaceInvite(session, token, controller.signal).then(
      (invite) => setState({ kind: "ready", invite }),
      (reason: { code?: string }) => {
        if (controller.signal.aborted) return;
        setState(refused(reason, t));
      },
    );
    return () => controller.abort();
  }, [session, t, token]);
  // While an owner decides, the request is read again every few seconds and
  // when the page shows again, so approving or turning it down shows here as
  // a reload would show it.
  const waiting = state.kind === "ready" && !state.invite.member && Boolean(state.invite.pending);
  useEffect(() => {
    if (!session || !token || !waiting) return;
    const controller = new AbortController();
    const reread = () => {
      if (document.visibilityState === "hidden") return;
      api.readWorkspaceInvite(session, token, controller.signal).then(
        (invite) => { if (!controller.signal.aborted) setState({ kind: "ready", invite }); },
        () => undefined,
      );
    };
    const timer = window.setInterval(reread, 4000);
    document.addEventListener("visibilitychange", reread);
    return () => {
      controller.abort();
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", reread);
    };
  }, [session, token, waiting]);
  // "열기" opens the workspace invited to, not the one the page was opened in.
  function openInvited(workspaceId: string) {
    pushRoute("/");
    requestOpenWorkspace({ workspaceId });
  }
  // A member of the workspace a link invites to, a new account the link
  // made one included, goes on into it.
  const memberOf = state.kind === "ready" && !state.invite.teamspaceId && state.invite.member ? state.invite.workspaceId : "";
  useEffect(() => {
    if (memberOf) openInvited(memberOf);
    // Once, when the invite reads as joined.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [memberOf]);
  async function join() {
    if (!session || busy) return;
    setBusy(true);
    try {
      const invite = await api.joinWorkspaceInvite(session, token);
      setJoined(Boolean(invite.teamspaceMember));
      setState({ kind: "ready", invite });
    } catch (reason) {
      setState(refused(reason, t));
    } finally {
      setBusy(false);
    }
  }
  const invite = state.kind === "ready" ? state.invite : null;
  const teamspace = invite?.teamspaceId ? invite : null;
  return (
    <main className={styles.page} data-join-workspace={state.kind}>
      <div className={styles.card}>
        {state.kind === "loading" ? <p className={styles.text}>{t("loading")}</p> : null}
        {state.kind === "invalid" ? <>
          <h1 className={styles.title}>{t("invalidTitle")}</h1>
          <p className={styles.text}>{state.why || t("invalidBody")}</p>
        </> : null}
        {state.kind === "failed" ? <p className={styles.text} role="alert" data-error>{t("failed")}</p> : null}
        {teamspace && !teamspace.member ? <>
          {/* Someone outside the workspace asks an owner first, as Notion's teamspace link shows them
              (seen 2026-11-09): no teamspace named, a way back and the request side by side. */}
          <h1 className={`${styles.title} ${styles.notMemberTitle}`}>{t("notMemberTitle")}</h1>
          <p className={styles.text} role={teamspace.pending ? "status" : undefined}>{t(teamspace.pending ? "pending" : "notMemberBody")}</p>
          <div className={styles.actions}>
            <button type="button" className={styles.secondary} onClick={() => pushRoute("/")}>{t("backToContent")}</button>
            {teamspace.pending ? null
              : <button type="button" className={styles.primary} disabled={busy} data-join-teamspace="request" onClick={() => void join()}>{t("request")}</button>}
          </div>
        </> : teamspace ? <>
          <div className={styles.icon} aria-hidden="true"><KeptIconGlyph icon={teamspace.teamspaceIcon} fallback={teamspace.teamspaceName?.trim().charAt(0) || "T"} /></div>
          <h1 className={styles.title}>{t("teamspaceTitle", { name: teamspace.teamspaceName })}</h1>
          {teamspace.member && teamspace.teamspaceMember ? <>
            <p className={styles.text} data-join-teamspace="member" role={joined ? "status" : undefined}>
              {t(joined ? "teamspaceJoined" : "teamspaceAlreadyMember")}
            </p>
            <button type="button" className={styles.primary} onClick={() => openInvited(teamspace.workspaceId)}>{t("openTeamspace")}</button>
          </> : <>
            <p className={styles.text}>{t("teamspaceBody")}</p>
            <button type="button" className={styles.primary} disabled={busy} data-join-teamspace="join" onClick={() => void join()}>
              {t("teamspaceJoin")}
            </button>
          </>}
        </> : invite ? <>
          <div className={styles.icon} aria-hidden="true"><WorkspaceIconGlyph icon={invite.icon} size={40} fallback={workspaceIconLetter(invite.name)} /></div>
          <h1 className={styles.title}>{t("title", { name: invite.name || t("untitledWorkspace") })}</h1>
          {invite.member ? <>
            <p className={styles.text}>{t("alreadyMember")}</p>
            <button type="button" className={styles.primary} onClick={() => openInvited(invite.workspaceId)}>{t("open")}</button>
          </> : invite.pending ? (
            <p className={styles.text} role="status">{t("pending")}</p>
          ) : <>
            <p className={styles.text}>{t("body")}</p>
            <button type="button" className={styles.primary} disabled={busy} onClick={() => void join()}>{t("join")}</button>
          </>}
        </> : null}
        {/* Leafen's own: a server's administrators see all on it. */}
        {invite && !invite.member ? (
          <p className={styles.note} data-server-admin-sees>{t("serverAdminSeesAll")}</p>
        ) : null}
      </div>
    </main>
  );
}
