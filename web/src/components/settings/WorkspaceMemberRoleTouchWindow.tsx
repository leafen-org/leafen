import { useState } from "react";
import { useTranslation } from "react-i18next";
import { PersonIcon, PersonKeyIcon, PersonSlashIcon } from "@/icons/leafen";
import { SettingsTouchMenuWindow } from "./SettingsTouchMenuWindow";
import { SettingsChevron } from "./SettingsChevron";
import { workspaceMemberRoleName, type WorkspaceMemberRole } from "./WorkspaceMemberRoleMenu";
import styles from "./WorkspaceMemberRoleMenu.module.css";

/**
 * A role's choice on a touch screen wider than a phone, as Notion's iPad
 * (seen 2026-11-13 in iPad Safari at 1210 landscape, light): not a menu
 * under the button but the window in the screen's middle the touch screen's
 * settings menus open in (SettingsTouchMenuWindow) — "역할 선택" over a line
 * with its blue "완료", then the computer's rows of the role's mark, name and
 * what it allows, the current one ticked at its end, and the red "워크스페이스
 * 나가기" ("워크스페이스에서 제거" on another's line). A pressed row closes
 * it and does what the computer's menu does.
 */
export function WorkspaceMemberRoleTouchWindow({ value, label, disabled, restrictable, own, roles, onChange, onRemove }: {
  value: WorkspaceMemberRole; label: string; disabled: boolean; restrictable: boolean; own: boolean;
  roles: WorkspaceMemberRole[]; onChange(role: WorkspaceMemberRole): void; onRemove(): void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [open, setOpen] = useState(false);
  return <>
    <button type="button" className={styles.button} aria-label={label} aria-haspopup="dialog" aria-expanded={open} disabled={disabled}
      data-workspace-member-role={value} onClick={() => setOpen(true)}>
      <span>{workspaceMemberRoleName(t, value)}</span><SettingsChevron filled phone={false} />
    </button>
    {open ? (
      <SettingsTouchMenuWindow title={t("memberRoleMenuTitle")} label={label} current={value} close={() => setOpen(false)}
        data={{ "data-member-role-window": "" }}
        rows={[...roles.map((role) => ({ key: role, label: workspaceMemberRoleName(t, role),
          icon: role === "owner" ? <PersonKeyIcon size="regular" aria-hidden="true" /> : <PersonIcon size="regular" aria-hidden="true" />,
          description: t(role === "owner" ? "memberRoleOwnerDescription" : role === "member" ? "memberRoleMemberDescription" : "memberRoleRestrictedDescription"),
          disabled: role === "restricted" && !restrictable, data: { "data-member-role-option": role },
          run: () => { if (role !== value) onChange(role); } })),
        { key: "remove", label: t(own ? "memberLeaveWorkspace" : "memberRemoveFromWorkspace"), danger: true,
          icon: <PersonSlashIcon size="regular" aria-hidden="true" />, data: { "data-member-role-remove": "" }, run: onRemove }]} />
    ) : null}
  </>;
}
