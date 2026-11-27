import { i18next } from "@/i18n";

/**
 * Notion's question before leaving a teamspace (seen 2026-11-12, from the
 * sidebar's "…"): the name asked about, what leaving takes away under it,
 * and a red-filled "팀스페이스 나가기" over "취소".
 */
export function teamspaceLeaveQuestion(name: string) {
  return {
    message: i18next.t("teamspaceDialog:leave.confirm", { name }), detail: i18next.t("teamspaceDialog:leave.detail"),
    confirmLabel: i18next.t("teamspaceDialog:leave.confirmTeamspace"), danger: "filled" as const,
  };
}

/**
 * Notion's question before you take away your own ownership of a teamspace
 * (seen 2026-11-17 in two tabs, from your row's role menu in its members):
 * one line, asking whether to give up ownership and saying the teamspace's
 * settings and access will no longer be yours to change, over red words on a
 * red outline,
 * "사용 권한 변경", which takes the focus, and "취소". Removing yourself is
 * leaving, and asks teamspaceLeaveQuestion as Notion's does.
 */
export function teamspaceOwnerDropQuestion() {
  return {
    message: i18next.t("teamspaceDialog:memberList.dropOwnConfirm"),
    confirmLabel: i18next.t("teamspaceDialog:memberList.dropOwnConfirmLabel"), danger: true,
  };
}

/**
 * Notion's question before someone else is taken out of a teamspace (seen
 * 2026-11-18 in two tabs and two accounts, ko and en, from their row's role
 * menu in its members): whether to take the person out of the teamspace,
 * over its own grey line saying the teamspace's unlinked pages keep their
 * sharing, and a red-filled "제거", which takes the focus, over "취소".
 */
export function teamspaceRemoveMemberQuestion(name: string, teamspace: string) {
  return {
    message: i18next.t("teamspaceDialog:memberList.removeQuestion", { name, teamspace }),
    detail: i18next.t("teamspaceDialog:memberList.removeQuestionDetail"),
    confirmLabel: i18next.t("teamspaceDialog:memberList.remove"), danger: "filled" as const,
  };
}
