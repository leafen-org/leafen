import type { TFunction } from "i18next";
import { inviteMail } from "@/lib/inviteMail";
import { fetchRuntimeConfig, refreshRuntimeConfig } from "@/lib/runtimeConfigService";

type MemberAddOutcome = { invited: boolean; inviteEmail: boolean; requested: boolean; signupToken?: string };

/** Adds each address in turn, one request each, with the role the dialog picked, and words what happened the
 *  same way wherever "멤버 추가하기" opens: the dialog keeps the ones not done. */
export async function addWorkspaceMembers(
  addMember: (email: string, message?: string, restricted?: boolean) => Promise<MemberAddOutcome | { failed: string } | null>,
  t: TFunction<"workspaceSettingsDialog">,
  emails: string[],
  message: string,
  // People picked by name, with no address (the dialog's "user:<id>" keys), added by their account.
  people: readonly { userId: string }[] = [],
  addMemberById?: (userId: string, restricted?: boolean) => Promise<MemberAddOutcome | { failed: string } | null>,
  // The role picked: 제한된 멤버, else 멤버.
  restricted = false,
): Promise<{ added: string[]; error: string; notice: string; signupToken?: string }> {
  const added: string[] = [];
  let last: (MemberAddOutcome & { email: string }) | null = null;
  for (const email of emails) {
    const outcome = await addMember(email, message || undefined, restricted);
    // The reason the server gave, when it gave one (2026-11-06: a deactivated person read "couldn't add").
    if (!outcome || "failed" in outcome) return { added, error: (outcome && outcome.failed) || t("memberAddFailed"), notice: "" };
    added.push(email);
    last = { email, ...outcome };
  }
  for (const { userId } of people) {
    const outcome = addMemberById ? await addMemberById(userId, restricted) : null;
    if (!outcome || "failed" in outcome) return { added, error: (outcome && outcome.failed) || t("memberAddFailed"), notice: "" };
    added.push(`user:${userId}`);
    last = { email: "", ...outcome };
  }
  const done = (notice: string) => ({ added, error: "", notice });
  if (!last) return done("");
  // An address no account has is invited, as Notion invites it.
  if (added.length > 1) return done(t(last.requested ? "membersRequestedMany" : "membersAddedMany", { count: added.length }));
  if (last.requested) return done(t("memberRequestSent", { email: last.email }));
  if (!last.invited) return done("");
  // Whether the mail went, and with its link, as the share window says it (lib/inviteMail.ts): one a server that
  // sends mail (read again now) didn't send is one the limits held back.
  const { config } = last.inviteEmail ? await fetchRuntimeConfig() : await refreshRuntimeConfig();
  const mail = inviteMail(last.inviteEmail || config.emailRecoveryAvailable, last.inviteEmail ? "" : "limited");
  // One line, as Notion's toasts: who was invited and, where no mail carries
  // the link, that it is to be handed on — the toast's own "링크 복사" copies
  // it (2026-11-15: five sentences spread across the window's width).
  const notice = t(mail === "sent" ? "memberInvitedMailed" : mail === "sentWithoutLink" ? "memberInvitedMailedNoLink"
    : mail === "limited" ? "memberInvitedMailLimited" : "memberInvited", { email: last.email });
  return mail === "sent" || !last.signupToken ? done(notice) : { ...done(notice), signupToken: last.signupToken };
}
