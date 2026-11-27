import { personValue, storePersonValue } from "@/lib/browserStorage";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { Fragment, useEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { Trans, useTranslation } from "react-i18next";
import { pageHref } from "@/lib/navigation";
import type { WorkspaceApp } from "@/appRegistrationWire.generated";
import { ChevronLeft, InfoIcon, LinkIcon, Settings, X } from "@/icons/leafen";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { publicMutationRoles, type PublicMutationInheritedPerson, type PublicMutationPermission, type PublicMutationPrincipalOption,
  type PublicMutationRole, type PublicMutationState } from "@/publicMutationWire.generated";
import { ShareAdvancedCount, TopBarShareAdvanced } from "./TopBarShareAdvanced";
import { TopBarShareGuestLinks } from "./TopBarShareGuestLinks";
import type { PageRulesAccess } from "./TopBarSharePageRules";
import type { WebLinkRole } from "./TopBarShareWebRole";
import { TopBarShareGeneralAccess, type GeneralAccessChange } from "./TopBarShareGeneralAccess";
import { TopBarShareInheritedGroups, TopBarShareUnlinkBanner } from "./TopBarShareInherited";
import { TopBarShareInviteField, type ShareAlreadyShared } from "./TopBarShareInviteField";
import { TopBarShareTeamspace, type TeamspaceShareGroup } from "./TopBarShareTeamspace";
import { sharePermissionLabel, TopBarSharePermissionPicker, type ShareRoleChoice } from "./TopBarSharePermissionPicker";
import { ShareRowTip, useShareRoleTip } from "./ShareRowTip";
import { SharePersonMark } from "./SharePersonMark";
import { SharePhoneInvite, SharePhonePerson, SharePhonePersonText, SharePhoneSearch, sharePhoneStyles as phoneCss } from "./TopBarSharePhone";
import { PhoneSheetGroup, PhoneSheetRow } from "@/components/shell/PhoneSheet";
import { shareIsYou, shareParentTitle, sharePrincipalNote, shareUnlinkDefaultsHref, type SelectedPrincipal } from "./topBarShareModel";
import { styles } from "./topBarStyles";
import { serverUrl } from "@/lib/serverOrigin";
import { inviteMail } from "@/lib/inviteMail";
import access from "./TopBarShareAccess.module.css";
import inherited from "./TopBarShareInherited.module.css";
import { useCopyNotice } from "@/lib/copyNotice";
import { ShareErrorLine } from "./ShareErrorLine";
import { SelfMark } from "@/components/people/SelfMark";

/** Sharing presentation has no transport or authority of its own. */
export function TopBarShareAccessPanel({ state, currentUser, disabled, busy, visible, focusRequest, inputRef, dialogRef, draft, onDraft, addressRows, invalidInvites,
  chips, onRemoveChip, candidates, onToggle, alreadyShared, inviting, onBack, onOpenInvite, role, onRole, onInvite, onMore, onPermission, onRemove,
  message, onMessage, onGeneralAccess, writeNote, onWorkspaceRole, onHideFromSearch, onWebRole, expiresAt, onExpiry, advanced, onAdvanced, loadApps,
  database, onAgentGrantScope, pageRules, propertyRules, onCopy, editRequest, pageTitle, onInheritedChange, onParentDefaults, onRelink, onTeamspaceRole, pageIcon,
  loadInheritedPeople, policyRefused = false, loading = false, inTrash = false }: {
  /** The page's sharing is still being read: the rows it will show keep their place (2026-11-06: on a slow network they came in later and pushed "고급" and the copy link down under the pointer). */
  loading?: boolean;
  /** A share the note's rule refused: the note itself says so, in red. */
  policyRefused?: boolean;
  /** The page is in the trash, or under a page in it: its sharing reads as it is until it is restored. */
  inTrash?: boolean;
  state: PublicMutationState | null; currentUser: { id: string; email?: string | null }; disabled: boolean; busy: boolean; visible: boolean; focusRequest: number;
  inputRef: RefObject<HTMLInputElement | null>; dialogRef: RefObject<HTMLDivElement | null>;
  draft: string; onDraft(next: string): void; chips: SelectedPrincipal[]; onRemoveChip(chip: SelectedPrincipal): void;
  candidates: PublicMutationPrincipalOption[]; onToggle(option: PublicMutationPrincipalOption): void;
  addressRows: boolean; invalidInvites: string[];
  alreadyShared: ShareAlreadyShared[]; inviting: boolean; onBack(): void; onOpenInvite(): void;
  role: PublicMutationRole; onRole(next: PublicMutationRole): void; onInvite(): void; onMore(): void;
  onPermission(permission: PublicMutationPermission, role: PublicMutationRole): void; onRemove(permission: PublicMutationPermission): void;
  message: string; onMessage(next: string): void;
  onGeneralAccess(change: GeneralAccessChange): Promise<boolean>;
  /** Words of the last change made on a row ("general" or "permission:<id>"), said under that row. */
  writeNote?: { at: string; text: string }; onWorkspaceRole(role: PublicMutationRole): void; onHideFromSearch(hide: boolean): void; expiresAt: string; onExpiry(expiresAt: string): void;
  onWebRole(role: WebLinkRole): void;
  /** "고급" is a screen of its own; its apps are read as it opens. */
  advanced: boolean; onAdvanced(next: boolean): void; loadApps(signal: AbortSignal): Promise<WorkspaceApp[]>; database: boolean;
  onAgentGrantScope(scope: "full_access" | "access"): void; pageRules: PageRulesAccess;
  /** The organization module's "속성 사용 권한" in "고급", where it is attached. */
  propertyRules: ReactNode;
  onCopy(): void;
  /** Asking for edit access, where someone who may only read or comment can ask. */
  editRequest: ReactNode;
  /** The page's title, which its people's menus name; sharing inherited from the pages above, and a page unlinked from it. */
  pageTitle: string; onInheritedChange(person: PublicMutationInheritedPerson, role: PublicMutationRole | null): void;
  /** Opens where an unlinked page's defaults are (shareUnlinkDefaultsHref). */
  onParentDefaults(href: string): void; onRelink(): void;
  /** A teamspace page's owners' or members' role there. */
  onTeamspaceRole(group: TeamspaceShareGroup, role: ShareRoleChoice): void;
  /** A page above's own icon, where the window knows the page. */
  pageIcon(pageId: string): ReactNode;
  /** Everyone a page above gives this page, every window of them. */
  loadInheritedPeople(pageAboveId: string): Promise<PublicMutationInheritedPerson[]>;
}) {
  const { t } = useTranslation("topBar");
  const roleTip = useShareRoleTip();
  // An address no account has yet: where the server sends no mail, a manager
  // copies its sign-up link to hand on (2026-10-18).
  // The row's words stay while a toast says it was copied, as the share menu's "링크 복사".
  const notice = useCopyNotice();
  function copyInviteLink(permission: PublicMutationPermission) {
    if (!state || !permission.signupToken) return;
    void notice(serverUrl(`/p/${encodeURIComponent(state.pageId)}#invitation=${encodeURIComponent(permission.signupToken)}`), t("copiedLink"));
  }
  const { phone } = useSidebarLayout(true);
  const advancedButton = useRef<HTMLButtonElement>(null), wasAdvanced = useRef(false);
  // Back from "고급", focus returns to it. Going there, the button goes with
  // the screen and takes the focus with it (data-focus-leaves; TopBarShareAdvanced).
  useEffect(() => { if (wasAdvanced.current && !advanced) advancedButton.current?.focus(); wasAdvanced.current = advanced; }, [advanced]);
  // Someone without full access reads who has the page, as Notion's (seen
  // 2026-11-04): the same window with the roles as words, nothing to invite
  // with, and "편집 권한 요청" where they may only read or comment.
  const manages = state?.canManage !== false;
  // A page in the trash, or under a page in it, keeps its sharing as it is
  // until it is restored: Notion's window reads it — the field and "공유하기"
  // greyed, every role as words — while "고급" and "링크 복사" stay (seen
  // 2026-11-13); the server refuses its changes too.
  const shares = manages && !inTrash;
  // A guest or a restricted member without full access reads only their own
  // line of the people, whether they read, comment or edit (the server sends
  // no other); how the page is open and "고급" stay, as words.
  const ownLineOnly = state?.ownLineOnly === true;
  // A restricted member's window is Notion's (seen 2026-11-07): the field and
  // "공유하기" greyed, "제한됨" on their own line, no band asking for edit access.
  const restrictedReader = ownLineOnly && state?.viewerGuest !== true;
  // A guest with full access shares with people; the rest stays as words, as Notion's (seen 2026-11-05).
  const guestManager = manages && state?.viewerGuest === true;
  const readStrip = state && !manages && editRequest && (state.viewerRole === publicMutationRoles.view || state.viewerRole === publicMutationRoles.comment)
    ? <div className={access.readStrip} data-share-read-strip>
      <span>{t(state.viewerRole === publicMutationRoles.comment ? "readOnlyStripComment" : "readOnlyStrip")}</span>{editRequest}</div> : null;
  if (advanced && !inviting) return <TopBarShareAdvanced state={state} loadApps={loadApps} database={database} disabled={disabled || !manages}
    pageRules={pageRules} propertyRules={propertyRules} guestLinks={shares && state && !guestManager
      ? <TopBarShareGuestLinks workspaceId={state.workspaceId} pageId={state.pageId} disabled={disabled} guestsAllowed={state.guestsAllowed !== false} /> : null} onAgentGrantScope={onAgentGrantScope} onBack={() => onAdvanced(false)} />;
  const owner = state?.accessOwner;
  const teamspace = state?.teamspace ?? null;
  // Their own line first, as Notion's (seen 2026-11-05).
  const ownFirst = { mine: (state?.permissions ?? []).filter(item => shareIsYou(item, currentUser)),
    others: (state?.permissions ?? []).filter(item => !shareIsYou(item, currentUser)) };
  // Every person's menu heads with where their role comes from, as Notion's
  // (seen 2026-10-28). The full access row's is held — a private page's
  // owner's on their page, else the workspace owner's on every page — which
  // no choice here changes: their menu shows it, greyed. The workspace
  // owner's is named as Notion names that source, its "Owners of <workspace>"
  // row, in Notion's "via …" words (Notion draws no source line for it).
  const pageAccess = <Trans t={t} i18nKey="topBar:pageAccessVia" values={{ title: pageTitle }} components={{ b: <strong /> }} />;
  const ownerAccess = <Trans t={t} i18nKey="topBar:ownerAccessVia" values={{ name: state?.workspaceName ?? "" }} components={{ b: <strong /> }} />;
  const ownerPicker = (person?: ReactNode) => <TopBarSharePermissionPicker more={Boolean(person)} person={person} value={publicMutationRoles.fullAccess}
    disabled={disabled} readOnly={!shares} held current={owner?.privatePage ? pageAccess : ownerAccess} onChange={() => undefined} />;
  // A linked page that gives someone a role of its own over one from above
  // says it differs from its parent, as Notion's (seen 2026-11-06); a
  // database's in Notion's words for one.
  const differsNote = state?.shareDiffers && !state.shareUnlink ? <div className={inherited.banner} role="note" data-share-differs>
    <span><Trans t={t} i18nKey={database ? "topBar:differsFromDatabase" : "topBar:differsFrom"} values={{ title: shareParentTitle({ title: state.shareDiffers.parentTitle, database: state.shareDiffers.parentDatabase }) }}
      components={{ page: <a className={inherited.bannerLink} href={pageHref(state.shareDiffers.parentId)} /> }} /></span></div> : null;
  // A phone's, as Notion's "공유 설정" sheet (seen 2026-10-27): the field opens
  // the invite screen over it; each person's role is under their name and
  // their "⋯" opens "사용 권한 변경"; a line, then the rest.
  if (phone) {
    const face = (userId: string | undefined, name: string, principalType = "user") => principalType === "user"
      ? <PersonAvatar className={phoneCss.avatar} userId={userId} name={name} size={40} />
      : <span className={`${phoneCss.avatar} ${styles.shareAvatar}`}>{name.trim().slice(0, 1).toUpperCase() || "?"}</span>;
    const ownerText = <SharePhonePersonText name={owner?.label ?? ""} you={Boolean(owner?.isCurrent)} role={t("fullAccess")} />;
    const phonePermission = (permission: PublicMutationPermission) => {
        const ruled = Boolean(permission.rule || permission.ruleCreator);
        const you = shareIsYou(permission, currentUser);
        const guest = <SharePersonMark guest={permission.guest} restricted={permission.restricted} />;
        const text = <SharePhonePersonText name={permission.principalLabel} you={you} guest={guest}
          role={ruled ? t("viaPageRule", { property: permission.rule || t("ruleCreator") }) : sharePermissionLabel(permission.role, t)} />;
        return <SharePhonePerson key={permission.id} face={face(permission.principalUserId || permission.principalId, permission.principalLabel, permission.principalUserId ? "user" : permission.principalType)}
          data={{ "data-share-permission-row": permission.principalType }}>
          {text}
          {/* A page-level rule's share changes with its rule. */}
          <TopBarSharePermissionPicker more value={permission.role} disabled={disabled || ruled} readOnly={!shares} current={pageAccess}
            person={<>{face(permission.principalUserId || permission.principalId, permission.principalLabel, permission.principalUserId ? "user" : permission.principalType)}{text}</>}
            onChange={next => { if (next !== "none") onPermission(permission, next); }} onRemove={ruled ? undefined : () => onRemove(permission)}
            extra={permission.signupToken ? <PhoneSheetGroup>
              <PhoneSheetRow icon={<LinkIcon size="small" />} label={t("copyInviteLink")} more={false}
                data={{ "data-share-invite-copy": "" }} onPress={() => void copyInviteLink(permission)} />
            </PhoneSheetGroup> : undefined} />
        </SharePhonePerson>;
    };
    const phoneRow = (permission: PublicMutationPermission) => {
      const said = writeNote?.at === `permission:${permission.id}` ? writeNote.text : "";
      return <Fragment key={permission.id}>{phonePermission(permission)}
        <ShareErrorLine text={said} className={styles.shareError} data-share-row-note /></Fragment>;
    };
    return <div className={styles.sharePanel} data-share-panel="share">
      {manages ? <SharePhoneSearch disabled={disabled || !shares} onOpen={onOpenInvite} /> : null}
      {inviting ? <SharePhoneInvite query={draft} onQuery={onDraft} chips={chips} onRemoveChip={onRemoveChip} candidates={candidates} onToggle={onToggle}
        addressRows={addressRows} invalidInvites={invalidInvites}
        role={role} onRole={onRole} disabled={disabled} busy={busy} hasMore={Boolean(state?.principalOptionsNextCursor)} onMore={onMore}
        selfId={currentUser.id} onSubmit={onInvite} onCancel={onBack} /> : null}
      {shares && (state?.guestsAllowed === false || state?.externalEmailAllowed === false) ? <p className={`${styles.shareSecurityNote} ${policyRefused ? styles.shareSecurityNoteRefused : ""}`} role={policyRefused ? "alert" : undefined} data-share-invites-limited>
        {t(state?.guestsAllowed === false ? "guestsTurnedOff" : "externalEmailTurnedOff")}
      </p> : null}
      {readStrip}
      {state?.shareUnlink ? <TopBarShareUnlinkBanner key={state.pageId} unlink={state.shareUnlink} database={database}
        disabled={disabled || !shares} onDefaults={() => onParentDefaults(shareUnlinkDefaultsHref(state.shareUnlink!))} onRestore={onRelink} /> : null}
      {differsNote}
      {ownFirst.mine.map(phoneRow)}
      {!state && loading ? <SharePhonePerson face={<span className={phoneCss.avatar} />} data={{ "data-share-loading": "" }}><span /></SharePhonePerson> : null}
      {state && !ownLineOnly ? <SharePhonePerson face={face(owner?.principalId, owner?.label ?? "")} data={{ "data-share-access-owner": "" }}>
        {ownerText}
        {ownerPicker(<>{face(owner?.principalId, owner?.label ?? "")}{ownerText}</>)}
      </SharePhonePerson> : null}
      {ownFirst.others.map(phoneRow)}
      <TopBarShareInheritedGroups groups={state?.inheritedGroups ?? []} disabled={disabled} readOnly={!shares || guestManager} pageIcon={pageIcon}
        onChange={onInheritedChange} loadPeople={loadInheritedPeople} />
      {teamspace && !ownLineOnly ? <TopBarShareTeamspace teamspace={teamspace} disabled={disabled} readOnly={!shares} onChange={onTeamspaceRole} /> : null}
      <div className={phoneCss.foot}>
        {!state && loading ? <div aria-hidden="true" data-share-loading><div className={phoneCss.sectionLabel}>{t("generalAccess")}</div><div className={phoneCss.general} /></div> : null}
        {state ? <TopBarShareGeneralAccess state={state} disabled={disabled} readOnly={!shares || guestManager} onAccess={onGeneralAccess} note={writeNote?.at === "general" ? writeNote.text : ""} onRole={onWorkspaceRole} onHideFromSearch={onHideFromSearch} onWebRole={onWebRole}
          expiresAt={expiresAt} onExpiry={onExpiry} /> : null}
        {shares && state?.publishingAllowed === false ? <p className={styles.shareSecurityNote} data-share-publishing-off>{t("publishingTurnedOff")}</p> : null}
        <button ref={advancedButton} type="button" className={phoneCss.advanced} data-share-advanced-open data-focus-leaves onClick={() => onAdvanced(true)}>
          <Settings size="regular" aria-hidden="true" /> {t("advanced")}
          <ShareAdvancedCount state={state} />
        </button>
        <button type="button" className={phoneCss.copy} data-menu-item data-share-copy-link onClick={onCopy}>{t("copyLink")}</button>
        {/* What a web link or a site lets anyone see, under "링크 복사" on a touch screen (Notion's, seen 2026-11-13). */}
        {state?.webSharingEnabled || state?.inheritedWebLink || state?.site?.published ? <p className={phoneCss.webNote} data-share-web-note>
          <InfoIcon size="small" aria-hidden="true" /><span>{t("webLinkNote")}</span></p> : null}
        {/* Under "링크 복사", as Notion's phone and iPad (seen 2026-11-12 at 402pt): put away on any screen, it stays away. */}
        {manages && !ownLineOnly ? <AgentAccessCard userId={currentUser.id} database={database} phone /> : null}
      </div>
    </div>;
  }
  // A page-level rule's share changes with its rule.
  const permissionPicker = (permission: PublicMutationPermission) => permission.rule || permission.ruleCreator
    ? <span className={styles.sharePermission} data-share-rule-role>{sharePermissionLabel(permission.role, t)}</span>
    : <TopBarSharePermissionPicker value={permission.role} disabled={disabled} readOnly={!shares} current={pageAccess}
      onChange={next => { if (next !== "none") onPermission(permission, next); }} onRemove={() => onRemove(permission)} />;
  // Who has it already keeps their role's menu while searching, as Notion's (seen 2026-11-05).
  const alreadySharedRole = (row: ShareAlreadyShared) => {
    if (row.key === `owner:${owner?.principalId}`) return ownerPicker();
    const permission = state?.permissions.find(item => item.id === row.key);
    return permission ? permissionPicker(permission) : null;
  };
  const permissionRow = (permission: PublicMutationPermission) => { const note = permission.rule || permission.ruleCreator
        ? t("viaPageRule", { property: permission.rule || t("ruleCreator") })
        // Mail not sent — none from this server, held back by the limits or failed — or sent from a server with no public
        // address and so without its link: the link is here (lib/inviteMail.ts).
        : permission.signupToken && ["notSent", "limited", "failed"].includes(inviteMail(Boolean(state?.emailSends), permission.inviteMail ?? "")) ? t("emailNotSent")
        : permission.signupToken && inviteMail(Boolean(state?.emailSends), permission.inviteMail ?? "") === "sentWithoutLink" ? t("emailNoLink")
        : sharePrincipalNote({ principalType: permission.principalType, label: permission.principalLabel, email: permission.principalEmail, principalId: permission.principalId }, t);
      const said = writeNote?.at === `permission:${permission.id}` ? writeNote.text : "";
      return <Fragment key={permission.id}><div className={styles.shareRow} data-share-row
        data-share-permission-row={permission.principalType}>
        {/* An address someone joined by is that person, with their face, as Notion's (seen 2026-11-15). */}
        {permission.principalType === "user" || permission.principalUserId
          ? <PersonAvatar className={styles.shareAvatar} userId={permission.principalUserId || permission.principalId} name={permission.principalLabel} size={32} />
          : <span className={styles.shareAvatar}>{permission.principalLabel.trim().slice(0, 1).toUpperCase() || "?"}</span>}
        <ShareRowTip className={styles.shareText} text={roleTip(permission.principalLabel, permission.role)}>
          <span className={inherited.name}><span>{permission.principalLabel}{shareIsYou(permission, currentUser)
            ? <SelfMark className={styles.shareYou} text={t("youMark")} /> : null}</span>
            <SharePersonMark guest={permission.guest} restricted={permission.restricted} /></span>
          {note || permission.signupToken ? <span>{note}
            {/* Where the server sends no mail, the link to hand on; under the address so the row stays face, name and role. */}
            {permission.signupToken ? <>{note ? " · " : null}<button type="button" className={access.inviteLinkCopy} data-share-invite-copy onClick={() => void copyInviteLink(permission)}>
              {t("copyInviteLink")}
            </button></> : null}</span> : null}</ShareRowTip>
        {permissionPicker(permission)}
      </div><ShareErrorLine text={said} className={styles.shareError} data-share-row-note /></Fragment>;
  };
  return <div className={styles.sharePanel} data-share-panel="share" data-share-inviting={inviting ? "true" : undefined}>
    {inviting ? <div className={access.stepHeader} data-share-invite-step>
      <button type="button" aria-label={t("backToSharing")} onClick={onBack}><ChevronLeft size="regular" aria-hidden="true" /></button>
      <span>{t("inviteStep")}</span>
    </div> : null}
    {/* One who can edit but not share, and a restricted member, see the field greyed, as Notion's (seen 2026-11-05, 2026-11-07). */}
    {manages || state?.viewerRole === publicMutationRoles.edit || restrictedReader ? <TopBarShareInviteField inputRef={inputRef} dialogRef={dialogRef} query={draft} onQuery={onDraft} chips={chips} onRemoveChip={onRemoveChip}
      candidates={candidates} onToggle={onToggle} addressRows={addressRows} invalidInvites={invalidInvites} alreadyShared={alreadyShared} alreadySharedRole={alreadySharedRole} inviting={inviting}
      disabled={disabled || !shares} busy={busy} visible={visible} focusRequest={focusRequest} role={role} onRole={onRole} onSubmit={onInvite} onOpen={onOpenInvite}
      message={message} onMessage={onMessage}
      hasMore={inviting && Boolean(state?.principalOptionsNextCursor)} onMore={onMore} /> : null}
    {shares && (state?.guestsAllowed === false || state?.externalEmailAllowed === false) ? <p className={`${styles.shareSecurityNote} ${policyRefused ? styles.shareSecurityNoteRefused : ""}`} role={policyRefused ? "alert" : undefined} data-share-invites-limited>
      {t(state?.guestsAllowed === false ? "guestsTurnedOff" : "externalEmailTurnedOff")}
    </p> : null}
    {!inviting ? <>
      {readStrip}
      {state?.shareUnlink ? <TopBarShareUnlinkBanner key={state.pageId} unlink={state.shareUnlink} database={database}
        disabled={disabled || !shares} onDefaults={() => onParentDefaults(shareUnlinkDefaultsHref(state.shareUnlink!))} onRestore={onRelink} /> : null}
      {differsNote}
      {ownFirst.mine.map(permissionRow)}
      {!state && loading ? <div className={styles.shareRow} aria-hidden="true" data-share-loading /> : null}
      {state && !ownLineOnly ? <div className={styles.shareRow} data-share-row data-share-access-owner>
        <PersonAvatar className={styles.shareAvatar} userId={owner?.principalId} name={owner?.label} size={32} />
        <ShareRowTip className={styles.shareText} text={roleTip(owner?.label ?? "", "full_access")}>
          <span data-share-access-owner-name>{owner?.label}{owner?.isCurrent ? <SelfMark className={styles.shareYou} text={t("youMark")} /> : null}</span>
          {owner?.email ? <span>{owner.email}</span> : null}</ShareRowTip>
        {ownerPicker()}
      </div> : null}
      {ownFirst.others.map(permissionRow)}
      <TopBarShareInheritedGroups groups={state?.inheritedGroups ?? []} disabled={disabled} readOnly={!shares || guestManager} pageIcon={pageIcon}
        onChange={onInheritedChange} loadPeople={loadInheritedPeople} />
      {teamspace && !ownLineOnly ? <TopBarShareTeamspace teamspace={teamspace} disabled={disabled} readOnly={!shares} onChange={onTeamspaceRole} /> : null}
      {!state && loading ? <div aria-hidden="true" data-share-loading><div className={access.generalLabel}>{t("generalAccess")}</div><div className={access.generalRow} /></div> : null}
      {state ? <TopBarShareGeneralAccess state={state} disabled={disabled} readOnly={!shares || guestManager} onAccess={onGeneralAccess} note={writeNote?.at === "general" ? writeNote.text : ""} onRole={onWorkspaceRole} onHideFromSearch={onHideFromSearch} onWebRole={onWebRole}
        expiresAt={expiresAt} onExpiry={onExpiry} /> : null}
      {shares && state?.publishingAllowed === false ? <p className={styles.shareSecurityNote} data-share-publishing-off>{t("publishingTurnedOff")}</p> : null}
      <div className={access.foot}>
        <button ref={advancedButton} type="button" className={access.footButton} data-share-advanced-open data-focus-leaves onClick={() => onAdvanced(true)}>
          <Settings size="regular" aria-hidden="true" /> {t("advanced")}
          <ShareAdvancedCount state={state} />
        </button>
        <button type="button" className={access.copyButton} data-menu-item data-share-copy-link onClick={onCopy}>
          <LinkIcon size="tiny" aria-hidden="true" /> {t("copyLink")}
        </button>
      </div>
      {manages && !ownLineOnly ? <AgentAccessCard userId={currentUser.id} database={database} /> : null}
    </> : null}
  </div>;
}

const AGENT_CARD_DISMISSED = "share-agent-access-card-dismissed";

/**
 * Says where agents' access is set — a database's where its page rules are too, as Notion's note does for each
 * (under "고급 기능", seen 2026-11-18 in Chrome at 1440 in a Business database); put away, it stays away for that person.
 */
function AgentAccessCard({ userId, database, phone = false }: { userId: string; database: boolean; phone?: boolean }) {
  const { t } = useTranslation("topBar");
  const [dismissed, setDismissed] = useState(() => personValue(AGENT_CARD_DISMISSED, { userId }) === "1");
  if (dismissed) return null;
  return <div className={access.agentCard} data-share-agent-card data-phone={phone ? "true" : undefined}>
    <span>{t(database ? "agentAccessCardDatabase" : "agentAccessCard")}</span>
    <button type="button" aria-label={t("agentAccessCardDismiss")} data-share-agent-card-dismiss
      onClick={() => { storePersonValue(AGENT_CARD_DISMISSED, "1", { userId }); setDismissed(true); }}>
      <X size="tiny" aria-hidden="true" />
    </button>
  </div>;
}
