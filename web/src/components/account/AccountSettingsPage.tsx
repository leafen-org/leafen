import { useCallback, useEffect, useId, useRef, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import type { Session } from "@/types";
import type { BrowserLocalLockSettings } from "@/lib/localLockBrowserSettings";
import { AccountAvatarPicker } from "./AccountAvatarPicker";
import { AccountDeletionSection } from "./AccountDeletionSection";
import { AccountNotificationSettings } from "./AccountNotificationSettings";
import { AccountPreferences } from "./AccountPreferences";
import { ThisComputerSettings } from "./ThisComputerSettings";
import { LegalNotice } from "@/components/LegalNotice";
import { AccountServerProfiles } from "./AccountServerProfiles";
import { styles } from "./accountSettingsStyles";
import { LocalLockSecurityPanel } from "./LocalLockSecurityPanel";
import { useAccountProfile } from "./useAccountProfile";
import { AccountPasskeysSection } from "./AccountPasskeysSection";
import { AccountEmailManagement } from "./AccountEmailManagement";
import { AccountMFASection } from "./AccountMFASection";
import { AccountPasswordSection } from "./AccountPasswordSection";
import { AccountPhoneSignInRows, AccountSecurityNotices, AccountSecurityShareProvider, type AccountSecurityShare,
  useAccountSecurityShare } from "./AccountSecurityShare";
import { AccountDevicesSection, AccountUserIdSection } from "./AccountDevicesSection";
import { AccountSignInHistory } from "./AccountSignInHistory";
import { ShieldCheckIcon } from "@/icons/leafen";
import emailStyles from "./AccountEmailManagement.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import { ListNotice } from "@/components/settings/SettingsRowNotice";

/** The account's sections of the settings window. */
export type AccountSettingsSection = "account" | "preferences" | "notifications" | "servers";

type AccountSettingsProps = {
  section: AccountSettingsSection;
  session: Session;
  /** The open workspace, for the preferences it holds per member. */
  workspaceId: string | null;
  onSession: (session: Session) => void;
  onAccountDeleted: () => void;
  localLockSettings: BrowserLocalLockSettings;
  emailManagementOpen: boolean;
  onEmailManagementChange: (open: boolean) => void;
};

/** The account's sections of the settings window. */
export function AccountSettingsContent(props: AccountSettingsProps) {
  const { section, session, workspaceId } = props;
  // Only the account reads its profile and security, so a phone's settings,
  // which lay these sections one after another, read them once.
  if (section === "account") return <AccountOwnSettings {...props} />;
  return (
    <div className={styles.adminConsole} data-account-console="true">
      {section === "preferences" ? (
        <AccountPreferences session={session} workspaceId={workspaceId}
          signIn={<AccountPhoneSignInRows session={session} />} />
      ) : section === "servers" ? (
        <ServerAccountsPage userId={session.user.id} />
      ) : (
        <AccountNotificationSettings session={session} />
      )}
    </div>
  );
}

/**
 * The account, as Notion's "계정" (seen 2026-10-12): the profile, the
 * account's security, its support (deleting the account), its devices and
 * its user ID. Its security is read once with the rows a phone lays
 * elsewhere (AccountSecurityShare.tsx).
 */
function AccountOwnSettings(props: AccountSettingsProps) {
  const share = useAccountSecurityShare();
  if (share) return <AccountOwnSettingsRows {...props} share={share} />;
  return (
    <AccountSecurityShareProvider session={props.session} onSession={props.onSession}>
      <AccountOwnSettings {...props} />
    </AccountSecurityShareProvider>
  );
}

function AccountOwnSettingsRows({
  session,
  onSession,
  onAccountDeleted,
  localLockSettings,
  emailManagementOpen,
  onEmailManagementChange,
  share,
}: AccountSettingsProps & { share: AccountSecurityShare }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const profileIconLabelId = useId();
  const profileIconButtonRef = useRef<HTMLButtonElement>(null);
  const emailManagementRef = useRef<HTMLButtonElement>(null);
  const { security, passkeyAdd, passkeyCount, setPasskeyCount, setActionOpen, setNoticesAt, phonePlaces } = share;
  const emailManagementWasOpen = useRef(false);
  useEffect(() => {
    if (emailManagementWasOpen.current && !emailManagementOpen) emailManagementRef.current?.focus();
    emailManagementWasOpen.current = emailManagementOpen;
  }, [emailManagementOpen]);
  const [profileIconPickerOpen, setProfileIconPickerOpen] = useState(false);
  // Opened from the hint's "사진을 추가": the photo chooser comes first.
  const [photoUploadFirst, setPhotoUploadFirst] = useState(false);
  const profile = useAccountProfile(session, onSession);
  const profileDisplayName = profile.snapshot?.displayName ?? session.user.name;

  function closeProfileIconPicker() {
    setProfileIconPickerOpen(false);
    setPhotoUploadFirst(false);
    window.requestAnimationFrame(() => profileIconButtonRef.current?.focus());
  }

  // A name left blank or unchanged keeps the saved one.
  function saveName() {
    const saved = profile.snapshot?.displayName ?? session.user.name;
    if (!profile.displayName.trim()) {
      profile.setDisplayName(saved);
      return;
    }
    if (profile.displayName.trim() !== saved) void profile.save();
  }

  if (emailManagementOpen) return <AccountEmailManagement session={session} onSession={onSession} />;

  return (
    <div className={styles.adminConsole} data-account-console="true">
      <>
        {/* As Notion's (seen 2026-10-12): the picture's circle beside the preferred name, kept when the field is left. */}
        <section className={styles.section} role="region" aria-label={t("navProfile")}>
          <div className={styles.sectionTitle}>{t("navProfile")}</div>
          {/* The profile not read: what failed, with a way to read it again. */}
          {profile.error && !profile.snapshot ? <ListNotice role="alert" onRetry={() => void profile.refresh()} data-profile-read-failed>{profile.error}</ListNotice>
            : profile.error ? <div className={styles.notice} role="alert">{profile.error}</div> : null}
          <div className={styles.accountProfileRow}>
            <div
              className={styles.iconPickerWrap}
              role="group"
              aria-labelledby={profileIconLabelId}
              onKeyDown={(event) => {
                if (event.key !== "Escape" || !profileIconPickerOpen) return;
                event.preventDefault();
                event.stopPropagation();
                closeProfileIconPicker();
              }}
            >
              <span id={profileIconLabelId} hidden>{t("profileIconField")}</span>
              <button
                ref={profileIconButtonRef}
                type="button"
                className={styles.accountProfileAvatar}
                aria-haspopup="dialog"
                aria-expanded={profileIconPickerOpen}
                aria-label={t("ariaChangeProfileIcon")}
                disabled={profile.loading || profile.busy || profile.locked || profile.held}
                onClick={() => { setPhotoUploadFirst(false); setProfileIconPickerOpen((current) => !current); }}
              >
                <PersonAvatar avatar={profile.avatar} name={profile.displayName || profileDisplayName} size={60} />
              </button>
              {profileIconPickerOpen ? (
                <AccountAvatarPicker
                  session={session}
                  uploadFirst={photoUploadFirst}
                  avatar={profile.avatar}
                  returnFocusRef={profileIconButtonRef}
                  onPick={(avatar) => {
                    profile.setAvatar(avatar);
                    void profile.save({ avatar });
                    closeProfileIconPicker();
                  }}
                  onClose={closeProfileIconPicker}
                />
              ) : null}
            </div>
            <label className={styles.accountProfileName}>
              <span>{t("preferredName")}</span>
              <input
                value={profile.displayName}
                aria-label={t("ariaProfileDisplayName")}
                placeholder={t("displayNamePlaceholder")}
                autoComplete="name"
                disabled={profile.loading || profile.busy || profile.locked || profile.held}
                onChange={(event) => profile.setDisplayName(event.target.value)}
                onBlur={saveName}
                onKeyDown={(event) => { if (fieldEnter(event)) event.currentTarget.blur(); }}
              />
            </label>
          </div>
          <p className={styles.profilePhotoHint} data-profile-photo-hint>
            <Trans t={t} i18nKey="profilePhotoHint" components={{
              add: <button type="button" className={styles.profilePhotoAdd} data-profile-photo-add
                disabled={profile.loading || profile.busy || profile.locked || profile.held}
                onClick={() => { setPhotoUploadFirst(true); setProfileIconPickerOpen(true); }} />,
              // Where Notion offers its faces (seen 2026-10-30), the icons.
              pick: <button type="button" className={styles.profilePhotoAdd}
                disabled={profile.loading || profile.busy || profile.locked || profile.held}
                onClick={() => { setPhotoUploadFirst(false); setProfileIconPickerOpen(true); }} />,
            }} />
          </p>
          {profile.locked ? <div className={styles.organizationMeta} data-profile-locked>{t("profileManagedByOrganization")}</div> : null}
          {/* With no passkey yet, Notion suggests one under the profile (seen 2026-10-30): a blue note and its button. */}
          {passkeyCount === 0 && passkeyAdd.current ? <div className={emailStyles.passkeySuggestion} data-passkey-suggestion>
            <p className={emailStyles.callout}><ShieldCheckIcon size="wide" aria-hidden="true" />{t("passkeySuggestion")}</p>
            <button type="button" onClick={() => passkeyAdd.current?.()}>{t("addPasskey")}</button>
          </div> : null}
        </section>
        {/* The notices go where the last press was, once it is done: moved on focus, they slid in above
            the button under a pressing pointer, and the press landed elsewhere. */}
        <section className={styles.section} role="region" aria-label={t("navAccountSecurity")} data-account-security
          onClickCapture={() => setNoticesAt("security")}>
          <div className={styles.sectionTitle}>{t("navAccountSecurity")}</div>
          <AccountSecurityNotices share={share} at="security" />

          {/* Backup sign-ins (the multi-server decision of 2026-10-20): the
              account's devices sign in; these let it in from any browser. */}
          <div className={styles.securityRow} data-backup-sign-in>
            <div className={styles.securityInstruction}><strong>{t("backupSignInTitle")}</strong><span>{t("backupSignInDesc")}</span></div>
          </div>
          <div className={styles.securityRow}>
            <div className={styles.securityInstruction}><strong>{t("accountEmail")}</strong><span>{session.user.email || t("accountNoEmail")}</span></div>
            <button ref={emailManagementRef} type="button" className={styles.secondaryButton} data-focus-leaves onClick={() => onEmailManagementChange(true)}>{t("emailManageButton")}</button>
          </div>
          {/* In Notion's order (seen 2026-11-04): email, passkeys, 2-step verification, password.
              A phone has the passkeys and the password at the top of its preferences. */}
          {phonePlaces ? null : <AccountPasskeysSection session={session} addRef={passkeyAdd} onKeys={setPasskeyCount} />}
          <AccountMFASection security={security} onOpenChange={setActionOpen} />
          {phonePlaces ? null : <AccountPasswordSection security={security} email={session.user.email} onOpenChange={setActionOpen} addPasskey={passkeyAdd} />}
          <LocalLockSecurityPanel settings={localLockSettings} />
        </section>
        {/* A phone has it under "위험 구역" at its end. */}
        {phonePlaces ? null : <section className={styles.section} role="region" aria-label={t("supportSection")}>
          <div className={styles.sectionTitle}>{t("supportSection")}</div>
          <AccountDeletionSection session={session} onDeleted={onAccountDeleted} />
        </section>}
        <AccountDevicesSection session={session} security={security} notices={<AccountSecurityNotices share={share} at="devices" />}
          onClickCapture={() => setNoticesAt("devices")} />
        <AccountSignInHistory session={session} devices={security.snapshot?.sessions.map((item) => item.id).join()} />
        <AccountUserIdSection userId={session.user.id} />
      </>
    </div>
  );
}

// How long this computer's sections wait for the server's list of pages
// before showing anyway.
const SWITCHER_KEYS_WAIT_MS = 3_000;

/**
 * The server's accounts, then this computer's sections. The list of other
 * servers' pages under the accounts comes from the server, which can answer
 * after this computer's sections are read: drawn first, they were pushed
 * down when it came (2026-11-07). They are read at once but shown only after
 * the list has come (or a while has passed), so nothing moves.
 */
function ServerAccountsPage({ userId }: { userId: string }) {
  const [keysSettled, setKeysSettled] = useState(false);
  useEffect(() => {
    const timer = window.setTimeout(() => setKeysSettled(true), SWITCHER_KEYS_WAIT_MS);
    return () => window.clearTimeout(timer);
  }, []);
  const settle = useCallback(() => setKeysSettled(true), []);
  return (
    <>
      <AccountServerProfiles userId={userId} onSwitcherKeysSettled={settle} />
      <ThisComputerSettings held={!keysSettled} />
      {/* Everyone's source, license and notices line (LegalNotice), at the section's foot. */}
      <LegalNotice place="settings" />
    </>
  );
}
