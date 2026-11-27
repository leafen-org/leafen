import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "@/components/shell/PhoneSheet";
import { CheckIcon, PersonIcon, PersonKeyIcon, PersonSlashIcon } from "@/icons/leafen";
import { useMenuButtonTrigger } from "@/lib/menuButtonTrigger";
import { NativeActionMenu, NativeActionMenuItem } from "@/lib/nativeActionMenu";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { SettingsChevron } from "./SettingsChevron";
import styles from "./WorkspaceMemberRoleMenu.module.css";
import { WorkspaceMemberRoleTouchWindow } from "./WorkspaceMemberRoleTouchWindow";

export type WorkspaceMemberRole = "owner" | "member" | "restricted";

/**
 * A workspace member's role, as Notion's Settings → People role menu (seen
 * 2026-10-12): a 264px menu under "역할 선택" of 워크스페이스 소유자, 멤버 and
 * 제한된 멤버, each with a line saying what it allows, the current one
 * checked, then "워크스페이스에서 제거" (on one's own line "워크스페이스 나가기"). Leafen's members have no plan, so no
 * "플러스" mark; 제한된 멤버 shows where the organization module offers it.
 * It opens from the button's start, right under it, with Notion's person
 * marks: a key for the owner, a slash for removing (seen 2026-11-12).
 */
export function WorkspaceMemberRoleMenu({ value, label, disabled, restrictable, own = false, onChange, onRemove }: {
  value: WorkspaceMemberRole; label: string; disabled: boolean; restrictable: boolean;
  /** The person's own line: its last row leaves the workspace, as Notion's (seen 2026-11-04). */
  own?: boolean;
  onChange(role: WorkspaceMemberRole): void; onRemove(): void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const trigger = useRef<HTMLButtonElement>(null), menu = useRef<HTMLDivElement>(null);
  const { state, menuProps, buttonProps, autoFocus } = useMenuButtonTrigger(trigger, disabled);
  const placement = usePopoverPlacement(menu, state.isOpen ? (size, viewport) => placeAround(
    popoverRectOf(trigger.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { align: "start", gap: 0 },
  ) : null, [state.isOpen], undefined, { kind: "menu", onEscape: state.close, onOutsidePress: state.close,
    inside: () => [trigger.current], canRestoreFocus: () => !disabled });
  // A member's own line says their role and offers leaving.
  const roles: WorkspaceMemberRole[] = own && value !== "owner" ? [value]
    : restrictable || value === "restricted" ? ["owner", "member", "restricted"] : ["owner", "member"];
  const { phone, touch } = useSidebarLayout(true);
  if (phone || touch) {
    const Choice = phone ? RolePhoneScreen : WorkspaceMemberRoleTouchWindow;
    return <Choice value={value} label={label} disabled={disabled} restrictable={restrictable} own={own} roles={roles}
      onChange={onChange} onRemove={onRemove} />;
  }
  return <>
    <button {...buttonProps} ref={trigger} className={styles.button} aria-label={label} data-workspace-member-role={value}>
      <span>{workspaceMemberRoleName(t, value)}</span><SettingsChevron filled phone={false} />
    </button>
    {state.isOpen ? createPortal(<div ref={menu} className={styles.menu}
      style={{ ...popoverPlacementStyle(placement), position: "fixed", right: "auto", bottom: "auto" }}>
      <div className={styles.title} aria-hidden="true">{t("memberRoleMenuTitle")}</div>
      <NativeActionMenu {...menuProps} aria-label={label} autoFocus={autoFocus}>
        {roles.map((role) => <NativeActionMenuItem id={role} key={role} role="menuitemradio" aria-checked={value === role}
          aria-label={workspaceMemberRoleName(t, role)} disabled={role === "restricted" && !restrictable}
          onClick={() => { state.close(); if (role !== value) onChange(role); }}>
          {role === "owner" ? <PersonKeyIcon size="regular" aria-hidden="true" /> : <PersonIcon size="regular" aria-hidden="true" />}
          <span className={styles.text}><span>{workspaceMemberRoleName(t, role)}</span>
            <span>{t(role === "owner" ? "memberRoleOwnerDescription" : role === "member" ? "memberRoleMemberDescription" : "memberRoleRestrictedDescription")}</span></span>
        </NativeActionMenuItem>)}
        <NativeActionMenuItem id="remove" onClick={() => { state.close(); onRemove(); }}>
          <PersonSlashIcon size="regular" aria-hidden="true" /><span>{t(own ? "memberLeaveWorkspace" : "memberRemoveFromWorkspace")}</span>
        </NativeActionMenuItem>
      </NativeActionMenu>
    </div>, document.body) : null}
  </>;
}

export function workspaceMemberRoleName(t: (key: string) => string, role: WorkspaceMemberRole) {
  return t(role === "owner" ? "memberRoleOwner" : role === "member" ? "memberRoleMember" : "memberRoleRestricted");
}

/**
 * A phone's role choice, as Notion's phone (seen 2026-11-12 at 402pt, iPhone
 * Safari, dark): the role and its ⌄ open a whole screen "역할 선택" with a
 * blue "완료", a row for each role with its mark, name and the line saying
 * what it allows, the current one ticked at its end, then the red
 * "워크스페이스에서 제거" (on one's own line "워크스페이스 나가기"). A
 * pressed row closes it and does what the computer's menu does.
 */
function RolePhoneScreen({ value, label, disabled, restrictable, own, roles, onChange, onRemove }: {
  value: WorkspaceMemberRole; label: string; disabled: boolean; restrictable: boolean; own: boolean;
  roles: WorkspaceMemberRole[]; onChange(role: WorkspaceMemberRole): void; onRemove(): void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const [open, setOpen] = useState(false);
  const close = () => setOpen(false);
  return <>
    <button type="button" className={styles.button} aria-label={label} aria-haspopup="dialog" aria-expanded={open} disabled={disabled}
      data-workspace-member-role={value} onClick={() => setOpen(true)}>
      <span>{workspaceMemberRoleName(t, value)}</span><SettingsChevron filled phone />
    </button>
    {open ? (
      <PhoneSheet size="screen" title={t("memberRoleMenuTitle")} label={label} tone="settings" onClose={close}
        action={{ label: t("common:actions.done"), onPress: close }} data={{ "data-member-role-screen": "" }}>
        <PhoneSheetGroup>
          {roles.map((role) => (
            <PhoneSheetRow key={role} more={false} label={workspaceMemberRoleName(t, role)} disabled={role === "restricted" && !restrictable}
              icon={role === "owner" ? <PersonKeyIcon size="regular" /> : <PersonIcon size="regular" />}
              description={t(role === "owner" ? "memberRoleOwnerDescription" : role === "member" ? "memberRoleMemberDescription" : "memberRoleRestrictedDescription")}
              value={value === role ? <CheckIcon size="regular" aria-hidden="true" /> : undefined}
              data={{ "data-member-role-option": role }}
              onPress={() => { close(); if (role !== value) onChange(role); }} />
          ))}
          <PhoneSheetRow more={false} danger label={t(own ? "memberLeaveWorkspace" : "memberRemoveFromWorkspace")}
            icon={<PersonSlashIcon size="regular" />} data={{ "data-member-role-remove": "" }}
            onPress={() => { close(); onRemove(); }} />
        </PhoneSheetGroup>
      </PhoneSheet>
    ) : null}
  </>;
}
