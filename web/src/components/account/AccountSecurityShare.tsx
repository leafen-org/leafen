import { createContext, type MutableRefObject, type ReactNode, useContext, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Session } from "@/types";
import { AccountDeletionSection } from "./AccountDeletionSection";
import { AccountPasskeysSection } from "./AccountPasskeysSection";
import { AccountPasswordSection } from "./AccountPasswordSection";
import { styles } from "./accountSettingsStyles";
import { useAccountSecurity } from "./useAccountSecurity";
import { ListNotice } from "@/components/settings/SettingsRowNotice";

/** Where the security's notices show: by the rows last pressed. */
export type AccountSecurityNoticesAt = "security" | "devices" | "sign-in";

/**
 * The account's security, read once for every row that shows it. A
 * desktop's account section holds all of them; a phone's long settings
 * screen (SettingsPhoneScreen.tsx) lays the password and the passkeys at the
 * top of its preferences and the account's deletion under "위험 구역" at its
 * end, as Notion's in iPhone Safari (seen 2026-11-14, 402pt: "기본 설정"
 * opens with 비밀번호 then 패스키, over the theme; "위험 구역" with "내 계정
 * 삭제" ends the screen; its phone has no account section), so the sections
 * there share one.
 */
export type AccountSecurityShare = {
  security: ReturnType<typeof useAccountSecurity>;
  /** The passkey row's add, for the profile's suggestion and the password card's link. */
  passkeyAdd: MutableRefObject<(() => void) | null>;
  passkeyCount: number | undefined;
  setPasskeyCount(count: number): void;
  /** A security card is open: its own error shows in it, not in the notices. */
  actionOpen: boolean;
  setActionOpen(open: boolean): void;
  noticesAt: AccountSecurityNoticesAt;
  setNoticesAt(at: AccountSecurityNoticesAt): void;
  /** The phone's places: the password and passkeys in the preferences, the deletion at the end. */
  phonePlaces: boolean;
};

const AccountSecurityContext = createContext<AccountSecurityShare | null>(null);

export function AccountSecurityShareProvider({ session, onSession, phonePlaces = false, children }: {
  session: Session;
  onSession(session: Session): void;
  phonePlaces?: boolean;
  children: ReactNode;
}) {
  const security = useAccountSecurity(session, onSession);
  const passkeyAdd = useRef<(() => void) | null>(null);
  const [passkeyCount, setPasskeyCount] = useState<number>();
  const [actionOpen, setActionOpen] = useState(false);
  const [noticesAt, setNoticesAt] = useState<AccountSecurityNoticesAt>(phonePlaces ? "sign-in" : "security");
  const share: AccountSecurityShare = {
    security, passkeyAdd, passkeyCount, setPasskeyCount, actionOpen, setActionOpen, noticesAt, setNoticesAt, phonePlaces,
  };
  return <AccountSecurityContext.Provider value={share}>{children}</AccountSecurityContext.Provider>;
}

export function useAccountSecurityShare(): AccountSecurityShare | null {
  return useContext(AccountSecurityContext);
}

/** The security's error and its last word, where the last press was, once it is done. */
export function AccountSecurityNotices({ share, at }: { share: AccountSecurityShare; at: AccountSecurityNoticesAt }) {
  const { security } = share;
  if (share.noticesAt !== at) return null;
  return (
    <>
      {/* The account's security not read: what failed, with a way to read it again. */}
      {security.error && !security.snapshot ? <ListNotice role="alert" onRetry={() => void security.refresh()} data-security-read-failed>{security.error}</ListNotice>
        : security.error && !share.actionOpen ? <div className={styles.notice} role="alert">{security.error}</div> : null}
      {security.notice ? <div className={styles.notice} data-tone="neutral" role="status">{security.notice}</div> : null}
    </>
  );
}

/** A phone's password and passkey rows, at the top of its preferences. */
export function AccountPhoneSignInRows({ session }: { session: Session }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const share = useAccountSecurityShare();
  if (!share?.phonePlaces) return null;
  return (
    <section className={styles.section} role="region" aria-label={t("backupSignInTitle")} data-account-sign-in
      onClickCapture={() => share.setNoticesAt("sign-in")}>
      <div className={styles.sectionTitle}>{t("backupSignInTitle")}</div>
      <AccountSecurityNotices share={share} at="sign-in" />
      <AccountPasswordSection security={share.security} email={session.user.email} onOpenChange={share.setActionOpen} addPasskey={share.passkeyAdd}
        emailBelow />
      <AccountPasskeysSection session={session} addRef={share.passkeyAdd} onKeys={share.setPasskeyCount} />
    </section>
  );
}

/** A phone's "위험 구역" at the end of the person's own settings: deleting the account. */
export function AccountPhoneDangerZone({ session, onDeleted }: { session: Session; onDeleted: () => void }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  return (
    <div className={styles.adminConsole} data-account-console="true" data-account-danger-zone>
      <section className={styles.section} role="region" aria-label={t("dangerZoneTitle")}>
        <AccountDeletionSection session={session} onDeleted={onDeleted} />
      </section>
    </div>
  );
}
