import { i18next } from "@/i18n";
import { confirmAction } from "@/components/ConfirmActionDialog";
import type { TeamspaceSecurity } from "./teamspaceDialogModel";

const NEXT_LINE = { display: "block", marginBlockStart: 8 } as const;

/**
 * Asks before a teamspace's kind changes from its menu — the settings'
 * teamspace table or the teamspace's own settings — as Notion's do (its
 * code and seen 2026-11-11): whether to make it a default teamspace, or open,
 * closed or private, what that means, and
 * for a default teamspace taken off, a line 8px under it that members stop
 * being added (alone for "공개", still 8px down); a red "확인" over "취소".
 * The default teamspaces' field and the create window don't ask, as Notion's
 * don't. Resolves true only when confirmed.
 */
export function confirmTeamspaceSecurity(name: string, from: TeamspaceSecurity, to: TeamspaceSecurity): Promise<boolean> {
  const t = (key: string) => i18next.t(`teamspaceDialog:securityChange.${key}`, { name });
  const leavingDefault = from === "default" && to !== "default";
  return confirmAction({
    message: t(`${to}Title`),
    detail: <>
      {leavingDefault && to === "open" ? null : <span>{t(`${to}Description`)}</span>}
      {leavingDefault ? <span style={NEXT_LINE}>{t("noLongerDefault")}</span> : null}
    </>,
    confirmLabel: t("confirm"),
    danger: "filled",
  });
}

/**
 * Asks before "워크스페이스 소유자만 팀스페이스 생성 가능" changes, as
 * Notion's does both ways (its code and seen 2026-11-11): whether to leave
 * making teamspaces to the workspace's owners, or to let every member make
 * them, alone over a red "확인" and "취소". Resolves true only when confirmed.
 */
export function confirmOwnersOnlyCreate(ownersOnly: boolean): Promise<boolean> {
  return confirmAction({
    message: i18next.t(`teamspaceDialog:securityChange.${ownersOnly ? "ownersOnlyCreateTitle" : "everyoneCreateTitle"}`),
    confirmLabel: i18next.t("teamspaceDialog:securityChange.confirm"),
    danger: "filled",
  });
}
