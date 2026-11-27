import { FormEvent, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { confirmAction } from "@/components/ConfirmActionDialog";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { SidebarActionMenu } from "@/components/sidebar/SidebarActionMenu";
import { DotsHorizontal, GroupPeopleIcon, Plus, Search, Trash } from "@/icons/leafen";
import type { OrganizationSettingsMutation } from "@/lib/workspaceOrganizationSettings";
import {
  organizationSettingsActions,
  type OrganizationSettingsGroup,
  type OrganizationSettingsMember,
} from "@/organizationWire.generated";
import dialogs from "./DeveloperSettings.module.css";
import { SettingsFormDialog } from "./SettingsFormDialog";
import rows from "./SettingsRows.module.css";
import { WorkspaceGroupAddMembers } from "./WorkspaceGroupAddMembers";
import people from "./WorkspaceGroupDetail.module.css";
import groupStyles from "./WorkspaceGroups.module.css";
import { styles } from "./workspaceSettingsOverviewStyles";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/**
 * The organization's groups, as 사람 → 그룹's list: what they are for, and a
 * group opens to its name and members. A group an identity provider keeps
 * (SCIM) only shows them.
 */
export function WorkspaceOrganizationGroups({
  groups,
  members,
  canManage,
  disabled,
  mutate,
}: {
  groups: OrganizationSettingsGroup[];
  members: OrganizationSettingsMember[];
  userId: string;
  canManage: boolean;
  disabled: boolean;
  mutate: (mutation: OrganizationSettingsMutation) => void | Promise<void>;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [creating, setCreating] = useState(false);
  const [openId, setOpenId] = useState("");
  const open = groups.find((group) => group.id === openId);
  return (
    <section className={styles.section} role="region" aria-label={t("groupsTitle")} data-organization-groups>
      <div className={styles.sectionTitle}>{t("groupsTitle")}</div>
      <div className={styles.securityRow}>
        <div className={styles.securityInstruction}>
          <strong>{t("organizationGroupsRow")}</strong>
          <span>{t("organizationGroupsDescription")}</span>
        </div>
        {canManage ? (
          <button type="button" className={rows.addLink} disabled={disabled} data-organization-group-add onClick={() => setCreating(true)}>
            <Plus size="small" aria-hidden="true" />{t("addGroup")}
          </button>
        ) : null}
      </div>
      {groups.length ? (
        <ul className={`${groupStyles.list} ${rows.listGap}`}>
          {groups.map((group) => (
            <li key={`${group.managedBy}:${group.id}`} className={groupStyles.row} data-organization-group={group.id}>
              <button type="button" className={groupStyles.rowButton} aria-haspopup="dialog" onClick={() => setOpenId(group.id)}>
                <span className={groupStyles.rowIcon} aria-hidden="true"><GroupPeopleIcon size="regular" /></span>
                <span className={groupStyles.rowName}>{group.name}</span>
                <span className={groupStyles.rowMeta}>
                  {[group.managedBy === "scim" ? "SCIM" : "", t("groupMemberCount", { count: group.memberCount })].filter(Boolean).join(" · ")}
                </span>
              </button>
            </li>
          ))}
        </ul>
      ) : <p className={rows.emptyLine}>{t("noGroups")}</p>}
      {creating ? (
        <GroupCreateDialog disabled={disabled} onClose={() => setCreating(false)}
          onCreate={(name) => mutate({ action: organizationSettingsActions.groupCreate, value: name })} />
      ) : null}
      {open ? (
        <GroupDialog group={open} members={members} canManage={canManage} disabled={disabled} mutate={mutate}
          onClose={() => setOpenId("")} />
      ) : null}
    </section>
  );
}

function GroupCreateDialog({ disabled, onCreate, onClose }: {
  disabled: boolean;
  onCreate(name: string): void | Promise<void>;
  onClose(): void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const [name, setName] = useState("");
  const nameRef = useRef<HTMLInputElement>(null);
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (!name.trim()) return;
    void onCreate(name.trim());
    onClose();
  };
  return (
    <SettingsFormDialog title={t("addGroup")} busy={false} initialFocus={nameRef} onClose={onClose}>
      <form className={dialogs.dialogBody} onSubmit={submit} data-organization-group-create>
        <label className={rows.dialogField}>
          {t("groupCreateNameLabel")}
          <input ref={nameRef} value={name} autoComplete="off" placeholder={t("placeholderGroupExamples")}
            onChange={(event) => setName(event.target.value)} />
        </label>
        <div className={dialogs.dialogActions}>
          <button type="button" className={dialogs.cancel} onClick={onClose}>{t("common:actions.cancel")}</button>
          <button type="submit" className={dialogs.primary} disabled={disabled || !name.trim()}>{t("addGroup")}</button>
        </div>
      </form>
    </SettingsFormDialog>
  );
}

/**
 * A group's window: its name, and its members as a workspace group's window
 * lists them (2026-11-13: the organization's read as a bare list of names) —
 * a search over them beside "멤버 추가", which finds the organization's
 * people by name or email, then each with their picture, name and email and
 * a "…" with "그룹에서 제거".
 */
function GroupDialog({ group, members, canManage, disabled, mutate, onClose }: {
  group: OrganizationSettingsGroup;
  members: OrganizationSettingsMember[];
  canManage: boolean;
  disabled: boolean;
  mutate: (mutation: OrganizationSettingsMutation) => void | Promise<void>;
  onClose(): void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "common"]);
  const editable = canManage && group.managedBy === "manual";
  const [name, setName] = useState(group.name);
  const nameRef = useRef<HTMLInputElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const [query, setQuery] = useState("");
  const [adding, setAdding] = useState(false);
  const [menuFor, setMenuFor] = useState<string | null>(null);
  const menuButton = useRef<HTMLButtonElement | null>(null);
  const inGroup = new Set(group.members.map((member) => member.organizationMemberId));
  const available = members.filter((member) => !inGroup.has(member.id));
  const wanted = query.trim().toLowerCase();
  const shown = group.members.filter((member) => !wanted || `${member.displayName} ${member.email}`.toLowerCase().includes(wanted));
  const menuMember = group.members.find((member) => member.id === menuFor);
  // The people picked go in one after another: each change is made against
  // the settings as the one before left them.
  const [queued, setQueued] = useState<string[]>([]);
  useEffect(() => {
    if (!queued.length || disabled) return;
    const [next, ...rest] = queued;
    setQueued(rest);
    void mutate({ action: organizationSettingsActions.groupMemberAdd, targetId: group.id, secondaryTargetId: next });
  }, [disabled, group.id, mutate, queued]);
  const addPeople = (userIds: string[]) => {
    setQueued(userIds.flatMap((picked) => available.filter((candidate) => candidate.userId === picked).map((member) => member.id)));
    setAdding(false);
  };
  const saveName = () => {
    if (name.trim() && name.trim() !== group.name) {
      void mutate({ action: organizationSettingsActions.groupUpdate, targetId: group.id, value: name.trim() });
    }
  };
  return (
    <SettingsFormDialog title={group.name} busy={false} wide initialFocus={editable ? nameRef : closeRef} onClose={() => { saveName(); onClose(); }}>
      <div className={dialogs.dialogBody} data-organization-group-dialog={group.id}>
        {editable ? (
          <label className={rows.dialogField}>
            {t("groupCreateNameLabel")}
            {/* Saved on Enter, on leaving the window and on closing it, not on moving to its own
                buttons: the save holds them a moment and their click was lost (2026-11-05: 제거 took two). */}
            <input ref={nameRef} value={name} disabled={disabled} onChange={(event) => setName(event.target.value)}
              onKeyDown={(event) => { if (fieldEnter(event)) saveName(); }}
              onBlur={(event) => {
                if (!event.currentTarget.closest("[role=dialog]")?.contains(event.relatedTarget as Node | null)) saveName();
              }} />
          </label>
        ) : <p className={dialogs.dialogNote}>{t("organizationGroupScim")}</p>}
        {/* The search and the list as one part, 20px apart, as a workspace group's members tab. */}
        <div>
          <div className={people.searchLine}>
            <label className={people.search}>
              <Search size="small" aria-hidden="true" />
              <input value={query} autoComplete="off" onChange={(event) => setQuery(event.target.value)}
                placeholder={t("groupMembersSearch")} aria-label={t("groupMembersSearch")} />
            </label>
            {editable ? (
              <button type="button" className={people.primary} disabled={disabled || !available.length} onClick={() => setAdding(true)}
                aria-label={t("ariaAddMemberToGroup", { name: group.name })}>{t("groupMembersAdd")}</button>
            ) : null}
          </div>
          {shown.length ? (
            <ul className={people.list} data-organization-group-members>
              {shown.map((member) => {
                const label = member.displayName || member.email || t("member");
                return (
                  <li key={member.id} className={people.person}>
                    <PersonAvatar userId={member.userId} name={member.displayName} size={28} />
                    <span className={people.personText}>
                      <span className={people.personName}>{label}</span>
                      {member.email && member.email !== label ? <span>{member.email}</span> : null}
                    </span>
                    {editable ? (
                      <button type="button" className={people.more} aria-label={t("groupMemberActions", { name: label })} aria-haspopup="menu"
                        disabled={disabled} onClick={(event) => { menuButton.current = event.currentTarget; setMenuFor(member.id); }}>
                        <DotsHorizontal size="small" aria-hidden="true" />
                      </button>
                    ) : null}
                  </li>
                );
              })}
            </ul>
          ) : <p className={people.empty}>{t(wanted ? "groupNoMatch" : "groupMembersNone")}</p>}
        </div>
        {group.membersHasMore ? <p className={dialogs.dialogNote}>{t("groupMemberCount", { count: group.memberCount })}</p> : null}
        <div className={dialogs.dialogActions}>
          {editable ? (
            <button type="button" className={dialogs.secondary} disabled={disabled}
              // Its members lose what was shared with it at once: asked first.
              onClick={() => void confirmAction({ message: t("groupDeleteConfirm"), detail: t("groupDeleteConfirmDetail"), confirmLabel: t("groupDelete"), danger: true })
                .then((confirmed) => {
                  if (!confirmed) return;
                  void mutate({ action: organizationSettingsActions.groupDelete, targetId: group.id });
                  onClose();
                })}>
              {t("groupDelete")}
            </button>
          ) : null}
          <button ref={closeRef} type="button" className={dialogs.primary} onClick={() => { saveName(); onClose(); }}>{t("common:actions.done")}</button>
        </div>
      </div>
      {menuFor && menuMember ? (
        <SidebarActionMenu button={menuButton} backdropLabel={t("common:actions.close")} width={248} gap={0} pastWindow
          label={t("groupMemberActions", { name: menuMember.displayName || menuMember.email })} close={() => setMenuFor(null)}
          rows={[{ key: "remove", label: t("groupRemoveMember"), icon: <Trash size="regular" aria-hidden="true" />,
            run: () => {
              setMenuFor(null);
              void mutate({ action: organizationSettingsActions.groupMemberRemove, targetId: group.id, secondaryTargetId: menuMember.organizationMemberId });
            } }]} />
      ) : null}
      {adding ? (
        <WorkspaceGroupAddMembers groupName={group.name} candidates={available} busy={disabled} lead={t("organizationGroupAddLead")}
          onClose={() => setAdding(false)} onAdd={addPeople} />
      ) : null}
    </SettingsFormDialog>
  );
}
