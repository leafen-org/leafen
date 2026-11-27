import { SwitchMark } from "@/components/Switch";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useShownChanges } from "@/lib/shownChanges";
import { ReadFailed } from "@/components/ReadFailed";
import type { TeamspacePageRole } from "@/teamspaceWire.generated";
import { HoverTip } from "@/components/IconTipButton";
import { LockIcon } from "@/icons/leafen";
import { defaultSecurityLock, securityKinds, teamspaceArchiveLock, teamspaceSecurity, type TeamspaceLeaveLock,
  type TeamspaceSecurity } from "./teamspaceDialogModel";
import { teamspaceGlyph } from "./KeptIconGlyph";
import { TeamspaceChoiceMenu, type TeamspaceChoice } from "./TeamspaceChoiceMenu";
import { TeamspaceOwnerGlyph } from "./TeamspaceOwnerGlyph";
import { TeamspaceKindIcon } from "./TeamspaceSecurityIcon";
import type { TeamspaceDialogController } from "./useTeamspaceDialog";
import { useTeamspaceInviteLink } from "./useTeamspaceInviteLink";
import styles from "./TeamspaceSettings.module.css";

const PAGE_SECURITY = ["disablePublishing", "disableGuests", "disableExport"] as const;

// Notion's order, the widest first.
const PAGE_ROLE_ORDER: TeamspacePageRole[] = ["full_access", "edit", "comment", "view"];

/** "설정": the description, the kind of teamspace with what each role may
 * do in its pages, and — for its members — leaving or archiving it. */
export function TeamspaceSettingsGeneral({ controller, workspaceName, leaveLock, onLeave, onArchive }: {
  controller: TeamspaceDialogController; workspaceName: string; leaveLock: TeamspaceLeaveLock; onLeave(): void; onArchive(): void;
}) {
  const { t } = useTranslation("teamspaceDialog");
  const { busy, canManageSelected, selected, workspaceAdmin } = controller;
  // The description shows as typed and goes out behind the dialog's other
  // writes, its field never locked by them, and an earlier answer never
  // replaces what is being typed; a refusal puts back what the teamspace
  // holds (lib/shownChanges.ts). Its kind, roles and sharing stay the
  // server's to decide first.
  const pending = useShownChanges<{ description: string }>();
  const [draft, setDraft] = useState<string | null>(null);
  if (!selected) return null;
  const description = draft ?? pending.shown.description ?? selected.description ?? "";
  function saveDescription() {
    const next = (draft ?? "").trim();
    setDraft(null);
    if (draft === null || next === (pending.shown.description ?? selected?.description ?? "")) return;
    void pending.change({ description: next }, () => controller.saveSelectedWith({ description: next }, saved));
  }
  const security = teamspaceSecurity(selected);
  const saved = t("notice.saved");
  const securityText = (kind: TeamspaceSecurity) => ({
    label: t(`security.${kind}`),
    description: t(`security.${kind}Description`, { workspace: workspaceName }),
  });
  // Each kind with its mark, the menu as wide as the box (Notion's, seen 2026-11-11).
  const securityChoices: TeamspaceChoice<TeamspaceSecurity>[] = securityKinds(workspaceAdmin, selected.isDefault).map((kind) => ({
    value: kind, ...securityText(kind), icon: <TeamspaceKindIcon security={kind} size="regular" />,
  }));
  // A default teamspace stays so while it is the only one, or for whoever
  // doesn't own the workspace; its tip says why (Notion's code, 2026-11-11).
  const lock = defaultSecurityLock(selected, controller.settings, workspaceAdmin);
  const ownsSelected = selected.membershipRole === "owner";
  const archiveLock = teamspaceArchiveLock(selected, controller.teamspaces, controller.memberCount);
  // Its members have at least what the rest of the workspace has: Notion
  // dims the members' roles under the rest's and offers the rest none over
  // the members' (seen 2026-11-18 at 1440 in two tabs); the server keeps no
  // other pair (teamspace_page_roles.go).
  const outsideRank = security === "open" || security === "closed" ? PAGE_ROLE_ORDER.indexOf(selected.openPageRole) : -1;
  const roleChoices: TeamspaceChoice<TeamspacePageRole>[] = PAGE_ROLE_ORDER.map((role, rank) => ({
    value: role, label: t(`pageRoles.${role}`), description: role === "view" ? undefined : t(`pageRoles.${role}Description`),
    disabled: outsideRank >= 0 && rank > outsideRank,
  }));
  // The rest of a closed teamspace may also have no access, as Notion's "사용 권한 없음"; an open one's always has a
  // role, as anyone may join it (seen 2026-11-18 at 1440 in two tabs).
  const outsideChoices: TeamspaceChoice<TeamspacePageRole>[] = [
    ...roleChoices.filter((choice) => PAGE_ROLE_ORDER.indexOf(choice.value) >= PAGE_ROLE_ORDER.indexOf(selected.memberPageRole))
      .map((choice) => ({ ...choice, disabled: false })),
    ...(security === "open" ? [] : [{ value: "none" as const, label: t("pageRoles.none") }]),
  ];
  const securityRow = <>
    <span className={styles.securityIcon}><TeamspaceKindIcon security={security} size="regular" /></span>
    <span className={styles.securityText}><span>{securityText(security).label}</span><span>{securityText(security).description}</span></span>
  </>;

  function changeSecurity(kind: TeamspaceSecurity) {
    void controller.changeSecurity(selected, kind, saved, t("notice.defaultSet"));
  }

  return (
    <div className={styles.general} data-teamspace-settings-general>
      <label className={styles.field} data-waiting-note={pending.note("description")}>
        <span>{t("fields.description")}</span>
        <textarea value={description} rows={1} maxLength={500} placeholder={t("general.descriptionEmpty")} disabled={!canManageSelected}
          onFocus={() => setDraft(description)} onChange={(event) => setDraft(event.target.value)} onBlur={saveDescription} />
      </label>
      <div className={styles.securityBox}>
        {/* Only its owners change its kind, even the workspace's owners not (Notion's code, 2026-11-11). */}
        {ownsSelected && !lock ? (
          <TeamspaceChoiceMenu value={security} choices={securityChoices} label={t("security.menu")} disabled={busy}
            className={styles.securityButton} wide gap={4} marks="ink" markInset={5} onChange={changeSecurity} data-teamspace-security={security}>
            {securityRow}
          </TeamspaceChoiceMenu>
        ) : ownsSelected ? (
          <HoverTip label={t(lock === "onlyDefault" ? "security.onlyDefaultLocked" : "security.notAdminLocked")} width={350} className={styles.securityTip}>
            <div className={styles.securityButton} data-choice-marks="ink" data-teamspace-security={security} data-teamspace-security-lock={lock}>{securityRow}</div>
          </HoverTip>
        ) : <div className={styles.securityButton} data-choice-marks="ink" data-teamspace-security={security}>{securityRow}</div>}
        <div className={styles.securityRule} aria-hidden="true" />
        <div className={styles.roleRow}>
          <TeamspaceOwnerGlyph teamspace={selected} />
          <span>{t("roleRows.owner")}</span>
          <span className={styles.boxRole}>{t("pageRoles.full_access")}</span>
        </div>
        <div className={styles.roleRow}>
          <span className={styles.roleGlyph}>{teamspaceGlyph(selected)}</span>
          <span>{t("roleRows.member")}</span>
          {canManageSelected ? <TeamspaceChoiceMenu value={selected.memberPageRole} choices={roleChoices} disabled={busy}
            className={styles.boxRole} label={t("roleRows.member")}
            onChange={(memberPageRole) => void controller.saveSelectedWith({ memberPageRole }, saved)}
            data-teamspace-member-role={selected.memberPageRole} />
            : <span className={styles.boxRole}>{t(`pageRoles.${selected.memberPageRole}`)}</span>}
        </div>
        {security !== "default" ? (
          <div className={styles.roleRow}>
            <span className={styles.roleGlyph}>{workspaceName.trim().slice(0, 1).toUpperCase() || "?"}</span>
            <span>{t("roleRows.others", { workspace: workspaceName })}</span>
            {/* A private teamspace is its members' alone, its row saying so in red with a lock (Notion's, seen 2026-11-18);
                an open or closed one may give the rest of the workspace a role. */}
            {security === "private" ? <span className={styles.boxPrivate} data-teamspace-open-role="none">
              <LockIcon size="small" weight="fill" aria-hidden="true" /><span>{t("roleRows.privateNone")}</span>
            </span> : canManageSelected ? <TeamspaceChoiceMenu value={selected.openPageRole} choices={outsideChoices}
              disabled={busy} className={styles.boxRole} label={t("roleRows.others", { workspace: workspaceName })}
              onChange={(openPageRole) => void controller.saveSelectedWith({ openPageRole }, saved)} data-teamspace-open-role={selected.openPageRole} />
              : <span className={styles.boxRole}>{t(`pageRoles.${selected.openPageRole}`)}</span>}
          </div>
        ) : null}
      </div>
      {/* Only its members manage it from here (Notion's code, 2026-11-11). */}
      {selected.joined ? <section className={styles.manage}>
        <h3>{t("manageSection.title")}</h3>
        <div className={styles.manageRow}>
          <span><strong>{t("manageSection.leave")}</strong><span>{t("manageSection.leaveDescription")}</span></span>
          {/* Its owners' plain, a member's red, as Notion's; its tip above saying why it is dim, or what it does
              (Notion's code and seen 2026-11-11). */}
          <HoverTip label={leaveLock === "group" ? t("manageSection.leaveGroup", { groupNames: (selected.membershipGroups ?? []).join(",") })
            : t(leaveLock === "default" ? "manageSection.leaveDefault" : leaveLock === "onlyOwner"
              ? "manageSection.leaveOnlyOwner" : "manageSection.leaveTip")} placement="top" wide>
            <button type="button" className={ownsSelected ? styles.secondary : styles.dangerButton} disabled={busy || Boolean(leaveLock)}
              onClick={onLeave} data-teamspace-leave data-teamspace-leave-lock={leaveLock || undefined}>{t("manageSection.leave")}</button>
          </HoverTip>
        </div>
        {/* Only its owners archive it, even the workspace's owners not (Notion's code, 2026-11-11). */}
        {ownsSelected ? <div className={styles.manageRow}>
          <span><strong>{t("manageSection.archive")}</strong><span>{t("manageSection.archiveDescription")}</span></span>
          {/* A default one goes while another stays; the only one waits, its tip saying why (Notion's code, 2026-11-11). */}
          {archiveLock ? <HoverTip label={t(archiveLock === "multipleMembers" ? "manageSection.archiveMultipleMembers"
            : "manageSection.archiveOtherTeamspaces")} placement="top" wide>
            <button type="button" className={styles.dangerButton} disabled data-teamspace-archive data-teamspace-archive-lock={archiveLock}>
              {t("manageSection.archive")}
            </button>
          </HoverTip> : <button type="button" className={styles.dangerButton} disabled={busy} onClick={onArchive} data-teamspace-archive>
            {t("manageSection.archive")}
          </button>}
        </div> : null}
      </section> : null}
    </div>
  );
}

/** "보안": who may invite or remove members, who may change the
 * teamspace's sidebar, and its invite link — its owners' to change; its other
 * readers see the same rows fixed, as Notion's (seen 2026-11-18: a workspace
 * owner and a restricted member who are only its members). */
export function TeamspaceSecurityTab({ controller }: { controller: TeamspaceDialogController }) {
  const { t } = useTranslation("teamspaceDialog");
  const { busy, canManageSelected: owns, selected } = controller;
  // Read by whoever may invite its members, as the server gives it (teamspace_invite_links.go).
  const invite = useTeamspaceInviteLink(controller.session, controller.workspaceId, selected?.id ?? null,
    Boolean(selected && controller.canInviteSelected));
  const [inviteBusy, setInviteBusy] = useState(false);
  const [inviteFailed, setInviteFailed] = useState(false);
  if (!selected) return null;
  const choices: TeamspaceChoice<"owners" | "members">[] = [
    { value: "owners", label: t("securityTab.ownersOnly") },
    { value: "members", label: t("securityTab.everyMember") },
  ];
  const policyLabel = (members: boolean) => t(members ? "securityTab.everyMember" : "securityTab.ownersOnly");
  const inviteFixed = selected.isDefault || selected.access === "open";
  const saved = t("notice.saved");
  return (
    <div className={styles.securityTab} data-teamspace-security-tab>
      <h3 data-security-heading>{t("securityTab.title")}</h3>
      <div className={styles.securityRow}>
        <span><strong>{t("securityTab.invite")}</strong></span>
        {/* Everyone is in the default teamspace, and anyone may join an open one: every member invites, fixed, as
            Notion's (seen 2026-11-18 at 1440: open fixed, closed and private a menu, whatever the member count). */}
        {inviteFixed || !owns ? <span className={styles.roleFixed}>{policyLabel(inviteFixed || selected.membersCanInvite)}</span>
          : <TeamspaceChoiceMenu value={selected.membersCanInvite ? "members" : "owners"} choices={choices} disabled={busy}
            label={t("securityTab.invite")} data-teamspace-invite-policy={selected.membersCanInvite ? "members" : "owners"}
            onChange={(value) => void controller.saveSelectedWith({ membersCanInvite: value === "members" }, saved)} />}
      </div>
      <div className={styles.securityRow}>
        <span><strong>{t("securityTab.sidebar")}</strong><span>{t("securityTab.sidebarDescription")}</span></span>
        {owns ? <TeamspaceChoiceMenu value={selected.membersCanEditSidebar ? "members" : "owners"} choices={choices} disabled={busy}
          label={t("securityTab.sidebar")} data-teamspace-sidebar-policy={selected.membersCanEditSidebar ? "members" : "owners"}
          onChange={(value) => void controller.saveSelectedWith({ membersCanEditSidebar: value === "members" }, saved)} />
          : <span className={styles.roleFixed}>{policyLabel(selected.membersCanEditSidebar)}</span>}
      </div>
      {controller.canInviteSelected ? (
        <div className={styles.securityRow}>
          <span><strong>{t("securityTab.inviteLink")}</strong><span>{t("securityTab.inviteLinkDescription")}</span></span>
          <button type="button" role="switch" aria-checked={invite.link?.enabled === true} aria-label={t("securityTab.inviteLink")}
            disabled={!owns || !invite.link || inviteBusy} className={styles.switchButton} data-teamspace-invite-link
            onClick={() => {
              setInviteBusy(true);
              setInviteFailed(false);
              void invite.set(!invite.link?.enabled).catch(() => setInviteFailed(true)).finally(() => setInviteBusy(false));
            }}>
            <SwitchMark on={invite.link?.enabled} />
          </button>
        </div>
      ) : null}
      {controller.canInviteSelected && invite.failed ? (
        <ReadFailed message={t("securityTab.inviteLinkLoadFailed")} onRetry={invite.retry} />
      ) : null}
      {owns && inviteFailed ? (
        <p className={styles.error} role="alert" data-teamspace-invite-link-failed>{t("securityTab.inviteLinkChangeFailed")}</p>
      ) : null}
      {/* Notion keeps these to its Enterprise plan; here everyone has them. They add to the organization's settings. */}
      <h3 data-security-heading>{t("securityTab.pageSecurity")}</h3>
      {PAGE_SECURITY.map((key) => (
        <div key={key} className={styles.securityRow}>
          <span><strong>{t(`securityTab.${key}`)}</strong><span>{t(`securityTab.${key}Description`)}</span></span>
          <button type="button" role="switch" aria-checked={selected[key]} aria-label={t(`securityTab.${key}`)} disabled={!owns || busy}
            className={styles.switchButton} data-teamspace-page-security={key}
            onClick={() => void controller.saveSelectedWith({ [key]: !selected[key] }, saved)}>
            <SwitchMark on={selected[key]} />
          </button>
        </div>
      ))}
    </div>
  );
}
