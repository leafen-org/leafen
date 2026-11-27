import { i18next } from "@/i18n";
import { WarningTriangleIcon } from "@/icons/leafen";
import type { ConfirmActionRequest } from "@/components/ConfirmActionDialog";
import styles from "./TeamspaceSettings.module.css";

/**
 * Notion's question before a workspace owner puts themselves in a teamspace
 * they don't manage (its code, 2026-11-18; an Enterprise workspace's screen,
 * read from the code, not seen): a yellow warning mark, "권한이 필요한
 * 작업입니다.", what the step is and that the workspace's audit log shows it,
 * a blue "계속하기" over "취소". Becoming its owner — joining as one or
 * rising from its member — says so; joining a closed or private one as its
 * member says that. The server lets the step through the same rule
 * (privilegedTeamspaceSelfMembership).
 */
export function teamspacePrivilegedQuestion(kind: "owner" | "join"): ConfirmActionRequest {
  return {
    icon: <WarningTriangleIcon className={styles.privilegedMark} size="large" aria-hidden="true" />,
    message: i18next.t("teamspaceDialog:privileged.title"),
    detail: i18next.t(kind === "owner" ? "teamspaceDialog:privileged.becomeOwner" : "teamspaceDialog:privileged.join"),
    confirmLabel: i18next.t("teamspaceDialog:privileged.continue"),
    cancelLabel: i18next.t("common:actions.cancel"),
    accent: true,
  };
}
