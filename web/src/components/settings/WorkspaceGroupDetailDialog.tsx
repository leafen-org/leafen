import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { SidebarActionMenu, type SidebarMenuRow } from "@/components/sidebar/SidebarActionMenu";
import { useSidebarLayout } from "@/components/shell/AppShell";
import type { TeamspaceFromGroup } from "@/components/teamspace/TeamspaceCreatePanel";
import { canCreateNativeTeamspace } from "@/lib/nativeWorkspaceRolePolicy";
import { Copy, DotsHorizontal, Pencil, PersonCircleFillIcon, Search, Trash, X } from "@/icons/leafen";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { PageIconPicker } from "@/components/page/PageIconPicker";
import { useCopyNotice } from "@/lib/copyNotice";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import { ReadFailed } from "@/components/ReadFailed";
import type { Session, Workspace } from "@/types";
import type { WorkspacePermissionGroupDetail } from "@/workspaceMembershipApi";
import type { WorkspaceMemberDirectoryItem } from "@/workspaceMembershipWire.generated";
import { WorkspaceGroupAddMembers } from "./WorkspaceGroupAddMembers";
import { WorkspaceGroupIcon } from "./WorkspaceGroupIcon";
import { SettingsPhoneMenuScreen } from "./SettingsPhoneMenuScreen";
import styles from "./WorkspaceGroupDetail.module.css";
import nameFieldStyles from "./NameInPlaceField.module.css";
import { caretAtEnd, nameFieldKeyDown } from "./nameInPlaceField";
import phoneLines from "./WorkspaceMemberPhoneList.module.css";
import { KeptIconGlyph, keptIconPicture, keptIconUploadTarget } from "@/components/teamspace/KeptIconGlyph";
import { HoverTip } from "@/components/IconTipButton";
import { useKeptTeamspaces } from "@/lib/workspaceTeamspaces";

type GroupTab = "general" | "members" | "teamspaces";

/**
 * A group opened from 설정 → 사람 → 그룹, as Notion's window (seen
 * 2026-11-09, measured 2026-11-11): its 64px mark — a person in a ring until
 * it has an icon — its name — the owner's pencil turns it into a field kept
 * as it is left — and how many are in it, then 일반 (its ID to copy, "그룹
 * 정보로 팀스페이스 생성" and, for the owner, deleting it), 멤버 (a search,
 * "멤버 추가" and each member's "…" with "그룹에서 제거") and 팀스페이스 (the
 * teamspaces it is in). "팀스페이스 생성하기" hands the group to the list,
 * which opens the new teamspace's window in this one's place
 * (onMakeTeamspace).
 */
export function WorkspaceGroupDetailDialog({ session, workspace, members, groupId, canManage, onClose, onChanged, onMakeTeamspace }: {
  session: Session;
  workspace: Workspace;
  members: WorkspaceMemberDirectoryItem[];
  groupId: string;
  canManage: boolean;
  onClose(): void;
  onChanged(): void;
  onMakeTeamspace(group: TeamspaceFromGroup): void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "peopleProfiles", "common"]);
  const { phone } = useSidebarLayout(true);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const menuButton = useRef<HTMLButtonElement>(null);
  const [group, setGroup] = useState<WorkspacePermissionGroupDetail | null>(null);
  const [tab, setTab] = useState<GroupTab>("general");
  const [naming, setNaming] = useState(false);
  const [name, setName] = useState("");
  const [query, setQuery] = useState("");
  const [adding, setAdding] = useState(false);
  const [menuFor, setMenuFor] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // A failed read of the group, said where it shows with a way to read again.
  const [readFailed, setReadFailed] = useState("");
  const [readTurn, setReadTurn] = useState(0);
  const [iconPicking, setIconPicking] = useState(false);
  // Whoever may make a teamspace makes one from the group; where the
  // workspace keeps that to its owners (as the sidebar last read it), the
  // button is dim for everyone else and its tip says why, as Notion's.
  const keptTeamspaces = useKeptTeamspaces(session.user.id, workspace.id);
  const canMakeTeamspace = canCreateNativeTeamspace(workspace, keptTeamspaces?.ownersOnlyCreate);
  const copy = useCopyNotice();
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => leave(),
  });

  useEffect(() => {
    const controller = new AbortController();
    api.readWorkspaceGroup(session, workspace.id, groupId, controller.signal).then((detail) => {
      if (controller.signal.aborted) return;
      setGroup(detail);
      setName(detail.name);
    }, (failure) => { if (!controller.signal.aborted) setReadFailed(settingsErrorMessage(failure, t("groupLoadFailed"))); });
    return () => controller.abort();
  }, [groupId, readTurn, session, t, workspace.id]);

  const byId = new Map(members.filter((member) => !member.app).map((member) => [member.userId, member]));

  async function change(input: { name?: string; icon?: string; add?: string[]; remove?: string[] }) {
    if (busy) return false;
    setBusy(true);
    setError("");
    try {
      const next = await api.changeWorkspaceGroup(session, workspace.id, groupId, input);
      setGroup(next);
      setName(next.name);
      onChanged();
      return true;
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("groupUpdateFailed")));
      if (group) setName(group.name);
      return false;
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    if (!await confirmAction({ message: t("groupDeleteConfirm"), detail: t("groupDeleteConfirmDetail"),
      confirmLabel: t("groupDeleteRow"), danger: true })) return;
    setBusy(true);
    setError("");
    try {
      await api.deleteWorkspaceGroup(session, workspace.id, groupId);
      onChanged();
      onClose();
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("groupUpdateFailed")));
      setBusy(false);
    }
  }

  // The name keeps as its field is left, or as the window closes with it
  // still in hand; left empty, it is the name it was.
  const nameAsked = useRef("");
  const saveName = () => {
    setNaming(false);
    const trimmed = name.trim();
    if (!group || !trimmed || trimmed === group.name) {
      if (group) setName(group.name);
      return;
    }
    if (trimmed === nameAsked.current) return;
    nameAsked.current = trimmed;
    void change({ name: trimmed });
  };
  const leave = () => {
    if (busy || adding || iconPicking) return;
    if (naming) saveName();
    onClose();
  };
  // The teamspace made from the group starts with its name and mark
  // (TeamspaceDialog takes them once a group). Its window takes this one's
  // place, as Notion's (seen 2026-11-16 in its free workspace, 1440 dark):
  // "취소" or closing it, or closing the new teamspace's window, leaves the
  // groups list.
  const makeTeamspace = () => {
    if (!group || busy || adding || iconPicking) return;
    // A name still in its field is kept first, and the teamspace takes it.
    const typed = naming ? name.trim() : "";
    if (naming) saveName();
    onMakeTeamspace({ id: group.id, name: typed || group.name, icon: group.icon });
  };

  const wanted = query.trim().toLowerCase();
  const shownMembers = (group?.members ?? []).map(({ userId }) => ({ userId, member: byId.get(userId) }))
    .filter(({ userId, member }) => !wanted || `${member?.displayName ?? ""} ${member?.email ?? ""} ${userId}`.toLowerCase().includes(wanted));
  const shownTeamspaces = (group?.teamspaces ?? []).filter((teamspace) => !wanted || teamspace.name.toLowerCase().includes(wanted));
  const openTab = (next: GroupTab) => { setTab(next); setQuery(""); };
  const menuMember = menuFor ? byId.get(menuFor) : undefined;
  const memberMenuRows = (userId: string, red: boolean): SidebarMenuRow[] => [{ key: "remove", label: t("groupRemoveMember"),
    icon: <Trash size="regular" aria-hidden="true" />, danger: red, run: () => { setMenuFor(null); void change({ remove: [userId] }); } }];

  const dialog = (
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }}>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} onClick={leave} />
      <section ref={dialogRef} className={styles.window} role="dialog" aria-modal="true" aria-labelledby={titleId}
        aria-busy={busy || undefined} tabIndex={-1} data-group-detail-dialog={groupId}>
        <button type="button" className={styles.close} aria-label={t("common:actions.close")} onClick={leave}><X size="small" aria-hidden="true" /></button>
        {/* Its icon; the owner picks another from it, as from Notion's. */}
        <span className={styles.markSlot}>
          {canManage && group ? (
            <button type="button" className={styles.mark} aria-label={t("groupIconChange")} disabled={busy}
              onClick={() => setIconPicking(true)} data-group-icon-button>
              <GroupWindowMark icon={group.icon} />
            </button>
          ) : <span className={styles.mark} aria-hidden="true"><GroupWindowMark icon={group?.icon} /></span>}
          {iconPicking ? (
            <ErrorBoundary scope="group-icon-picker">
              <PageIconPicker placement="inline" offerRandom={false} current={group?.icon} uploadTarget={keptIconUploadTarget(workspace.id, "group")}
                onPick={(emoji) => { setIconPicking(false); void change({ icon: emoji }); }}
                onPickBuiltInIcon={(value) => { setIconPicking(false); void change({ icon: value }); }}
                onPickImage={(value) => { const picture = keptIconPicture(value); if (!picture) return; setIconPicking(false); void change({ icon: picture }); }}
                onRemove={() => { setIconPicking(false); void change({ icon: "" }); }}
                onClose={() => setIconPicking(false)} />
            </ErrorBoundary>
          ) : null}
        </span>
        {naming ? (
          <input className={`${nameFieldStyles.field} ${styles.nameField}`} value={name} maxLength={200} autoComplete="off" autoFocus
            aria-label={t("groupCreateNameLabel")} onFocus={caretAtEnd} data-focus-leaves
            onChange={(event) => setName(event.target.value)} onBlur={saveName}
            onKeyDown={(event) => nameFieldKeyDown(event, {
              onEnter: (field) => field.blur(),
              onEscape: () => { setName(group?.name ?? ""); nameAsked.current = group?.name ?? ""; setNaming(false); },
            })} />
        ) : (
          <div className={styles.nameLine}>
            <h2 id={titleId}>{group?.name ?? ""}</h2>
            {canManage && group ? (
              <button type="button" className={styles.pencil} aria-label={t("groupRename")} disabled={busy} onClick={() => setNaming(true)}>
                <Pencil size="regular" aria-hidden="true" />
              </button>
            ) : null}
          </div>
        )}
        <p className={styles.count}>{group ? t("groupDetailCount", { count: group.memberCount }) : ""}</p>
        <div className={styles.tabs} role="tablist">
          {(["general", "members", "teamspaces"] as const).map((key) => (
            <button key={key} type="button" role="tab" aria-selected={tab === key} className={styles.tab} onClick={() => openTab(key)}>
              {t(key === "general" ? "groupTabGeneral" : key === "members" ? "groupTabMembers" : "groupTabTeamspaces")}
            </button>
          ))}
        </div>
        {error ? <p className={styles.error} role="alert">{error}</p> : null}
        {readFailed && !group ? <ReadFailed message={readFailed} onRetry={() => { setReadFailed(""); setReadTurn((turn) => turn + 1); }} /> : null}
        {tab === "general" && group ? (
          <div className={styles.panel} role="tabpanel">
            <h3 className={styles.heading}>{t("groupManageTitle")}</h3>
            <div className={styles.row}>
              <span className={styles.rowText}><strong>{t("groupIdLabel")}</strong></span>
              <span className={styles.groupId}>{group.id}
                <button type="button" aria-label={t("groupIdCopy")} onClick={() => void copy(group.id, t("groupIdCopied"))}>
                  <Copy size="tiny" aria-hidden="true" /></button></span>
            </div>
            <div className={styles.row}>
              <span className={styles.rowText}><strong>{t("groupCreateTeamspaceRow")}</strong><span>{t("groupCreateTeamspaceDetail")}</span></span>
              {canMakeTeamspace ? (
                <button type="button" className={styles.secondary} disabled={busy} data-group-create-teamspace onClick={makeTeamspace}>
                  {t("groupCreateTeamspace")}
                </button>
              ) : (
                <HoverTip label={t("groupCreateTeamspaceDisabled")} width={200}>
                  <button type="button" className={styles.secondary} disabled data-group-create-teamspace>{t("groupCreateTeamspace")}</button>
                </HoverTip>
              )}
            </div>
            {canManage ? (
              <div className={styles.row}>
                <span className={styles.rowText}><strong>{t("groupDeleteRow")}</strong><span>{t("groupDeleteConfirmDetail")}</span></span>
                <button type="button" className={styles.danger} disabled={busy} onClick={() => void remove()}>{t("groupDeleteRow")}</button>
              </div>
            ) : null}
          </div>
        ) : null}
        {tab !== "general" && group ? (
          <div className={styles.panel} role="tabpanel">
            <div className={styles.searchLine}>
              <label className={styles.search}>
                <Search size="small" aria-hidden="true" />
                <input value={query} autoComplete="off" onChange={(event) => setQuery(event.target.value)}
                  placeholder={t(tab === "members" ? "groupMembersSearch" : "groupTeamspacesSearch")}
                  aria-label={t(tab === "members" ? "groupMembersSearch" : "groupTeamspacesSearch")} />
              </label>
              {tab === "members" && canManage ? (
                <button type="button" className={styles.primary} disabled={busy} onClick={() => setAdding(true)}>{t("groupMembersAdd")}</button>
              ) : null}
            </div>
            {tab === "members" ? (
              shownMembers.length ? (
                <ul className={styles.list} data-group-members>
                  {shownMembers.map(({ userId, member }) => {
                    const label = member?.displayName || member?.email || userId;
                    return (
                      <li key={userId} className={phone ? `${styles.person} ${phoneLines.personLine}` : styles.person} data-phone={phone ? "" : undefined}>
                        <PersonAvatar userId={userId} name={member?.displayName} size={28} />
                        <span className={phone ? `${styles.personText} ${phoneLines.personWords}` : styles.personText}>
                          <span className={phone ? `${styles.personName} ${phoneLines.personWordsName}` : styles.personName}>{label}
                            {member?.restricted ? <span className={phone ? styles.phoneBadge : styles.badge}>{t("peopleProfiles:restricted")}</span> : null}</span>
                          {member?.email && member.email !== label ? <span>{member.email}</span> : null}
                        </span>
                        {canManage ? (
                          <button type="button" className={styles.more} aria-label={t("groupMemberActions", { name: label })} aria-haspopup="menu"
                            disabled={busy} onClick={(event) => { menuButton.current = event.currentTarget; setMenuFor(userId); }}>
                            <DotsHorizontal size="small" aria-hidden="true" />
                          </button>
                        ) : null}
                      </li>
                    );
                  })}
                </ul>
              ) : <p className={styles.empty}>{t(wanted ? "groupNoMatch" : "groupMembersNone")}</p>
            ) : shownTeamspaces.length ? (
              <ul className={styles.list} data-group-teamspaces>
                {shownTeamspaces.map((teamspace) => (
                  <li key={teamspace.id} className={styles.teamspace}>
                    <span className={styles.teamspaceMark} aria-hidden="true"><KeptIconGlyph icon={teamspace.icon} fallback={teamspace.name.trim().slice(0, 1).toUpperCase() || "T"} /></span>
                    <span>{teamspace.name}</span>
                  </li>
                ))}
              </ul>
            ) : <p className={styles.empty}>{t(wanted ? "groupNoMatch" : "groupTeamspacesNone")}</p>}
          </div>
        ) : null}
      </section>
      {/* A member's "…" opens right under it from its start, past the window's edge, as Notion's (2026-11-11);
          on a phone, the whole screen its settings menus take (SettingsPhoneMenuScreen), its removal red. */}
      {menuFor && phone ? (
        <SettingsPhoneMenuScreen title={menuMember?.displayName || menuMember?.email || menuFor}
          label={t("groupMemberActions", { name: menuMember?.displayName || menuFor })} close={() => setMenuFor(null)}
          rows={memberMenuRows(menuFor, true)} />
      ) : menuFor ? (
        <SidebarActionMenu button={menuButton} backdropLabel={t("common:actions.close")} width={248} gap={0} pastWindow
          label={t("groupMemberActions", { name: menuMember?.displayName || menuFor })} close={() => setMenuFor(null)}
          rows={memberMenuRows(menuFor, false)} />
      ) : null}
      {adding && group ? (
        <WorkspaceGroupAddMembers groupName={group.name} candidates={[...byId.values()].filter((member) => !group.members.some((held) => held.userId === member.userId))}
          busy={busy} onClose={() => setAdding(false)}
          onAdd={async (userIds) => { if (await change({ add: userIds })) setAdding(false); }} />
      ) : null}
    </div>
  );
  return typeof document === "undefined" ? dialog : createPortal(dialog, document.body);
}

/**
 * The window's 64px mark: the group's icon, or a filled person in a ring, as
 * Notion's (2026-11-11; its glyph 2026-11-15 — Notion draws a group in three
 * marks: the new group window's two people, a row's filled two people and this).
 */
function GroupWindowMark({ icon }: { icon?: string }) {
  return icon ? <WorkspaceGroupIcon icon={icon} size={64} /> : <PersonCircleFillIcon pixels={64} aria-hidden="true" />;
}
