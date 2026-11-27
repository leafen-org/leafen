import { SwitchMark } from "@/components/Switch";
import { Fragment, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { userErrorText, writeUncertainMark } from "@/lib/settingsErrors";

import type {
  EnterpriseOperationContext,
  EnterpriseSettingsService,
} from "@/lib/enterpriseSettingsAdapter";
import { enterpriseOperationContext } from "@/lib/enterpriseSettingsAdapter";
import { useAppPlatformAvailable } from "@/lib/appPlatformAvailability";
import type { OrganizationSharingSecurityPolicy } from "@/organizationWire.generated";
import { organizationModule } from "@/lib/organizationModule";
import { NativeTab, useNativeTabs } from "@/lib/nativeTabs";
import { styles } from "./workspaceSettingsOverviewStyles";
import analyticsStyles from "./WorkspaceAnalytics.module.css";
import tabStyles from "./WorkspaceSharingSecurity.module.css";
import { AppSelect } from "@/components/AppSelect";

// Notion's 보안 tabs (seen 2026-10-12): 일반 — public sharing, copies to other
// workspaces and exports, each a "비활성화" switch (Leafen's view links and
// user HTML follow); 멤버 및 게스트 — who invites whom; 데이터 보존 — how long
// the trash keeps pages. Notion's "워크스페이스 섹션 편집 제한" is left out and
// its support access has no Leafen counterpart.
type SecurityTab = "general" | "members" | "retention";
const SECURITY_TABS: readonly SecurityTab[] = ["general", "members", "retention"];
const SECURITY_TAB_LABELS: Record<SecurityTab, string> = {
  general: "securityTabGeneral", members: "membersGuestsTitle", retention: "securityTabRetention",
};
/** The tab 보안 shows again when it opens, as last left. */
let lastSecurityTab: SecurityTab = "general";

type SharingPolicyKey = keyof Pick<OrganizationSharingSecurityPolicy,
  | "publicWebSharing"
  | "externalEmailSharing"
  | "guestAccess"
  | "exports"
  | "crossWorkspaceCopies"
>;

// Who invites whom, as Notion's "멤버 및 게스트" (seen 2026-10-06): who may
// invite guests to pages, who may add members — the workspace's owners, or
// any member with an owner's approval — and whether people outside the
// workspace may ask for its pages. A member's request waits in "요청".
type InvitePolicy = Pick<OrganizationSharingSecurityPolicy, "guestInvites" | "guestInviteRequests" | "memberAdds" | "pageAccessRequests"
  | "membershipRequests" | "restrictedTeamspaceAdds" | "restrictedPageAdds" | "pendingGuestPrivatePages" | "restrictedPrivatePages">;

// User HTML: uploaded HTML files and "HTML" blocks drawn in a frame
// with no origin, which an organization can turn off; only a build with the
// app platform has them.

// View links, which an agent opens a page through to see it:
// whether people make them, how long one lasts at most, whether each opens
// once, and whether one may open without its secret.
type ViewLinkPolicy = Pick<OrganizationSharingSecurityPolicy,
  "viewLinks" | "viewLinkMaxSeconds" | "viewLinkSingleUseOnly" | "viewLinkWithoutSecret">;

const viewLinkLifetimes = [600, 1800, 3600] as const;


export function WorkspaceSharingSecurity({
  canManageSecurity,
  isCurrent,
  onRefresh,
  organizationId,
  policy,
  requestGeneration,
  service,
  sessionIdentity,
  signal,
  snapshotIdentity,
  ownerControls,
  deletedPages,
  membersOnly = false,
}: {
  canManageSecurity: boolean;
  isCurrent: (context: EnterpriseOperationContext) => boolean;
  onRefresh: () => void;
  organizationId: string;
  policy: OrganizationSharingSecurityPolicy;
  requestGeneration: number;
  service: EnterpriseSettingsService;
  sessionIdentity: string;
  signal: AbortSignal;
  snapshotIdentity: string;
  /** The organization owner's controls the organization module adds here. */
  ownerControls?: ReactNode;
  /** The workspace owner's list of pages deleted for good, under 데이터 보존. */
  deletedPages?: ReactNode;
  /**
   * Only who invites whom, without the tabs, as 요청's "제어" shows it: the
   * same settings as switches (Notion's, seen 2026-11-09 and 2026-11-21).
   */
  membersOnly?: boolean;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [current, setCurrent] = useState(policy);
  const [busy, setBusy] = useState<SharingPolicyKey | keyof InvitePolicy | keyof ViewLinkPolicy | "userHtml" | "">("");
  const appPlatform = useAppPlatformAvailable();
  // The tab stays through the reload a saved setting asks for (2026-11-05: a
  // switch on 멤버 및 게스트 went back to 일반).
  const [chosenTab, setTabState] = useState<SecurityTab>(() => lastSecurityTab);
  const tab: SecurityTab = membersOnly ? "members" : chosenTab;
  const setTab = (next: SecurityTab) => {
    lastSecurityTab = next;
    setTabState(next);
  };
  const tabListRef = useRef<HTMLDivElement>(null);
  const tabList = useNativeTabs<SecurityTab>({
    tabs: SECURITY_TABS.map((item) => ({ key: item, text: t(SECURITY_TAB_LABELS[item]) })),
    selected: tab, onSelect: setTab, label: t("navSharingSecurity"), listRef: tabListRef,
  });
  const [error, setError] = useState("");
  // A change that wasn't saved is told under its own row, not above the
  // tabs: a box put in there moved every row down, and a second press went
  // to the switch below (2026-11-06, on a network that stopped).
  const pressedRow = useRef("");
  const [failedRow, setFailedRow] = useState("");
  useEffect(() => {
    setCurrent(policy);
    setBusy("");
    setError("");
  }, [policy, snapshotIdentity]);
  const context = useMemo(() => enterpriseOperationContext({
    sessionIdentity,
    organizationId,
    requestGeneration,
    snapshotIdentity,
    signal,
  }), [organizationId, requestGeneration, sessionIdentity, signal, snapshotIdentity]);

  const updateSharingSecurityPolicy = async (change: Partial<OrganizationSharingSecurityPolicy>) => {
    const key = Object.keys(change)[0] as SharingPolicyKey | keyof InvitePolicy | keyof ViewLinkPolicy | "userHtml";
    if (!canManageSecurity || busy || !isCurrent(context)) return;
    setBusy(key);
    setError("");
    try {
      const next = await service.updateSharingSecurity(context, {
        expectedPolicyVersion: current.policyVersion,
        publicWebSharing: current.publicWebSharing,
        externalEmailSharing: current.externalEmailSharing,
        guestAccess: current.guestAccess,
        exports: current.exports,
        crossWorkspaceCopies: current.crossWorkspaceCopies,
        guestInvites: current.guestInvites,
        memberAdds: current.memberAdds,
        pageAccessRequests: current.pageAccessRequests,
        membershipRequests: current.membershipRequests,
        restrictedTeamspaceAdds: current.restrictedTeamspaceAdds,
        restrictedPageAdds: current.restrictedPageAdds,
        pendingGuestPrivatePages: current.pendingGuestPrivatePages,
        guestInviteRequests: current.guestInviteRequests,
        restrictedPrivatePages: current.restrictedPrivatePages,
        ...change,
      });
      if (!isCurrent(context)) return;
      setCurrent(next);
      onRefresh();
    } catch (caught) {
      if (caught instanceof Error && caught.name === "AbortError") return;
      if (isCurrent(context)) {
        setFailedRow(pressedRow.current);
        setError(userErrorText(caught, t("sharingSecuritySaveFailed")));
      }
    } finally {
      if (isCurrent(context)) setBusy("");
    }
  };

  const disabled = !canManageSecurity || Boolean(busy);
  // A row with a switch, as Notion's security rows (seen 2026-10-12): its
  // name and what it does on the left, the switch on the right.
  const rowError = (marker: string) => error && failedRow === marker ? <div className={styles.notice} role="alert" {...writeUncertainMark(error)}>{error}</div> : null;
  // A row Notion marks "베타" carries the mark after its name (seen 2026-11-21).
  const switchRow = (marker: string, title: string, description: ReactNode, on: boolean, toggle: () => void, off = false, badge = "") => (<>
    <div className={styles.securityRow} data-security-row={marker}>
      <div className={styles.securityInstruction}>
        {badge ? <strong className={tabStyles.badged}>{title}<b className={tabStyles.badge}>{badge}</b></strong> : <strong>{title}</strong>}
        {description ? <span>{description}</span> : null}
      </div>
      <button type="button" role="switch" aria-checked={on} aria-label={title} disabled={disabled || off}
        onClick={() => { pressedRow.current = marker; toggle(); }}>
        <SwitchMark on={on} />
      </button>
    </div>
    {rowError(marker)}
  </>);
  const sharingBlocks = Boolean(organizationModule()?.sharingBlocks);
  const restrictable = organizationModule()?.restrictMember !== undefined;
  // Who may ask to become a member, as two switches: those with no page shared from the workspace, and its guests.
  const membershipFrom = (others: boolean, guests: boolean): InvitePolicy["membershipRequests"] =>
    others && guests ? "all" : others ? "non_guests" : guests ? "guests" : "none";
  const othersAsk = current.membershipRequests === "all" || current.membershipRequests === "non_guests";
  const guestsAsk = current.membershipRequests === "all" || current.membershipRequests === "guests";
  // Who invites whom, as Notion's 요청 › "제어" and 보안 › "멤버 및 게스트" (seen 2026-11-21 in the business
  // trial's owner, both places the same column): switches in its order and words, the guests' and restricted
  // members' private pages where the organization module offers those people. Its "업그레이드" rows have no
  // Leafen plan (WorkspaceRequestsSection).
  const requestControls = <div data-members-guests data-request-controls>
    {switchRow("page-access-requests", t("requestControlPageAccess"), t("requestControlPageAccessDescription"), current.pageAccessRequests,
      () => void updateSharingSecurityPolicy({ pageAccessRequests: !current.pageAccessRequests }))}
    {switchRow("guest-invites", t("requestControlGuestInvites"), t("requestControlGuestInvitesDescription"), current.guestInvites === "members",
      () => void updateSharingSecurityPolicy({ guestInvites: current.guestInvites === "members" ? "owners" : "members" }))}
    {switchRow("guest-invite-requests", t("requestControlGuestInviteRequests"), t("requestControlGuestInviteRequestsDescription"),
      current.guestInviteRequests, () => void updateSharingSecurityPolicy({ guestInviteRequests: !current.guestInviteRequests }))}
    {switchRow("member-adds", t("requestControlMemberAdds"), t("requestControlMemberAddsDescription"), current.memberAdds === "approval",
      () => void updateSharingSecurityPolicy({ memberAdds: current.memberAdds === "approval" ? "owners" : "approval" }))}
    {switchRow("membership-requests-others", t("requestControlMembershipOthers"), t("requestControlMembershipOthersDescription"), othersAsk,
      () => void updateSharingSecurityPolicy({ membershipRequests: membershipFrom(!othersAsk, guestsAsk) }))}
    {switchRow("membership-requests-guests", t("requestControlMembershipGuests"), t("requestControlMembershipGuestsDescription"), guestsAsk,
      () => void updateSharingSecurityPolicy({ membershipRequests: membershipFrom(othersAsk, !guestsAsk) }))}
    {restrictable ? <>
      {switchRow("pending-guest-private-pages", t("pendingGuestPrivatePagesLabel"), t("pendingGuestPrivatePagesDescription"),
        current.pendingGuestPrivatePages, () => void updateSharingSecurityPolicy({ pendingGuestPrivatePages: !current.pendingGuestPrivatePages }))}
      {switchRow("restricted-private-pages", t("restrictedPrivatePagesLabel"), t("restrictedPrivatePagesDescription"),
        current.restrictedPrivatePages, () => void updateSharingSecurityPolicy({ restrictedPrivatePages: !current.restrictedPrivatePages }), false, t("betaMark"))}
    </> : null}
  </div>;

  return (
    <section id={membersOnly ? undefined : "sharing-security"} className={styles.section}
      aria-label={membersOnly ? t("requestsControlsTitle") : t("navSharingSecurity")}>
      {membersOnly ? null : <div {...tabList.tabListProps} ref={tabListRef} className={analyticsStyles.tabs}>
        {SECURITY_TABS.map((item) => (
          <NativeTab key={item} state={tabList.state} tabKey={item} data-security-tab={item} className={analyticsStyles.tab}>
            {t(SECURITY_TAB_LABELS[item])}
          </NativeTab>
        ))}
      </div>}
      {error && !failedRow ? <div className={styles.notice} {...writeUncertainMark(error)}>{error}</div> : null}
      <div className={membersOnly ? undefined : tabStyles.panel} data-security-panel={membersOnly ? undefined : tab} aria-busy={Boolean(busy)}>
        {tab === "general" ? <>
          {/* Blocking sharing outside the organization is the organization module's. */}
          {sharingBlocks ? <>
            {switchRow("public-web", t("disablePublicSharing"), t("disablePublicSharingDescription"), !current.publicWebSharing,
              () => void updateSharingSecurityPolicy({ publicWebSharing: !current.publicWebSharing }))}
            {switchRow("cross-workspace", t("disableCrossWorkspaceCopies"), t("disableCrossWorkspaceCopiesDescription"), !current.crossWorkspaceCopies,
              () => void updateSharingSecurityPolicy({ crossWorkspaceCopies: !current.crossWorkspaceCopies }))}
            {switchRow("exports", t("disableExports"), t("disableExportsDescription"), !current.exports,
              () => void updateSharingSecurityPolicy({ exports: !current.exports }))}
          </> : null}
          <div className={styles.organizationPolicyBlock} data-view-link-policy>
            <div className={styles.organizationSubhead}><span>{t("viewLinksTitle")}</span></div>
            {([["viewLinks", "viewLinksLabel"], ["viewLinkSingleUseOnly", "viewLinkSingleUseOnlyLabel"],
              ["viewLinkWithoutSecret", "viewLinkWithoutSecretLabel"]] as const).map(([key, label]) => (
              <Fragment key={key}>{switchRow(key, t(label), null, current[key],
                () => void updateSharingSecurityPolicy({ [key]: !current[key] }), key !== "viewLinks" && !current.viewLinks)}</Fragment>
            ))}
            <div className={styles.securityRow}>
              <div className={styles.securityInstruction}><strong>{t("viewLinkMaxSecondsLabel")}</strong></div>
              <AppSelect look="setting" aria-label={t("viewLinkMaxSecondsLabel")} value={current.viewLinkMaxSeconds} data-view-link-max-seconds
                disabled={disabled || !current.viewLinks}
                onChange={(event) => {
                  pressedRow.current = "view-link-max-seconds";
                  void updateSharingSecurityPolicy({ viewLinkMaxSeconds: Number(event.target.value) });
                }}>
                {viewLinkLifetimes.includes(current.viewLinkMaxSeconds as typeof viewLinkLifetimes[number]) ? null
                  : <option value={current.viewLinkMaxSeconds}>{t("viewLinkMinutes", { count: Math.round(current.viewLinkMaxSeconds / 60) })}</option>}
                {viewLinkLifetimes.map((seconds) => (
                  <option key={seconds} value={seconds}>{t("viewLinkMinutes", { count: seconds / 60 })}</option>
                ))}
              </AppSelect>
            </div>
            {rowError("view-link-max-seconds")}
          </div>
          {appPlatform ? (
            <div className={styles.organizationPolicyBlock} data-user-html-policy>
              <div className={styles.organizationSubhead}><span>{t("userHtmlTitle")}</span></div>
              {switchRow("user-html", t("userHtmlLabel"), t("userHtmlNote"), current.userHtml,
                () => void updateSharingSecurityPolicy({ userHtml: !current.userHtml }))}
            </div>
          ) : null}
        </> : tab === "members" ? (<>
          {requestControls}
          {/* The restricted member rows after it, as Notion's (seen 2026-11-21), where the organization module offers the role. */}
          {membersOnly ? null : <div data-members-guests data-security-members>
            {restrictable ? <>
              <div className={styles.securityRow}>
                <div className={styles.securityInstruction}><strong>{t("restrictedTeamspaceAddsLabel")}</strong><span>{t("restrictedAddsDescription")}</span></div>
                <AppSelect look="setting" menuWidth={300} aria-label={t("restrictedTeamspaceAddsLabel")} value={current.restrictedTeamspaceAdds} data-restricted-teamspace-adds disabled={disabled}
                  onChange={(event) => void updateSharingSecurityPolicy({ restrictedTeamspaceAdds: event.target.value as InvitePolicy["restrictedTeamspaceAdds"] })}>
                  <option value="members">{t("restrictedAddsMembers")}</option>
                  <option value="teamspace_owners">{t("restrictedAddsTeamspaceOwners")}</option>
                  <option value="owners">{t("memberAddsOwners")}</option>
                </AppSelect>
              </div>
              <div className={styles.securityRow}>
                <div className={styles.securityInstruction}><strong>{t("restrictedPageAddsLabel")}</strong><span>{t("restrictedAddsDescription")}</span></div>
                <AppSelect look="setting" menuWidth={300} aria-label={t("restrictedPageAddsLabel")} value={current.restrictedPageAdds} data-restricted-page-adds disabled={disabled}
                  onChange={(event) => void updateSharingSecurityPolicy({ restrictedPageAdds: event.target.value as InvitePolicy["restrictedPageAdds"] })}>
                  <option value="members">{t("restrictedAddsMembers")}</option>
                  <option value="approval">{t("restrictedAddsApproval")}</option>
                  <option value="owners">{t("memberAddsOwners")}</option>
                </AppSelect>
              </div>
            </> : null}
            {sharingBlocks ? <>
              {switchRow("guests", t("guestAccessRow"), t("guestAccessRowDescription"), current.guestAccess,
                () => void updateSharingSecurityPolicy({ guestAccess: !current.guestAccess }))}
              {switchRow("external-email", t("externalEmailSharingRow"), t("externalEmailSharingRowDescription"), current.externalEmailSharing,
                () => void updateSharingSecurityPolicy({ externalEmailSharing: !current.externalEmailSharing }))}
            </> : null}
            {ownerControls ? <div className={tabStyles.ownerControls}>{ownerControls}</div> : null}
          </div>}
        </>) : (<>
          <div className={styles.securityRow} data-security-row="trash-retention">
            <div className={styles.securityInstruction}>
              <strong>{t("trashRetentionLabel")}</strong><span>{t("trashRetentionDescription")}</span>
            </div>
            <span className={tabStyles.fixedValue} data-row-end>{t("trashRetentionDays", { count: 30 })}</span>
          </div>
          <div className={styles.securityRow} data-security-row="deleted-page-retention">
            <div className={styles.securityInstruction}>
              <strong>{t("deletedPagesRetentionLabel")}</strong><span>{t("deletedPagesRetentionDescription")}</span>
            </div>
            <span className={tabStyles.fixedValue} data-row-end>{t("trashRetentionDays", { count: 30 })}</span>
          </div>
          {deletedPages}
        </>)}
      </div>
      {!canManageSecurity ? (
        <div className={styles.notice} data-tone="neutral">{t("orgSecurityAdminRequired")}</div>
      ) : null}
    </section>
  );
}
