import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { CheckIcon, ChevronDown } from "@/icons/leafen";
import { useMenuButtonTrigger } from "@/lib/menuButtonTrigger";
import { NativeActionMenu, NativeActionMenuItem } from "@/lib/nativeActionMenu";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { TeamspaceMemberRole } from "@/teamspaceWire.generated";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { SettingsPhoneMenuScreen } from "@/components/settings/SettingsPhoneMenuScreen";
import { SettingsTouchMenuWindow } from "@/components/settings/SettingsTouchMenuWindow";
import styles from "./TeamspaceMembers.module.css";

const ROLES: TeamspaceMemberRole[] = ["owner", "member"];

/** A teamspace role, as Notion's (seen 2026-10-06): "팀스페이스 소유자" or
 * "팀스페이스 멤버", each with a line saying what it allows, and "제거" for a
 * member already in it; it opens from the button's start, right under it,
 * 280 wide in the member list and 264 in the invite window, the chosen
 * role's ✓ at its end (seen 2026-11-12). On a phone the role and its ⌄
 * open the whole "역할 선택" screen a workspace member's role opens there
 * (SettingsPhoneMenuScreen), its removal red; on a touch screen wider than
 * a phone, the window in the screen's middle a workspace member's role
 * opens there (SettingsTouchMenuWindow, as WorkspaceMemberRoleTouchWindow). */
export function TeamspaceRoleMenu({ value, disabled, compact = false, label, rolesLocked = false, openRole, onChange, onRemove, removeDisabled }: {
  value: TeamspaceMemberRole; disabled: boolean; compact?: boolean; label?: string;
  /** Shows the roles without letting them change, for one who may only remove. */
  rolesLocked?: boolean;
  /** The one role still chosen while the rest are locked: a workspace owner's own "팀스페이스 소유자". */
  openRole?: TeamspaceMemberRole;
  onChange(role: TeamspaceMemberRole): void; onRemove?(): void; removeDisabled?: boolean;
}) {
  const { t } = useTranslation(["teamspaceDialog", "workspaceSettingsDialog"]);
  const trigger = useRef<HTMLButtonElement>(null), menu = useRef<HTMLDivElement>(null);
  const { state, menuProps, buttonProps, autoFocus } = useMenuButtonTrigger(trigger, disabled);
  const placement = usePopoverPlacement(menu, state.isOpen ? (size, viewport) => placeAround(
    popoverRectOf(trigger.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { align: "start", gap: 0 },
  ) : null, [state.isOpen], undefined, { kind: "menu", onEscape: state.close, onOutsidePress: state.close,
    inside: () => [trigger.current], canRestoreFocus: () => !disabled });
  const name = (role: TeamspaceMemberRole) => t(role === "owner" ? "memberList.ownerRole" : "memberList.memberRole");
  const { phone, touch } = useSidebarLayout(true);
  const [screenOpen, setScreenOpen] = useState(false);
  if (phone || touch) {
    const Choice = phone ? SettingsPhoneMenuScreen : SettingsTouchMenuWindow;
    return <>
      <button type="button" className={compact ? styles.roleButtonCompact : styles.roleButton} aria-label={label ?? name(value)}
        aria-haspopup="dialog" aria-expanded={screenOpen} disabled={disabled} data-teamspace-role={value} onClick={() => setScreenOpen(true)}>
        <span>{name(value)}</span><ChevronDown size="tiny" aria-hidden="true" />
      </button>
      {screenOpen ? <Choice title={t("workspaceSettingsDialog:memberRoleMenuTitle")} label={label ?? name(value)}
        current={value} close={() => setScreenOpen(false)}
        data={phone ? { "data-teamspace-role-screen": "" } : { "data-teamspace-role-window": "" }}
        rows={[...ROLES.map((role) => ({ key: role, label: name(role), disabled: rolesLocked && role !== value && role !== openRole,
          description: t(role === "owner" ? "memberList.ownerRoleDescription" : "memberList.memberRoleDescription"),
          data: { "data-teamspace-role-option": role }, run: () => { if (role !== value) onChange(role); } })),
        ...(onRemove ? [{ key: "remove", label: t("memberList.remove"), danger: true, separatorBefore: false, disabled: removeDisabled,
          data: { "data-teamspace-role-remove": "" }, run: onRemove }] : [])]} /> : null}
    </>;
  }
  return <>
    <button {...buttonProps} ref={trigger} className={compact ? styles.roleButtonCompact : styles.roleButton} aria-label={label ?? name(value)}
      data-teamspace-role={value}>
      <span>{name(value)}</span><ChevronDown size="tiny" aria-hidden="true" />
    </button>
    {state.isOpen ? createPortal(<div ref={menu} className={styles.roleMenu} data-role-menu={compact ? "invite" : "member"}
      style={{ ...popoverPlacementStyle(placement), position: "fixed", right: "auto", bottom: "auto" }}>
      <NativeActionMenu {...menuProps} aria-label={label ?? name(value)} autoFocus={autoFocus}>
        {ROLES.map((role) => <NativeActionMenuItem id={role} key={role} role="menuitemradio" aria-checked={value === role} aria-label={name(role)}
          disabled={rolesLocked && role !== value && role !== openRole}
          onClick={() => { state.close(); if (role !== value) onChange(role); }}>
          <span className={styles.roleText}><span>{name(role)}</span>
            <span>{t(role === "owner" ? "memberList.ownerRoleDescription" : "memberList.memberRoleDescription")}</span></span>
          {value === role ? <CheckIcon className={`${styles.roleCheck} ${styles.roleCheckTop}`} size="small" aria-hidden="true" /> : null}
        </NativeActionMenuItem>)}
        {onRemove ? <NativeActionMenuItem id="remove" className={styles.roleRemove} disabled={removeDisabled}
          onClick={() => { state.close(); onRemove(); }}>{t("memberList.remove")}</NativeActionMenuItem> : null}
      </NativeActionMenu>
    </div>, document.body) : null}
  </>;
}
