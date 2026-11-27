import type { MutableRefObject, ReactNode } from "react";
import { Trans, type useTranslation } from "react-i18next";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { ArrowRight, RelinkSharingIcon, UnlinkSharingIcon } from "@/icons/leafen";
import type {
  PublicMutationInheritedPerson, PublicMutationRole, PublicMutationShareInheritanceInput, PublicMutationState,
} from "@/publicMutationWire.generated";
import { PageIconGlyph } from "./PageIcon";
import { sharePermissionLabel, type ShareRoleChoice } from "./TopBarSharePermissionPicker";
import type { TeamspaceShareGroup } from "./TopBarShareTeamspace";
import type { WebLinkRole } from "./TopBarShareWebRole";
import relink from "./TopBarShareRelink.module.css";
import { shareParentTitle } from "./topBarShareModel";

const ROLE_ORDER: readonly PublicMutationRole[] = ["view", "comment", "edit", "full_access"];
const roleRank = (role: PublicMutationRole) => ROLE_ORDER.indexOf(role);

// How many of the changes linking sharing again makes its question lists.
const RELINK_CHANGES_SHOWN = 20;

/** The parent a relink matches, after a page's mark, as Notion's names it (seen 2026-11-06). */
function RelinkParentName({ children }: { children?: ReactNode }) {
  return <span className={relink.relinkPage}><PageIconGlyph page={{ icon: "", iconType: "none" }} size={12} />{children}</span>;
}

/**
 * Whether everyone in the workspace opens the page by a page above alone,
 * which the page may lower or take off by unlinking: not by an open
 * teamspace, set there, nor by a share of the page's own. A person's page
 * takes People's own rule so, as Notion's (seen 2026-11-16: "초대받은
 * 사용자만" on and the role menu's "제거"); People's database page keeps its
 * rows fixed (people_database_access.go).
 */
export function inheritedWorkspaceLowers(state: PublicMutationState | null | undefined) {
  return Boolean(state?.workspaceInheritedRole && !state.workspacePermission && !state.teamspace?.openRole && !state.generalAccessFixed);
}

type Inheritance = { inheritance: Omit<PublicMutationShareInheritanceInput, "expectedRevision"> };
type Grant = { action: "grant_permission"; principalType: PublicMutationInheritedPerson["principalType"]; principalId: string; role: PublicMutationRole };

/**
 * What the share window does with sharing a page has from the pages above
 * it: raising someone is a share of its own; lowering or removing them,
 * changing its teamspace's roles or the web link it has from above unlinks
 * its sharing from its parent's once confirmed, and "설정 복원" links it
 * again, as Notion's (seen 2026-10-11, 2026-10-12, 2026-11-06).
 */
export function useShareInheritanceActions({ stateRef, runMutation, t }: {
  stateRef: MutableRefObject<PublicMutationState | null>;
  runMutation(input: Inheritance | Grant): Promise<string>;
  t: ReturnType<typeof useTranslation>["t"];
}) {
  // The ask before a change unlinks the page's sharing from its parent's, as
  // Notion's: its mark, a small line and a filled red action (seen 2026-11-06).
  const unlinkConfirm = () => ({ message: t("topBar:unlinkConfirmTitle"), detail: t("topBar:unlinkConfirmDetail"),
    confirmLabel: t("topBar:unlinkConfirm"), danger: "filled" as const, compact: true,
    icon: <UnlinkSharingIcon size="card" aria-hidden="true" /> });

  async function changeInherited(person: PublicMutationInheritedPerson, role: PublicMutationRole | null) {
    if (role && roleRank(role) > roleRank(person.role)) {
      void runMutation({ action: "grant_permission", principalType: person.principalType, principalId: person.principalId, role });
      return;
    }
    if (!await confirmAction(unlinkConfirm())) return;
    void runMutation({ inheritance: { action: "unlink", principalType: person.principalType, principalId: person.principalId, role: role ?? "" } });
  }

  async function changeTeamspaceRole(group: TeamspaceShareGroup, role: ShareRoleChoice) {
    if (stateRef.current?.teamspace?.changeUnlinks && !await confirmAction(unlinkConfirm())) return;
    void runMutation({ inheritance: { action: "teamspace_role", principalId: group, role } });
  }

  // The web link from a page above, changed here, leaves a changed copy.
  const changeInheritedWeb = async (role: WebLinkRole | "none") => await confirmAction(unlinkConfirm())
    && await runMutation({ inheritance: { action: "web_role", role } }) === "succeeded";

  // Everyone in the workspace from a page above, lowered, taken off by "제거"
  // or by "초대받은 사용자만" here, asks the same and unlinks the page,
  // keeping the lower role or none of it, as Notion's (seen 2026-11-16 at
  // 1440 on two pages under a page shared with the workspace: the general
  // access menu and the role menu's "제거" both ask first whether to change
  // the access and unlink the page's sharing, then the page is
  // "초대받은 사용자만" under the unlinked band). Its role menu's "검색 결과에서
  // 숨기기" is a change of it too, keeping the role, as Notion's row offers it
  // under the same note (seen 2026-11-16 on a person's page).
  const changeInheritedWorkspace = async (role: PublicMutationRole | null, hideFromSearch?: boolean) => {
    const workspaceId = stateRef.current?.workspaceId ?? "";
    return await confirmAction(unlinkConfirm()) && await runMutation({ inheritance: {
      action: "unlink", principalType: "workspace", principalId: workspaceId, role: role ?? "",
      ...(hideFromSearch === undefined ? {} : { hideFromSearch }) } }) === "succeeded";
  };
  const lowersInheritedWorkspace = () => inheritedWorkspaceLowers(stateRef.current);
  const inheritedWebRole = () => !stateRef.current?.webSharingEnabled ? stateRef.current?.inheritedWebLink?.role : undefined;

  async function relinkSharing() {
    const unlink = stateRef.current?.shareUnlink;
    if (!unlink) return;
    const changes = unlink.changes.map(change => ({
      name: change.label || stateRef.current?.workspaceName || "", to: sharePermissionLabel(change.toRole, t),
      from: change.fromRole ? sharePermissionLabel(change.fromRole, t) : t("topBar:noAccess"),
    })).concat(unlink.teamspaceChanges.map(change => ({
      name: t("topBar:teamspaceGroup", { name: stateRef.current?.teamspace?.name ?? "",
        group: t(change.group === "owner" ? "topBar:teamspaceOwners" : "topBar:teamspaceMembersGroup") }),
      from: sharePermissionLabel(change.fromRole, t), to: sharePermissionLabel(change.toRole, t),
    })));
    // Every change is read; the box names the first of them and counts the
    // rest: each person's name over their role before and after, as Notion's
    // (seen 2026-11-06), under the parent named with a page's mark — a
    // teamspace, for a page at its top, by its name alone (seen 2026-11-16).
    const box = changes.length ? <>
      {changes.slice(0, RELINK_CHANGES_SHOWN).map((change, index) => <span key={index} className={relink.relinkChange}>
        <span>{change.name}</span>
        <span>{change.from}<ArrowRight size="regular" aria-hidden="true" />{change.to}</span>
      </span>)}
      {changes.length > RELINK_CHANGES_SHOWN
        ? <span className={relink.relinkMore}>{t("topBar:relinkMore", { count: changes.length - RELINK_CHANGES_SHOWN })}</span> : null}
    </> : undefined;
    if (!await confirmAction({ message: t("topBar:relinkTitle"),
      detail: <Trans t={t} i18nKey="topBar:relinkDetail" values={{ title: shareParentTitle({ title: unlink.parentTitle, database: unlink.parentDatabase }) }}
        components={{ page: unlink.teamspace ? <span /> : <RelinkParentName /> }} />,
      box, confirmLabel: t("topBar:relinkConfirm"), accent: true, compact: true,
      icon: <RelinkSharingIcon size="card" aria-hidden="true" /> })) return;
    void runMutation({ inheritance: { action: "relink" } });
  }

  return { changeInherited, changeTeamspaceRole, changeInheritedWeb, inheritedWebRole, relinkSharing, changeInheritedWorkspace, lowersInheritedWorkspace };
}
