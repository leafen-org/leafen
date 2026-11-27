import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "@/components/shell/PhoneSheet";
import { CheckIcon, ChevronDown, ChevronRight, PersonIcon } from "@/icons/leafen";
import { useMenuButtonTrigger } from "@/lib/menuButtonTrigger";
import { NativeActionMenu, NativeActionMenuItem } from "@/lib/nativeActionMenu";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { SettingsTouchMenuWindow } from "./SettingsTouchMenuWindow";
import styles from "./WorkspaceAddMembersDialog.module.css";
import roleStyles from "./WorkspaceMemberRoleMenu.module.css";
import phoneCss from "./WorkspaceAddMembersPhone.module.css";
import own from "./WorkspaceAddMembersRole.module.css";

/** The role "멤버 추가하기" brings: 멤버, or 제한된 멤버 where the organization module gives it. */
export type AddMembersRole = "member" | "restricted";

const roleName = (role: AddMembersRole) => (role === "member" ? "memberRoleMember" : "memberRoleRestricted");
const roleDescription = (role: AddMembersRole) => (role === "member" ? "memberRoleMemberDescription" : "memberRoleRestrictedDescription");

/**
 * "역할 선택"'s card, as Notion's "멤버 추가하기" (seen 2026-11-16 at 1440,
 * the free workspace's owner): the role's mark, name and what it allows over
 * a ⌄, pressed to open a menu as wide as the card 8 under it — "역할 선택"
 * over a 45 row for each role, the current one ticked at its end — whose
 * pick becomes the card. Leafen gives the roles it has (user decision
 * 2026-10-30): 멤버 and, where the organization module gives it, 제한된 멤버;
 * never the owner, who is one. With one role the card is the role alone.
 * Notion's menu ends with a help link, Leafen's has none.
 */
export function AddMembersRoleCard({ value, roles, disabled, onChange }: {
  value: AddMembersRole; roles: readonly AddMembersRole[]; disabled: boolean; onChange(role: AddMembersRole): void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const trigger = useRef<HTMLButtonElement>(null), menu = useRef<HTMLDivElement>(null);
  const choosable = roles.length > 1;
  const { state, menuProps, buttonProps, autoFocus } = useMenuButtonTrigger(trigger, disabled || !choosable);
  const placement = usePopoverPlacement(menu, state.isOpen ? (size, viewport) => placeAround(
    popoverRectOf(trigger.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { align: "start", gap: 8 },
  ) : null, [state.isOpen], undefined, { kind: "picker", onEscape: state.close, onOutsidePress: state.close,
    inside: () => [trigger.current], canRestoreFocus: () => !disabled });
  const body = <>
    <PersonIcon size="regular" aria-hidden="true" />
    <span className={styles.roleText}><span>{t(roleName(value))}</span><span>{t(roleDescription(value))}</span></span>
  </>;
  if (!choosable) return <div className={styles.role} data-add-members-role={value}>{body}</div>;
  return <>
    <button {...buttonProps} ref={trigger} type="button" className={`${styles.role} ${own.roleChoice}`} data-add-members-role={value}
      aria-label={`${t("memberRoleMenuTitle")}: ${t(roleName(value))}`}>
      {body}<ChevronDown size="small" className={own.roleChevron} aria-hidden="true" />
    </button>
    {state.isOpen ? createPortal(<div ref={menu} className={`${roleStyles.menu} ${own.roleMenu}`} data-add-members-role-menu
      style={{ ...popoverPlacementStyle(placement), position: "fixed", width: trigger.current?.getBoundingClientRect().width }}>
      <div className={`${roleStyles.title} ${own.roleTitle}`} aria-hidden="true">{t("memberRoleMenuTitle")}</div>
      <NativeActionMenu {...menuProps} aria-label={t("memberRoleMenuTitle")} autoFocus={autoFocus}>
        {roles.map((role) => <NativeActionMenuItem id={role} key={role} role="menuitemradio" aria-checked={value === role}
          aria-label={t(roleName(role))} data-add-members-role-option={role}
          onClick={() => { state.close(); if (role !== value) onChange(role); }}>
          <PersonIcon size="regular" aria-hidden="true" />
          <span className={roleStyles.text}><span>{t(roleName(role))}</span><span>{t(roleDescription(role))}</span></span>
        </NativeActionMenuItem>)}
      </NativeActionMenu>
    </div>, document.body) : null}
  </>;
}

/**
 * "사용 권한 수준"'s row on a touch screen, as Notion's phone and iPad "멤버
 * 추가" ("워크스페이스 소유자 ›", seen 2026-11-16 by w1540): the role and a
 * ›, pressed to choose — on a phone a whole screen "역할 선택" with a blue
 * "완료", each role's row with its mark, name and what it allows, the current
 * one ticked (the members list's role screen, WorkspaceMemberRoleMenu); wider,
 * the settings' middle window. With one role the row is the role alone.
 */
export function AddMembersRoleRow({ value, roles, wide, disabled, onChange }: {
  value: AddMembersRole; roles: readonly AddMembersRole[]; wide: boolean; disabled: boolean; onChange(role: AddMembersRole): void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const [open, setOpen] = useState(false);
  if (roles.length < 2) return <div className={phoneCss.row} data-add-members-role={value}>{t(roleName(value))}</div>;
  const close = () => setOpen(false);
  const label = t("memberRoleMenuTitle");
  return <>
    <button type="button" className={phoneCss.row} data-add-members-role={value} aria-haspopup="dialog" aria-expanded={open}
      aria-label={`${label}: ${t(roleName(value))}`} disabled={disabled} onClick={() => setOpen(true)}>
      {t(roleName(value))}<ChevronRight size="page" aria-hidden="true" />
    </button>
    {open && wide ? <SettingsTouchMenuWindow title={label} label={label} current={value} close={close}
      data={{ "data-add-members-role-window": "" }}
      rows={roles.map((role) => ({ key: role, label: t(roleName(role)), icon: <PersonIcon size="regular" aria-hidden="true" />,
        description: t(roleDescription(role)), data: { "data-add-members-role-option": role },
        run: () => { if (role !== value) onChange(role); } }))} /> : null}
    {open && !wide ? (
      <PhoneSheet size="screen" title={label} label={label} tone="settings" onClose={close}
        action={{ label: t("common:actions.done"), onPress: close }} data={{ "data-add-members-role-screen": "" }}>
        <PhoneSheetGroup>
          {roles.map((role) => (
            <PhoneSheetRow key={role} more={false} label={t(roleName(role))} icon={<PersonIcon size="regular" />}
              description={t(roleDescription(role))} value={value === role ? <CheckIcon size="regular" aria-hidden="true" /> : undefined}
              data={{ "data-add-members-role-option": role }} onPress={() => { close(); if (role !== value) onChange(role); }} />
          ))}
        </PhoneSheetGroup>
      </PhoneSheet>
    ) : null}
  </>;
}
